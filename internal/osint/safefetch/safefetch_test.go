package safefetch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
)

func TestPublicOnlyClassification(t *testing.T) {
	blocked := []string{
		"127.0.0.1", "10.1.2.3", "172.16.0.1", "172.31.255.255", "192.168.1.1", "169.254.169.254", "0.0.0.0",
		"100.64.0.1", "224.0.0.1", "240.0.0.1", "198.18.0.1", "203.0.113.5", "::1", "::", "fe80::1", "fc00::1",
		"::ffff:127.0.0.1", "::ffff:10.0.0.1", "::ffff:169.254.169.254", "ff02::1", "64:ff9b::1.2.3.4",
	}
	for _, s := range blocked {
		if PublicOnly(netip.MustParseAddr(s)) {
			t.Errorf("%s engellenmeliydi", s)
		}
	}
	for _, s := range []string{"8.8.8.8", "1.1.1.1", "93.184.216.34", "2606:4700:4700::1111"} {
		if !PublicOnly(netip.MustParseAddr(s)) {
			t.Errorf("%s izinli olmalıydı", s)
		}
	}
}

func TestLoopbackServerBlockedByDefault(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer srv.Close()
	_, err := New().Fetch(context.Background(), srv.URL)
	if err == nil || !errors.Is(err, ErrBlocked) {
		t.Fatalf("loopback reddedilmeli, hata: %v", err)
	}
	if hits.Load() != 0 {
		t.Fatal("sunucuya istek ulaşmamalıydı")
	}
}

func TestRedirectToBlockedAddressIsRejected(t *testing.T) {
	var secret atomic.Int32
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { secret.Add(1) }))
	defer internal.Close()
	public := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, internal.URL, http.StatusFound)
	}))
	defer public.Close()
	// İlke yalnızca ilk bağlantıya izin verir; yönlendirmeyle gidilen ikinci bağlantı reddedilmeli.
	c := New()
	calls := 0
	c.Policy = func(a netip.Addr) bool { calls++; return a.IsLoopback() && calls == 1 }
	if _, err := c.Fetch(context.Background(), public.URL); err == nil || !errors.Is(err, ErrBlocked) {
		t.Fatalf("yönlendirme hedefi yeniden doğrulanmalı: %v", err)
	}
	if secret.Load() != 0 {
		t.Fatal("iç sunucuya istek ulaşmamalıydı")
	}
}

func TestAllowedPolicyStreamsAndTruncates(t *testing.T) {
	big := strings.Repeat("a", 5000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte("<html><head><title>Merhaba  Dünya</title><script>alert(1)</script></head><body>" + big + "</body></html>"))
	}))
	defer srv.Close()
	c := New()
	c.Policy = func(a netip.Addr) bool { return a.IsLoopback() }
	c.MaxBytes = 1000
	p, err := c.Fetch(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Truncated || p.Bytes != 1000 {
		t.Fatalf("sınırda kesilmeli: truncated=%v bytes=%d", p.Truncated, p.Bytes)
	}
	if p.Title != "Merhaba Dünya" {
		t.Fatalf("başlık: %q", p.Title)
	}
	if strings.Contains(p.Text, "alert") {
		t.Fatal("script içeriği metne girmemeli")
	}
	if p.SHA256 == "" {
		t.Fatal("içerik özeti boş")
	}
}

func TestRejectsBadSchemesAndCredentials(t *testing.T) {
	for _, u := range []string{"file:///etc/passwd", "ftp://example.com/x", "gopher://x", "http://user:pass@example.com/", "http:///x", "javascript:alert(1)"} {
		if _, err := New().Fetch(context.Background(), u); err == nil {
			t.Errorf("%q reddedilmeliydi", u)
		}
	}
}

func TestHTTPErrorIsAnErrorNotAPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "x", 503) }))
	defer srv.Close()
	c := New()
	c.Policy = func(a netip.Addr) bool { return a.IsLoopback() }
	if p, err := c.Fetch(context.Background(), srv.URL); err == nil || p != nil {
		t.Fatalf("HTTP 503 hata olmalı, sonuç: %+v %v", p, err)
	}
}

func TestRedirectLimit(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, srv.URL+r.URL.Path+"x", http.StatusFound)
	}))
	defer srv.Close()
	c := New()
	c.Policy = func(a netip.Addr) bool { return a.IsLoopback() }
	c.MaxRedirects = 3
	if _, err := c.Fetch(context.Background(), srv.URL); err == nil {
		t.Fatal("sonsuz yönlendirme sınırla kesilmeli")
	}
}

func TestProbeReturnsStatusForErrorPagesAndStillBlocksPrivate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/yok":
			http.Error(w, "kullanıcı bulunamadı", 404)
		case "/var":
			w.Write([]byte("profil sayfası: ayse"))
		default:
			http.Error(w, "x", 503)
		}
	}))
	defer srv.Close()
	c := New()
	c.Policy = func(a netip.Addr) bool { return a.IsLoopback() }
	for path, want := range map[string]int{"/yok": 404, "/var": 200, "/baska": 503} {
		p, err := c.Probe(context.Background(), srv.URL+path)
		if err != nil || p.Status != want {
			t.Fatalf("%s: %+v %v", path, p, err)
		}
	}
	if p, _ := c.Probe(context.Background(), srv.URL+"/yok"); !strings.Contains(p.Body, "bulunamadı") {
		t.Fatal("hata sayfasının gövdesi de dönmeli")
	}
	if _, err := New().Probe(context.Background(), srv.URL+"/var"); err == nil || !errors.Is(err, ErrBlocked) {
		t.Fatalf("varsayılan ilkede loopback engellenmeli: %v", err)
	}
	if _, err := New().Probe(context.Background(), "file:///etc/passwd"); err == nil {
		t.Fatal("http dışı şema reddedilmeli")
	}
}
