package council

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/MustafaKemal0146/joe/internal/audit"
	"github.com/MustafaKemal0146/joe/internal/budget"
	"github.com/MustafaKemal0146/joe/internal/llm"
	"github.com/MustafaKemal0146/joe/internal/personas"
	"github.com/MustafaKemal0146/joe/internal/store"
)

// Caller bir LLM çağrısı yapar. Gerçek uygulama sağlayıcıya gider; testlerde sahtesi kullanılır.
type Caller interface {
	Complete(ctx context.Context, system, user string, maxTokens int) (string, llm.Usage, error)
}

// LLMCaller bir llm.Transport'u Caller olarak sarar.
type LLMCaller struct {
	Transport llm.Transport
	Model     string
	// Effort düşünme düzeyidir ("" = sağlayıcı varsayılanı); güçlü model adımlarında "high" kullanılır.
	Effort string
}

// Complete tek istek-yanıt çağrısı yapar.
func (c LLMCaller) Complete(ctx context.Context, system, user string, maxTokens int) (string, llm.Usage, error) {
	temp := 0.3
	return llm.Complete(ctx, c.Transport, llm.Request{
		Model: c.Model, MaxTokens: maxTokens, Temperature: &temp, ReasoningEffort: c.Effort,
		Messages: []llm.Message{{Role: llm.RoleSystem, Content: system}, {Role: llm.RoleUser, Content: user}},
	})
}

// Çıktı token sınırları (çağrı başına).
const (
	// Türkçe JSON, token açısından pahalıdır (≈2,4 karakter/token); gerçek denemede 1800 token'lık sınır
	// persona çıktısını ortasında kesti. Sınırlar bu yüzden geniş tutulur; fazla çıktı zaten istemle kısıtlanır.
	PersonaMaxOut   = 10000
	ModeratorMaxOut = 10000
	SynthesisMaxOut = 16000
	AskMaxOut       = 3000
	// truncRetryCap kesilen bir çıktının yeniden denenirken ulaşabileceği en yüksek sınır.
	truncRetryCap = 32000
)

// ErrBudget çağrı bütçe tavanı yüzünden yapılmadı.
var ErrBudget = errors.New("bütçe tavanı")

// Engine konsey motorudur.
type Engine struct {
	Store       *store.Store
	Caller      Caller
	Provider    string
	Model       string
	CapUSD      float64
	Concurrency int
	Personas    []personas.Persona
	// Strong moderatör, sentez ve doğrulama için güçlü çağırıcıdır (nil: Caller). StrongModel onun model adıdır.
	Strong      Caller
	StrongModel string
	// Depth "derin" ise persona aşamasından önce dönem özetleri üretilir.
	Depth string

	mu       sync.Mutex
	reserved float64
}

const pairNote = "\n\nBu analiz İKİ KİŞİ arasındaki dinamiği (ritim, karşılıklılık, çatışma ve yakınlık örüntüleri) ele alır. İki tarafı da EŞİT ayrıntıyla oku: her gözlem ve hipotezin \"about\" alanına kimin hakkında olduğunu yaz (\"a\" ilk kişi, \"b\" ikinci kişi, \"ikisi\" aralarındaki etkileşim); en az bir hipotezi ilk kişi, en az birini ikinci kişi, en az birini ikisi hakkında kur. Taraflardan birini haklı ya da haksız ilan etme, uyum puanı ya da hüküm üretme; her iki tarafın olası duygu, ihtiyaç ve davranış nedenlerini, kanıta bağlı örüntüleri ve alternatif açıklamalarla birlikte yaz."

func hash(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		fmt.Fprintf(h, "%d:%s|", len(p), p)
	}
	return hex.EncodeToString(h.Sum(nil))[:24]
}

func (e *Engine) persona(id string) (personas.Persona, bool) {
	for _, p := range e.Personas {
		if p.ID == id {
			return p, true
		}
	}
	return personas.Persona{}, false
}

// validIDs oturumun paket kimliklerini döndürür.
func validIDs(sess store.CouncilSession) map[string]bool {
	var pk struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	json.Unmarshal(sess.PackJSON, &pk)
	m := map[string]bool{}
	for _, it := range pk.Items {
		m[it.ID] = true
	}
	return m
}

// target bir çağrının gideceği çağırıcı ve modeldir.
type target struct {
	caller Caller
	model  string
}

func (e *Engine) main() target { return target{e.Caller, e.Model} }

// strong güçlü adımların hedefidir; ayrı bir model tanımlı değilse ana hedeftir.
func (e *Engine) strong() target {
	if e.Strong != nil && e.StrongModel != "" {
		return target{e.Strong, e.StrongModel}
	}
	return e.main()
}

type result struct {
	tgt       target
	raw       string
	usage     llm.Usage
	cost      float64
	fromCache bool
	// Kesilme tespiti ve yeniden deneme için çağrının girdileri.
	system, user string
	maxOut       int
	truncated    bool // çıktı token sınırına dayandı: JSON büyük olasılıkla yarım
}

// call, bütçeyi rezerve edip çağrıyı yapar ve defteri günceller. cacheKey boş değilse önbellek kullanılır.
// Önbellekten dönen yanıt para harcamaz. Rezervasyon, eşzamanlı çağrıların tavanı birlikte aşmasını önler.
func (e *Engine) call(ctx context.Context, sessionID, kind, system, user string, maxOut int, cacheKey string) (result, error) {
	return e.callOn(ctx, e.main(), sessionID, kind, system, user, maxOut, cacheKey)
}

// callOn call gibi çalışır; çağrıyı verilen hedefe (çağırıcı + model) yapar ve maliyeti o modele göre yazar.
func (e *Engine) callOn(ctx context.Context, tgt target, sessionID, kind, system, user string, maxOut int, cacheKey string) (result, error) {
	if cacheKey != "" {
		if c, ok, err := e.Store.CacheGet(cacheKey); err == nil && ok {
			u := llm.Usage{PromptTokens: c.PromptTokens, CompletionTokens: c.CompletionTokens}
			_ = e.Store.RecordUsage(store.Usage{Session: sessionID, Kind: kind, Provider: e.Provider, Model: tgt.model,
				PromptTokens: u.PromptTokens, CompletionTokens: u.CompletionTokens, FromCache: true})
			return result{tgt: tgt, raw: c.Response, usage: u, fromCache: true}, nil
		}
	}
	est := budget.Compute(budget.Plan{Model: tgt.model, SharedPrefixTokens: budget.EstimateTokens(system + user), Calls: 1, OutputTokensPerCall: maxOut})
	high := est.HighUSD
	spent, err := e.Store.SessionSpend(sessionID)
	if err != nil {
		return result{}, err
	}
	e.mu.Lock()
	if gerr := (budget.Guard{CapUSD: e.CapUSD}).Allow(spent+e.reserved, high); gerr != nil {
		e.mu.Unlock()
		return result{}, fmt.Errorf("%w: %v", ErrBudget, gerr)
	}
	e.reserved += high
	e.mu.Unlock()
	defer func() {
		e.mu.Lock()
		e.reserved -= high
		e.mu.Unlock()
	}()

	raw, usage, err := tgt.caller.Complete(ctx, system, user, maxOut)
	if err != nil {
		return result{}, err
	}
	if usage.Total() == 0 { // sağlayıcı kullanım bildirmediyse tahminle deftere yaz
		usage = llm.Usage{PromptTokens: budget.EstimateTokens(system + user), CompletionTokens: budget.EstimateTokens(raw)}
	}
	cost, _ := llm.EstimateCost(tgt.model, usage.PromptTokens, usage.CompletionTokens)
	if err := e.Store.RecordUsage(store.Usage{Session: sessionID, Kind: kind, Provider: e.Provider, Model: tgt.model,
		PromptTokens: usage.PromptTokens, CompletionTokens: usage.CompletionTokens, CostUSD: cost}); err != nil {
		return result{}, err
	}
	return result{tgt: tgt, raw: raw, usage: usage, cost: cost, system: system, user: user, maxOut: maxOut, truncated: usage.CompletionTokens >= maxOut-4}, nil
}

const repairSystem = "Aşağıda bir modelin şemaya uymayan çıktısı ve hata mesajı var. Çıktıyı, içeriğine yeni bilgi ya da kanıt kimliği EKLEMEDEN, şemaya uygun tek bir JSON nesnesi olarak yeniden yaz. Yalnızca JSON döndür."

// withRepair ham çıktıyı ayrıştırır. Çıktı token sınırında kesildiyse (yarım JSON) onarım işe yaramaz: aynı istek
// daha yüksek sınırla bir kez yeniden denenir. Biçim hatası varsa tek bir küçük onarım çağrısı yapılır.
// İkisi de başarısız olursa hata döner ve uydurma çıktı üretilmez.
func (e *Engine) withRepair(ctx context.Context, sessionID, kind, schema string, res result, parse func(string) error) (result, int, error) {
	attempts := 1
	err := parse(res.raw)
	if err == nil {
		return res, attempts, nil
	}
	if res.truncated && res.system != "" {
		attempts++
		bigger := res.maxOut * 2
		if bigger > truncRetryCap {
			bigger = truncRetryCap
		}
		redo, cerr := e.callOn(ctx, res.tgt, sessionID, kind, res.system, res.user, bigger, "")
		if cerr != nil {
			return res, attempts, fmt.Errorf("kesilen çıktı yeniden denenemedi: %w", cerr)
		}
		redo.cost += res.cost
		redo.usage = redo.usage.Add(res.usage)
		if perr := parse(redo.raw); perr == nil {
			return redo, attempts, nil
		} else if redo.truncated {
			return redo, attempts, fmt.Errorf("çıktı %d token sınırında bile kesildi: %w", bigger, perr)
		}
		res, err = redo, parse(redo.raw)
	}
	attempts++
	user := fmt.Sprintf("Şema:\n%s\n\nHata: %v\n\nÖnceki çıktı:\n%s", schema, err, res.raw)
	fixed, cerr := e.callOn(ctx, res.tgt, sessionID, kind+"_repair", repairSystem, user, PersonaMaxOut, "")
	if cerr != nil {
		return res, attempts, fmt.Errorf("onarım çağrısı başarısız: %w", cerr)
	}
	fixed.cost += res.cost
	fixed.usage = fixed.usage.Add(res.usage)
	if err2 := parse(fixed.raw); err2 != nil {
		return fixed, attempts, err2
	}
	return fixed, attempts, nil
}

func (e *Engine) save(o store.CouncilOutput) {
	_ = e.Store.SaveOutput(o)
}

// runPersona bir persona'nın tam akışını yürütür ve sonucu kaydeder.
func (e *Engine) runPersona(ctx context.Context, sess store.CouncilSession, p personas.Persona, valid map[string]bool, extra string) {
	out := store.CouncilOutput{SessionID: sess.ID, Stage: "persona", PersonaID: p.ID, Status: store.OutRunning}
	e.save(out)
	note := ""
	if sess.Kind == "pair" {
		note = pairNote
	}
	system := personas.CommonSystem()
	user := sess.PackText + "\n\n"
	if extra != "" {
		user += extra + "\n\n" // dönem özetleri: tüm persona'lar için aynı, bölümden önce (ortak ön ek korunur)
	}
	user += personas.Section(p) + note
	key := store.CacheKey(sess.PackHash, p.ID, e.Model, personas.PromptVersion, sess.Kind, hash(extra))

	res, err := e.call(ctx, sess.ID, "persona", system, user, PersonaMaxOut, key)
	if err != nil {
		e.fail(&out, err)
		return
	}
	var parsed personas.Output
	var pruned []string
	fixed, attempts, err := e.withRepair(ctx, sess.ID, "persona", personas.Schema, res, func(raw string) error {
		o, perr := personas.ParseOutput(raw)
		if perr == nil {
			parsed, pruned = o, nil
			return nil
		}
		// Katı ayrıştırma bir maddeyi geçersiz bulduysa: geçersizleri ayıklayıp kalanı kabul et (çıktı çöpe gitmesin).
		if lo, notes, lerr := personas.ParseOutputLenient(raw); lerr == nil {
			parsed, pruned = lo, notes
			return nil
		}
		return perr
	})
	out.Raw, out.Attempts = fixed.raw, attempts
	out.PromptTokens, out.CompletionTokens, out.CostUSD, out.FromCache = fixed.usage.PromptTokens, fixed.usage.CompletionTokens, fixed.cost, fixed.fromCache
	if err != nil {
		e.fail(&out, err)
		return
	}
	if !fixed.fromCache {
		_ = e.Store.CachePut(key, store.CachedResponse{Response: fixed.raw, PromptTokens: fixed.usage.PromptTokens, CompletionTokens: fixed.usage.CompletionTokens})
	}
	out.Output, _ = json.Marshal(parsed)
	rep := parsed.Audit(valid)
	rep.Violations = append(rep.Violations, quoteViolations(parsed, sess.PackText)...)
	if len(pruned) > 0 {
		rep.Violations = append(rep.Violations, audit.Violation{Rule: audit.RulePruned, Claim: -1,
			Message: fmt.Sprintf("%d geçersiz madde ayıklandı, kalanı kabul edildi: %s", len(pruned), strings.Join(pruned, "; "))})
	}
	out.Audit, _ = json.Marshal(rep)
	out.Status = store.OutDone
	e.save(out)
}

func (e *Engine) fail(out *store.CouncilOutput, err error) {
	switch {
	case errors.Is(err, ErrBudget):
		out.Status = store.OutSkipped
	default:
		out.Status = store.OutFailed
	}
	out.Error = err.Error()
	e.save(*out)
}

// summaries yalnızca tamamlanmış persona çıktılarının kompakt özetini üretir (moderatör ve sentez girdisi).
func summaries(outs []store.CouncilOutput, order []string) (string, []string) {
	// Çıktılar, bitiş sırasına değil oturumdaki persona sırasına göre dizilir: moderatör/sentez önbellek
	// anahtarı bu metne bağlıdır ve eşzamanlı çalışmada bitiş sırası her seferinde farklı olabilir.
	rank := map[string]int{}
	for i, id := range order {
		rank[id] = i
	}
	outs = append([]store.CouncilOutput(nil), outs...)
	sort.SliceStable(outs, func(i, j int) bool { return rank[outs[i].PersonaID] < rank[outs[j].PersonaID] })
	type view struct {
		Persona      string                 `json:"persona"`
		Observations []personas.Observation `json:"observations,omitempty"`
		Hypotheses   []personas.Hypothesis  `json:"hypotheses,omitempty"`
		Abstain      string                 `json:"abstain_reason,omitempty"`
	}
	var vs []view
	var ids []string
	for _, o := range outs {
		if o.Stage != "persona" || o.Status != store.OutDone {
			continue
		}
		var po personas.Output
		if json.Unmarshal(o.Output, &po) != nil {
			continue
		}
		v := view{Persona: o.PersonaID, Observations: po.Observations, Hypotheses: po.Hypotheses}
		if po.Abstain {
			v.Abstain = po.AbstainReason
		}
		vs = append(vs, v)
		ids = append(ids, o.PersonaID)
	}
	b, _ := json.MarshalIndent(vs, "", " ")
	return string(b), ids
}

const moderatorSystem = `Sen bir moderatörsün. Birden çok kuramsal merceğin (persona) çıktılarını okuyup uzlaştıkları ve ayrıştıkları noktaları çıkarırsın. Kendi yorumunu ekleme, yeni kanıt uydurma, yalnızca verilen persona çıktılarındaki K<n> kimliklerini kullan. Tanı, tehlikelilik ya da yargı üretme.
DİL: Bu metni kuram bilmeyen sıradan insanlar da okuyacak. Sade, günlük Türkçe kullan; kuram terimi geçerse aynı cümlede ne demek olduğunu anlat. Kısa cümleler kur, somut ol.
Uzunluk ve içerik:
- "agreements": 4–6 madde; her "point" 2–3 cümle, mercekler neden aynı yere varıyor onu da anlat.
- "disagreements": 3–6 konu. "topic" kısa ve anlaşılır bir başlık olsun (ör. "... ne anlama geliyor?"). "meaning": bu konunun halk diliyle ne demek olduğunu ve neden önemli olduğunu 2–3 cümleyle açıkla. Her konuda en az iki "position" yaz; her "view" 4–6 cümle olsun (yaklaşık 400–700 karakter): o merceğin konuya nasıl baktığını, neden öyle okuduğunu ve kanıtlardan hangi somut anın buna dayanak olduğunu kendi cümlelerinle, uzun uzun anlat. Persona çıktılarındaki ifadeleri aynen kopyalama, sadeleştirerek yeniden yaz.
- "gaps": 3–5 madde, her biri 1–2 cümle.
Yanıtın yalnızca şu JSON olsun:
{"agreements":[{"point":"","personas":["jung"],"evidence":["K1"]}],
 "disagreements":[{"topic":"","meaning":"","positions":[{"persona":"","view":"","evidence":["K2"]},{"persona":"","view":"","evidence":["K3"]}]}],
 "gaps":["hiçbir persona'nın ele almadığı ya da veride eksik olan noktalar"]}`

const synthesisSystem = `Sen nötr bir sentezcisin. Persona çıktılarının özetini ve moderatör çıktısını okuyup tek, ayrıntılı ve kanıta bağlı bir GENEL DEĞERLENDİRME yazarsın. Bu metni kuram bilmeyen sıradan insanlar okuyacak.
Kurallar: klinik tanı, kişilik etiketi, tehlikelilik/suçluluk çıkarımı yok ("genel değerlendirme" bir sağlık tanısı DEĞİL, verinin genel bir okumasıdır); yalnızca verilen K<n> kimlikleri; yorumlar "olabilir" kipiyle; her yakınsama ve ayrışma maddesinde 1–3 kanıt kimliği olmalı (uzun kimlik listeleri yazma); en az bir alternatif açıklama; veri yetersizse bunu söyle.
DİL: Sade, günlük, akıcı Türkçe. Kuram terimi geçerse aynı cümlede ne demek olduğunu anlat; kısa cümleler kur; somut örnekler ver (hangi dönem, hangi davranış). Akademik üslup ve terim yığınından kaçın.
Uzunluk ve içerik:
- "sections": GENEL DEĞERLENDİRME, 8–10 başlıklı bölüm. Her bölüm {"title": sade ve kısa başlık, "text": 500–900 karakterlik akıcı metin, "evidence": en çok 3 kanıt kimliği}. Bölüm metinlerinin TOPLAMI EN AZ 4000, EN FAZLA 8000 karakter olmalı; kısa yazma, bu alan metnin en değerli bölümüdür. Şu sırayı izle: (1) Kısa özet: kim, ne kadar veri, hangi dönem; (2) ilişkinin ya da kişinin genel resmi; (3) iyi giden, güçlü görünen yanlar; (4) zorlayan ya da sorunlu görünen yanlar; (5) zaman içinde nasıl değişmiş; (6) merceklerin ortak vardığı çekirdek okuma; (7) merceklerin ayrıştığı noktalar ve bunun neden önemli olduğu; (8) belirsizlikler, veride görünmeyenler ve temkinler; (9) bu veriye bakarak insanın kendine sorabileceği sorular (öneri ya da hüküm değil). Kullanıcı bir amaç yazdıysa ona doğrudan cevap veren ayrı bir bölüm ekle. "summary" alanını boş bırak ("").
- "evolution": ilişkinin zaman içindeki gelişimi, 2–3 paragraf (yaklaşık 1000–1800 karakter); ay örnekleri, yoğun/sessiz dönem ve sessizlik kesitlerine dayan; hangi dönemde ne değişmiş olabilir; veri bu gelişimi göstermeye yetmiyorsa açıkça söyle. "evolution_evidence" alanına en çok 5 kimlik yaz.
- "convergences" ve "divergences": 5–7'şer madde, her biri 3 cümle.
- "alternative_explanations", "open_questions", "data_needed": her biri 3–5 madde, 1–2 cümle.
Yanıtın yalnızca şu JSON olsun:
{"summary":"","sections":[{"title":"","text":"","evidence":["K1"]}],"evolution":"","evolution_evidence":["K1"],"convergences":[{"text":"","evidence":["K1"]}],"divergences":[{"text":"","evidence":["K2"]}],
 "alternative_explanations":[""],"open_questions":[""],"data_needed":[""],"confidence":"low | medium | high","limits":""}`

func metricsPart(packText string) string {
	if i := strings.Index(packText, "\n## K"); i > 0 {
		return packText[:i]
	}
	return packText
}

// Run oturumu çalıştırır; daha önce tamamlanan çıktılar atlanır (devam etme). Bağlam iptal edilirse
// oturum "canceled" olur; eldeki çıktılar korunur.
func (e *Engine) Run(ctx context.Context, sessionID string) error {
	sess, err := e.Store.Council(sessionID)
	if err != nil {
		return err
	}
	if err := e.Store.SetCouncilStatus(sessionID, store.CouncilRunning, ""); err != nil {
		return err
	}
	if sess.Kind == "ask" {
		return e.runAsk(ctx, sess)
	}
	valid := validIDs(sess)
	existing, err := e.Store.Outputs(sessionID)
	if err != nil {
		return err
	}
	done := map[string]bool{}
	for _, o := range existing {
		if o.Status == store.OutDone {
			done[o.Stage+"|"+o.PersonaID] = true
		}
	}

	// 0) Derin mod: kesitleri dönemlere bölüp her dönemi özetlet (tüm persona'ların okuduğu ortak ek)
	extraText := ""
	if e.Depth == "derin" {
		extraText = e.runDigests(ctx, sess, existing)
		if ctx.Err() != nil {
			return e.Store.SetCouncilStatus(sessionID, store.CouncilCanceled, "")
		}
	}

	// 1) Persona aşaması
	conc := e.Concurrency
	if conc <= 0 {
		conc = 3
	}
	sem := make(chan struct{}, conc)
	var wg sync.WaitGroup
	for _, pid := range sess.PersonaIDs {
		if done["persona|"+pid] {
			continue
		}
		p, ok := e.persona(pid)
		if !ok {
			e.save(store.CouncilOutput{SessionID: sess.ID, Stage: "persona", PersonaID: pid, Status: store.OutFailed, Error: "persona tanımı bulunamadı"})
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			if ctx.Err() != nil {
				return
			}
			e.runPersona(ctx, sess, p, valid, extraText)
		}()
	}
	wg.Wait()
	if ctx.Err() != nil {
		return e.Store.SetCouncilStatus(sessionID, store.CouncilCanceled, "")
	}

	outs, err := e.Store.Outputs(sessionID)
	if err != nil {
		return err
	}
	summary, doneIDs := summaries(outs, sess.PersonaIDs)
	failedAny := len(doneIDs) < len(sess.PersonaIDs)
	budgetStopped := false
	for _, o := range outs {
		if o.Status == store.OutSkipped {
			budgetStopped = true
		}
	}
	if len(doneIDs) == 0 {
		st, msg := store.CouncilFailed, "hiçbir persona kullanılabilir çıktı üretemedi"
		if budgetStopped {
			st, msg = store.CouncilBudgetStop, "bütçe tavanı nedeniyle hiçbir persona çalıştırılamadı"
		}
		return e.Store.SetCouncilStatus(sessionID, st, msg)
	}

	// 2) Moderatör (en az iki persona gerekir)
	var modJSON string
	if len(doneIDs) >= 2 && !done["moderator|"] {
		o := store.CouncilOutput{SessionID: sess.ID, Stage: "moderator", Status: store.OutRunning}
		e.save(o)
		cited := citedItems(sess.PackText, summary, citedBudgetChars)
		user := "Persona çıktıları:\n" + summary + cited
		if extraText != "" {
			user += "\n\n" + extraText
		}
		key := store.CacheKey(sess.PackHash, "moderator", e.strong().model, personas.PromptVersion, hash(summary, cited, extraText))
		res, cerr := e.callOn(ctx, e.strong(), sess.ID, "moderator", moderatorSystem, user, ModeratorMaxOut, key)
		if cerr == nil {
			var mod Moderator
			fixed, attempts, perr := e.withRepair(ctx, sess.ID, "moderator", moderatorSystem, res, func(raw string) error {
				m, err := ParseModerator(raw)
				if err == nil {
					mod = m
				}
				return err
			})
			o.Raw, o.Attempts, o.CostUSD, o.FromCache = fixed.raw, attempts, fixed.cost, fixed.fromCache
			o.PromptTokens, o.CompletionTokens = fixed.usage.PromptTokens, fixed.usage.CompletionTokens
			if perr == nil {
				if !fixed.fromCache {
					_ = e.Store.CachePut(key, store.CachedResponse{Response: fixed.raw, PromptTokens: fixed.usage.PromptTokens, CompletionTokens: fixed.usage.CompletionTokens})
				}
				o.Output, _ = json.Marshal(mod)
				rep := audit.Check(mod.Claims(), "", valid)
				o.Audit, _ = json.Marshal(rep)
				o.Status = store.OutDone
				e.save(o)
				modJSON = string(o.Output)
			} else {
				e.fail(&o, perr)
			}
		} else {
			e.fail(&o, cerr)
		}
	} else if done["moderator|"] {
		for _, o := range outs {
			if o.Stage == "moderator" && o.Status == store.OutDone {
				modJSON = string(o.Output)
			}
		}
	}
	if ctx.Err() != nil {
		return e.Store.SetCouncilStatus(sessionID, store.CouncilCanceled, "")
	}

	// 3) Sentez
	synthOK := done["synthesis|"]
	if !synthOK {
		o := store.CouncilOutput{SessionID: sess.ID, Stage: "synthesis", Status: store.OutRunning}
		e.save(o)
		cited := citedItems(sess.PackText, summary+modJSON, citedBudgetChars)
		user := "Metrikler:\n" + metricsPart(sess.PackText) + "\n\nPersona çıktıları:\n" + summary
		if modJSON != "" {
			user += "\n\nModeratör çıktısı:\n" + modJSON
		}
		user += cited
		if extraText != "" {
			user += "\n\n" + extraText
		}
		if sess.Kind == "pair" {
			user += pairSynthesisNote(sess)
		}
		key := store.CacheKey(sess.PackHash, "synthesis", e.strong().model, personas.PromptVersion, hash(summary, modJSON, cited, extraText))
		res, cerr := e.callOn(ctx, e.strong(), sess.ID, "synthesis", synthesisSystem, user, SynthesisMaxOut, key)
		if cerr == nil {
			var syn Synthesis
			fixed, attempts, perr := e.withRepair(ctx, sess.ID, "synthesis", synthesisSystem, res, func(raw string) error {
				s, err := ParseSynthesis(raw)
				if err == nil {
					syn = s
				}
				return err
			})
			o.Raw, o.Attempts, o.CostUSD, o.FromCache = fixed.raw, attempts, fixed.cost, fixed.fromCache
			o.PromptTokens, o.CompletionTokens = fixed.usage.PromptTokens, fixed.usage.CompletionTokens
			if perr == nil {
				if !fixed.fromCache {
					_ = e.Store.CachePut(key, store.CachedResponse{Response: fixed.raw, PromptTokens: fixed.usage.PromptTokens, CompletionTokens: fixed.usage.CompletionTokens})
				}
				o.Output, _ = json.Marshal(syn)
				rep := audit.Check(syn.Claims(), strings.Join(append(append([]string{}, syn.OpenQuestion...), syn.DataNeeded...), " "), valid)
				if n := len([]rune(syn.Summary)); n < minOverviewChars {
					rep.Violations = append(rep.Violations, audit.Violation{Rule: audit.RuleShortOverview, Claim: -1,
						Message: fmt.Sprintf("Genel değerlendirme %d karakter; en az %d istenmişti. Yeniden çalıştırırsan daha uzun yazılabilir.", n, minOverviewChars)})
				}
				if lost := lostDivergence(modJSON, syn); lost != "" {
					rep.Violations = append(rep.Violations, audit.Violation{Rule: audit.RuleLostDivergence, Claim: -1, Message: lost})
				}
				o.Audit, _ = json.Marshal(rep)
				o.Status = store.OutDone
				e.save(o)
				synthOK = true
			} else {
				e.fail(&o, perr)
			}
		} else {
			e.fail(&o, cerr)
		}
	}
	if ctx.Err() != nil {
		return e.Store.SetCouncilStatus(sessionID, store.CouncilCanceled, "")
	}

	// 4) Doğrulama: sentezdeki iddiaların kanıt kesitleriyle desteklenip desteklenmediği (ek güvence; başarısızlığı
	// oturumu bozmaz).
	if synthOK && !done["verify|"] && ctx.Err() == nil {
		for _, o := range func() []store.CouncilOutput { x, _ := e.Store.Outputs(sessionID); return x }() {
			if o.Stage == "synthesis" && o.Status == store.OutDone {
				var syn Synthesis
				if json.Unmarshal(o.Output, &syn) == nil {
					e.runVerify(ctx, sess, syn)
				}
			}
		}
	}
	if ctx.Err() != nil {
		return e.Store.SetCouncilStatus(sessionID, store.CouncilCanceled, "")
	}

	// Son durum
	final, _ := e.Store.Outputs(sessionID)
	for _, o := range final {
		if o.Status == store.OutSkipped && o.Stage != "verify" { // doğrulama ek güvencedir; bütçe yüzünden atlanması oturumu durdurmaz
			budgetStopped = true
		}
	}
	switch {
	case budgetStopped:
		return e.Store.SetCouncilStatus(sessionID, store.CouncilBudgetStop, "bütçe tavanı nedeniyle bazı çağrılar yapılmadı; eldeki sonuçlar korundu")
	case !synthOK:
		return e.Store.SetCouncilStatus(sessionID, store.CouncilPartial, "sentez üretilemedi; persona çıktıları kullanılabilir")
	case failedAny:
		return e.Store.SetCouncilStatus(sessionID, store.CouncilPartial, "bazı persona'lar çıktı üretemedi")
	}
	return e.Store.SetCouncilStatus(sessionID, store.CouncilDone, "")
}

// minOverviewChars genel değerlendirmenin (summary) istenen en az uzunluğudur.
const minOverviewChars = 4000

// lostDivergence moderatör persona görüşlerinde ayrışma bulduğu hâlde sentezin "ayrışan noktalar"
// bölümü boşsa açıklayıcı bir uyarı döndürür: ayrışan görüşler sentezde kaybolmamalıdır.
func lostDivergence(modJSON string, syn Synthesis) string {
	if modJSON == "" || len(syn.Divergences) > 0 {
		return ""
	}
	m, err := ParseModerator(modJSON)
	if err != nil || len(m.Disagreements) == 0 {
		return ""
	}
	return fmt.Sprintf("Moderatör %d ayrışma saptadı ama sentezde ayrışan nokta yok; merceklerin farklı okumaları yansıtılmamış olabilir.", len(m.Disagreements))
}

// citedBudgetChars moderatör ve sentez girdisine eklenen kanıt kesitlerinin üst sınırıdır (karakter; ≈ 40 bin token).
const citedBudgetChars = 100_000

var reKID = regexp.MustCompile(`K(\d+)`)

// citedItems persona çıktılarında (ya da verilen metinde) atıf yapılan kanıt kesitlerinin ham metnini döndürür:
// moderatör ve sentez, atıf yapılan kimliklerin ne dediğini görebilsin; yalnızca persona özetlerine bakıp
// körlemesine birleştirmesin. Sıra K numarasına göredir; sınır aşılırsa kalanı atılır.
func citedItems(packText, from string, maxChars int) string {
	blocks := kBlocks(packText)
	seen := map[int]bool{}
	var nums []int
	for _, m := range reKID.FindAllStringSubmatch(from, -1) {
		n, _ := strconv.Atoi(m[1])
		if !seen[n] && blocks["K"+m[1]] != "" {
			seen[n] = true
			nums = append(nums, n)
		}
	}
	sort.Ints(nums)
	var b strings.Builder
	b.WriteString("\n\nAtıf yapılan kanıt kesitleri (ham metin; yalnızca bu kesitlere dayan):\n")
	used := 0
	for _, n := range nums {
		blk := blocks["K"+strconv.Itoa(n)]
		if used+len(blk) > maxChars {
			break
		}
		b.WriteString(blk)
		used += len(blk)
	}
	if used == 0 {
		return ""
	}
	return b.String()
}

// pairSynthesisNote iki kişilik analizde sentezin her iki tarafı da eşit ayrıntıyla ele almasını ister.
func pairSynthesisNote(sess store.CouncilSession) string {
	names := "iki kişi"
	var pk struct {
		Subject string `json:"subject"`
	}
	if json.Unmarshal(sess.PackJSON, &pk) == nil && strings.Contains(pk.Subject, " × ") {
		names = pk.Subject
	}
	parts := strings.SplitN(names, " × ", 2)
	a, b := "ilk kişi", "ikinci kişi"
	if len(parts) == 2 {
		a, b = parts[0], parts[1]
	}
	return pairNote + fmt.Sprintf("\n\nİKİ TARAFLI OKUMA: genel değerlendirmede (\"sections\") en az şu üç bölüm olsun: \"%s'in gözünden\", \"%s'in gözünden\" ve \"İkisinin arasındaki dinamik\". Her iki tarafın duygusunu, ihtiyacını ve davranışını eşit ayrıntıyla yaz; birini diğerinin yan karakteri yapma.", a, b)
}

func personasVersion() string { return personas.PromptVersion }
