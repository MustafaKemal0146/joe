package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/MustafaKemal0146/joe/internal/analytics"
	"github.com/MustafaKemal0146/joe/internal/budget"
	"github.com/MustafaKemal0146/joe/internal/config"
	"github.com/MustafaKemal0146/joe/internal/evidence"
	"github.com/MustafaKemal0146/joe/internal/ingest"
	"github.com/MustafaKemal0146/joe/internal/ingest/zipfs"
	"github.com/MustafaKemal0146/joe/internal/llm"
	"github.com/MustafaKemal0146/joe/internal/mask"
	"github.com/MustafaKemal0146/joe/internal/model"
	"github.com/MustafaKemal0146/joe/internal/personas"
	"github.com/MustafaKemal0146/joe/internal/report"
	"github.com/MustafaKemal0146/joe/internal/safety"
	"github.com/MustafaKemal0146/joe/internal/store"
	"github.com/MustafaKemal0146/joe/internal/version"
)

const maxUpload = 8 << 30

func (s *Server) state(w http.ResponseWriter, r *http.Request) {
	srcs, err := s.Store.Sources()
	if err != nil {
		serverErr(w, err)
		return
	}
	people, err := s.Store.People()
	if err != nil {
		serverErr(w, err)
		return
	}
	cfg := s.Cfg.Get()
	writeJSON(w, 200, map[string]any{
		"sources": len(srcs), "people": len(people),
		"provider_ready": s.Cfg.Ready(), "provider": cfg.ActiveProvider, "model": cfg.ActiveModel,
		"needs_owner": needsOwner(srcs),
	})
}

func needsOwner(srcs []store.Source) []int64 {
	out := []int64{}
	for _, s := range srcs {
		if !s.OwnerConfirmed {
			out = append(out, s.ID)
		}
	}
	return out
}

// ---- Ayarlar ----

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	cfg := s.Cfg.Get()
	models := map[string][]string{}
	for _, p := range config.Providers() {
		models[p.ID] = llm.KnownModels(p.ID)
	}
	type price struct {
		In  float64 `json:"in"`
		Out float64 `json:"out"`
	}
	prices := map[string]price{}
	for _, ms := range models {
		for _, m := range ms {
			if info, ok := llm.ModelMeta(m); ok && (info.CostPer1MIn > 0 || info.CostPer1MOut > 0) {
				prices[m] = price{info.CostPer1MIn, info.CostPer1MOut}
			}
		}
	}
	eu, em, ek, _ := s.Cfg.Embed()
	writeJSON(w, 200, map[string]any{
		"embed":     map[string]any{"url": eu, "model": em, "has_key": ek != "", "auto": s.Cfg.Get().EmbedAuto},
		"providers": s.Cfg.Statuses(), "active_provider": cfg.ActiveProvider, "active_model": cfg.ActiveModel, "strong_model": cfg.StrongModel, "strong_model_effective": cfg.EffectiveStrongModel(),
		"budget_usd": cfg.BudgetUSD, "chat_budget_usd": cfg.ChatBudgetUSD, "chat_model": cfg.ChatModel, "models": models, "prices": prices, "ready": s.Cfg.Ready(),
	})
}

func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) {
	var u config.Update
	if !decode(w, r, &u) {
		return
	}
	if err := s.Cfg.Apply(u); err != nil {
		fail(w, 400, err.Error())
		return
	}
	s.getSettings(w, r)
}

// testProvider etkin sağlayıcıya gerçek ve çok küçük bir istek gönderir (birkaç token).
func (s *Server) testProvider(w http.ResponseWriter, r *http.Request) {
	cfg := s.Cfg.Get()
	res, ok := s.Cfg.Resolve(cfg.ActiveProvider)
	if !ok || !s.Cfg.Ready() {
		fail(w, 400, "Sağlayıcı hazır değil: anahtar ve model gerekli.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
	defer cancel()
	t := llm.NewTransport(res.Provider.ID, res.BaseURL, res.APIKey)
	start := time.Now()
	text, usage, err := llm.Complete(ctx, t, llm.Request{
		Model: cfg.ActiveModel, MaxTokens: 16,
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "Yalnızca 'tamam' yaz."}},
	})
	if err != nil {
		var pe *llm.ProviderError
		msg := err.Error()
		if errors.As(err, &pe) && pe.IsUnauthorized() {
			msg = "Anahtar reddedildi (geçersiz ya da yetkisiz)."
		}
		writeJSON(w, 200, map[string]any{"ok": false, "error": msg})
		return
	}
	cost, known := llm.EstimateCost(cfg.ActiveModel, usage.PromptTokens, usage.CompletionTokens)
	out := map[string]any{"ok": true, "reply": strings.TrimSpace(text), "ms": time.Since(start).Milliseconds(), "tokens": usage.Total()}
	if known {
		out["cost_usd"] = cost
	}
	writeJSON(w, 200, out)
}

// ---- Maliyet ----

type estimateRequest struct {
	Model               string `json:"model"`
	PrefixText          string `json:"prefix_text"`
	SharedPrefixTokens  int    `json:"shared_prefix_tokens"`
	PerCallTokens       int    `json:"per_call_tokens"`
	Calls               int    `json:"calls"`
	OutputTokensPerCall int    `json:"output_tokens_per_call"`
	Session             string `json:"session"`
}

// estimate bir çalışmanın maliyetini önceden hesaplar. Tavana sığıyorsa tek kullanımlık bir onay
// kimliği döndürür; pahalı işler (konsey) bu kimlik olmadan başlatılamaz.
func (s *Server) estimate(w http.ResponseWriter, r *http.Request) {
	var req estimateRequest
	if !decode(w, r, &req) {
		return
	}
	cfg := s.Cfg.Get()
	if req.Model == "" {
		req.Model = cfg.ActiveModel
	}
	if req.Calls < 1 || req.Calls > 200 {
		fail(w, 400, "çağrı sayısı 1 ile 200 arasında olmalı")
		return
	}
	if req.SharedPrefixTokens < 0 || req.PerCallTokens < 0 || req.OutputTokensPerCall < 0 || req.OutputTokensPerCall > 200_000 {
		fail(w, 400, "token değerleri geçersiz")
		return
	}
	prefix := req.SharedPrefixTokens
	if req.PrefixText != "" {
		prefix = budget.EstimateTokens(req.PrefixText)
	}
	est := budget.Compute(budget.Plan{Model: req.Model, SharedPrefixTokens: prefix, PerCallTokens: req.PerCallTokens, Calls: req.Calls, OutputTokensPerCall: req.OutputTokensPerCall})
	var spent float64
	if req.Session != "" {
		spent, _ = s.Store.SessionSpend(req.Session)
	}
	guard := budget.Guard{CapUSD: cfg.BudgetUSD}
	out := map[string]any{"estimate": est, "cap_usd": cfg.BudgetUSD, "spent_usd": spent, "remaining_usd": guard.Remaining(spent)}
	switch {
	case !est.Known:
		out["allowed"], out["reason"] = false, "Modelin fiyatı bilinmediği için maliyet doğrulanamıyor; onay verilmedi."
	default:
		if err := guard.Allow(spent, est.HighUSD); err != nil {
			out["allowed"], out["reason"] = false, err.Error()
		} else {
			if s.Approvals == nil {
				s.Approvals = budget.NewApprovals()
			}
			ap := s.Approvals.Grant(est, cfg.BudgetUSD, "")
			out["allowed"], out["approval_id"], out["expires_at"] = true, ap.ID, ap.ExpiresAt
		}
	}
	writeJSON(w, 200, out)
}

func (s *Server) personas(w http.ResponseWriter, r *http.Request) {
	ps, err := personas.Load(s.PersonaDir)
	if err != nil {
		fail(w, 500, "persona dosyaları okunamadı: "+err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"prompt_version": personas.PromptVersion, "personas": ps, "moderator_question": ModeratorQuestion})
}

func (s *Server) ledger(w http.ResponseWriter, r *http.Request) {
	sum, err := s.Store.Spend()
	if err != nil {
		serverErr(w, err)
		return
	}
	writeJSON(w, 200, sum)
}

// ---- Kaynaklar ----

func (s *Server) listSources(w http.ResponseWriter, r *http.Request) {
	srcs, err := s.Store.Sources()
	if err != nil {
		serverErr(w, err)
		return
	}
	writeJSON(w, 200, srcs)
}

type importResult struct {
	Source      store.Source  `json:"source"`
	Duplicate   bool          `json:"duplicate"`
	OwnerNeeded bool          `json:"owner_needed"`
	Person      *store.Person `json:"person,omitempty"`
	// Summary içe aktarılan dosyanın özetidir (sohbet içi kart için); Duplicate ise yinelenen dosyanın özetidir.
	Summary importSummary `json:"summary"`
	// Partners bu içe aktarmada kendiliğinden açılan karşı taraf kişi dosyalarıdır (WhatsApp gibi tek sohbetlerde).
	Partners []store.Person `json:"partners"`
}

type importSummary struct {
	Conversations int       `json:"conversations"`
	Messages      int       `json:"messages"`
	First         time.Time `json:"first"`
	Last          time.Time `json:"last"`
	Warnings      []string  `json:"warnings"`
	Titles        []string  `json:"titles"`
	Participants  []string  `json:"participants"`
}

func summarize(imp *model.Import) importSummary {
	sum := importSummary{Conversations: len(imp.Conversations), Warnings: imp.Warnings}
	if sum.Warnings == nil {
		sum.Warnings = []string{}
	}
	for _, c := range imp.Conversations {
		sum.Messages += len(c.Messages)
		if len(sum.Titles) < 5 && c.Title != "" {
			sum.Titles = append(sum.Titles, c.Title)
		}
		for _, pn := range c.Participants {
			if len(sum.Participants) < 8 && !slices.Contains(sum.Participants, pn) {
				sum.Participants = append(sum.Participants, pn)
			}
		}
		for _, m := range c.Messages {
			if m.TS.IsZero() {
				continue
			}
			if sum.First.IsZero() || m.TS.Before(sum.First) {
				sum.First = m.TS
			}
			if m.TS.After(sum.Last) {
				sum.Last = m.TS
			}
		}
	}
	if sum.Titles == nil {
		sum.Titles = []string{}
	}
	if sum.Participants == nil {
		sum.Participants = []string{}
	}
	return sum
}

// importSource iki biçim kabul eder: multipart "file" yüklemesi (birden çok parça olabilir)
// ya da JSON {"path": "..."} / {"paths": ["...", "..."]}.
func (s *Server) importSource(w http.ResponseWriter, r *http.Request) {
	var paths []string
	ct := r.Header.Get("Content-Type")
	switch {
	case strings.HasPrefix(ct, "multipart/form-data"):
		p, cleanup, err := s.receiveUploads(w, r)
		if err != nil {
			fail(w, 400, err.Error())
			return
		}
		defer cleanup()
		paths = p
	case strings.HasPrefix(ct, "application/json"):
		var body struct {
			Path  string   `json:"path"`
			Paths []string `json:"paths"`
		}
		if !decode(w, r, &body) {
			return
		}
		if strings.TrimSpace(body.Path) != "" {
			paths = append(paths, strings.TrimSpace(body.Path))
		}
		for _, p := range body.Paths {
			if strings.TrimSpace(p) != "" {
				paths = append(paths, strings.TrimSpace(p))
			}
		}
		if len(paths) == 0 {
			fail(w, 400, "yol boş")
			return
		}
		if len(paths) > 64 {
			fail(w, 400, "en çok 64 parça birleştirilebilir")
			return
		}
	default:
		fail(w, 415, "multipart/form-data ya da application/json bekleniyor")
		return
	}
	src, err := zipfs.OpenMulti(paths)
	if err != nil {
		fail(w, 400, "Kaynak açılamadı: "+err.Error())
		return
	}
	defer src.Close()
	imp, err := ingest.Parse(src, s.Loc)
	if err != nil {
		fail(w, 422, err.Error())
		return
	}
	id, err := s.Store.SaveImport(imp, src.SHA256)
	res := importResult{Summary: summarize(imp)}
	var dup store.ErrDuplicate
	switch {
	case errors.As(err, &dup):
		res.Duplicate = true
		id = dup.SourceID
	case err != nil:
		serverErr(w, err)
		return
	}
	s.invalidate()
	if !res.Duplicate {
		s.maybeAutoIndex()
	}
	row, err := s.Store.Source(id)
	if err != nil {
		serverErr(w, err)
		return
	}
	res.Source = row
	res.OwnerNeeded = row.OwnerName == "" && row.OwnerHandle == "" || !row.OwnerConfirmed
	// Yalnızca onaylı sahip için kişi dosyası açılır; tahmin yanlış kişi üretmesin.
	if row.OwnerConfirmed && !res.Duplicate {
		if p, err := s.Store.EnsureOwnerPerson(row); err == nil {
			res.Person = &p
		}
	}
	res.Partners = s.ensurePartners(row)
	if res.Partners == nil {
		res.Partners = []store.Person{}
	}
	code := 201
	if res.Duplicate {
		code = 200
	}
	writeJSON(w, code, res)
}

// receiveUploads multipart gövdedeki tüm "file" parçalarını geçici klasöre yazar.
func (s *Server) receiveUploads(w http.ResponseWriter, r *http.Request) ([]string, func(), error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload)
	mr, err := r.MultipartReader()
	if err != nil {
		return nil, nil, err
	}
	dir, err := os.MkdirTemp(s.TempDir, "upload-")
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() { os.RemoveAll(dir) }
	var out []string
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			cleanup()
			return nil, nil, err
		}
		if part.FormName() != "file" {
			continue
		}
		if len(out) >= 64 {
			cleanup()
			return nil, nil, errors.New("en çok 64 dosya yüklenebilir")
		}
		name := filepath.Base(strings.ReplaceAll(part.FileName(), "\\", "/"))
		if name == "." || name == "/" || name == "" {
			name = "upload"
		}
		// Aynı adlı iki dosya üst üste yazılmasın; özgün ad (parça numarası için) korunur.
		sub := filepath.Join(dir, fmt.Sprintf("%02d", len(out)))
		if err := os.Mkdir(sub, 0o700); err != nil {
			cleanup()
			return nil, nil, err
		}
		dst, err := os.OpenFile(filepath.Join(sub, name), os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
		if err != nil {
			cleanup()
			return nil, nil, err
		}
		_, err = io.Copy(dst, part)
		dst.Close()
		if err != nil {
			cleanup()
			return nil, nil, err
		}
		out = append(out, dst.Name())
	}
	if len(out) == 0 {
		cleanup()
		return nil, nil, errors.New("'file' alanı bulunamadı")
	}
	return out, cleanup, nil
}

func (s *Server) deleteSource(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	if err := s.Store.DeleteSource(id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			fail(w, 404, "kaynak bulunamadı")
			return
		}
		serverErr(w, err)
		return
	}
	s.invalidate()
	w.WriteHeader(204)
}

func (s *Server) setOwner(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var body struct {
		Name   string `json:"name"`
		Handle string `json:"handle"`
	}
	if !decode(w, r, &body) {
		return
	}
	if err := s.Store.SetOwner(id, body.Name, body.Handle); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			fail(w, 404, "kaynak bulunamadı")
			return
		}
		fail(w, 400, err.Error())
		return
	}
	s.invalidate()
	row, _ := s.Store.Source(id)
	res := importResult{Source: row}
	if p, err := s.Store.EnsureOwnerPerson(row); err == nil {
		res.Person = &p
	}
	res.Partners = s.ensurePartners(row)
	if res.Partners == nil {
		res.Partners = []store.Person{}
	}
	writeJSON(w, 200, res)
}

type contact struct {
	Name     string    `json:"name"`
	ConvID   int64     `json:"conv_id"`
	IsGroup  bool      `json:"is_group"`
	Count    int       `json:"count"`
	First    time.Time `json:"first"`
	Last     time.Time `json:"last"`
	PersonID int64     `json:"person_id,omitempty"`
}

// contacts bir kaynaktaki sohbet karşılarını döndürür; kişi dosyasına bağlıysa PersonID dolar.
func (s *Server) contacts(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	src, err := s.Store.Source(id)
	if err != nil {
		fail(w, 404, "kaynak bulunamadı")
		return
	}
	convs, err := s.Store.Conversations(id)
	if err != nil {
		serverErr(w, err)
		return
	}
	people, _ := s.Store.People()
	byNorm := map[string]int64{}
	for _, p := range people {
		for _, i := range p.Identities {
			byNorm[i.Norm] = p.ID
		}
	}
	ownerN := map[string]bool{model.Norm(src.OwnerName): true, model.Norm(src.OwnerHandle): true}
	out := []contact{}
	for _, c := range convs {
		name := c.Title
		if !c.IsGroup {
			for _, p := range c.Participants {
				if !ownerN[model.Norm(p)] {
					name = p
					break
				}
			}
		}
		ct := contact{Name: name, ConvID: c.ID, IsGroup: c.IsGroup, Count: c.Count, First: c.First, Last: c.Last}
		if !c.IsGroup {
			ct.PersonID = byNorm[model.Norm(name)]
		}
		out = append(out, ct)
	}
	writeJSON(w, 200, out)
}

func (s *Server) conversations(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	convs, err := s.Store.Conversations(id)
	if err != nil {
		serverErr(w, err)
		return
	}
	writeJSON(w, 200, convs)
}

type messageView struct {
	ID        int64            `json:"id"`
	Sender    string           `json:"sender"`
	TS        time.Time        `json:"ts"`
	Kind      string           `json:"kind"`
	Text      string           `json:"text"`
	Reactions []model.Reaction `json:"reactions,omitempty"`
	Meta      map[string]any   `json:"meta,omitempty"`
}

func view(m store.MessageRow) messageView {
	return messageView{m.ID, m.Sender, m.TS, m.Kind, m.Text, m.Reactions, m.Meta}
}

func intQuery(r *http.Request, key string, def, min, max int) int {
	v, err := strconv.Atoi(r.URL.Query().Get(key))
	if err != nil || v < min {
		return def
	}
	if v > max {
		return max
	}
	return v
}

func (s *Server) messages(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	limit, offset := intQuery(r, "limit", 200, 1, 1000), intQuery(r, "offset", 0, 0, 1<<30)
	// around=<mesaj kimliği>: o mesajı ortalayan bir pencere döndürür (sohbetin derinindeki bir ana atlamak için).
	// Kullanılan başlangıç sırası X-Offset başlığında verilir.
	if a := intQuery(r, "around", 0, 0, 1<<30); a > 0 {
		if idx, ok := s.Store.MessageIndex(id, int64(a)); ok {
			offset = max(0, idx-limit/2)
		}
	}
	w.Header().Set("X-Offset", strconv.Itoa(offset))
	rows, err := s.Store.Messages(id, limit, offset)
	if err != nil {
		serverErr(w, err)
		return
	}
	out := make([]messageView, len(rows))
	for i, m := range rows {
		out[i] = view(m)
	}
	writeJSON(w, 200, out)
}

// ---- Kişiler ----

func (s *Server) listPeople(w http.ResponseWriter, r *http.Request) {
	ps, err := s.Store.People()
	if err != nil {
		serverErr(w, err)
		return
	}
	writeJSON(w, 200, ps)
}

func (s *Server) createPerson(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name       string   `json:"name"`
		Note       string   `json:"note"`
		Identities []string `json:"identities"`
	}
	if !decode(w, r, &body) {
		return
	}
	p, err := s.Store.CreatePerson(body.Name, body.Note, false, body.Identities...)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	s.dropProfiles()
	writeJSON(w, 201, p)
}

func (s *Server) getPerson(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	p, err := s.Store.Person(id)
	if err != nil {
		fail(w, 404, "kişi bulunamadı")
		return
	}
	writeJSON(w, 200, p)
}

func (s *Server) patchPerson(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var body struct {
		Name   *string `json:"name"`
		Note   *string `json:"note"`
		IsSelf *bool   `json:"is_self"`
	}
	if !decode(w, r, &body) {
		return
	}
	if err := s.Store.UpdatePerson(id, body.Name, body.Note, body.IsSelf); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			fail(w, 404, "kişi bulunamadı")
			return
		}
		fail(w, 400, err.Error())
		return
	}
	s.dropProfiles()
	p, _ := s.Store.Person(id)
	writeJSON(w, 200, p)
}

func (s *Server) deletePerson(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	// Kişi dosyası silinince adı ve kimlikleri haritada yalın düğüm olarak yeniden belirmesin diye
	// bu düğümler gizlenir (mesaj verisi silinmez; haritadaki "Gizlenenler"den geri getirilebilir).
	var toHide []store.HiddenNode
	if p, err := s.Store.Person(id); err == nil && !p.IsSelf {
		for _, idn := range p.Identities {
			if idn.Norm != "" {
				toHide = append(toHide, store.HiddenNode{Key: "n" + idn.Norm, Label: p.Name})
			}
		}
	}
	if err := s.Store.DeletePerson(id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			fail(w, 404, "kişi bulunamadı")
			return
		}
		serverErr(w, err)
		return
	}
	for _, h := range toHide {
		_ = s.Store.HideNode(h.Key, h.Label)
	}
	s.dropProfiles()
	w.WriteHeader(204)
}

func (s *Server) addIdentity(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var body struct {
		Value string `json:"value"`
	}
	if !decode(w, r, &body) {
		return
	}
	if err := s.Store.AddIdentity(id, body.Value); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			fail(w, 404, "kişi bulunamadı")
			return
		}
		fail(w, 400, err.Error())
		return
	}
	s.dropProfiles()
	p, _ := s.Store.Person(id)
	writeJSON(w, 200, p)
}

func (s *Server) removeIdentity(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	iid, ok := pathID(w, r, "iid")
	if !ok {
		return
	}
	if err := s.Store.RemoveIdentity(id, iid); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			fail(w, 404, "kimlik bulunamadı")
			return
		}
		fail(w, 400, err.Error())
		return
	}
	s.dropProfiles()
	p, _ := s.Store.Person(id)
	writeJSON(w, 200, p)
}

func (s *Server) dropProfiles() {
	s.mu.Lock()
	s.profile = nil
	s.mu.Unlock()
}

func identKey(p store.Person) string {
	ns := make([]string, len(p.Identities))
	for i, id := range p.Identities {
		ns[i] = id.Norm
	}
	sort.Strings(ns)
	return strconv.FormatInt(p.ID, 10) + "|" + strings.Join(ns, ",")
}

// subject kişiyi ve veri yığınını hazırlar.
func (s *Server) subject(w http.ResponseWriter, r *http.Request, name string) (store.Person, *store.Corpus, analytics.Subject, bool) {
	id, ok := pathID(w, r, name)
	if !ok {
		return store.Person{}, nil, analytics.Subject{}, false
	}
	return s.subjectByID(w, id)
}

func (s *Server) subjectByID(w http.ResponseWriter, id int64) (store.Person, *store.Corpus, analytics.Subject, bool) {
	p, err := s.Store.Person(id)
	if err != nil {
		fail(w, 404, "kişi bulunamadı")
		return p, nil, analytics.Subject{}, false
	}
	c, err := s.loadCorpus()
	if err != nil {
		serverErr(w, err)
		return p, nil, analytics.Subject{}, false
	}
	return p, c, analytics.NewSubject(p, c), true
}

func (s *Server) profileFor(p store.Person, c *store.Corpus, subj analytics.Subject) analytics.Profile {
	key := identKey(p)
	s.mu.Lock()
	if s.profile != nil {
		if pr, ok := s.profile[key]; ok {
			s.mu.Unlock()
			return pr
		}
	}
	s.mu.Unlock()
	pr := analytics.BuildProfile(c, subj, s.opts())
	s.mu.Lock()
	if s.profile != nil && s.corpus == c {
		s.profile[key] = pr
	}
	s.mu.Unlock()
	return pr
}

func (s *Server) profileOf(w http.ResponseWriter, r *http.Request) {
	p, c, subj, ok := s.subject(w, r, "id")
	if !ok {
		return
	}
	writeJSON(w, 200, s.profileFor(p, c, subj))
}

func (s *Server) timelineOf(w http.ResponseWriter, r *http.Request) {
	p, c, subj, ok := s.subject(w, r, "id")
	if !ok {
		return
	}
	writeJSON(w, 200, analytics.BuildTimeline(c, subj, s.profileFor(p, c, subj), s.Loc))
}

func (s *Server) reportOf(w http.ResponseWriter, r *http.Request) {
	in, ok := s.reportInput(w, r)
	if !ok {
		return
	}
	p := in.Person
	md := report.Markdown(in)
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="joe-rapor-%d.md"`, p.ID))
	w.Header().Set("Cache-Control", "no-store")
	io.WriteString(w, md)
}

// evidenceOf kişinin kanıt paketini kurar ve konseye gidecek (maskelenmiş) metnin önizlemesini
// döndürür. Kesitler yerel görüntüleme için ham metinle, "preview" ise sağlayıcıya gidecek
// maskelenmiş haliyle verilir. Kullanıcı `exclude` ile kesit başlıklarını sepetten çıkarabilir.
func (s *Server) evidenceOf(w http.ResponseWriter, r *http.Request) {
	p, c, subj, ok := s.subject(w, r, "id")
	if !ok {
		return
	}
	exclude := map[string]bool{}
	for _, t := range r.URL.Query()["exclude"] {
		exclude[t] = true
	}
	pr := s.profileFor(p, c, subj)
	pack := evidence.Build(c, subj, pr, evidence.Options{
		MaxTokens: intQuery(r, "max_tokens", evidence.DefaultMaxTokens, 1000, 200000), Loc: s.Loc, ExcludeTitles: exclude,
	})
	m := mask.New()
	preview := pack.Render(m)
	report := map[string]int{}
	for k, v := range m.Report() {
		report[string(k)] = v
	}
	writeJSON(w, 200, map[string]any{
		"subject": pack.Subject, "items": pack.Items, "metrics": pack.Metrics, "skipped": pack.Skipped,
		"tokens": budget.EstimateTokens(preview), "max_tokens": pack.MaxTokens,
		"hash": pack.Hash(mask.New()), "preview": preview, "mask_report": report,
	})
}

type riskItem struct {
	ID         int64     `json:"id"`
	ConvID     int64     `json:"conv_id"`
	TS         time.Time `json:"ts"`
	Level      string    `json:"level"`
	Categories []string  `json:"categories"`
}

// riskOf kişinin kendi yazdığı mesajlarda kendine zarar/şiddet ifadesi içerenleri bulur. Mesaj
// metni döndürülmez, yalnızca konumu: kullanıcı bağlamı sohbette kendisi okur. Bu bir
// değerlendirme ya da tanı değildir; yalnızca "dikkatle oku" işaretidir.
func (s *Server) riskOf(w http.ResponseWriter, r *http.Request) {
	_, c, subj, ok := s.subject(w, r, "id")
	if !ok {
		return
	}
	out := struct {
		Concern  int        `json:"concern"`
		Acute    int        `json:"acute"`
		Messages []riskItem `json:"messages"`
		Note     string     `json:"note"`
	}{Messages: []riskItem{}}
	worst := safety.Result{}
	for _, m := range subj.Msgs(c) {
		if m.Text == "" {
			continue
		}
		res := safety.Detect(m.Text)
		if res.Level == safety.None {
			continue
		}
		if res.Level == safety.Acute {
			out.Acute++
		} else {
			out.Concern++
		}
		if res.Level > worst.Level {
			worst = res
		}
		if len(out.Messages) < 50 {
			out.Messages = append(out.Messages, riskItem{m.ID, m.ConvID, m.TS, res.Level.String(), res.Categories})
		}
	}
	out.Note = safety.AnalysisNote(worst)
	writeJSON(w, 200, out)
}

func (s *Server) reportInput(w http.ResponseWriter, r *http.Request) (report.Input, bool) {
	p, c, subj, ok := s.subject(w, r, "id")
	if !ok {
		return report.Input{}, false
	}
	pr := s.profileFor(p, c, subj)
	in := report.Input{Person: p, Profile: pr, Timeline: analytics.BuildTimeline(c, subj, pr, s.Loc), GeneratedAt: time.Now(), Version: version.Version, Loc: s.Loc}
	// Varsayılan olarak kişinin en güncel iki konsey oturumu eklenir; ?konsey=0 yalnızca metrikleri verir.
	if r.URL.Query().Get("konsey") != "0" {
		in.Councils = s.reportCouncils(p.ID, 2)
	}
	return in, true
}

// reportCouncils kişinin sentezi tamamlanmış en güncel tek/çift kişilik konsey oturumlarını toplar.
func (s *Server) reportCouncils(personID int64, n int) []report.CouncilInput {
	list, err := s.Store.Councils(200)
	if err != nil {
		return nil
	}
	ps, _ := personas.Load(s.PersonaDir)
	var out []report.CouncilInput
	for _, c := range list {
		if len(out) >= n {
			break
		}
		if (c.Kind != "single" && c.Kind != "pair") || !slices.Contains(c.SubjectIDs, personID) {
			continue
		}
		outs, err := s.Store.Outputs(c.ID)
		if err != nil || !hasSynthesis(outs) {
			continue
		}
		full, err := s.Store.Council(c.ID) // paket (kanıt listesi) tam kayıtta
		if err != nil {
			continue
		}
		var names []string
		for _, pid := range full.SubjectIDs {
			if p, err := s.Store.Person(pid); err == nil {
				names = append(names, p.Name)
			}
		}
		out = append(out, report.CouncilInput{Session: full, Outputs: outs, Subjects: names, Personas: ps, Version: version.Version, Loc: s.Loc})
	}
	return out
}

func hasSynthesis(outs []store.CouncilOutput) bool {
	for _, o := range outs {
		if o.Stage == "synthesis" && o.Status == store.OutDone && len(o.Output) > 0 {
			return true
		}
	}
	return false
}

// reportHTML raporu yazdırmaya hazır HTML olarak verir; PDF, tarayıcının "PDF olarak kaydet"iyle alınır.
func (s *Server) reportHTML(w http.ResponseWriter, r *http.Request) {
	in, ok := s.reportInput(w, r)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	io.WriteString(w, report.HTML(in))
}

func (s *Server) reportPrintJS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	io.WriteString(w, report.PrintJS)
}

// hiddenSet kullanıcının haritadan gizlediği düğüm anahtarlarıdır.
func (s *Server) hiddenSet() (map[string]bool, error) {
	hs, err := s.Store.HiddenNodes()
	if err != nil {
		return nil, err
	}
	m := make(map[string]bool, len(hs))
	for _, h := range hs {
		m[h.Key] = true
	}
	return m, nil
}

func (s *Server) graph(w http.ResponseWriter, r *http.Request) {
	c, err := s.loadCorpus()
	if err != nil {
		serverErr(w, err)
		return
	}
	hidden, err := s.hiddenSet()
	if err != nil {
		serverErr(w, err)
		return
	}
	people, _ := s.Store.People()
	g := analytics.BuildGraphHidden(c, people, intQuery(r, "max", 60, 5, 200), hidden)
	hs, _ := s.Store.HiddenNodes()
	writeJSON(w, 200, map[string]any{"nodes": g.Nodes, "edges": g.Edges, "total_nodes": g.Total, "hidden": hs})
}

// graphHide düğümü haritadan gizler; veri silinmez. Hesap sahibi düğümleri gizlenemez.
func (s *Server) graphHide(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID    string `json:"id"`
		Label string `json:"label"`
	}
	if !decode(w, r, &body) {
		return
	}
	if len(body.ID) < 2 || (body.ID[0] != 'p' && body.ID[0] != 'n') || len(body.ID) > 300 {
		fail(w, 400, "geçersiz düğüm")
		return
	}
	c, err := s.loadCorpus()
	if err != nil {
		serverErr(w, err)
		return
	}
	people, _ := s.Store.People()
	for _, n := range analytics.BuildGraph(c, people, 0).Nodes {
		if n.ID == body.ID && n.IsOwner {
			fail(w, 400, "Hesap sahibi haritadan gizlenemez; haritanın merkezidir.")
			return
		}
	}
	label := strings.TrimSpace(body.Label)
	if label == "" {
		label = body.ID
	}
	if err := s.Store.HideNode(body.ID, label); err != nil {
		serverErr(w, err)
		return
	}
	w.WriteHeader(204)
}

func (s *Server) graphUnhide(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	if !decode(w, r, &body) {
		return
	}
	if err := s.Store.UnhideNode(body.ID); err != nil {
		serverErr(w, err)
		return
	}
	w.WriteHeader(204)
}

// networkOf bir kişinin çevresini (ego ağı) döndürür: bağlantılar, etkileşim türleri ve yakın/geniş çevre.
func (s *Server) networkOf(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	if _, err := s.Store.Person(id); err != nil {
		fail(w, 404, "kişi bulunamadı")
		return
	}
	c, err := s.loadCorpus()
	if err != nil {
		serverErr(w, err)
		return
	}
	hidden, err := s.hiddenSet()
	if err != nil {
		serverErr(w, err)
		return
	}
	people, _ := s.Store.People()
	writeJSON(w, 200, analytics.EgoNetwork(c, people, id, hidden, intQuery(r, "max", 40, 5, 150)))
}

func (s *Server) suggestions(w http.ResponseWriter, r *http.Request) {
	c, err := s.loadCorpus()
	if err != nil {
		serverErr(w, err)
		return
	}
	people, _ := s.Store.People()
	writeJSON(w, 200, analytics.Suggest(c, people, 0.6))
}

func (s *Server) compare(w http.ResponseWriter, r *http.Request) {
	a, errA := strconv.ParseInt(r.URL.Query().Get("a"), 10, 64)
	b, errB := strconv.ParseInt(r.URL.Query().Get("b"), 10, 64)
	if errA != nil || errB != nil || a <= 0 || b <= 0 || a == b {
		fail(w, 400, "iki farklı kişi seçilmeli (a, b)")
		return
	}
	_, c, sa, ok := s.subjectByID(w, a)
	if !ok {
		return
	}
	_, _, sb, ok := s.subjectByID(w, b)
	if !ok {
		return
	}
	writeJSON(w, 200, analytics.Compare(c, sa, sb, s.opts()))
}

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	if mode := r.URL.Query().Get("mode"); mode == "semantic" || mode == "hybrid" {
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		if q == "" {
			writeJSON(w, 200, []searchHit{})
			return
		}
		s.semanticSearch(w, r, q, mode, intQuery(r, "limit", 30, 1, 100))
		return
	}
	hits, err := s.Store.Search(r.URL.Query().Get("q"), intQuery(r, "limit", 50, 1, 200))
	if err != nil {
		serverErr(w, err)
		return
	}
	type hit struct {
		messageView
		ConvID   int64  `json:"conv_id"`
		SourceID int64  `json:"source_id"`
		Title    string `json:"title"`
		Snippet  string `json:"snippet"`
	}
	out := make([]hit, len(hits))
	for i, h := range hits {
		out[i] = hit{view(h.MessageRow), h.ConvID, h.SourceID, h.Title, h.Snippet}
	}
	writeJSON(w, 200, out)
}
