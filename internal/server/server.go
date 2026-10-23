// Package server Joe'nun yerel HTTP API'sini ve gömülü arayüzünü sunar. Yalnızca loopback'te
// dinlenir; DNS yeniden bağlama ve çapraz site istekleri Host/Origin denetimiyle reddedilir.
package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/MustafaKemal0146/joe/internal/analytics"
	"github.com/MustafaKemal0146/joe/internal/budget"
	"github.com/MustafaKemal0146/joe/internal/config"
	"github.com/MustafaKemal0146/joe/internal/osint/directory"
	"github.com/MustafaKemal0146/joe/internal/osint/safefetch"
	"github.com/MustafaKemal0146/joe/internal/store"
)

// Server API sunucusudur.
type Server struct {
	Store *store.Store
	Cfg   *config.Holder
	UI    fs.FS
	Loc   *time.Location
	// TempDir yüklenen dosyaların geçici olarak yazıldığı klasördür.
	TempDir string

	// PersonaDir kullanıcı persona dosyalarının klasörüdür (boş olabilir).
	PersonaDir string

	// CallerFactory nil ise sağlayıcı ayarlarından gerçek LLM çağrıcısı kurulur.
	CallerFactory CallerFactory

	// ChatModelFactory nil ise sohbet modeli sağlayıcı ayarlarından kurulur.
	ChatModelFactory ChatModelFactory

	// OllamaURL otomatik gömme keşfinde taranan adrestir (boşsa yerel varsayılan).
	OllamaURL string

	// DataDir, OSINT site listesi gibi yerel dosyaların klasörüdür (boşsa TempDir kullanılır).
	DataDir string
	// OSINTListURL ve OSINTPolicy testlerde değiştirilir; boşsa resmî adres ve yalnızca genel IP'ler kullanılır.
	OSINTListURL string
	// OSINTListURLs sherlock/maigret listelerinin adreslerini testlerde değiştirir.
	OSINTListURLs map[string]string
	// OSINTDirectories testlerde dizin API adreslerini değiştirir; nil ise gerçek adresler kullanılır.
	OSINTDirectories *directory.Endpoints
	OSINTPolicy      safefetch.Policy

	osint osintJobs

	partnersOnce sync.Once
	runs         councilRuns
	embed        embedJob

	// Approvals pahalı işler için tek kullanımlık onaylardır (nil ise ilk kullanımda oluşur).
	Approvals *budget.Approvals

	mu      sync.Mutex
	corpus  *store.Corpus
	profile map[string]analytics.Profile
}

// invalidate veri değiştiğinde önbelleği boşaltır.
func (s *Server) invalidate() {
	s.mu.Lock()
	s.corpus, s.profile = nil, nil
	s.mu.Unlock()
	s.embed.mu.Lock()
	s.embed.vec = nil
	s.embed.mu.Unlock()
}

func (s *Server) loadCorpus() (*store.Corpus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.corpus != nil {
		return s.corpus, nil
	}
	c, err := s.Store.LoadCorpus(nil)
	if err != nil {
		return nil, err
	}
	s.corpus, s.profile = c, map[string]analytics.Profile{}
	return c, nil
}

func (s *Server) opts() analytics.Options { return analytics.Options{Loc: s.Loc} }

// Handler tüm yolları bağlar.
func (s *Server) Handler() http.Handler {
	s.partnersOnce.Do(s.ensureAllPartners) // önceden yüklenmiş kaynaklarda eksik karşı taraf dosyalarını tamamla
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("GET /api/state", s.state)

	mux.HandleFunc("GET /api/settings", s.getSettings)
	mux.HandleFunc("PUT /api/settings", s.putSettings)
	mux.HandleFunc("POST /api/settings/test", s.testProvider)
	mux.HandleFunc("POST /api/estimate", s.estimate)
	mux.HandleFunc("GET /api/ledger", s.ledger)
	mux.HandleFunc("GET /api/personas", s.personas)
	mux.HandleFunc("GET /api/osint/sites", s.osintSites)
	mux.HandleFunc("POST /api/osint/sites/update", s.osintUpdateSites)
	mux.HandleFunc("POST /api/osint/runs", s.osintStart)
	mux.HandleFunc("POST /api/osint/directories", s.osintDirectories)
	mux.HandleFunc("GET /api/osint/plan", s.osintPlan)
	mux.HandleFunc("GET /api/osint/runs", s.osintList)
	mux.HandleFunc("GET /api/osint/runs/{id}", s.osintGet)
	mux.HandleFunc("POST /api/osint/runs/{id}/cancel", s.osintCancel)
	mux.HandleFunc("DELETE /api/osint/runs/{id}", s.osintDelete)
	mux.HandleFunc("PATCH /api/osint/findings/{id}", s.osintReview)
	mux.HandleFunc("POST /api/osint/findings/{id}/attach", s.osintAttach)
	mux.HandleFunc("POST /api/sources/{id}/partners", s.sourcePartners)
	mux.HandleFunc("GET /api/people/{id}/network", s.networkOf)
	mux.HandleFunc("GET /api/people/{id}/guide", s.guideOf)
	mux.HandleFunc("GET /api/people/{id}/meta", s.metaOf)
	mux.HandleFunc("PUT /api/contact-labels", s.setContactLabel)
	mux.HandleFunc("POST /api/graph/hide", s.graphHide)
	mux.HandleFunc("POST /api/graph/unhide", s.graphUnhide)
	mux.HandleFunc("GET /api/people/{id}/osint", s.personOSINT)
	mux.HandleFunc("POST /api/chats", s.chatCreate)
	mux.HandleFunc("GET /api/chats", s.chatList)
	mux.HandleFunc("GET /api/chats/{id}", s.chatGet)
	mux.HandleFunc("PATCH /api/chats/{id}", s.chatPatch)
	mux.HandleFunc("DELETE /api/chats/{id}", s.chatDelete)
	mux.HandleFunc("POST /api/chats/{id}/messages", s.chatSend)
	mux.HandleFunc("POST /api/chats/{id}/actions/{aid}/confirm", s.chatAction(true))
	mux.HandleFunc("POST /api/chats/{id}/actions/{aid}/reject", s.chatAction(false))
	mux.HandleFunc("GET /api/people/{id}/notes", s.notesList)
	mux.HandleFunc("POST /api/people/{id}/notes", s.notesAdd)
	mux.HandleFunc("DELETE /api/notes/{id}", s.notesDelete)
	mux.HandleFunc("GET /api/embed/status", s.embedStatus)
	mux.HandleFunc("POST /api/embed/run", s.embedRun)
	mux.HandleFunc("GET /api/embed/discover", s.embedDiscover)
	mux.HandleFunc("POST /api/embed/auto", s.embedAuto)
	mux.HandleFunc("POST /api/embed/cancel", s.embedCancel)
	mux.HandleFunc("POST /api/council/plan", s.councilPlan)
	mux.HandleFunc("POST /api/council", s.councilStart)
	mux.HandleFunc("GET /api/council", s.councilList)
	mux.HandleFunc("GET /api/council/{id}", s.councilGet)
	mux.HandleFunc("GET /api/council/{id}/report.md", s.councilReport)
	mux.HandleFunc("POST /api/council/{id}/cancel", s.councilCancel)
	mux.HandleFunc("POST /api/council/{id}/resume", s.councilResume)
	mux.HandleFunc("DELETE /api/council/{id}", s.councilDelete)

	mux.HandleFunc("GET /api/sources", s.listSources)
	mux.HandleFunc("POST /api/import", s.importSource)
	mux.HandleFunc("DELETE /api/sources/{id}", s.deleteSource)
	mux.HandleFunc("POST /api/sources/{id}/owner", s.setOwner)
	mux.HandleFunc("GET /api/sources/{id}/contacts", s.contacts)
	mux.HandleFunc("GET /api/sources/{id}/conversations", s.conversations)
	mux.HandleFunc("GET /api/conversations/{id}/messages", s.messages)
	mux.HandleFunc("GET /api/conversations/{id}/highlights", s.highlightsOf)

	mux.HandleFunc("GET /api/people/overview", s.peopleOverview)
	mux.HandleFunc("GET /api/people", s.listPeople)
	mux.HandleFunc("POST /api/people", s.createPerson)
	mux.HandleFunc("GET /api/people/{id}", s.getPerson)
	mux.HandleFunc("PATCH /api/people/{id}", s.patchPerson)
	mux.HandleFunc("DELETE /api/people/{id}", s.deletePerson)
	mux.HandleFunc("POST /api/people/{id}/identities", s.addIdentity)
	mux.HandleFunc("DELETE /api/people/{id}/identities/{iid}", s.removeIdentity)
	mux.HandleFunc("GET /api/people/{id}/profile", s.profileOf)
	mux.HandleFunc("GET /api/people/{id}/timeline", s.timelineOf)
	mux.HandleFunc("GET /api/people/{id}/report.md", s.reportOf)
	mux.HandleFunc("GET /api/people/{id}/report.html", s.reportHTML)
	mux.HandleFunc("GET /api/report-print.js", s.reportPrintJS)
	mux.HandleFunc("GET /api/people/{id}/risk", s.riskOf)
	mux.HandleFunc("GET /api/people/{id}/evidence", s.evidenceOf)
	mux.HandleFunc("GET /api/people/{id}/topics", s.topicsOf)

	mux.HandleFunc("GET /api/graph", s.graph)
	mux.HandleFunc("GET /api/suggestions", s.suggestions)
	mux.HandleFunc("GET /api/compare", s.compare)
	mux.HandleFunc("GET /api/search", s.search)

	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { fail(w, 404, "bilinmeyen uç nokta") })
	mux.Handle("/", s.static())
	return guard(mux)
}

// ---- Yardımcılar ----

type apiError struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, code int, msg string) { writeJSON(w, code, apiError{msg}) }

func serverErr(w http.ResponseWriter, err error) {
	fmt.Fprintln(os.Stderr, "joe: hata:", err)
	fail(w, 500, "sunucu hatası: "+err.Error())
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		fail(w, 400, "geçersiz istek gövdesi: "+err.Error())
		return false
	}
	return true
}

func pathID(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || id <= 0 {
		fail(w, 400, "geçersiz kimlik")
		return 0, false
	}
	return id, true
}

// guard Host ve Origin denetimiyle DNS yeniden bağlama ve çapraz site yazma isteklerini engeller.
func guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !loopbackHost(r.Host) {
			http.Error(w, "geçersiz Host", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			if o := r.Header.Get("Origin"); o != "" {
				u, err := url.Parse(o)
				if err != nil || u.Host != r.Host {
					http.Error(w, "çapraz site isteği reddedildi", http.StatusForbidden)
					return
				}
			}
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

func loopbackHost(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (s *Server) static() http.Handler {
	if s.UI == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
	}
	files := http.FileServer(http.FS(s.UI))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p != "" {
			if _, err := fs.Stat(s.UI, p); err == nil {
				files.ServeHTTP(w, r)
				return
			}
		}
		if _, err := fs.Stat(s.UI, "index.html"); err != nil {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(503)
			io.WriteString(w, "Arayüz derlenmemiş. `cd web && pnpm install && pnpm build` çalıştırıp joe'yu yeniden derle.")
			return
		}
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/" // SPA: bilinmeyen yollar index.html
		files.ServeHTTP(w, r2)
	})
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"status": "ok", "time": time.Now().UTC()})
}

var errNotFound = errors.New("bulunamadı")
