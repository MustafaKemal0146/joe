package server

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MustafaKemal0146/joe/internal/config"
	"github.com/MustafaKemal0146/joe/internal/store"
)

func newServer(t *testing.T) (*Server, http.Handler) {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	cfg, _ := config.Load(filepath.Join(t.TempDir(), "c.enc"))
	s := &Server{Store: st, Cfg: cfg, Loc: time.UTC, TempDir: t.TempDir()}
	return s, s.Handler()
}

func do(h http.Handler, method, path string, body any, hdr map[string]string) *httptest.ResponseRecorder {
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rd)
	req.Host = "127.0.0.1:8777"
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func whatsappFile(t *testing.T) string {
	t.Helper()
	var sb strings.Builder
	sb.WriteString("26.09.2026 20:37 - Mesajlar ve aramalar uçtan uca şifrelidir.\n")
	for i := 0; i < 30; i++ {
		who := "Ben"
		if i%2 == 1 {
			who = "Ayşe"
		}
		fmt.Fprintf(&sb, "26.09.2026 20:%02d - %s: merhaba şey %d\n", 40+i%19, who, i)
	}
	p := filepath.Join(t.TempDir(), "WhatsApp Sohbeti - Ayşe.txt")
	os.WriteFile(p, []byte(sb.String()), 0o600)
	return p
}

func TestHostAndOriginGuard(t *testing.T) {
	_, h := newServer(t)
	req := httptest.NewRequest("GET", "/api/health", nil)
	req.Host = "evil.example:8777"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatalf("yabancı Host reddedilmeli: %d", rec.Code)
	}
	rec = do(h, "POST", "/api/people", map[string]any{"name": "x"}, map[string]string{"Origin": "http://evil.example"})
	if rec.Code != 403 {
		t.Fatalf("çapraz site yazma reddedilmeli: %d", rec.Code)
	}
	rec = do(h, "POST", "/api/people", map[string]any{"name": "x"}, map[string]string{"Origin": "http://127.0.0.1:8777"})
	if rec.Code != 201 {
		t.Fatalf("aynı kaynaklı istek geçmeli: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "GET", "/api/health", nil, nil); rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("güvenlik başlıkları eksik")
	}
}

func TestImportByPathThenOwnerAndAnalysis(t *testing.T) {
	_, h := newServer(t)
	path := whatsappFile(t)
	rec := do(h, "POST", "/api/import", map[string]string{"path": path}, nil)
	if rec.Code != 201 {
		t.Fatalf("içe aktarma: %d %s", rec.Code, rec.Body)
	}
	var res importResult
	json.Unmarshal(rec.Body.Bytes(), &res)
	if res.Source.Platform != "whatsapp" || res.Source.OwnerName != "Ben" || res.OwnerNeeded != true {
		t.Fatalf("sonuç: %+v", res)
	}
	if res.Person != nil {
		t.Fatalf("tahmini sahip için kişi açılmamalı: %+v", res.Person)
	}
	// aynı dosya → yinelenen
	rec = do(h, "POST", "/api/import", map[string]string{"path": path}, nil)
	var dup importResult
	json.Unmarshal(rec.Body.Bytes(), &dup)
	if rec.Code != 200 || !dup.Duplicate {
		t.Fatalf("yinelenen içe aktarma: %d %s", rec.Code, rec.Body)
	}
	// sahibi onayla
	rec = do(h, "POST", fmt.Sprintf("/api/sources/%d/owner", res.Source.ID), map[string]string{"name": "Ben"}, nil)
	if rec.Code != 200 {
		t.Fatalf("sahip: %d %s", rec.Code, rec.Body)
	}
	var owned importResult
	json.Unmarshal(rec.Body.Bytes(), &owned)
	if owned.Person == nil || !owned.Person.IsSelf {
		t.Fatalf("onaylı sahip için 'ben' kişisi açılmalı: %+v", owned.Person)
	}
	res.Person = owned.Person
	// kişiler ve temaslar
	rec = do(h, "GET", fmt.Sprintf("/api/sources/%d/contacts", res.Source.ID), nil, nil)
	var cs []contact
	json.Unmarshal(rec.Body.Bytes(), &cs)
	// tek sohbetli kaynakta karşı taraf için kişi dosyası kendiliğinden açılır
	if len(cs) != 1 || cs[0].Name != "Ayşe" || cs[0].PersonID == 0 {
		t.Fatalf("temaslar: %+v", cs)
	}
	var ayse store.Person
	rec = do(h, "GET", fmt.Sprintf("/api/people/%d", cs[0].PersonID), nil, nil)
	json.Unmarshal(rec.Body.Bytes(), &ayse)
	rec = do(h, "GET", fmt.Sprintf("/api/people/%d/profile", ayse.ID), nil, nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"messages":15`) {
		t.Fatalf("profil: %d %s", rec.Code, rec.Body)
	}
	rec = do(h, "GET", fmt.Sprintf("/api/compare?a=%d&b=%d", res.Person.ID, ayse.ID), nil, nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "direct") {
		t.Fatalf("karşılaştırma: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "GET", "/api/compare?a=1&b=1", nil, nil); rec.Code != 400 {
		t.Fatalf("aynı kişi karşılaştırılamaz: %d", rec.Code)
	}
	if rec := do(h, "GET", "/api/graph", nil, nil); rec.Code != 200 || !strings.Contains(rec.Body.String(), "Ayşe") {
		t.Fatalf("grafik: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "GET", "/api/search?q=merhaba", nil, nil); rec.Code != 200 || !strings.Contains(rec.Body.String(), "«merhaba»") {
		t.Fatalf("arama: %d %s", rec.Code, rec.Body)
	}
	// silince kişi verisi boş döner, çökmez
	do(h, "DELETE", fmt.Sprintf("/api/sources/%d", res.Source.ID), nil, nil)
	rec = do(h, "GET", fmt.Sprintf("/api/people/%d/profile", ayse.ID), nil, nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "eşleşen mesaj ya da etkinlik bulunamadı") {
		t.Fatalf("silinen veri sonrası profil: %d %s", rec.Code, rec.Body)
	}
}

func TestImportByUploadZip(t *testing.T) {
	_, h := newServer(t)
	var zbuf bytes.Buffer
	zw := zip.NewWriter(&zbuf)
	w, _ := zw.Create("_chat.txt")
	w.Write([]byte("[9/26/26, 8:37:09 PM] Ali: selam\n[9/26/26, 8:38:09 PM] Veli: nasılsın\n"))
	zw.Close()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", "../../etc/passwd.zip") // yol geçişi adı temizlenmeli
	fw.Write(zbuf.Bytes())
	mw.Close()
	req := httptest.NewRequest("POST", "/api/import", &body)
	req.Host = "127.0.0.1:8777"
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 201 {
		t.Fatalf("yükleme: %d %s", rec.Code, rec.Body)
	}
}

func TestImportRejectsGarbageAndBadContentType(t *testing.T) {
	_, h := newServer(t)
	p := filepath.Join(t.TempDir(), "x.txt")
	os.WriteFile(p, []byte("sohbet değil"), 0o600)
	if rec := do(h, "POST", "/api/import", map[string]string{"path": p}, nil); rec.Code != 422 {
		t.Fatalf("anlamsız dosya 422 vermeli: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "POST", "/api/import", map[string]string{"path": "/yok/yok.zip"}, nil); rec.Code != 400 {
		t.Fatalf("olmayan yol 400 vermeli: %d", rec.Code)
	}
	req := httptest.NewRequest("POST", "/api/import", strings.NewReader("x"))
	req.Host = "127.0.0.1:8777"
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 415 {
		t.Fatalf("içerik türü reddedilmeli: %d", rec.Code)
	}
}

func TestSettingsNeverLeakKey(t *testing.T) {
	_, h := newServer(t)
	rec := do(h, "PUT", "/api/settings", map[string]any{"provider_id": "deepseek", "api_key": "sk-cok-gizli-anahtar"}, nil)
	if rec.Code != 200 {
		t.Fatalf("ayar: %d %s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "sk-cok-gizli-anahtar") {
		t.Fatal("yanıt anahtarı sızdırıyor")
	}
	rec = do(h, "GET", "/api/settings", nil, nil)
	if strings.Contains(rec.Body.String(), "sk-cok-gizli") || !strings.Contains(rec.Body.String(), `"has_key":true`) {
		t.Fatalf("ayarlar: %s", rec.Body)
	}
	if rec := do(h, "PUT", "/api/settings", map[string]any{"bilinmeyen": 1}, nil); rec.Code != 400 {
		t.Fatalf("bilinmeyen alan reddedilmeli: %d", rec.Code)
	}
}

func TestPersonValidationAndNotFound(t *testing.T) {
	_, h := newServer(t)
	if rec := do(h, "POST", "/api/people", map[string]any{"name": "  "}, nil); rec.Code != 400 {
		t.Fatalf("boş ad: %d", rec.Code)
	}
	if rec := do(h, "GET", "/api/people/999/profile", nil, nil); rec.Code != 404 {
		t.Fatalf("olmayan kişi: %d", rec.Code)
	}
	if rec := do(h, "GET", "/api/people/abc", nil, nil); rec.Code != 400 {
		t.Fatalf("geçersiz kimlik: %d", rec.Code)
	}
	if rec := do(h, "GET", "/api/yok", nil, nil); rec.Code != 404 {
		t.Fatalf("bilinmeyen uç nokta: %d", rec.Code)
	}
}

func TestImportMultipleUploadedParts(t *testing.T) {
	_, h := newServer(t)
	mkzip := func(files map[string]string) []byte {
		var b bytes.Buffer
		zw := zip.NewWriter(&b)
		for n, c := range files {
			w, _ := zw.Create(n)
			w.Write([]byte(c))
		}
		zw.Close()
		return b.Bytes()
	}
	thread := `{"participants":[{"name":"Ben"},{"name":"Ayşe"}],"messages":[{"sender_name":"Ayşe","timestamp_ms":1700000000000,"content":"merhaba"}],"title":"Ayşe"}`
	profile := `{"profile_user":[{"string_map_data":{"Username":{"value":"ben"},"Name":{"value":"Ben"}}}]}`
	p1 := mkzip(map[string]string{"messages/inbox/a_1/message_1.json": thread})
	p2 := mkzip(map[string]string{"personal_information/personal_information/personal_information.json": profile})
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for i, p := range [][]byte{p1, p2} {
		fw, _ := mw.CreateFormFile("file", fmt.Sprintf("instagram-ben-%d.zip", i+1))
		fw.Write(p)
	}
	mw.Close()
	req := httptest.NewRequest("POST", "/api/import", &body)
	req.Host = "127.0.0.1:8777"
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 201 {
		t.Fatalf("çok parçalı yükleme: %d %s", rec.Code, rec.Body)
	}
	var res importResult
	json.Unmarshal(rec.Body.Bytes(), &res)
	if res.Source.OwnerName != "Ben" || res.Source.Stats["messages"] != 1 {
		t.Fatalf("iki parça birleşmeli (profil 2. parçada): %+v", res.Source)
	}
	if res.Person == nil {
		t.Fatal("profilden gelen onaylı sahip için kişi açılmalı")
	}
}

func TestImportByJSONPaths(t *testing.T) {
	_, h := newServer(t)
	p := whatsappFile(t)
	rec := do(h, "POST", "/api/import", map[string]any{"paths": []string{p}}, nil)
	if rec.Code != 201 {
		t.Fatalf("paths ile içe aktarma: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "POST", "/api/import", map[string]any{"paths": []string{}}, nil); rec.Code != 400 {
		t.Fatalf("boş liste: %d", rec.Code)
	}
}

func TestReportDownload(t *testing.T) {
	_, h := newServer(t)
	path := whatsappFile(t)
	do(h, "POST", "/api/import", map[string]string{"path": path}, nil)
	rec := do(h, "POST", "/api/people", map[string]any{"name": "Ayşe"}, nil)
	var p store.Person
	json.Unmarshal(rec.Body.Bytes(), &p)
	rec = do(h, "GET", fmt.Sprintf("/api/people/%d/report.md", p.ID), nil, nil)
	if rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/markdown") {
		t.Fatalf("rapor: %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	if !strings.Contains(rec.Header().Get("Content-Disposition"), "attachment") || !strings.Contains(rec.Body.String(), "# Ayşe — kişi dosyası raporu") {
		t.Fatalf("başlıklar/içerik: %s", rec.Body.String()[:200])
	}
	if rec := do(h, "GET", "/api/people/999/report.md", nil, nil); rec.Code != 404 {
		t.Fatalf("olmayan kişi: %d", rec.Code)
	}
}

func TestEstimateGrantsApprovalOnlyWithinCap(t *testing.T) {
	srv, h := newServer(t)
	body := map[string]any{"model": "deepseek-flash", "shared_prefix_tokens": 10000, "per_call_tokens": 500, "calls": 15, "output_tokens_per_call": 1500}
	rec := do(h, "POST", "/api/estimate", body, nil)
	if rec.Code != 200 {
		t.Fatalf("tahmin: %d %s", rec.Code, rec.Body)
	}
	var res struct {
		Allowed    bool   `json:"allowed"`
		ApprovalID string `json:"approval_id"`
		Estimate   struct {
			Known   bool    `json:"known"`
			HighUSD float64 `json:"high_usd"`
		} `json:"estimate"`
	}
	json.Unmarshal(rec.Body.Bytes(), &res)
	if !res.Allowed || res.ApprovalID == "" || !res.Estimate.Known || res.Estimate.HighUSD <= 0 {
		t.Fatalf("varsayılan tavana sığmalı: %s", rec.Body)
	}
	if _, err := srv.Approvals.Consume(res.ApprovalID); err != nil {
		t.Fatalf("onay tüketilebilmeli: %v", err)
	}
	// çok pahalı plan reddedilir, onay verilmez
	body["calls"], body["shared_prefix_tokens"], body["output_tokens_per_call"] = 200, 900000, 100000
	rec = do(h, "POST", "/api/estimate", body, nil)
	res.Allowed, res.ApprovalID = false, "" // Unmarshal, yanıtta olmayan alanları sıfırlamaz
	json.Unmarshal(rec.Body.Bytes(), &res)
	if res.Allowed || res.ApprovalID != "" {
		t.Fatalf("tavanı aşan plan reddedilmeli: %s", rec.Body)
	}
	// fiyatı bilinmeyen model onaylanmaz
	body["model"], body["calls"], body["shared_prefix_tokens"], body["output_tokens_per_call"] = "bilinmeyen-x", 1, 10, 10
	rec = do(h, "POST", "/api/estimate", body, nil)
	res.Allowed, res.ApprovalID = false, "" // Unmarshal, yanıtta olmayan alanları sıfırlamaz
	json.Unmarshal(rec.Body.Bytes(), &res)
	if res.Allowed {
		t.Fatalf("fiyatı bilinmeyen model onaylanmamalı: %s", rec.Body)
	}
	if rec := do(h, "POST", "/api/estimate", map[string]any{"calls": 0}, nil); rec.Code != 400 {
		t.Fatalf("geçersiz çağrı sayısı: %d", rec.Code)
	}
}

func TestEstimateRespectsSessionSpend(t *testing.T) {
	srv, h := newServer(t)
	srv.Store.RecordUsage(store.Usage{Session: "s1", CostUSD: 0.249})
	rec := do(h, "POST", "/api/estimate", map[string]any{"model": "deepseek-flash", "shared_prefix_tokens": 100000, "calls": 1, "output_tokens_per_call": 1000, "session": "s1"}, nil)
	if !strings.Contains(rec.Body.String(), `"allowed":false`) {
		t.Fatalf("harcanan + tahmin tavanı aşıyorsa reddedilmeli: %s", rec.Body)
	}
}

func TestLedgerEndpoint(t *testing.T) {
	srv, h := newServer(t)
	srv.Store.RecordUsage(store.Usage{Session: "s1", Model: "m", PromptTokens: 100, CostUSD: 0.05})
	rec := do(h, "GET", "/api/ledger", nil, nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"total_usd":0.05`) {
		t.Fatalf("defter: %d %s", rec.Code, rec.Body)
	}
}

func TestPersonasEndpoint(t *testing.T) {
	_, h := newServer(t)
	rec := do(h, "GET", "/api/personas", nil, nil)
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	var res struct {
		Version  string `json:"prompt_version"`
		Personas []struct {
			ID string `json:"id"`
		} `json:"personas"`
	}
	json.Unmarshal(rec.Body.Bytes(), &res)
	if len(res.Personas) != 13 || res.Personas[0].ID != "freud" || res.Version == "" {
		t.Fatalf("persona listesi: %+v", res)
	}
}

func TestRiskEndpointReportsPositionsNotText(t *testing.T) {
	_, h := newServer(t)
	p := filepath.Join(t.TempDir(), "chat.txt")
	os.WriteFile(p, []byte("26.09.2026 20:38 - Ayşe: ölüyorum gülmekten\n26.09.2026 20:39 - Ayşe: artık ölmek istiyorum\n26.09.2026 20:40 - Ben: yanındayım\n"), 0o600)
	do(h, "POST", "/api/import", map[string]string{"path": p}, nil)
	rec := do(h, "POST", "/api/people", map[string]any{"name": "Ayşe"}, nil)
	var ayse store.Person
	json.Unmarshal(rec.Body.Bytes(), &ayse)
	rec = do(h, "GET", fmt.Sprintf("/api/people/%d/risk", ayse.ID), nil, nil)
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"concern":1`) || !strings.Contains(body, `"acute":0`) {
		t.Fatalf("yalnızca 'ölmek istiyorum' işaretlenmeli, deyim değil: %s", body)
	}
	if strings.Contains(body, "ölmek istiyorum") || strings.Contains(body, "gülmekten") {
		t.Fatalf("mesaj metni yanıtta olmamalı: %s", body)
	}
	if !strings.Contains(body, "tanı değildir") {
		t.Fatalf("not eksik: %s", body)
	}
}

func TestEvidenceEndpointPreviewIsMaskedAndExcludable(t *testing.T) {
	_, h := newServer(t)
	var sb strings.Builder
	for i := 0; i < 40; i++ {
		who := "Ben"
		if i%2 == 1 {
			who = "Ayşe"
		}
		text := fmt.Sprintf("selam %d", i)
		if i == 0 {
			text = "ara beni 0532 123 45 67"
		}
		fmt.Fprintf(&sb, "26.09.2026 20:%02d - %s: %s\n", i, who, text)
	}
	p := filepath.Join(t.TempDir(), "WhatsApp Sohbeti - Ayşe.txt")
	os.WriteFile(p, []byte(sb.String()), 0o600)
	rec := do(h, "POST", "/api/import", map[string]string{"path": p}, nil)
	var imp importResult
	json.Unmarshal(rec.Body.Bytes(), &imp)
	do(h, "POST", fmt.Sprintf("/api/sources/%d/owner", imp.Source.ID), map[string]string{"name": "Ben"}, nil)
	rec = do(h, "GET", "/api/people", nil, nil)
	var people []store.Person
	json.Unmarshal(rec.Body.Bytes(), &people)
	me := people[0]
	rec = do(h, "GET", fmt.Sprintf("/api/people/%d/evidence", me.ID), nil, nil)
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	var res struct {
		Items []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"items"`
		Preview    string         `json:"preview"`
		Hash       string         `json:"hash"`
		Tokens     int            `json:"tokens"`
		MaskReport map[string]int `json:"mask_report"`
	}
	json.Unmarshal(rec.Body.Bytes(), &res)
	if len(res.Items) == 0 || res.Items[0].ID != "K1" || res.Hash == "" || res.Tokens == 0 {
		t.Fatalf("paket: %+v", res)
	}
	if strings.Contains(res.Preview, "0532 123 45 67") || res.MaskReport["TEL"] != 1 {
		t.Fatalf("önizleme maskeli olmalı: rapor=%v\n%s", res.MaskReport, res.Preview)
	}
	// ilk kesiti sepetten çıkar
	rec = do(h, "GET", fmt.Sprintf("/api/people/%d/evidence?exclude=%s", me.ID, url.QueryEscape(res.Items[0].Title)), nil, nil)
	var res2 struct {
		Items []struct{ Title string } `json:"items"`
	}
	json.Unmarshal(rec.Body.Bytes(), &res2)
	if len(res2.Items) != len(res.Items)-1 {
		t.Fatalf("çıkarılan kesit eksilmeli: %d → %d", len(res.Items), len(res2.Items))
	}
	if rec := do(h, "GET", "/api/people/999/evidence", nil, nil); rec.Code != 404 {
		t.Fatalf("olmayan kişi: %d", rec.Code)
	}
}

func TestReportHTMLAndPrintScript(t *testing.T) {
	_, h := newServer(t)
	do(h, "POST", "/api/import", map[string]string{"path": whatsappFile(t)}, nil)
	rec := do(h, "POST", "/api/people", map[string]any{"name": "Ayşe"}, nil)
	var p store.Person
	json.Unmarshal(rec.Body.Bytes(), &p)
	rec = do(h, "GET", fmt.Sprintf("/api/people/%d/report.html", p.ID), nil, nil)
	if rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") || !strings.Contains(rec.Body.String(), "<h1>Ayşe — kişi dosyası raporu</h1>") {
		t.Fatalf("html rapor: %d %.200s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Header().Get("Content-Security-Policy"), "default-src 'self'") {
		t.Fatal("CSP korunmalı; betik harici dosyadan gelmeli")
	}
	js := do(h, "GET", "/api/report-print.js", nil, nil)
	if js.Code != 200 || !strings.HasPrefix(js.Header().Get("Content-Type"), "text/javascript") || !strings.Contains(js.Body.String(), "window.print()") {
		t.Fatalf("betik: %d", js.Code)
	}
	if rec := do(h, "GET", "/api/people/999/report.html", nil, nil); rec.Code != 404 {
		t.Fatalf("olmayan kişi: %d", rec.Code)
	}
}
