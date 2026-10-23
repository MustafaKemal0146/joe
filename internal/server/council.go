package server

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/MustafaKemal0146/joe/internal/analytics"
	"github.com/MustafaKemal0146/joe/internal/budget"
	"github.com/MustafaKemal0146/joe/internal/council"
	"github.com/MustafaKemal0146/joe/internal/embed"
	"github.com/MustafaKemal0146/joe/internal/evidence"
	"github.com/MustafaKemal0146/joe/internal/llm"
	"github.com/MustafaKemal0146/joe/internal/mask"
	"github.com/MustafaKemal0146/joe/internal/personas"
	"github.com/MustafaKemal0146/joe/internal/report"
	"github.com/MustafaKemal0146/joe/internal/store"
	"github.com/MustafaKemal0146/joe/internal/version"
)

// CallerFactory bir model için konsey çağrıcısı üretir (testlerde sahtesiyle değiştirilir).
type CallerFactory func(provider, model string) (council.Caller, error)

type councilRuns struct {
	mu     sync.Mutex
	cancel map[string]context.CancelFunc
	wg     sync.WaitGroup
}

type councilRequest struct {
	Kind       string   `json:"kind"` // single | pair | ask
	SubjectID  int64    `json:"subject_id"`
	PartnerID  int64    `json:"partner_id"`
	PersonaIDs []string `json:"persona_ids"`
	Question   string   `json:"question"`
	MaxTokens  int      `json:"max_tokens"`
	Consent    bool     `json:"consent"`
	// IncludeOSINT kişiye bağlı, kullanıcının DOĞRULADIĞI OSINT bulgularını pakete ekler (varsayılan: hayır).
	IncludeOSINT bool   `json:"include_osint"`
	ApprovalID   string `json:"approval_id"`
	// FollowUpOf aynı kişiye sorulmuş önceki bir soru oturumudur; sorunun ve cevabın kısa özeti yeni soruya eklenir.
	FollowUpOf string `json:"follow_up_of"`
	// ContextOf tartışma modunda moderatör sorusuna eklenecek mercek yanıtlarının oturum kimlikleridir (en çok 5).
	ContextOf []string `json:"context_of"`
	// PartnerConvID iki kişilik analizi yalnızca bu bire-bir sohbetle sınırlar. Aynı adı paylaşan birçok sohbet
	// varsa (ör. Instagram'ın silinmiş hesapları: "Instagram User") hangisinin kastedildiğini belirler.
	PartnerConvID int64 `json:"partner_conv_id"`
	// Goal kullanıcının bu analizden beklentisidir (≤600 karakter, isteğe bağlı).
	Goal string `json:"goal"`
	// Depth "derin" ise persona aşamasından önce dönem özetleri üretilir (daha pahalı, daha derin).
	Depth string `json:"depth"`
}

type prepared struct {
	kind        string
	subjectIDs  []int64
	personaIDs  []string
	pack        evidence.Pack
	text        string // sağlayıcıya gidecek maskeli metin
	question    string // maskelenmiş soru
	hash        string
	binding     string
	estimate    budget.Estimate
	maskReport  map[string]int
	model       string
	strongModel string
	depth       string
	provider    string
	totalMsgs   int // kişinin profilindeki toplam mesaj
	monthsTotal int // profilin kapsadığı ay sayısı
}

func (s *Server) lookupPersonas(ids []string, kind string) ([]string, []personas.Persona, string) {
	all, err := personas.Load(s.PersonaDir)
	if err != nil {
		return nil, nil, "persona dosyaları okunamadı: " + err.Error()
	}
	byID := map[string]personas.Persona{}
	for _, p := range all {
		byID[p.ID] = p
	}
	if len(ids) == 0 && kind != "ask" {
		for _, p := range all {
			ids = append(ids, p.ID)
		}
	}
	seen := map[string]bool{}
	var chosen []personas.Persona
	var out []string
	for _, id := range ids {
		p, ok := byID[id]
		if !ok {
			return nil, nil, fmt.Sprintf("bilinmeyen persona: %q", id)
		}
		if !seen[id] {
			seen[id] = true
			chosen = append(chosen, p)
			out = append(out, id)
		}
	}
	if len(out) > 20 {
		return nil, nil, "en çok 20 persona seçilebilir"
	}
	if kind == "ask" && len(out) > 1 {
		return nil, nil, "soru yalnızca tek bir persona'ya sorulabilir"
	}
	return out, chosen, ""
}

// prepare isteği doğrular, paketi kurar, maskeler ve maliyeti hesaplar. Plan ve başlatma aynı
// yoldan geçer; bu yüzden onaylanan içerik ile çalıştırılan içerik özdeştir.
func (s *Server) prepare(w http.ResponseWriter, req councilRequest) (*prepared, bool) {
	kind := req.Kind
	if kind == "" {
		kind = "single"
	}
	if kind != "single" && kind != "pair" && kind != "ask" {
		fail(w, 400, "tür single, pair ya da ask olmalı")
		return nil, false
	}
	person, c, subj, ok := s.subjectByID(w, req.SubjectID)
	if !ok {
		return nil, false
	}
	// Rıza: kişi "ben" değilse (ya da iki kişilik analizse) verinin kendi verin ya da rızalı olduğunu onaylamalısın.
	if (kind == "pair" || !person.IsSelf) && !req.Consent {
		fail(w, 400, "Bu analiz için verinin sana ait ya da paylaşılma rızası alınmış olduğunu onaylaman gerekir.")
		return nil, false
	}
	ids, chosen, msg := s.lookupPersonas(req.PersonaIDs, kind)
	if msg != "" {
		fail(w, 400, msg)
		return nil, false
	}
	question := strings.TrimSpace(req.Question)
	if kind == "ask" {
		if n := len([]rune(question)); n < 3 || n > 2000 {
			fail(w, 400, "soru 3 ile 2000 karakter arasında olmalı")
			return nil, false
		}
	}
	rawQuestion := question
	if kind == "ask" && len(req.ContextOf) > 0 {
		if len(req.ContextOf) > 5 {
			fail(w, 400, "en çok 5 yanıt bağlam olarak eklenebilir")
			return nil, false
		}
		var b strings.Builder
		b.WriteString("Mercek yanıtları (özet; kanıt değildir, kanıtlar paketteki K kimlikleridir):")
		for _, sid := range req.ContextOf {
			line, ok := s.answerLine(sid, person.ID)
			if !ok {
				fail(w, 400, "bağlam yanıtı bulunamadı ya da bu kişiye ait değil")
				return nil, false
			}
			b.WriteString("\n- " + line)
		}
		question = b.String() + followMarker + question
	}
	if kind == "ask" && req.FollowUpOf != "" {
		prev, ok := s.followUpContext(req.FollowUpOf, person.ID)
		if !ok {
			fail(w, 400, "önceki soru bulunamadı ya da bu kişiye ait değil")
			return nil, false
		}
		question = prev + followMarker + question
	}
	cfg := s.Cfg.Get()
	ctxWindow := 0
	if mi, ok := llm.ModelMeta(cfg.ActiveModel); ok {
		ctxWindow = mi.ContextWindow
	}
	maxTok := req.MaxTokens
	if maxTok <= 0 {
		maxTok = evidence.MaxTokensFor(ctxWindow)
	}
	if limit := evidence.MaxTokensFor(ctxWindow) * 2; ctxWindow > 0 && maxTok > limit { // bağlamı taşan paket modelde hata verir
		maxTok = limit
	}
	goal := strings.TrimSpace(req.Goal)
	if n := len([]rune(goal)); n > 600 {
		fail(w, 400, "amaç en çok 600 karakter olabilir")
		return nil, false
	}
	if req.Depth != "" && req.Depth != "normal" && req.Depth != "derin" {
		fail(w, 400, "derinlik normal ya da derin olmalı")
		return nil, false
	}
	opt := evidence.Options{MaxTokens: maxTok, Loc: s.Loc, Goal: goal}
	if req.IncludeOSINT {
		fs, err := s.Store.PersonFindings(person.ID)
		if err != nil {
			serverErr(w, err)
			return nil, false
		}
		if len(fs) > 0 {
			var b strings.Builder
			b.WriteString("OSINT bulguları (kullanıcı onaylı aday profiller; kimlik doğrulaması DEĞİLDİR):\n")
			for _, f := range fs {
				fmt.Fprintf(&b, "- %s: %s (%s)\n", f.Site, f.URL, f.FoundAt.In(s.Loc).Format("02.01.2006"))
			}
			opt.ExtraMetrics = strings.TrimRight(b.String(), "\n")
		}
	}
	if kind == "ask" {
		opt.RelevantIDs = s.relevantIDs(rawQuestion)
	}
	pr := s.profileFor(person, c, subj)
	if kind == "single" && person.IsSelf {
		// Bağlanma örüntüsü için: kullanıcının etiketlediği ilişkiler ve en yoğun sohbetlerin ölçümleri.
		if extra := s.guideExtra(c, subj, person); extra != "" {
			opt.ExtraMetrics = joinExtra(opt.ExtraMetrics, extra)
		}
	} else if kind != "ask" && !person.IsSelf {
		if l := s.labelOfPerson(person); l != "" {
			opt.ExtraMetrics = joinExtra(opt.ExtraMetrics, "Kullanıcının bu kişiye koyduğu ilişki etiketi: "+analytics.LabelTR[l]+" (hüküm değildir).")
		}
	}
	var pack evidence.Pack
	subjectIDs := []int64{person.ID}
	if kind == "pair" {
		if req.PartnerID == 0 || req.PartnerID == req.SubjectID {
			fail(w, 400, "iki kişilik analiz için farklı bir ikinci kişi seçilmeli")
			return nil, false
		}
		other, _, osubj, ok := s.subjectByID(w, req.PartnerID)
		if !ok {
			return nil, false
		}
		pc := c // analiz veri kümesi: varsayılan tümü; PartnerConvID varsa yalnızca o sohbet
		po := s.profileFor(other, c, osubj)
		if req.PartnerConvID > 0 {
			ok := false
			for _, conv := range c.Convs {
				if name, isDM := analytics.PartnerOf(conv, subj); conv.ID == req.PartnerConvID && isDM && osubj.Is(name) {
					ok = true
				}
			}
			if !ok {
				fail(w, 400, "partner_conv_id bu iki kişinin bire-bir sohbeti değil")
				return nil, false
			}
			pc = analytics.CorpusForConv(c, req.PartnerConvID)
			pr = analytics.BuildProfile(pc, subj, s.opts())
			po = analytics.BuildProfile(pc, osubj, s.opts())
		}
		if ctx := s.pairContext(pc, subj, other); ctx != "" {
			opt.ExtraMetrics = joinExtra(opt.ExtraMetrics, ctx)
		}
		// Karşı tarafın Instagram'daki davranış izleri (takip, izleme, beğeni…): yalnızca sahibin kendi hesabından.
		names := []string{other.Name}
		for _, id := range other.Identities {
			names = append(names, id.Value)
		}
		if meta := analytics.PartnerMetaText(c, subj.OwnSources, names, s.Loc); meta != "" {
			opt.ExtraMetrics = joinExtra(opt.ExtraMetrics, meta)
		}
		cmp := analytics.Compare(pc, subj, osubj, s.opts())
		pack = evidence.BuildPair(pc, subj, osubj, pr, po, cmp, opt)
		subjectIDs = append(subjectIDs, other.ID)
	} else {
		pack = evidence.Build(c, subj, pr, opt)
	}
	m := mask.New()
	text := pack.Render(m)
	strongModel := cfg.EffectiveStrongModel()
	depth := "normal"
	if req.Depth == "derin" && kind != "ask" {
		depth = "derin"
	}
	p := &prepared{kind: kind, subjectIDs: subjectIDs, personaIDs: ids, pack: pack, text: text, model: cfg.ActiveModel, strongModel: strongModel, depth: depth, provider: cfg.ActiveProvider, totalMsgs: pr.Messages, monthsTotal: len(pr.Monthly)}
	if kind == "ask" {
		p.question = m.Apply(question)
	}
	for k, v := range m.Report() {
		if p.maskReport == nil {
			p.maskReport = map[string]int{}
		}
		p.maskReport[string(k)] = v
	}
	p.hash = pack.Hash(mask.New())
	sorted := append([]string(nil), ids...)
	sort.Strings(sorted)
	p.binding = store.CacheKey(p.hash, kind, strings.Join(sorted, ","), cfg.ActiveModel, strongModel, depth, p.question)
	packTokens := budget.EstimateTokens(text)
	switch kind {
	case "ask":
		extra := budget.EstimateTokens(p.question)
		if len(chosen) == 1 {
			extra += budget.EstimateTokens(personas.Section(chosen[0]))
		}
		p.estimate = council.AskEstimate(cfg.ActiveModel, packTokens, extra)
	default:
		section := 0
		for _, ch := range chosen {
			section += budget.EstimateTokens(personas.Section(ch))
		}
		if len(chosen) > 0 {
			section /= len(chosen)
		}
		p.estimate = council.PlanEstimate(council.PlanInput{Model: cfg.ActiveModel, StrongModel: strongModel, PackTokens: packTokens, Section: section, Personas: len(chosen), Items: len(pack.Items), Deep: depth == "derin"})
	}
	return p, true
}

// councilPlan maliyeti hesaplar, önizleme verir ve tavana sığarsa tek kullanımlık onay döndürür.
func (s *Server) councilPlan(w http.ResponseWriter, r *http.Request) {
	var req councilRequest
	if !decode(w, r, &req) {
		return
	}
	p, ok := s.prepare(w, req)
	if !ok {
		return
	}
	cfg := s.Cfg.Get()
	guard := budget.Guard{CapUSD: cfg.BudgetUSD}
	items := make([]map[string]any, len(p.pack.Items))
	for i, it := range p.pack.Items {
		items[i] = map[string]any{"id": it.ID, "title": it.Title, "kind": it.Kind, "why": it.Why, "messages": len(it.Messages), "tokens": it.Tokens}
	}
	out := map[string]any{
		"kind": p.kind, "persona_ids": p.personaIDs, "estimate": p.estimate, "cap_usd": cfg.BudgetUSD,
		"preview": p.text, "mask_report": p.maskReport, "pack_tokens": budget.EstimateTokens(p.text),
		"items": items, "skipped": p.pack.Skipped, "model": p.model, "strong_model": p.strongModel, "depth": p.depth, "coverage": coverage(p.pack, p.totalMsgs, p.monthsTotal), "provider": p.provider, "ready": s.Cfg.Ready() || s.CallerFactory != nil,
	}
	switch {
	case !p.estimate.Known:
		out["allowed"], out["reason"] = false, "Modelin fiyatı bilinmediği için maliyet doğrulanamıyor; çalıştırılamaz."
	case !(s.Cfg.Ready() || s.CallerFactory != nil):
		out["allowed"], out["reason"] = false, "Yapay zekâ sağlayıcısı hazır değil: Ayarlar'dan anahtar ekle."
	default:
		if err := guard.Allow(0, p.estimate.HighUSD); err != nil {
			out["allowed"], out["reason"] = false, err.Error()+" Ayarlar'dan bütçeyi artır ya da persona/kanıt sayısını azalt."
		} else {
			if s.Approvals == nil {
				s.Approvals = budget.NewApprovals()
			}
			ap := s.Approvals.Grant(p.estimate, cfg.BudgetUSD, p.binding)
			out["allowed"], out["approval_id"], out["expires_at"] = true, ap.ID, ap.ExpiresAt
		}
	}
	writeJSON(w, 200, out)
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// councilStart onaylanmış bir planı oturum olarak kaydeder ve arka planda çalıştırır.
func (s *Server) councilStart(w http.ResponseWriter, r *http.Request) {
	var req councilRequest
	if !decode(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.ApprovalID) == "" {
		fail(w, 400, "Onay olmadan çalıştırılamaz: önce maliyeti hesapla ve onayla.")
		return
	}
	p, ok := s.prepare(w, req)
	if !ok {
		return
	}
	if s.Approvals == nil {
		s.Approvals = budget.NewApprovals()
	}
	ap, err := s.Approvals.Consume(req.ApprovalID)
	if err != nil {
		fail(w, 400, "Onay geçersiz, kullanılmış ya da süresi dolmuş: maliyeti yeniden hesapla.")
		return
	}
	if ap.Binding != p.binding {
		fail(w, 409, "Veri ya da ayarlar önizlemeden sonra değişti; maliyeti yeniden hesaplayıp onayla.")
		return
	}
	if !s.Cfg.Ready() && s.CallerFactory == nil {
		fail(w, 400, "Yapay zekâ sağlayıcısı hazır değil.")
		return
	}
	packJSON, _ := json.Marshal(map[string]any{"subject": p.pack.Subject, "items": p.pack.Items, "skipped": p.pack.Skipped, "metrics": p.pack.Metrics})
	estJSON, _ := json.Marshal(map[string]any{"estimate": p.estimate, "cap_usd": ap.CapUSD, "consent": req.Consent, "mask_report": p.maskReport, "strong_model": p.strongModel, "depth": p.depth})
	id := newID()
	err = s.Store.CreateCouncil(store.CouncilSession{
		ID: id, Kind: p.kind, SubjectIDs: p.subjectIDs, PersonaIDs: p.personaIDs, Provider: p.provider, Model: p.model,
		PackHash: p.hash, PackText: p.text, PackJSON: packJSON, Question: p.question, EstimateRaw: estJSON,
	})
	if err != nil {
		serverErr(w, err)
		return
	}
	s.runCouncil(id)
	writeJSON(w, 202, map[string]any{"id": id})
}

func (s *Server) newEngine(sess store.CouncilSession) (*council.Engine, error) {
	all, err := personas.Load(s.PersonaDir)
	if err != nil {
		return nil, err
	}
	var capUSD float64
	var est struct {
		Cap float64 `json:"cap_usd"`
	}
	if json.Unmarshal(sess.EstimateRaw, &est) == nil {
		capUSD = est.Cap
	}
	var caller council.Caller
	if s.CallerFactory != nil {
		caller, err = s.CallerFactory(sess.Provider, sess.Model)
	} else {
		res, ok := s.Cfg.Resolve(sess.Provider)
		if !ok || !s.Cfg.Ready() {
			return nil, errors.New("yapay zekâ sağlayıcısı hazır değil")
		}
		caller = council.LLMCaller{Transport: llm.NewTransport(res.Provider.ID, res.BaseURL, res.APIKey), Model: sess.Model}
	}
	if err != nil {
		return nil, err
	}
	eng := &council.Engine{Store: s.Store, Caller: caller, Provider: sess.Provider, Model: sess.Model, CapUSD: capUSD, Concurrency: 3, Personas: all}
	var opts struct {
		Strong string `json:"strong_model"`
		Depth  string `json:"depth"`
	}
	_ = json.Unmarshal(sess.EstimateRaw, &opts)
	eng.Depth = opts.Depth
	if opts.Strong != "" && opts.Strong != sess.Model {
		var sc council.Caller
		if s.CallerFactory != nil {
			sc, err = s.CallerFactory(sess.Provider, opts.Strong)
		} else if res, ok := s.Cfg.Resolve(sess.Provider); ok {
			sc = council.LLMCaller{Transport: llm.NewTransport(res.Provider.ID, res.BaseURL, res.APIKey), Model: opts.Strong, Effort: "high"}
		}
		if err == nil && sc != nil {
			eng.Strong, eng.StrongModel = sc, opts.Strong
		}
	}
	return eng, nil
}

// runCouncil oturumu arka planda çalıştırır.
func (s *Server) runCouncil(id string) {
	s.runs.mu.Lock()
	if s.runs.cancel == nil {
		s.runs.cancel = map[string]context.CancelFunc{}
	}
	if _, running := s.runs.cancel[id]; running {
		s.runs.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.runs.cancel[id] = cancel
	s.runs.wg.Add(1)
	s.runs.mu.Unlock()
	go func() {
		defer func() {
			s.runs.mu.Lock()
			delete(s.runs.cancel, id)
			s.runs.mu.Unlock()
			cancel()
			s.runs.wg.Done()
		}()
		sess, err := s.Store.Council(id)
		if err != nil {
			return
		}
		eng, err := s.newEngine(sess)
		if err != nil {
			_ = s.Store.SetCouncilStatus(id, store.CouncilFailed, err.Error())
			return
		}
		if err := eng.Run(ctx, id); err != nil {
			_ = s.Store.SetCouncilStatus(id, store.CouncilFailed, "motor hatası: "+err.Error())
		}
	}()
}

// StopCouncils çalışan bütün oturumları iptal eder ve bitmelerini bekler (kapanışta çağrılır);
// eldeki çıktılar korunur, oturumlar sonradan devam ettirilebilir.
func (s *Server) StopCouncils() {
	s.runs.mu.Lock()
	for _, cancel := range s.runs.cancel {
		cancel()
	}
	s.runs.mu.Unlock()
	s.runs.wg.Wait()
}

// WaitCouncil çalışan tüm konsey oturumlarının bitmesini bekler (testler ve kapanış için).
func (s *Server) WaitCouncil() { s.runs.wg.Wait() }

func (s *Server) councilList(w http.ResponseWriter, r *http.Request) {
	list, err := s.Store.Councils(intQuery(r, "limit", 50, 1, 200))
	if err != nil {
		serverErr(w, err)
		return
	}
	type row struct {
		store.CouncilSession
		SpentUSD float64 `json:"spent_usd"`
		Running  bool    `json:"running"`
	}
	out := make([]row, len(list))
	s.runs.mu.Lock()
	for i, c := range list {
		_, running := s.runs.cancel[c.ID]
		out[i] = row{CouncilSession: c, Running: running}
	}
	s.runs.mu.Unlock()
	for i := range out {
		out[i].SpentUSD, _ = s.Store.SessionSpend(out[i].ID)
		out[i].PackJSON = nil
	}
	writeJSON(w, 200, out)
}

func (s *Server) councilGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sess, err := s.Store.Council(id)
	if err != nil {
		fail(w, 404, "konsey oturumu bulunamadı")
		return
	}
	outs, err := s.Store.Outputs(id)
	if err != nil {
		serverErr(w, err)
		return
	}
	spent, _ := s.Store.SessionSpend(id)
	s.runs.mu.Lock()
	_, running := s.runs.cancel[id]
	s.runs.mu.Unlock()
	writeJSON(w, 200, map[string]any{"session": sess, "outputs": outs, "spent_usd": spent, "running": running})
}

func (s *Server) councilCancel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.runs.mu.Lock()
	cancel, ok := s.runs.cancel[id]
	s.runs.mu.Unlock()
	if !ok {
		fail(w, 409, "oturum çalışmıyor")
		return
	}
	cancel()
	w.WriteHeader(204)
}

// councilResume yarıda kalan oturumu devam ettirir; yalnızca eksik çağrılar yapılır ve oturumun
// onaylanmış bütçe tavanı geçerlidir.
func (s *Server) councilResume(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sess, err := s.Store.Council(id)
	if err != nil {
		fail(w, 404, "konsey oturumu bulunamadı")
		return
	}
	switch sess.Status {
	case store.CouncilInterrupted, store.CouncilPartial, store.CouncilFailed, store.CouncilCanceled:
	case store.CouncilBudgetStop:
		fail(w, 409, "Bütçe tavanı aşıldı; devam için Ayarlar'dan bütçeyi artırıp yeni bir oturum başlat (önbellek sayesinde tamamlananlar tekrar ödenmez).")
		return
	default:
		fail(w, 409, "oturum devam ettirilemez: durum "+sess.Status)
		return
	}
	if !s.Cfg.Ready() && s.CallerFactory == nil {
		fail(w, 400, "Yapay zekâ sağlayıcısı hazır değil.")
		return
	}
	s.runCouncil(id)
	w.WriteHeader(202)
}

func (s *Server) councilDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.runs.mu.Lock()
	if cancel, ok := s.runs.cancel[id]; ok {
		cancel()
	}
	s.runs.mu.Unlock()
	if err := s.Store.DeleteCouncil(id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			fail(w, 404, "konsey oturumu bulunamadı")
			return
		}
		serverErr(w, err)
		return
	}
	w.WriteHeader(204)
}

// councilReport oturumun sentezini, persona görüşlerini ve kanıt listesini Markdown olarak indirir.
func (s *Server) councilReport(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sess, err := s.Store.Council(id)
	if err != nil {
		fail(w, 404, "konsey oturumu bulunamadı")
		return
	}
	outs, err := s.Store.Outputs(id)
	if err != nil {
		serverErr(w, err)
		return
	}
	var names []string
	for _, pid := range sess.SubjectIDs {
		if p, err := s.Store.Person(pid); err == nil {
			names = append(names, p.Name)
		}
	}
	ps, _ := personas.Load(s.PersonaDir)
	md := report.CouncilMarkdown(report.CouncilInput{Session: sess, Outputs: outs, Subjects: names, Personas: ps,
		GeneratedAt: time.Now(), Version: version.Version, Loc: s.Loc})
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="joe-konsey-%s.md"`, id))
	w.Header().Set("Cache-Control", "no-store")
	io.WriteString(w, md)
}

// followMarker takip sorusunda önceki özet ile yeni soruyu ayırır; arayüz yalnızca sonrasını gösterir.
const followMarker = "\n\nYeni soru: "

func clip(s string, n int) string {
	r := []rune(strings.Join(strings.Fields(s), " "))
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return string(r)
}

// followUpContext önceki soru-cevabın kısa özetini döndürür. Cevap metni yeniden kanıt sayılmaz:
// model yeni cevabı yine paketteki K kimliklerine dayandırmak zorundadır.
func (s *Server) followUpContext(sessionID string, personID int64) (string, bool) {
	sess, err := s.Store.Council(sessionID)
	if err != nil || sess.Kind != "ask" || len(sess.SubjectIDs) != 1 || sess.SubjectIDs[0] != personID {
		return "", false
	}
	outs, err := s.Store.Outputs(sessionID)
	if err != nil {
		return "", false
	}
	var b strings.Builder
	q := sess.Question
	if i := strings.LastIndex(q, followMarker); i >= 0 {
		q = q[i+len(followMarker):]
	}
	fmt.Fprintf(&b, "Önceki soru: %s", clip(q, 400))
	for _, o := range outs {
		var a council.Answer
		if o.Stage == "answer" && o.Status == store.OutDone && json.Unmarshal(o.Output, &a) == nil && !a.Abstain {
			fmt.Fprintf(&b, "\nÖnceki cevabın özeti (kanıt değildir): %s", clip(a.Answer, 600))
		}
	}
	return b.String(), true
}

// relevantIDs sorunun anahtar sözcüklerine (tam metin) ve, gömme sunucusu YEREL ise, anlamına göre
// ilgili mesajları bulur. Uzak bir gömme sunucusuna soru gönderilmez; yalnızca maskelenmiş soru
// yerel sunucuya gider. Hata durumunda sessizce yalnızca metin aramasıyla devam edilir: bu bir
// iyileştirmedir, analiz bu yüzden başarısız olmaz.
func (s *Server) relevantIDs(question string) []int64 {
	const limit = 60
	var ftsIDs []int64
	if terms := evidence.QueryTerms(question); len(terms) > 0 {
		if hits, err := s.Store.SearchAny(terms, limit); err == nil {
			for _, h := range hits {
				ftsIDs = append(ftsIDs, h.ID)
			}
		}
	}
	c, ok := s.embedClient()
	if !ok || !embed.IsLoopbackURL(s.Cfg.Get().EmbedURL) {
		return ftsIDs
	}
	idx, err := s.vectors(c.Model)
	if err != nil || idx == nil || len(idx.IDs) == 0 {
		return ftsIDs
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	qv, err := c.Embed(ctx, []string{mask.New().Apply(question)})
	if err != nil {
		return ftsIDs
	}
	sem := embed.Relative(embed.Search(idx, qv[0], limit, 0), 0.10, 3)
	semIDs := make([]int64, len(sem))
	for i, h := range sem {
		semIDs[i] = h.ID
	}
	return embed.Fuse(ftsIDs, semIDs, limit)
}

// ModeratorQuestion tartışma modunda mercek yanıtlarını karşılaştıran sabit sorudur; arayüz de bunu tanır.
const ModeratorQuestion = "Bu mercek yanıtları arasındaki uzlaşı ve ayrışma noktalarını, her biri için paketteki kanıt kimlikleriyle özetle. Hangi görüşün hangi mercekten geldiğini belirt; yanıtların dayanmadığı bir şeyi ekleme."

// answerLine bir soru oturumunun "@mercek: cevap özeti" satırını döndürür.
func (s *Server) answerLine(sessionID string, personID int64) (string, bool) {
	sess, err := s.Store.Council(sessionID)
	if err != nil || sess.Kind != "ask" || len(sess.SubjectIDs) != 1 || sess.SubjectIDs[0] != personID {
		return "", false
	}
	outs, err := s.Store.Outputs(sessionID)
	if err != nil {
		return "", false
	}
	who := "genel"
	if len(sess.PersonaIDs) == 1 {
		who = sess.PersonaIDs[0]
	}
	for _, o := range outs {
		var a council.Answer
		if o.Stage == "answer" && o.Status == store.OutDone && json.Unmarshal(o.Output, &a) == nil {
			if a.Abstain {
				return fmt.Sprintf("@%s: çekimser kaldı (%s)", who, clip(a.AbstainReason, 200)), true
			}
			return fmt.Sprintf("@%s: %s", who, clip(a.Answer, 500)), true
		}
	}
	return "", false
}

// coverage kanıt paketinin kapsamını özetler: kaç kesit/mesaj, hangi türlerden, kaç ayın hangi payı. Paket
// büyüdükçe tam metni okumak zorlaştığı için onay ekranında özet olarak gösterilir.
func coverage(pk evidence.Pack, totalMsgs, monthsTotal int) map[string]any {
	kinds := map[string]int{}
	months := map[string]bool{}
	msgs := 0
	for _, it := range pk.Items {
		kinds[it.Kind]++
		msgs += len(it.Messages)
		for t := it.From; !t.After(it.To); t = t.AddDate(0, 1, 0) {
			months[t.Format("2006-01")] = true
		}
		months[it.To.Format("2006-01")] = true
	}
	return map[string]any{"items": len(pk.Items), "messages": msgs, "total_messages": totalMsgs, "kinds": kinds, "months_covered": len(months), "months_total": monthsTotal}
}
