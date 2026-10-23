package scan

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/MustafaKemal0146/joe/internal/osint/safefetch"
)

func listFor(base string) []byte {
	return []byte(fmt.Sprintf(`{"license":["x"],"sites":[
 {"name":"VarSitesi","uri_check":"%[1]s/var/{account}","uri_pretty":"%[1]s/@{account}","e_code":200,"e_string":"profil sayfası","m_code":404,"m_string":"","cat":"social"},
 {"name":"YokSitesi","uri_check":"%[1]s/yok/{account}","e_code":200,"e_string":"profil","m_code":404,"m_string":"bulunamadı","cat":"gaming"},
 {"name":"YumusakYok","uri_check":"%[1]s/yumusak/{account}","e_code":200,"e_string":"profil","m_code":200,"m_string":"böyle bir kullanıcı yok","cat":"coding"},
 {"name":"HizSiniri","uri_check":"%[1]s/hiz/{account}","e_code":200,"e_string":"profil","m_code":404,"cat":"misc"},
 {"name":"Engelli","uri_check":"%[1]s/engel/{account}","e_code":200,"e_string":"profil","m_code":404,"cat":"misc"},
 {"name":"Coken","uri_check":"%[1]s/coken/{account}","e_code":200,"e_string":"profil","m_code":404,"cat":"misc"},
 {"name":"KalipYok","uri_check":"%[1]s/kalipyok","e_code":200,"e_string":"x","cat":"misc"},
 {"name":"JavaScriptSemasi","uri_check":"javascript:alert('{account}')","e_code":200,"e_string":"x","cat":"misc"},
 {"name":"","uri_check":"%[1]s/adsiz/{account}","e_code":200,"e_string":"x","cat":"misc"}
]}`, base))
}

func server(hits *atomic.Int32) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch {
		case strings.HasPrefix(r.URL.Path, "/var/ayse"):
			w.Write([]byte("<h1>profil sayfası</h1> ayse"))
		case strings.HasPrefix(r.URL.Path, "/yok/"):
			http.Error(w, "kullanıcı bulunamadı", 404)
		case strings.HasPrefix(r.URL.Path, "/yumusak/"):
			w.Write([]byte("profil — böyle bir kullanıcı yok"))
		case strings.HasPrefix(r.URL.Path, "/hiz/"):
			http.Error(w, "yavaş", 429)
		case strings.HasPrefix(r.URL.Path, "/engel/"):
			http.Error(w, "yasak", 403)
		case strings.HasPrefix(r.URL.Path, "/coken/"):
			http.Error(w, "hata", 502)
		default:
			http.Error(w, "?", 404)
		}
	}))
}

func client() *safefetch.Client {
	c := safefetch.New()
	c.Policy = func(a netip.Addr) bool { return a.IsLoopback() }
	return c
}

func TestParseListKeepsOnlyUsableSitesAndCountsSkipped(t *testing.T) {
	l, err := ParseList(listFor("http://127.0.0.1:1"))
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Sites) != 6 || l.Skipped != 3 {
		t.Fatalf("geçerli %d, atlanan %d (6 ve 3 beklenir): %+v", len(l.Sites), l.Skipped, l.Sites)
	}
	for _, bad := range []string{`{}`, `{"sites":[]}`, `not json`, `{"sites":[{"name":"x","uri_check":"file:///{account}","e_code":200}]}`} {
		if _, err := ParseList([]byte(bad)); err == nil {
			t.Errorf("%q reddedilmeliydi", bad)
		}
	}
}

func TestRunClassifiesFoundNotFoundAndFailuresSeparately(t *testing.T) {
	var hits atomic.Int32
	srv := server(&hits)
	defer srv.Close()
	l, _ := ParseList(listFor(srv.URL))
	var last Progress
	res, err := Run(context.Background(), client(), l.Sites, "ayse", 3, func(p Progress) { last = p })
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]Result{}
	for _, r := range res {
		byName[r.Site] = r
	}
	if r := byName["VarSitesi"]; r.Outcome != Found || r.URL != srv.URL+"/@ayse" {
		t.Fatalf("bulunan: %+v", r)
	}
	for _, name := range []string{"YokSitesi", "YumusakYok"} {
		if _, ok := byName[name]; ok {
			t.Errorf("%s: 'bulunamadı' sonuç listesinde olmamalı", name)
		}
	}
	for name, want := range map[string]string{"HizSiniri": "429", "Engelli": "403", "Coken": "502"} {
		r, ok := byName[name]
		if !ok || r.Outcome != Failed || !strings.Contains(r.Detail, want) {
			t.Errorf("%s: hata olarak raporlanmalı (hesap yok sayılmamalı): %+v", name, r)
		}
	}
	if last.Total != 6 || last.Done != 6 || last.Found != 1 || last.Failed != 3 || hits.Load() != 6 {
		t.Fatalf("ilerleme: %+v istek: %d", last, hits.Load())
	}
}

func TestRunRejectsBadUsernamesAndBlocksPrivateTargets(t *testing.T) {
	var hits atomic.Int32
	srv := server(&hits)
	defer srv.Close()
	l, _ := ParseList(listFor(srv.URL))
	for _, u := range []string{"", "a", strings.Repeat("a", 41), "a b", "../etc", "ayse/../x", "a?x=1", "ayşe"} {
		if _, err := Run(context.Background(), client(), l.Sites, u, 2, nil); err == nil {
			t.Errorf("%q reddedilmeliydi", u)
		}
	}
	if hits.Load() != 0 {
		t.Fatal("geçersiz kullanıcı adıyla istek atılmamalı")
	}
	// varsayılan ilke: loopback hedefleri engellenir ve hata olarak raporlanır
	res, _ := Run(context.Background(), safefetch.New(), l.Sites[:1], "ayse", 1, nil)
	if len(res) != 1 || res[0].Outcome != Failed || hits.Load() != 0 {
		t.Fatalf("özel adrese istek atılmamalı: %+v", res)
	}
}

func TestRunCancellationStopsAndKeepsPartialResults(t *testing.T) {
	var hits atomic.Int32
	srv := server(&hits)
	defer srv.Close()
	l, _ := ParseList(listFor(srv.URL))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := Run(ctx, client(), l.Sites, "ayse", 2, nil)
	if err == nil || len(res) != 0 {
		t.Fatalf("iptal edilmiş bağlamda sonuç olmamalı: %v %d", err, len(res))
	}
}

func TestClassifyAmbiguousCodes(t *testing.T) {
	s := Site{ECode: 200, EString: "profil", MCode: 404}
	for status, want := range map[int]Outcome{200: NotFound, 404: NotFound, 410: NotFound, 429: Failed, 403: Failed, 500: Failed, 302: Failed, 418: Failed} {
		got, _ := classify(s, status, "boş sayfa")
		if got != want {
			t.Errorf("HTTP %d → %s, beklenen %s", status, got, want)
		}
	}
	if got, _ := classify(s, 200, "profil var"); got != Found {
		t.Fatal("e_code ve e_string eşleşince bulundu")
	}
}
