package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/MustafaKemal0146/joe/internal/osint/directory"
)

func TestOSINTDirectoriesFindCandidatesAndReportFailuresHonestly(t *testing.T) {
	srv, h := newServer(t)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case p == "/users/ayse":
			w.Write([]byte(`{"login":"ayse","html_url":"https://github.com/ayse"}`))
		case strings.HasPrefix(p, "/api/v4/users"):
			http.Error(w, "limit", 429)
		case p == "/api/v1/users/ayse":
			http.NotFound(w, r)
		case strings.HasPrefix(p, "/2.3/users"):
			w.Write([]byte(`{"items":[{"display_name":"Ayşe Y.","link":"https://stackoverflow.com/users/1/ayse"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()
	srv.OSINTPolicy = func(a netip.Addr) bool { return a.IsLoopback() }
	srv.OSINTDirectories = &directory.Endpoints{GitHub: api.URL, GitLab: api.URL, Codeberg: api.URL, StackExchange: api.URL}

	if rec := do(h, "POST", "/api/osint/directories", map[string]any{"query": "ayse", "kind": "username"}, nil); rec.Code != 400 {
		t.Fatalf("onaysız: %d", rec.Code)
	}
	if rec := do(h, "POST", "/api/osint/directories", map[string]any{"query": "a", "kind": "username", "acknowledge": true}, nil); rec.Code != 400 {
		t.Fatalf("kısa: %d", rec.Code)
	}
	rec := do(h, "POST", "/api/osint/directories", map[string]any{"query": "@ayse", "kind": "username", "acknowledge": true}, nil)
	if rec.Code != 202 {
		t.Fatalf("başlat: %d %s", rec.Code, rec.Body)
	}
	var id struct{ ID string }
	json.Unmarshal(rec.Body.Bytes(), &id)
	out := waitRun(t, srv, h, id.ID)
	run := out["run"].(map[string]any)
	if run["status"] != "done" || run["kind"] != "dir_username" || run["username"] != "ayse" {
		t.Fatalf("koşu: %v", run)
	}
	var found, failed []string
	for _, f := range out["findings"].([]any) {
		m := f.(map[string]any)
		if m["outcome"] == "found" {
			found = append(found, m["site"].(string))
		} else {
			failed = append(failed, m["site"].(string)+":"+m["detail"].(string))
		}
	}
	if len(found) != 1 || found[0] != "GitHub" {
		t.Fatalf("bulunan: %v", found)
	}
	if len(failed) != 1 || !strings.Contains(failed[0], "GitLab") || !strings.Contains(failed[0], "hız sınırı") {
		t.Fatalf("başarısız: %v", failed)
	}

	// tam ad: Stack Overflow da sorgulanır
	rec = do(h, "POST", "/api/osint/directories", map[string]any{"query": "Ayşe Yılmaz", "kind": "full_name", "acknowledge": true}, nil)
	json.Unmarshal(rec.Body.Bytes(), &id)
	out = waitRun(t, srv, h, id.ID)
	if !strings.Contains(rec.Body.String(), "id") || !strings.Contains(toJSON(out), "Stack Overflow") {
		t.Fatalf("tam ad: %s", toJSON(out))
	}
}

func TestOSINTDirectoriesAllUnreachableFails(t *testing.T) {
	srv, h := newServer(t)
	srv.OSINTPolicy = func(a netip.Addr) bool { return a.IsLoopback() }
	srv.OSINTDirectories = &directory.Endpoints{GitHub: "http://127.0.0.1:1", GitLab: "http://127.0.0.1:1", Codeberg: "http://127.0.0.1:1", StackExchange: "http://127.0.0.1:1"}
	rec := do(h, "POST", "/api/osint/directories", map[string]any{"query": "ayse", "kind": "username", "acknowledge": true}, nil)
	var id struct{ ID string }
	json.Unmarshal(rec.Body.Bytes(), &id)
	run := waitRun(t, srv, h, id.ID)["run"].(map[string]any)
	if run["status"] != "failed" {
		t.Fatalf("ulaşılamayan dizin başarı sayıldı: %v", run)
	}
}

func TestOSINTManualPlanMakesNoRequests(t *testing.T) {
	_, h := newServer(t)
	rec := do(h, "GET", "/api/osint/plan?q=Ayşe+Yılmaz&kind=full_name", nil, nil)
	var p directory.Plan
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil || rec.Code != 200 {
		t.Fatalf("plan: %d %v", rec.Code, err)
	}
	if len(p.Searches) != 15 || !strings.HasPrefix(p.Searches[0].URL, "https://www.google.com/search?q=%22Ay%C5%9Fe+Y") || len(p.Direct) != 0 {
		t.Fatalf("plan içeriği: %+v", p)
	}
	rec = do(h, "GET", "/api/osint/plan?q=ayse_y&kind=username", nil, nil)
	json.Unmarshal(rec.Body.Bytes(), &p)
	if len(p.Direct) != 3 || p.Direct[0].URL != "https://github.com/ayse_y" {
		t.Fatalf("doğrudan: %+v", p.Direct)
	}
	if rec := do(h, "GET", "/api/osint/plan?q=x&kind=bilinmez", nil, nil); rec.Code != 400 {
		t.Fatalf("tür: %d", rec.Code)
	}
}

func toJSON(v any) string { b, _ := json.Marshal(v); return string(b) }

func TestOSINTSherlockAndMaigretListsDownloadScanAndLimit(t *testing.T) {
	srv, h := newServer(t)
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		base := "http://" + r.Host
		switch r.URL.Path {
		case "/sherlock.json":
			fmt.Fprintf(w, `{"$schema":"x","Var":{"errorType":"status_code","url":"%[1]s/var/{}","urlMain":"%[1]s/","username_claimed":"blue"},"Yok":{"errorType":"status_code","url":"%[1]s/yok/{}","urlMain":"%[1]s/","username_claimed":"blue"}}`, base)
		case "/maigret.json":
			fmt.Fprintf(w, `{"engines":{},"sites":{"Pop":{"url":"%[1]s/var/{username}","urlMain":"%[1]s","checkType":"status_code","alexaRank":10,"usernameClaimed":"a","usernameUnclaimed":"b"},"Az":{"url":"%[1]s/var/{username}?x=1","urlMain":"%[1]s","checkType":"status_code","alexaRank":99999,"usernameClaimed":"a","usernameUnclaimed":"b"}}}`, base)
		case "/bozuk.json":
			w.Write([]byte(`{"sites":{}}`))
		case "/var/ayse":
			w.Write([]byte("profil"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer site.Close()
	srv.DataDir = t.TempDir()
	srv.OSINTPolicy = func(a netip.Addr) bool { return a.IsLoopback() }
	srv.OSINTListURLs = map[string]string{"sherlock": site.URL + "/sherlock.json", "maigret": site.URL + "/maigret.json"}

	rec := do(h, "GET", "/api/osint/sites", nil, nil)
	if !strings.Contains(rec.Body.String(), `"id":"sherlock"`) || !strings.Contains(rec.Body.String(), `"id":"maigret"`) || !strings.Contains(rec.Body.String(), "MIT") {
		t.Fatalf("listeler: %s", rec.Body)
	}
	// liste yokken tarama açıklayıcı hata verir
	if rec := do(h, "POST", "/api/osint/runs", map[string]any{"username": "ayse", "acknowledge": true, "list": "sherlock"}, nil); rec.Code != 409 || !strings.Contains(rec.Body.String(), "Sherlock") {
		t.Fatalf("liste yokken: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "POST", "/api/osint/sites/update", map[string]any{"list": "yok"}, nil); rec.Code != 400 {
		t.Fatalf("bilinmeyen liste: %d", rec.Code)
	}
	for _, id := range []string{"sherlock", "maigret"} {
		if rec := do(h, "POST", "/api/osint/sites/update", map[string]any{"list": id}, nil); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"count":2`) {
			t.Fatalf("%s indirme: %d %s", id, rec.Code, rec.Body)
		}
	}
	// geçersiz liste diske yazılmaz
	srv.OSINTListURLs["sherlock"] = site.URL + "/bozuk.json"
	if rec := do(h, "POST", "/api/osint/sites/update", map[string]any{"list": "sherlock"}, nil); rec.Code != 422 {
		t.Fatalf("bozuk liste reddedilmeli: %d", rec.Code)
	}

	start := func(body map[string]any) map[string]any {
		body["username"], body["acknowledge"] = "ayse", true
		rec := do(h, "POST", "/api/osint/runs", body, nil)
		if rec.Code != 202 {
			t.Fatalf("başlat: %d %s", rec.Code, rec.Body)
		}
		var id struct{ ID string }
		json.Unmarshal(rec.Body.Bytes(), &id)
		return waitRun(t, srv, h, id.ID)
	}
	out := start(map[string]any{"list": "sherlock"})
	run := out["run"].(map[string]any)
	if run["kind"] != "sites:sherlock" || run["status"] != "done" || run["found"].(float64) != 1 || run["total"].(float64) != 2 {
		t.Fatalf("sherlock koşusu: %v", run)
	}
	// maigret: popülerliğe göre en çok 1 site → yalnızca "Pop" taranır
	out = start(map[string]any{"list": "maigret", "limit": 1})
	run = out["run"].(map[string]any)
	if run["kind"] != "sites:maigret" || run["total"].(float64) != 1 {
		t.Fatalf("maigret sınırı: %v", run)
	}
	if fs := out["findings"].([]any); len(fs) != 1 || fs[0].(map[string]any)["site"] != "Pop" {
		t.Fatalf("en popüler site taranmalı: %v", fs)
	}
	if rec := do(h, "POST", "/api/osint/runs", map[string]any{"username": "ayse", "acknowledge": true, "list": "maigret", "limit": -1}, nil); rec.Code != 400 {
		t.Fatalf("negatif sınır: %d", rec.Code)
	}
}

func TestOSINTFindingsAreVisibleWhileScanIsStillRunning(t *testing.T) {
	srv, h := newServer(t)
	release := make(chan struct{})
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		base := "http://" + r.Host
		switch r.URL.Path {
		case "/sherlock.json":
			fmt.Fprintf(w, `{"Hizli":{"errorType":"status_code","url":"%[1]s/hizli/{}","urlMain":"%[1]s/","username_claimed":"blue"},"Yavas":{"errorType":"status_code","url":"%[1]s/yavas/{}","urlMain":"%[1]s/","username_claimed":"blue"}}`, base)
		case "/hizli/ayse":
			w.Write([]byte("profil"))
		case "/yavas/ayse":
			<-release // tarama bu istekte bekler
			w.Write([]byte("profil"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer site.Close()
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	srv.DataDir = t.TempDir()
	srv.OSINTPolicy = func(a netip.Addr) bool { return a.IsLoopback() }
	srv.OSINTListURLs = map[string]string{"sherlock": site.URL + "/sherlock.json"}
	do(h, "POST", "/api/osint/sites/update", map[string]any{"list": "sherlock"}, nil)
	rec := do(h, "POST", "/api/osint/runs", map[string]any{"username": "ayse", "acknowledge": true, "list": "sherlock"}, nil)
	var id struct{ ID string }
	json.Unmarshal(rec.Body.Bytes(), &id)

	var mid struct {
		Run      struct{ Status string } `json:"run"`
		Findings []struct{ Site string } `json:"findings"`
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		json.Unmarshal(do(h, "GET", "/api/osint/runs/"+id.ID, nil, nil).Body.Bytes(), &mid)
		if len(mid.Findings) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if mid.Run.Status != "running" || len(mid.Findings) != 1 || mid.Findings[0].Site != "Hizli" {
		t.Fatalf("tarama sürerken ilk bulgu görünmeli: %+v", mid)
	}
	close(release)
	out := waitRun(t, srv, h, id.ID)
	if fs := out["findings"].([]any); len(fs) != 2 || out["run"].(map[string]any)["status"] != "done" {
		t.Fatalf("bitişte iki bulgu, kopya yok: %v", out)
	}
}
