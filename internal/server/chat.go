package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/MustafaKemal0146/joe/internal/analytics"
	"github.com/MustafaKemal0146/joe/internal/chat"
	"github.com/MustafaKemal0146/joe/internal/llm"
	"github.com/MustafaKemal0146/joe/internal/store"
)

// ChatModelFactory bir sağlayıcı/model için sohbet modeli üretir (testlerde sahtesiyle değiştirilir).
type ChatModelFactory func(provider, model string) (chat.Model, error)

// notReady sağlayıcı hazır değilken kullanılır: kısa komutlar yine çalışır, model gerektirenler açıklayıcı hata verir.
type notReady struct{}

func (notReady) Chat(context.Context, llm.Request) (llm.Message, llm.Usage, error) {
	return llm.Message{}, llm.Usage{}, errors.New("yapay zekâ sağlayıcısı hazır değil: Ayarlar'dan anahtar ekle. Kısa komutlar (kişiler, kaynaklar, harcama, harita) anahtarsız çalışır")
}

func (s *Server) chatEngine() *chat.Engine {
	cfg := s.Cfg.Get()
	name := cfg.ChatModel
	if name == "" {
		name = cfg.ActiveModel
	}
	var model chat.Model = notReady{}
	switch {
	case s.ChatModelFactory != nil:
		if m, err := s.ChatModelFactory(cfg.ActiveProvider, name); err == nil {
			model = m
		}
	case s.Cfg.Ready():
		if res, ok := s.Cfg.Resolve(cfg.ActiveProvider); ok {
			model = chat.TransportModel{T: llm.NewTransport(res.Provider.ID, res.BaseURL, res.APIKey)}
		}
	}
	return &chat.Engine{Store: s.Store, Model: model, Env: serverEnv{s}, ModelName: name, Provider: cfg.ActiveProvider, CapUSD: cfg.ChatBudgetUSD}
}

// serverEnv sohbet araçlarının uygulamaya eriştiği köprüdür. Döndürdükleri yalnızca özet metriktir.
type serverEnv struct{ s *Server }

func (e serverEnv) ListPeople() []map[string]any {
	ps, _ := e.s.Store.People()
	out := make([]map[string]any, 0, len(ps))
	for _, p := range ps {
		out = append(out, map[string]any{"id": p.ID, "name": p.Name, "is_self": p.IsSelf, "identities": len(p.Identities)})
	}
	return out
}

func (e serverEnv) ListSources() []map[string]any {
	ss, _ := e.s.Store.Sources()
	out := make([]map[string]any, 0, len(ss))
	for _, s := range ss {
		owner := s.OwnerName
		if owner == "" {
			owner = s.OwnerHandle
		}
		out = append(out, map[string]any{"id": s.ID, "platform": s.Platform, "owner": owner, "owner_confirmed": s.OwnerConfirmed, "messages": s.Stats["messages"]})
	}
	return out
}

func (e serverEnv) subject(id int64) (store.Person, *store.Corpus, analytics.Subject, error) {
	p, err := e.s.Store.Person(id)
	if err != nil {
		return p, nil, analytics.Subject{}, fmt.Errorf("#%d numaralı kişi bulunamadı", id)
	}
	c, err := e.s.loadCorpus()
	if err != nil {
		return p, nil, analytics.Subject{}, err
	}
	return p, c, analytics.NewSubject(p, c), nil
}

func (e serverEnv) ProfileSummary(id int64) (map[string]any, error) {
	p, c, subj, err := e.subject(id)
	if err != nil {
		return nil, err
	}
	pr := e.s.profileFor(p, c, subj)
	partners := []map[string]any{}
	for i, pt := range pr.Partners {
		if i >= 5 {
			break
		}
		partners = append(partners, map[string]any{"name": pt.Name, "platform": pt.Platform, "messages": pt.Total, "subject_share": pt.SubjectShare, "sessions": pt.Sessions, "longest_silence_days": int(pt.LongestSilenceDays)})
	}
	out := map[string]any{
		"name": p.Name, "messages": pr.Messages, "active_days": pr.Days, "night_ratio": pr.NightRatio,
		"median_reply_sec": pr.Latency.Median, "periods": len(pr.Periods), "groups": len(pr.Groups), "top_partners": partners,
	}
	if pr.SleepKnown {
		out["sleep_window"] = fmt.Sprintf("%02d:00–%02d:00", pr.SleepStart, (pr.SleepStart+6)%24)
	}
	if !pr.First.IsZero() {
		out["first"], out["last"] = pr.First.Format("2006-01-02"), pr.Last.Format("2006-01-02")
	}
	return out, nil
}

func (e serverEnv) Compare(a, b int64) (map[string]any, error) {
	_, c, sa, err := e.subject(a)
	if err != nil {
		return nil, err
	}
	_, _, sb, err := e.subject(b)
	if err != nil {
		return nil, err
	}
	cmp := analytics.Compare(c, sa, sb, e.s.opts())
	dims := []map[string]any{}
	for _, d := range cmp.Dimensions {
		dims = append(dims, map[string]any{"label": d.Label, "score": int(d.Score * 100), "detail": d.Detail})
	}
	return map[string]any{"a": cmp.A, "b": cmp.B, "overall": int(cmp.Overall * 100), "dimensions": dims, "missing": cmp.Missing, "note": cmp.Caveat}, nil
}

func (e serverEnv) SearchCount(q string) (map[string]any, error) {
	hits, err := e.s.Store.Search(q, 500)
	if err != nil {
		return nil, err
	}
	byConv := map[string]int{}
	for _, h := range hits {
		byConv[h.Title]++
	}
	top := []map[string]any{}
	for t, n := range byConv {
		top = append(top, map[string]any{"conversation": t, "count": n})
		if len(top) >= 5 {
			break
		}
	}
	return map[string]any{"count": len(hits), "capped_at_500": len(hits) == 500, "conversations": top}, nil
}

func (e serverEnv) CreatePerson(name string, ids []string) (string, error) {
	p, err := e.s.Store.CreatePerson(name, "", false, ids...)
	if err != nil {
		return "", err
	}
	e.s.dropProfiles()
	return fmt.Sprintf("«%s» için kişi dosyası açıldı (#%d).", p.Name, p.ID), nil
}

func (e serverEnv) AddIdentity(id int64, v string) (string, error) {
	if err := e.s.Store.AddIdentity(id, v); err != nil {
		return "", err
	}
	e.s.dropProfiles()
	return fmt.Sprintf("«%s» kimliği #%d numaralı kişiye eklendi.", v, id), nil
}

func (e serverEnv) SetOwner(id int64, name, handle string) (string, error) {
	if err := e.s.Store.SetOwner(id, name, handle); err != nil {
		return "", err
	}
	e.s.invalidate()
	if src, err := e.s.Store.Source(id); err == nil {
		_, _ = e.s.Store.EnsureOwnerPerson(src)
	}
	return fmt.Sprintf("#%d numaralı kaynağın sahibi «%s» olarak belirlendi.", id, name), nil
}

func (e serverEnv) AddNote(id int64, text, source string) (string, error) {
	if _, err := e.s.Store.AddNote(id, text, source); err != nil {
		return "", err
	}
	return "Not eklendi (kaynak: " + source + ").", nil
}

func (e serverEnv) DeleteSource(id int64) (string, error) {
	if err := e.s.Store.DeleteSource(id); err != nil {
		return "", err
	}
	e.s.invalidate()
	return fmt.Sprintf("#%d numaralı kaynak ve bağlı verisi silindi.", id), nil
}

// ---- Uç noktalar ----

func (s *Server) chatCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Title    string `json:"title"`
		PersonID int64  `json:"person_id"`
	}
	if !decode(w, r, &body) {
		return
	}
	if body.PersonID > 0 {
		if _, err := s.Store.Person(body.PersonID); err != nil {
			fail(w, 404, "kişi bulunamadı")
			return
		}
	}
	c, err := s.Store.CreateChat(newID(), body.Title, body.PersonID)
	if err != nil {
		serverErr(w, err)
		return
	}
	writeJSON(w, 201, c)
}

func (s *Server) chatList(w http.ResponseWriter, r *http.Request) {
	cs, err := s.Store.Chats(intQuery(r, "limit", 50, 1, 200))
	if err != nil {
		serverErr(w, err)
		return
	}
	writeJSON(w, 200, cs)
}

func (s *Server) chatDetail(w http.ResponseWriter, id string) (map[string]any, bool) {
	c, err := s.Store.Chat(id)
	if err != nil {
		fail(w, 404, "sohbet bulunamadı")
		return nil, false
	}
	msgs, err := s.Store.ChatMessages(id)
	if err != nil {
		serverErr(w, err)
		return nil, false
	}
	acts, err := s.Store.Actions(id)
	if err != nil {
		serverErr(w, err)
		return nil, false
	}
	return map[string]any{"session": c, "messages": msgs, "actions": acts, "cap_usd": s.Cfg.Get().ChatBudgetUSD}, true
}

func (s *Server) chatGet(w http.ResponseWriter, r *http.Request) {
	if d, ok := s.chatDetail(w, r.PathValue("id")); ok {
		writeJSON(w, 200, d)
	}
}

func (s *Server) chatPatch(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Title string `json:"title"`
	}
	if !decode(w, r, &body) {
		return
	}
	if err := s.Store.RenameChat(r.PathValue("id"), body.Title); err != nil {
		if errors.Is(err, store.ErrChatNotFound) {
			fail(w, 404, "sohbet bulunamadı")
			return
		}
		fail(w, 400, err.Error())
		return
	}
	if d, ok := s.chatDetail(w, r.PathValue("id")); ok {
		writeJSON(w, 200, d["session"])
	}
}

func (s *Server) chatDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.DeleteChat(r.PathValue("id")); err != nil {
		if errors.Is(err, store.ErrChatNotFound) {
			fail(w, 404, "sohbet bulunamadı")
			return
		}
		serverErr(w, err)
		return
	}
	w.WriteHeader(204)
}

func (s *Server) chatSend(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Text string `json:"text"`
	}
	if !decode(w, r, &body) {
		return
	}
	if _, err := s.Store.Chat(id); err != nil {
		fail(w, 404, "sohbet bulunamadı")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	added, err := s.chatEngine().Turn(ctx, id, body.Text)
	out := map[string]any{"messages": added}
	if err != nil {
		if len(added) == 0 { // doğrulama hatası: mesaj kaydedilmedi
			fail(w, 400, err.Error())
			return
		}
		n, _ := s.Store.AppendChat(store.ChatMessage{SessionID: id, Role: "notice", Content: "Hata: " + err.Error()})
		out["messages"] = append(added, n)
		out["error"] = err.Error()
	}
	if d, ok := s.chatDetail(w, id); ok {
		out["actions"] = d["actions"]
		out["session"] = d["session"]
	}
	writeJSON(w, 200, out)
}

func (s *Server) chatAction(confirm bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, aid := r.PathValue("id"), r.PathValue("aid")
		eng := s.chatEngine()
		var err error
		if confirm {
			_, err = eng.Confirm(r.Context(), id, aid)
		} else {
			err = eng.Reject(id, aid)
		}
		if err != nil {
			code := 409
			if strings.Contains(err.Error(), "bulunamadı") {
				code = 404
			}
			fail(w, code, err.Error())
			return
		}
		if d, ok := s.chatDetail(w, id); ok {
			writeJSON(w, 200, d)
		}
	}
}

// ---- Kişi notları ----

func (s *Server) notesList(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	if _, err := s.Store.Person(id); err != nil {
		fail(w, 404, "kişi bulunamadı")
		return
	}
	ns, err := s.Store.Notes(id)
	if err != nil {
		serverErr(w, err)
		return
	}
	writeJSON(w, 200, ns)
}

func (s *Server) notesAdd(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var body struct {
		Text string `json:"text"`
	}
	if !decode(w, r, &body) {
		return
	}
	n, err := s.Store.AddNote(id, body.Text, "user")
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			fail(w, 404, "kişi bulunamadı")
			return
		}
		fail(w, 400, err.Error())
		return
	}
	writeJSON(w, 201, n)
}

func (s *Server) notesDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	if err := s.Store.DeleteNote(id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			fail(w, 404, "not bulunamadı")
			return
		}
		serverErr(w, err)
		return
	}
	w.WriteHeader(204)
}
