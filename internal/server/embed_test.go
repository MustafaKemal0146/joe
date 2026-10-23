package server

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fakeEmbedServer(t *testing.T) *httptest.Server {
	t.Helper()
	syn := map[string]string{"sinema": "film", "dizi": "film"}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Input []string }
		json.NewDecoder(r.Body).Decode(&req)
		data := []map[string]any{}
		for i, text := range req.Input {
			v := make([]float32, 64)
			for _, w := range strings.Fields(strings.ToLower(text)) {
				if s, ok := syn[w]; ok {
					w = s
				}
				h := fnv.New32a()
				h.Write([]byte(w))
				v[h.Sum32()%64]++
			}
			data = append(data, map[string]any{"index": i, "embedding": v})
		}
		json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
}

func TestSemanticSearchEndToEnd(t *testing.T) {
	_, h := newServer(t)
	p := filepath.Join(t.TempDir(), "WhatsApp Sohbeti - Ayşe.txt")
	os.WriteFile(p, []byte("26.09.2026 20:38 - Ayşe: bu akşam film izleyelim mi\n26.09.2026 20:39 - Ben: yarın toplantı var\n26.09.2026 20:40 - Ayşe: çay demledim\n"), 0o600)
	do(h, "POST", "/api/import", map[string]string{"path": p}, nil)

	// ayar yokken
	if rec := do(h, "GET", "/api/search?q=dizi&mode=semantic", nil, nil); rec.Code != 400 {
		t.Fatalf("ayarsız semantik arama: %d", rec.Code)
	}
	if rec := do(h, "POST", "/api/embed/run", nil, nil); rec.Code != 400 {
		t.Fatalf("ayarsız dizinleme: %d", rec.Code)
	}
	srv := fakeEmbedServer(t)
	defer srv.Close()
	if rec := do(h, "PUT", "/api/settings", map[string]any{"embed_url": "ftp://x", "embed_model": "m"}, nil); rec.Code != 400 {
		t.Fatalf("geçersiz adres: %d", rec.Code)
	}
	if rec := do(h, "PUT", "/api/settings", map[string]any{"embed_url": srv.URL, "embed_model": "fake-1", "embed_key": "gizli"}, nil); rec.Code != 200 || strings.Contains(rec.Body.String(), "gizli") {
		t.Fatalf("ayar: %d %s", rec.Code, rec.Body)
	}
	// henüz dizinlenmemiş
	if rec := do(h, "GET", "/api/search?q=dizi&mode=semantic", nil, nil); rec.Code != 409 {
		t.Fatalf("dizinsiz arama 409 olmalı: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "POST", "/api/embed/run", nil, nil); rec.Code != 202 {
		t.Fatalf("dizinleme: %d %s", rec.Code, rec.Body)
	}
	for i := 0; i < 100; i++ {
		rec := do(h, "GET", "/api/embed/status", nil, nil)
		var st struct {
			Running bool `json:"running"`
			Done    int  `json:"done"`
			Total   int  `json:"total"`
		}
		json.Unmarshal(rec.Body.Bytes(), &st)
		if !st.Running && st.Done == st.Total && st.Total == 3 {
			break
		}
		if i == 99 {
			t.Fatalf("dizinleme bitmedi: %s", rec.Body)
		}
		time.Sleep(20 * time.Millisecond)
	}
	rec := do(h, "GET", "/api/search?q=dizi&mode=semantic", nil, nil)
	if rec.Code != 200 {
		t.Fatalf("semantik arama: %d %s", rec.Code, rec.Body)
	}
	var hits []struct {
		Text  string  `json:"text"`
		Score float64 `json:"score"`
	}
	json.Unmarshal(rec.Body.Bytes(), &hits)
	if len(hits) == 0 || !strings.Contains(hits[0].Text, "film") || hits[0].Score <= 0 {
		t.Fatalf("'dizi' sorgusu 'film' mesajını bulmalı (ortak sözcük yok): %s", rec.Body)
	}
	rec = do(h, "GET", "/api/search?q=film&mode=hybrid", nil, nil)
	json.Unmarshal(rec.Body.Bytes(), &hits)
	if rec.Code != 200 || len(hits) == 0 || !strings.Contains(hits[0].Text, "film") {
		t.Fatalf("karma arama: %d %s", rec.Code, rec.Body)
	}
	// ikinci dizinleme hiçbir şey yapmadan biter
	if rec := do(h, "POST", "/api/embed/run", nil, nil); rec.Code != 202 {
		t.Fatalf("yeniden dizinleme: %d", rec.Code)
	}
	if rec := do(h, "POST", "/api/embed/cancel", nil, nil); rec.Code != 409 && rec.Code != 204 {
		t.Fatalf("iptal: %d", rec.Code)
	}
	// boş sorgu
	if rec := do(h, "GET", "/api/search?q=%20&mode=semantic", nil, nil); rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("boş sorgu: %d %s", rec.Code, rec.Body)
	}
}

func TestTopicsEndpointGroupsMessagesByMeaning(t *testing.T) {
	_, h := newServer(t)
	var sb strings.Builder
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&sb, "26.09.2026 20:%02d - Ben: akşam film izleyelim sinema bileti %d\n", i, i)
		fmt.Fprintf(&sb, "27.09.2026 20:%02d - Ben: yarın kahve içelim çay demle %d\n", i, i)
		fmt.Fprintf(&sb, "27.09.2026 21:%02d - Ayşe: tamam\n", i)
	}
	p := filepath.Join(t.TempDir(), "WhatsApp Sohbeti - Ayşe.txt")
	os.WriteFile(p, []byte(sb.String()), 0o600)
	rec := do(h, "POST", "/api/import", map[string]string{"path": p}, nil)
	var imp importResult
	json.Unmarshal(rec.Body.Bytes(), &imp)
	do(h, "POST", fmt.Sprintf("/api/sources/%d/owner", imp.Source.ID), map[string]string{"name": "Ben"}, nil)
	rec = do(h, "GET", "/api/people", nil, nil)
	var people []struct {
		ID int64 `json:"id"`
	}
	json.Unmarshal(rec.Body.Bytes(), &people)
	url := fmt.Sprintf("/api/people/%d/topics", people[0].ID)

	if rec := do(h, "GET", url, nil, nil); rec.Code != 400 {
		t.Fatalf("ayarsız: %d", rec.Code)
	}
	srv := fakeEmbedServer(t)
	defer srv.Close()
	do(h, "PUT", "/api/settings", map[string]any{"embed_url": srv.URL, "embed_model": "fake-1"}, nil)
	if rec := do(h, "GET", url, nil, nil); rec.Code != 409 {
		t.Fatalf("dizinsiz: %d %s", rec.Code, rec.Body)
	}
	do(h, "POST", "/api/embed/run", nil, nil)
	for i := 0; i < 200; i++ {
		var st struct {
			Running bool `json:"running"`
			Done    int  `json:"done"`
			Total   int  `json:"total"`
		}
		json.Unmarshal(do(h, "GET", "/api/embed/status", nil, nil).Body.Bytes(), &st)
		if !st.Running && st.Done == st.Total && st.Total > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	rec = do(h, "GET", url, nil, nil)
	if rec.Code != 200 {
		t.Fatalf("konular: %d %s", rec.Code, rec.Body)
	}
	var out struct {
		Coverage struct{ Embedded, Total int } `json:"coverage"`
		Overall  []struct {
			Size     int      `json:"size"`
			Keywords []string `json:"keywords"`
			Rep      struct {
				Text string `json:"text"`
			} `json:"rep"`
		} `json:"overall"`
	}
	json.Unmarshal(rec.Body.Bytes(), &out)
	if out.Coverage.Embedded != 80 || out.Coverage.Total != 80 {
		t.Fatalf("kapsam: %+v", out.Coverage)
	}
	if len(out.Overall) < 2 {
		t.Fatalf("en az iki konu beklenir: %s", rec.Body)
	}
	joined := ""
	for _, tp := range out.Overall {
		joined += strings.Join(tp.Keywords, " ") + " | "
	}
	if !strings.Contains(joined, "film") || !strings.Contains(joined, "kahve") {
		t.Fatalf("film ve kahve konuları ayrışmalı: %s", joined)
	}
}

func fakeOllamaServer(t *testing.T) *httptest.Server {
	t.Helper()
	embedSrv := fakeEmbedServer(t) // /embeddings (OpenAI uyumlu) — Ollama'nın /v1/embeddings'ine yönlendirilir
	t.Cleanup(embedSrv.Close)
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			json.NewEncoder(w).Encode(map[string]any{"models": []map[string]any{
				{"name": "nomic-embed-text:latest", "size": 274000000, "details": map[string]any{"family": "nomic-bert"}},
				{"name": "embeddinggemma:latest", "size": 600000000, "details": map[string]any{"family": "gemma-embedding"}},
				{"name": "sohbet:9b", "size": 5000000000, "details": map[string]any{"family": "qwen"}},
			}})
		case "/api/show":
			json.NewEncoder(w).Encode(map[string]any{"capabilities": []string{}})
		case "/v1/embeddings":
			http.Redirect(w, r, embedSrv.URL+"/embeddings", http.StatusTemporaryRedirect)
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestEmbedDiscoverEndpointAndLocalOnlyGuard(t *testing.T) {
	srv, h := newServer(t)
	ol := fakeOllamaServer(t)
	defer ol.Close()
	srv.OllamaURL = ol.URL
	rec := do(h, "GET", "/api/embed/discover", nil, nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"recommended":"embeddinggemma:latest"`) || !strings.Contains(rec.Body.String(), `"verified":true`) {
		t.Fatalf("keşif: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "GET", "/api/embed/discover?url=http://example.com:11434", nil, nil); rec.Code != 400 || !strings.Contains(rec.Body.String(), "yerel") {
		t.Fatalf("uzak adres reddedilmeli: %d %s", rec.Code, rec.Body)
	}
}

func TestEmbedAutoSavesSettingsAndIndexesThenAutoIndexesNewImports(t *testing.T) {
	srv, h := newServer(t)
	ol := fakeOllamaServer(t)
	defer ol.Close()
	srv.OllamaURL = ol.URL
	// içe aktarma önce: auto-index henüz kapalı → hiçbir iş başlamaz
	p1 := filepath.Join(t.TempDir(), "WhatsApp Sohbeti - Ayşe.txt")
	os.WriteFile(p1, []byte("26.09.2026 20:38 - Ayşe: bu akşam film izleyelim mi\n26.09.2026 20:39 - Ben: yarın toplantı var\n"), 0o600)
	do(h, "POST", "/api/import", map[string]string{"path": p1}, nil)
	if st := do(h, "GET", "/api/embed/status", nil, nil).Body.String(); !strings.Contains(st, `"configured":false`) {
		t.Fatalf("ayarsızken yapılandırılmamış: %s", st)
	}
	rec := do(h, "POST", "/api/embed/auto", nil, nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"model":"embeddinggemma:latest"`) || !strings.Contains(rec.Body.String(), `"indexing_started":true`) {
		t.Fatalf("otomatik kurulum: %d %s", rec.Code, rec.Body)
	}
	srv.WaitEmbed()
	set := do(h, "GET", "/api/settings", nil, nil).Body.String()
	if !strings.Contains(set, `"auto":true`) || !strings.Contains(set, ol.URL+"/v1") {
		t.Fatalf("ayarlar kaydedilmeli: %s", set)
	}
	var st struct{ Done, Total int }
	json.Unmarshal(do(h, "GET", "/api/embed/status", nil, nil).Body.Bytes(), &st)
	if st.Total != 2 || st.Done != 2 {
		t.Fatalf("mevcut mesajlar gömülmeli: %+v", st)
	}
	// yeni içe aktarma: otomatik dizinleme tetiklenir
	p2 := filepath.Join(t.TempDir(), "WhatsApp Sohbeti - Can.txt")
	os.WriteFile(p2, []byte("27.09.2026 10:00 - Can: kahve içelim mi bugün\n27.09.2026 10:01 - Ben: olur çay da var\n"), 0o600)
	do(h, "POST", "/api/import", map[string]string{"path": p2}, nil)
	srv.WaitEmbed()
	json.Unmarshal(do(h, "GET", "/api/embed/status", nil, nil).Body.Bytes(), &st)
	if st.Total != 4 || st.Done != 4 {
		t.Fatalf("yeni içe aktarma otomatik dizinlenmeli: %+v", st)
	}
}

func TestAutoIndexNeverRunsAgainstRemoteEmbeddingServer(t *testing.T) {
	srv, h := newServer(t)
	do(h, "PUT", "/api/settings", map[string]any{"embed_url": "https://embeddings.example.com/v1", "embed_model": "m", "embed_auto": true}, nil)
	p := filepath.Join(t.TempDir(), "WhatsApp Sohbeti - Ayşe.txt")
	os.WriteFile(p, []byte("26.09.2026 20:38 - Ayşe: merhaba nasılsın\n26.09.2026 20:39 - Ben: iyiyim sen\n"), 0o600)
	do(h, "POST", "/api/import", map[string]string{"path": p}, nil)
	srv.WaitEmbed()
	var st struct {
		Running bool
		Done    int
	}
	json.Unmarshal(do(h, "GET", "/api/embed/status", nil, nil).Body.Bytes(), &st)
	if st.Running || st.Done != 0 {
		t.Fatalf("uzak sunucuya kullanıcı fark etmeden mesaj gönderilmemeli: %+v", st)
	}
}

func TestEmbedAutoFailsClearlyWhenNothingToUse(t *testing.T) {
	srv, h := newServer(t)
	srv.OllamaURL = "http://127.0.0.1:1"
	rec := do(h, "POST", "/api/embed/auto", nil, nil)
	if rec.Code != 409 || !strings.Contains(rec.Body.String(), "Ollama") {
		t.Fatalf("açıklayıcı hata: %d %s", rec.Code, rec.Body)
	}
}
