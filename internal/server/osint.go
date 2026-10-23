package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/MustafaKemal0146/joe/internal/osint/directory"
	"github.com/MustafaKemal0146/joe/internal/osint/safefetch"
	"github.com/MustafaKemal0146/joe/internal/osint/scan"
	"github.com/MustafaKemal0146/joe/internal/store"
)

// siteList bir kullanıcı adı site listesi kaynağıdır. Listeler lisansları nedeniyle depoya gömülmez;
// kullanıcı isteğiyle indirilir.
type siteList struct {
	ID, Name, URL, License string
	Format                 scan.ListFormat
	DefaultLimit           int // taramada varsayılan en çok site (0: hepsi); popülerliğe göre
}

var siteLists = []siteList{
	{"wmn", "WhatsMyName", DefaultSiteListURL, "CC BY-SA 4.0", scan.FormatWMN, 0},
	{"sherlock", "Sherlock", "https://raw.githubusercontent.com/sherlock-project/sherlock/master/sherlock_project/resources/data.json", "MIT", scan.FormatSherlock, 0},
	{"maigret", "Maigret", "https://raw.githubusercontent.com/soxoj/maigret/main/maigret/resources/data.json", "MIT", scan.FormatMaigret, 1500},
}

func findList(id string) (siteList, bool) {
	if id == "" {
		id = "wmn"
	}
	for _, l := range siteLists {
		if l.ID == id {
			return l, true
		}
	}
	return siteList{}, false
}

// DefaultSiteListURL WhatsMyName'in resmî site listesidir. Lisansı (CC BY-SA 4.0) nedeniyle depoya
// gömülmez; kullanıcı isteğiyle indirilir.
const DefaultSiteListURL = "https://raw.githubusercontent.com/WebBreacher/WhatsMyName/main/wmn-data.json"

type osintJobs struct {
	mu     sync.Mutex
	cancel map[string]context.CancelFunc
	wg     sync.WaitGroup
}

func (s *Server) siteListPath(id string) string {
	return filepath.Join(s.DataDir, "osint", id+"-data.json")
}

func (s *Server) siteFetcher() *safefetch.Client {
	c := safefetch.New()
	c.MaxBytes = 20 << 20
	c.Timeout = 60 * time.Second
	if s.OSINTPolicy != nil {
		c.Policy = s.OSINTPolicy
	}
	return c
}

func (s *Server) loadSites(id string) (*scan.List, time.Time, error) {
	sl, ok := findList(id)
	if !ok {
		return nil, time.Time{}, errors.New("bilinmeyen site listesi")
	}
	p := s.siteListPath(sl.ID)
	st, err := os.Stat(p)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("%s listesi yok: önce 'Listeyi indir'e bas", sl.Name)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, time.Time{}, err
	}
	l, err := scan.Parse(sl.Format, data)
	return l, st.ModTime(), err
}

func (s *Server) listURL(sl siteList) string {
	switch {
	case sl.ID == "wmn":
		return s.siteListURL()
	case s.OSINTListURLs[sl.ID] != "":
		return s.OSINTListURLs[sl.ID]
	}
	return sl.URL
}

func (s *Server) osintSites(w http.ResponseWriter, r *http.Request) {
	lists := make([]map[string]any, 0, len(siteLists))
	for _, sl := range siteLists {
		l, mod, err := s.loadSites(sl.ID)
		m := map[string]any{"id": sl.ID, "name": sl.Name, "available": err == nil, "source": s.listURL(sl), "license": sl.Name + ", " + sl.License, "default_limit": sl.DefaultLimit}
		if err != nil {
			m["reason"] = err.Error()
		} else {
			m["count"], m["skipped"], m["updated_at"] = len(l.Sites), l.Skipped, mod
		}
		lists = append(lists, m)
	}
	// Üst düzey alanlar WhatsMyName içindir (eski istemciler ve testler için); "lists" hepsini verir.
	out := map[string]any{"lists": lists}
	for k, v := range lists[0] {
		if k != "id" && k != "name" {
			out[k] = v
		}
	}
	writeJSON(w, 200, out)
}

func (s *Server) siteListURL() string {
	if s.OSINTListURL != "" {
		return s.OSINTListURL
	}
	return DefaultSiteListURL
}

// osintUpdateSites seçilen listeyi (varsayılan: WhatsMyName) indirir, doğrular ve yerele yazar
// (kullanıcı isteğiyle).
func (s *Server) osintUpdateSites(w http.ResponseWriter, r *http.Request) {
	var body struct {
		List string `json:"list"`
	}
	if r.ContentLength != 0 && !decode(w, r, &body) {
		return
	}
	sl, ok := findList(body.List)
	if !ok {
		fail(w, 400, "bilinmeyen site listesi")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	pr, err := s.siteFetcher().Probe(ctx, s.listURL(sl))
	if err != nil {
		fail(w, 502, "Liste indirilemedi: "+err.Error())
		return
	}
	if pr.Status != 200 {
		fail(w, 502, "Liste indirilemedi: HTTP "+http.StatusText(pr.Status))
		return
	}
	if pr.Truncated {
		fail(w, 502, "Liste beklenenden büyük; indirme reddedildi.")
		return
	}
	l, err := scan.Parse(sl.Format, []byte(pr.Body))
	if err != nil {
		fail(w, 422, err.Error())
		return
	}
	path := s.siteListPath(sl.ID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		serverErr(w, err)
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(pr.Body), 0o600); err != nil {
		serverErr(w, err)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		serverErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"id": sl.ID, "count": len(l.Sites), "skipped": l.Skipped})
}

func (s *Server) osintStart(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username    string `json:"username"`
		Acknowledge bool   `json:"acknowledge"`
		List        string `json:"list"`  // wmn (varsayılan) | sherlock | maigret
		Limit       *int   `json:"limit"` // en çok site (popülerliğe göre); boşsa listenin varsayılanı, 0 = hepsi
	}
	if !decode(w, r, &body) {
		return
	}
	body.Username = strings.TrimSpace(strings.TrimPrefix(body.Username, "@"))
	if err := scan.ValidUsername(body.Username); err != nil {
		fail(w, 400, err.Error())
		return
	}
	if !body.Acknowledge {
		fail(w, 400, "Taramadan önce şunu onaylamalısın: yalnızca herkese açık profil adreslerine istek atılır ve eşleşme kimlik doğrulaması değildir.")
		return
	}
	sl, ok := findList(body.List)
	if !ok {
		fail(w, 400, "bilinmeyen site listesi")
		return
	}
	l, _, err := s.loadSites(sl.ID)
	if err != nil {
		fail(w, 409, err.Error())
		return
	}
	limit := sl.DefaultLimit
	if body.Limit != nil {
		if *body.Limit < 0 || *body.Limit > 20000 {
			fail(w, 400, "site sınırı 0 ile 20000 arasında olmalı")
			return
		}
		limit = *body.Limit
	}
	sites := l.Limit(limit)
	kind := "sites"
	if sl.ID != "wmn" {
		kind = "sites:" + sl.ID
	}
	id := newID()
	if err := s.Store.CreateOsintRunKind(id, body.Username, kind, len(sites)); err != nil {
		serverErr(w, err)
		return
	}
	s.runOSINT(id, func(ctx context.Context, progress func(scan.Progress), emit func(scan.Result)) error {
		_, err := scan.RunStream(ctx, s.siteFetcher(), sites, body.Username, 10, progress, emit)
		return err
	})
	writeJSON(w, 202, map[string]any{"id": id})
}

// runOSINT bir taramayı arka planda çalıştırır; iptalde ve hatada eldeki bulgular korunur.
func (s *Server) runOSINT(id string, fn func(ctx context.Context, progress func(scan.Progress), emit func(scan.Result)) error) {
	ctx, cancel := context.WithCancel(context.Background())
	s.osint.mu.Lock()
	if s.osint.cancel == nil {
		s.osint.cancel = map[string]context.CancelFunc{}
	}
	s.osint.cancel[id] = cancel
	s.osint.wg.Add(1)
	s.osint.mu.Unlock()
	go func() {
		defer func() {
			s.osint.mu.Lock()
			delete(s.osint.cancel, id)
			s.osint.mu.Unlock()
			cancel()
			s.osint.wg.Done()
		}()
		// Bulgular bulunur bulunmaz yazılır: arayüz taramanın sürdüğü sırada da görür; iptalde ve hatada
		// eldekiler zaten kayıtlıdır.
		emit := func(r scan.Result) {
			_ = s.Store.AddOsintFindings(id, []store.OsintFinding{{Site: r.Site, Category: r.Category, URL: r.URL, Outcome: string(r.Outcome), Detail: r.Detail}})
		}
		err := fn(ctx, func(p scan.Progress) { _ = s.Store.UpdateOsintProgress(id, p.Done, p.Found, p.Failed) }, emit)
		switch {
		case ctx.Err() != nil:
			_ = s.Store.FinishOsintRun(id, store.OsintCanceled, "")
		case err != nil:
			_ = s.Store.FinishOsintRun(id, store.OsintFailed, err.Error())
		default:
			_ = s.Store.FinishOsintRun(id, store.OsintDone, "")
		}
	}()
}

// osintDirectories kamuya açık profil dizinlerinde (GitHub, GitLab, Codeberg, Stack Overflow) arar.
func (s *Server) osintDirectories(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Query       string `json:"query"`
		Kind        string `json:"kind"`
		Acknowledge bool   `json:"acknowledge"`
	}
	if !decode(w, r, &body) {
		return
	}
	body.Query = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(body.Query), "@"))
	k := directory.Kind(body.Kind)
	if err := directory.Validate(body.Query, k); err != nil {
		fail(w, 400, err.Error())
		return
	}
	if !body.Acknowledge {
		fail(w, 400, "Aramadan önce şunu onaylamalısın: yalnızca herkese açık dizinlere sorgu gider ve eşleşme kimlik doğrulaması değildir.")
		return
	}
	id := newID()
	if err := s.Store.CreateOsintRunKind(id, body.Query, "dir_"+body.Kind, len(directory.Sites(k))); err != nil {
		serverErr(w, err)
		return
	}
	e := directory.Defaults
	if s.OSINTDirectories != nil {
		e = *s.OSINTDirectories
	}
	s.runOSINT(id, func(ctx context.Context, progress func(scan.Progress), emit func(scan.Result)) error {
		res, err := directory.Search(ctx, s.siteFetcher(), e, body.Query, k, progress)
		for _, r := range res {
			emit(r)
		}
		return err
	})
	writeJSON(w, 202, map[string]any{"id": id})
}

// osintPlan elle araştırma için arama bağlantıları üretir. Hiçbir istek atmaz; bağlantılar kanıt
// değil, doğrulanması gereken araştırma adaylarıdır.
func (s *Server) osintPlan(w http.ResponseWriter, r *http.Request) {
	q := strings.Join(strings.Fields(r.URL.Query().Get("q")), " ")
	k := directory.Kind(r.URL.Query().Get("kind"))
	if err := directory.Validate(q, k); err != nil {
		fail(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, directory.ManualPlan(q, k))
}

// WaitOSINT çalışan taramaların bitmesini bekler (testler ve kapanış için).
func (s *Server) WaitOSINT() { s.osint.wg.Wait() }

// StopOSINT çalışan taramaları iptal eder ve bekler.
func (s *Server) StopOSINT() {
	s.osint.mu.Lock()
	for _, c := range s.osint.cancel {
		c()
	}
	s.osint.mu.Unlock()
	s.osint.wg.Wait()
}

func (s *Server) osintList(w http.ResponseWriter, r *http.Request) {
	runs, err := s.Store.OsintRuns(intQuery(r, "limit", 30, 1, 100))
	if err != nil {
		serverErr(w, err)
		return
	}
	writeJSON(w, 200, runs)
}

func (s *Server) osintGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	run, err := s.Store.OsintRun(id)
	if err != nil {
		fail(w, 404, err.Error())
		return
	}
	fs, err := s.Store.OsintFindings(id)
	if err != nil {
		serverErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"run": run, "findings": fs})
}

func (s *Server) osintCancel(w http.ResponseWriter, r *http.Request) {
	s.osint.mu.Lock()
	cancel, ok := s.osint.cancel[r.PathValue("id")]
	s.osint.mu.Unlock()
	if !ok {
		fail(w, 409, "tarama çalışmıyor")
		return
	}
	cancel()
	w.WriteHeader(204)
}

func (s *Server) osintDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.osint.mu.Lock()
	if c, ok := s.osint.cancel[id]; ok {
		c()
	}
	s.osint.mu.Unlock()
	if err := s.Store.DeleteOsintRun(id); err != nil {
		fail(w, 404, err.Error())
		return
	}
	w.WriteHeader(204)
}

func (s *Server) osintReview(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var body struct {
		Review string `json:"review"`
	}
	if !decode(w, r, &body) {
		return
	}
	if err := s.Store.ReviewFinding(id, body.Review); err != nil {
		fail(w, 400, err.Error())
		return
	}
	w.WriteHeader(204)
}

func (s *Server) osintAttach(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var body struct {
		PersonID int64 `json:"person_id"`
	}
	if !decode(w, r, &body) {
		return
	}
	if err := s.Store.AttachFinding(id, body.PersonID); err != nil {
		code := 400
		if strings.Contains(err.Error(), "no rows") {
			code, err = 404, errors.New("kişi bulunamadı")
		}
		fail(w, code, err.Error())
		return
	}
	s.dropProfiles()
	w.WriteHeader(204)
}

func (s *Server) personOSINT(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	if _, err := s.Store.Person(id); err != nil {
		fail(w, 404, "kişi bulunamadı")
		return
	}
	fs, err := s.Store.PersonFindings(id)
	if err != nil {
		serverErr(w, err)
		return
	}
	writeJSON(w, 200, fs)
}
