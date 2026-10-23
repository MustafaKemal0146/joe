package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

func osintFixture(t *testing.T) (*Server, http.Handler, *httptest.Server) {
	t.Helper()
	srv, h := newServer(t)
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/list.json":
			base := "http://" + r.Host
			fmt.Fprintf(w, `{"sites":[{"name":"VarSitesi","uri_check":"%[1]s/var/{account}","e_code":200,"e_string":"profil","m_code":404,"cat":"social"},{"name":"YokSitesi","uri_check":"%[1]s/yok/{account}","e_code":200,"e_string":"profil","m_code":404,"cat":"coding"},{"name":"HizSiniri","uri_check":"%[1]s/hiz/{account}","e_code":200,"e_string":"profil","m_code":404,"cat":"misc"}]}`, base)
		case strings.HasPrefix(r.URL.Path, "/var/ayse"):
			w.Write([]byte("profil ayse"))
		case strings.HasPrefix(r.URL.Path, "/hiz/"):
			http.Error(w, "yavaş", 429)
		default:
			http.Error(w, "yok", 404)
		}
	}))
	t.Cleanup(site.Close)
	srv.DataDir = t.TempDir()
	srv.OSINTListURL = site.URL + "/list.json"
	srv.OSINTPolicy = func(a netip.Addr) bool { return a.IsLoopback() }
	return srv, h, site
}

func waitRun(t *testing.T, srv *Server, h http.Handler, id string) map[string]any {
	t.Helper()
	srv.WaitOSINT()
	rec := do(h, "GET", "/api/osint/runs/"+id, nil, nil)
	var out map[string]any
	json.Unmarshal(rec.Body.Bytes(), &out)
	return out
}

func TestOSINTSiteListDownloadScanAndHonestResults(t *testing.T) {
	srv, h, _ := osintFixture(t)
	// liste yokken tarama açıklayıcı hata verir
	if rec := do(h, "POST", "/api/osint/runs", map[string]any{"username": "ayse", "acknowledge": true}, nil); rec.Code != 409 {
		t.Fatalf("liste yokken: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "GET", "/api/osint/sites", nil, nil); !strings.Contains(rec.Body.String(), `"available":false`) {
		t.Fatalf("liste durumu: %s", rec.Body)
	}
	rec := do(h, "POST", "/api/osint/sites/update", nil, nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"count":3`) {
		t.Fatalf("liste indirme: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "GET", "/api/osint/sites", nil, nil); !strings.Contains(rec.Body.String(), `"available":true`) || !strings.Contains(rec.Body.String(), "CC BY-SA 4.0") {
		t.Fatalf("liste durumu: %s", rec.Body)
	}
	// onay ve doğrulama
	if rec := do(h, "POST", "/api/osint/runs", map[string]any{"username": "ayse"}, nil); rec.Code != 400 {
		t.Fatalf("onaysız tarama: %d", rec.Code)
	}
	for _, bad := range []string{"", "a", "ayşe", "a/b", strings.Repeat("x", 50)} {
		if rec := do(h, "POST", "/api/osint/runs", map[string]any{"username": bad, "acknowledge": true}, nil); rec.Code != 400 {
			t.Fatalf("%q: %d", bad, rec.Code)
		}
	}
	rec = do(h, "POST", "/api/osint/runs", map[string]any{"username": "@ayse", "acknowledge": true}, nil)
	if rec.Code != 202 {
		t.Fatalf("başlatma: %d %s", rec.Code, rec.Body)
	}
	var started struct{ ID string }
	json.Unmarshal(rec.Body.Bytes(), &started)
	got := waitRun(t, srv, h, started.ID)
	run := got["run"].(map[string]any)
	if run["status"] != "done" || run["username"] != "ayse" || run["done"].(float64) != 3 || run["found"].(float64) != 1 || run["failed"].(float64) != 1 {
		t.Fatalf("tarama: %v", run)
	}
	var found, failed int
	for _, f := range got["findings"].([]any) {
		m := f.(map[string]any)
		switch m["outcome"] {
		case "found":
			found++
			if m["review"] != "unreviewed" {
				t.Fatalf("yeni bulgu 'doğrulanmadı' olmalı: %v", m)
			}
		case "failed":
			failed++
		}
	}
	if found != 1 || failed != 1 {
		t.Fatalf("bulunan %d hata %d", found, failed)
	}
	// liste ve silme
	if !strings.Contains(do(h, "GET", "/api/osint/runs", nil, nil).Body.String(), started.ID) {
		t.Fatal("liste")
	}
	if rec := do(h, "DELETE", "/api/osint/runs/"+started.ID, nil, nil); rec.Code != 204 {
		t.Fatalf("silme: %d", rec.Code)
	}
	if rec := do(h, "GET", "/api/osint/runs/"+started.ID, nil, nil); rec.Code != 404 {
		t.Fatalf("silinen: %d", rec.Code)
	}
}

func TestOSINTSiteListDownloadRejectsGarbageAndPrivateTargets(t *testing.T) {
	srv, h, site := osintFixture(t)
	srv.OSINTListURL = site.URL + "/yok.json" // 404
	if rec := do(h, "POST", "/api/osint/sites/update", nil, nil); rec.Code != 502 {
		t.Fatalf("404 liste: %d %s", rec.Code, rec.Body)
	}
	srv.OSINTListURL = site.URL + "/var/ayse" // JSON değil
	if rec := do(h, "POST", "/api/osint/sites/update", nil, nil); rec.Code != 422 {
		t.Fatalf("geçersiz liste: %d %s", rec.Code, rec.Body)
	}
	// varsayılan ilke: yerel adres listesi indirilemez (SSRF)
	srv.OSINTPolicy = nil
	srv.OSINTListURL = site.URL + "/list.json"
	if rec := do(h, "POST", "/api/osint/sites/update", nil, nil); rec.Code != 502 {
		t.Fatalf("özel adresten indirme engellenmeli: %d %s", rec.Code, rec.Body)
	}
}

func TestOSINTReviewAttachFlowAndCouncilInclusion(t *testing.T) {
	srv, h, _ := osintFixture(t)
	_, _, _, _, ayse := councilFixture2(t, srv, h)
	do(h, "POST", "/api/osint/sites/update", nil, nil)
	rec := do(h, "POST", "/api/osint/runs", map[string]any{"username": "ayse", "acknowledge": true}, nil)
	var started struct{ ID string }
	json.Unmarshal(rec.Body.Bytes(), &started)
	got := waitRun(t, srv, h, started.ID)
	var findingID int64
	for _, x := range got["findings"].([]any) {
		if m := x.(map[string]any); m["outcome"] == "found" {
			findingID = int64(m["id"].(float64))
		}
	}
	attach := func() int {
		return do(h, "POST", fmt.Sprintf("/api/osint/findings/%d/attach", findingID), map[string]any{"person_id": ayse.ID}, nil).Code
	}
	if attach() != 400 {
		t.Fatal("doğrulanmamış bulgu kişiye bağlanamaz")
	}
	if rec := do(h, "PATCH", fmt.Sprintf("/api/osint/findings/%d", findingID), map[string]any{"review": "belki"}, nil); rec.Code != 400 {
		t.Fatalf("geçersiz karar: %d", rec.Code)
	}
	if rec := do(h, "PATCH", fmt.Sprintf("/api/osint/findings/%d", findingID), map[string]any{"review": "confirmed"}, nil); rec.Code != 204 {
		t.Fatalf("onay: %d", rec.Code)
	}
	if attach() != 204 {
		t.Fatal("doğrulanmış bulgu bağlanabilmeli")
	}
	if rec := do(h, "GET", fmt.Sprintf("/api/people/%d/osint", ayse.ID), nil, nil); !strings.Contains(rec.Body.String(), "/var/ayse") {
		t.Fatalf("kişinin bulguları: %s", rec.Body)
	}

	// konseye yalnızca istenirse girer; istenmeyince girmez
	base := map[string]any{"subject_id": ayse.ID, "persona_ids": []string{"freud"}, "consent": true}
	pl := plan(t, h, base)
	if strings.Contains(pl["preview"].(string), "OSINT bulguları") {
		t.Fatal("varsayılanda OSINT bulgusu pakete girmemeli")
	}
	base["include_osint"] = true
	pl2 := plan(t, h, base)
	prev := pl2["preview"].(string)
	if !strings.Contains(prev, "OSINT bulguları (kullanıcı onaylı") || !strings.Contains(prev, "VarSitesi") || !strings.Contains(prev, "DEĞİLDİR") {
		t.Fatalf("onaylı bulgu uyarıyla birlikte pakete girmeli:\n%.500s", prev)
	}
	// onay içeriğe bağlıdır: osint'li planın onayı osint'siz istekle kullanılamaz
	body := map[string]any{"subject_id": ayse.ID, "persona_ids": []string{"freud"}, "consent": true, "approval_id": pl2["approval_id"]}
	if rec := do(h, "POST", "/api/council", body, nil); rec.Code != 409 {
		t.Fatalf("içerik farklıysa onay geçersiz olmalı: %d", rec.Code)
	}
	// onay geri çekilince paketten çıkar
	do(h, "PATCH", fmt.Sprintf("/api/osint/findings/%d", findingID), map[string]any{"review": "rejected"}, nil)
	pl3 := plan(t, h, base)
	if strings.Contains(pl3["preview"].(string), "OSINT bulguları") {
		t.Fatal("reddedilen bulgu pakete girmemeli")
	}
}

func TestOSINTCancelAndNotFound(t *testing.T) {
	_, h, _ := osintFixture(t)
	if rec := do(h, "POST", "/api/osint/runs/yok/cancel", nil, nil); rec.Code != 409 {
		t.Fatalf("çalışmayan tarama: %d", rec.Code)
	}
	if rec := do(h, "DELETE", "/api/osint/runs/yok", nil, nil); rec.Code != 404 {
		t.Fatalf("olmayan tarama: %d", rec.Code)
	}
	if rec := do(h, "POST", "/api/osint/findings/9999/attach", map[string]any{"person_id": 1}, nil); rec.Code != 400 && rec.Code != 404 {
		t.Fatalf("olmayan bulgu: %d", rec.Code)
	}
	if rec := do(h, "GET", "/api/people/999/osint", nil, nil); rec.Code != 404 {
		t.Fatalf("olmayan kişi: %d", rec.Code)
	}
}
