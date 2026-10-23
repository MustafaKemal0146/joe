package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/MustafaKemal0146/joe/internal/cluster"
	"github.com/MustafaKemal0146/joe/internal/config"
	"github.com/MustafaKemal0146/joe/internal/embed"
	"github.com/MustafaKemal0146/joe/internal/store"
)

type embedJob struct {
	mu       sync.Mutex
	running  bool
	cancel   context.CancelFunc
	progress embed.Progress
	err      string
	wg       sync.WaitGroup

	// vektör dizini önbelleği; gömülü mesaj sayısı değişince yenilenir
	vecModel string
	vec      *store.VectorIndex
}

func (s *Server) embedClient() (*embed.Client, bool) {
	url, model, key, ok := s.Cfg.Embed()
	if !ok {
		return nil, false
	}
	return embed.New(url, key, model), true
}

func (s *Server) embedStatus(w http.ResponseWriter, r *http.Request) {
	c, ok := s.embedClient()
	out := map[string]any{"configured": ok}
	if ok {
		total, done, err := s.Store.EmbedStats(c.Model)
		if err != nil {
			serverErr(w, err)
			return
		}
		s.embed.mu.Lock()
		out["running"], out["error"] = s.embed.running, s.embed.err
		s.embed.mu.Unlock()
		out["total"], out["done"], out["model"], out["url"] = total, done, c.Model, c.BaseURL
	}
	writeJSON(w, 200, out)
}

// startEmbedJob dizinleme işini arka planda başlatır; zaten çalışıyorsa ya da ayar yoksa hata döner.
func (s *Server) startEmbedJob() (int, error) {
	c, ok := s.embedClient()
	if !ok {
		return 400, errors.New("Gömme sunucusu ayarlı değil: Ayarlar'dan adres ve model gir.")
	}
	s.embed.mu.Lock()
	if s.embed.running {
		s.embed.mu.Unlock()
		return 409, errors.New("Dizinleme zaten çalışıyor.")
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.embed.running, s.embed.cancel, s.embed.err = true, cancel, ""
	s.embed.wg.Add(1)
	s.embed.mu.Unlock()
	go func() {
		defer s.embed.wg.Done()
		err := embed.Index(ctx, s.Store, c, 64, func(p embed.Progress) {
			s.embed.mu.Lock()
			s.embed.progress = p
			s.embed.mu.Unlock()
		})
		s.embed.mu.Lock()
		s.embed.running = false
		if err != nil && ctx.Err() == nil {
			s.embed.err = err.Error()
		}
		s.embed.mu.Unlock()
		cancel()
	}()
	return 202, nil
}

func (s *Server) embedRun(w http.ResponseWriter, r *http.Request) {
	if code, err := s.startEmbedJob(); err != nil {
		fail(w, code, err.Error())
		return
	}
	w.WriteHeader(202)
}

// maybeAutoIndex yeni içe aktarmadan sonra, kullanıcı otomatik dizinlemeyi açtıysa ve gömme adresi
// yerelse dizinlemeyi başlatır.
func (s *Server) maybeAutoIndex() {
	cfg := s.Cfg.Get()
	if !cfg.EmbedAuto || cfg.EmbedURL == "" || !embed.IsLoopbackURL(cfg.EmbedURL) {
		return
	}
	_, _ = s.startEmbedJob()
}

// embedDiscover yerel Ollama'yı tarayıp uygun gömme modelini önerir (kaydetmez).
func (s *Server) embedDiscover(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	target := r.URL.Query().Get("url")
	if target == "" {
		target = s.OllamaURL
	}
	d, err := embed.Discover(ctx, target)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, d)
}

// embedAuto keşfi yapar, doğrulanmış en iyi modeli ayar olarak kaydeder, otomatik dizinlemeyi açar ve
// dizinlemeyi başlatır: tek tıkla kurulum.
func (s *Server) embedAuto(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	d, err := embed.Discover(ctx, s.OllamaURL)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	if !d.Reachable || d.Recommended == "" || !d.Verified {
		msg := d.Hint
		if msg == "" {
			msg = "Uygun bir gömme modeli bulunamadı."
		}
		fail(w, 409, msg)
		return
	}
	on := true
	if err := s.Cfg.Apply(config.Update{EmbedURL: &d.OpenAIURL, EmbedModel: &d.Recommended, EmbedAuto: &on}); err != nil {
		fail(w, 400, err.Error())
		return
	}
	started := false
	if code, err := s.startEmbedJob(); err == nil && code == 202 {
		started = true
	}
	writeJSON(w, 200, map[string]any{"model": d.Recommended, "url": d.OpenAIURL, "dim": d.Dim, "indexing_started": started})
}
func (s *Server) embedCancel(w http.ResponseWriter, r *http.Request) {
	s.embed.mu.Lock()
	cancel, running := s.embed.cancel, s.embed.running
	s.embed.mu.Unlock()
	if !running {
		fail(w, 409, "Dizinleme çalışmıyor.")
		return
	}
	cancel()
	w.WriteHeader(204)
}

// WaitEmbed dizinleme işinin bitmesini bekler (testler ve kapanış için).
func (s *Server) WaitEmbed() { s.embed.wg.Wait() }

// vectors güncel vektör dizinini döndürür; gömülü sayı değişmediyse önbellektekini kullanır.
func (s *Server) vectors(model string) (*store.VectorIndex, error) {
	_, done, err := s.Store.EmbedStats(model)
	if err != nil {
		return nil, err
	}
	s.embed.mu.Lock()
	defer s.embed.mu.Unlock()
	if s.embed.vec != nil && s.embed.vecModel == model && len(s.embed.vec.IDs) == done {
		return s.embed.vec, nil
	}
	idx, err := s.Store.LoadVectors(model)
	if err != nil {
		return nil, err
	}
	s.embed.vec, s.embed.vecModel = idx, model
	return idx, nil
}

type searchHit struct {
	messageView
	ConvID   int64   `json:"conv_id"`
	SourceID int64   `json:"source_id"`
	Title    string  `json:"title"`
	Snippet  string  `json:"snippet"`
	Score    float32 `json:"score,omitempty"`
}

func clipSnippet(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 160 {
		return string(r[:160]) + "…"
	}
	return s
}

// semanticSearch anlamsal ya da karma arama yapar.
func (s *Server) semanticSearch(w http.ResponseWriter, r *http.Request, q, mode string, limit int) {
	c, ok := s.embedClient()
	if !ok {
		fail(w, 400, "Anlamsal arama için gömme sunucusu ayarlanmalı (Ayarlar).")
		return
	}
	idx, err := s.vectors(c.Model)
	if err != nil {
		serverErr(w, err)
		return
	}
	if idx == nil || len(idx.IDs) == 0 {
		fail(w, 409, "Henüz hiçbir mesaj gömülmemiş: Ayarlar'dan dizinlemeyi başlat.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	qv, err := c.Embed(ctx, []string{q})
	if err != nil {
		fail(w, 502, err.Error())
		return
	}
	sem := embed.Relative(embed.Search(idx, qv[0], limit, 0), 0.10, 3)
	scores := map[int64]float32{}
	semIDs := make([]int64, len(sem))
	for i, h := range sem {
		semIDs[i], scores[h.ID] = h.ID, h.Score
	}
	order := semIDs
	byID := map[int64]searchHit{}
	if mode == "hybrid" {
		fts, err := s.Store.Search(q, limit)
		if err != nil {
			serverErr(w, err)
			return
		}
		ftsIDs := make([]int64, len(fts))
		for i, h := range fts {
			ftsIDs[i] = h.ID
			byID[h.ID] = searchHit{messageView: view(h.MessageRow), ConvID: h.ConvID, SourceID: h.SourceID, Title: h.Title, Snippet: h.Snippet}
		}
		order = embed.Fuse(ftsIDs, semIDs, limit)
	}
	var missing []int64
	for _, id := range order {
		if _, ok := byID[id]; !ok {
			missing = append(missing, id)
		}
	}
	rows, err := s.Store.MessagesByIDs(missing)
	if err != nil {
		serverErr(w, err)
		return
	}
	for id, h := range rows {
		byID[id] = searchHit{messageView: view(h.MessageRow), ConvID: h.ConvID, SourceID: h.SourceID, Title: h.Title, Snippet: clipSnippet(h.Text)}
	}
	out := make([]searchHit, 0, len(order))
	for _, id := range order {
		if h, ok := byID[id]; ok {
			h.Score = scores[id]
			out = append(out, h)
		}
	}
	writeJSON(w, 200, out)
}

type topicRep struct {
	ID     int64     `json:"id"`
	ConvID int64     `json:"conv_id"`
	TS     time.Time `json:"ts"`
	Text   string    `json:"text"`
}

type topicOut struct {
	Size     int      `json:"size"`
	Share    float64  `json:"share"`
	Rep      topicRep `json:"rep"`
	Keywords []string `json:"keywords"`
}

// topicsOf kişinin gömülmüş mesajlarını konulara ayırır: genel konular ve profildeki yoğun/sessiz
// dönemlerin konuları. LLM çağrısı yapmaz.
func (s *Server) topicsOf(w http.ResponseWriter, r *http.Request) {
	p, c, subj, ok := s.subject(w, r, "id")
	if !ok {
		return
	}
	cl, configured := s.embedClient()
	if !configured {
		fail(w, 400, "Konu analizi için gömme sunucusu ayarlanmalı (Ayarlar).")
		return
	}
	idx, err := s.vectors(cl.Model)
	if err != nil {
		serverErr(w, err)
		return
	}
	if idx == nil || len(idx.IDs) == 0 {
		fail(w, 409, "Henüz hiçbir mesaj gömülmemiş: Ayarlar'dan dizinlemeyi başlat.")
		return
	}
	have := make(map[int64]bool, len(idx.IDs))
	for _, id := range idx.IDs {
		have[id] = true
	}
	msgs := subj.Msgs(c)
	byID := map[int64]store.MessageRow{}
	texts := map[int64]string{}
	var ids []int64
	type dated struct {
		id    int64
		month string
	}
	var all []dated
	total := 0
	for _, m := range msgs {
		if (m.Kind != "text" && m.Kind != "share") || len([]rune(m.Text)) < 3 {
			continue
		}
		total++
		if !have[m.ID] {
			continue
		}
		byID[m.ID], texts[m.ID] = m, m.Text
		ids = append(ids, m.ID)
		all = append(all, dated{m.ID, m.TS.In(s.Loc).Format("2006-01")})
	}
	conv := func(ts []cluster.Topic) []topicOut {
		out := make([]topicOut, len(ts))
		for i, t := range ts {
			m := byID[t.RepID]
			out[i] = topicOut{Size: t.Size, Share: t.Share, Keywords: t.Keywords, Rep: topicRep{ID: m.ID, ConvID: m.ConvID, TS: m.TS, Text: clipSnippet(m.Text)}}
			if out[i].Keywords == nil {
				out[i].Keywords = []string{}
			}
		}
		return out
	}
	type periodOut struct {
		From     string     `json:"from"`
		To       string     `json:"to"`
		Kind     string     `json:"kind"`
		Messages int        `json:"messages"`
		Topics   []topicOut `json:"topics"`
	}
	periods := []periodOut{}
	for _, per := range s.profileFor(p, c, subj).Periods {
		var pids []int64
		for _, d := range all {
			if d.month >= per.From && d.month <= per.To {
				pids = append(pids, d.id)
			}
		}
		if len(pids) < 8 {
			continue
		}
		periods = append(periods, periodOut{per.From, per.To, per.Kind, len(pids), conv(cluster.Topics(idx, pids, texts, cluster.KFor(len(pids), 3)))})
	}
	writeJSON(w, 200, map[string]any{
		"coverage": map[string]int{"embedded": len(ids), "total": total},
		"overall":  conv(cluster.Topics(idx, ids, texts, cluster.KFor(len(ids), 6))),
		"periods":  periods,
	})
}
