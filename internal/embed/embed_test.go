package embed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MustafaKemal0146/joe/internal/model"
	"github.com/MustafaKemal0146/joe/internal/store"
)

var synonyms = map[string]string{"sinema": "film", "filmi": "film", "dizi": "film", "kahve": "kahve", "çay": "kahve"}

// fakeServer deterministik bir torba-sözcük gömmesi üretir; eşanlamlılar aynı kovaya düşer.
func fakeServer(t *testing.T, calls *atomic.Int32, mutate func(map[string]any)) *httptest.Server {
	t.Helper()
	const dim = 64
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embeddings" || r.Method != "POST" {
			http.NotFound(w, r)
			return
		}
		if calls != nil {
			calls.Add(1)
		}
		var req struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		data := []map[string]any{}
		for i, text := range req.Input {
			v := make([]float32, dim)
			for _, w := range strings.Fields(strings.ToLower(text)) {
				if s, ok := synonyms[w]; ok {
					w = s
				}
				h := fnv.New32a()
				h.Write([]byte(w))
				v[h.Sum32()%dim]++
			}
			data = append(data, map[string]any{"index": i, "embedding": v})
		}
		out := map[string]any{"data": data}
		if mutate != nil {
			mutate(out)
		}
		json.NewEncoder(w).Encode(out)
	}))
}

func TestEmbedNormalizesAndKeepsOrder(t *testing.T) {
	srv := fakeServer(t, nil, nil)
	defer srv.Close()
	c := New(srv.URL, "", "m")
	vs, err := c.Embed(context.Background(), []string{"film izleyelim", "kahve içelim"})
	if err != nil || len(vs) != 2 {
		t.Fatalf("%v %d", err, len(vs))
	}
	for _, v := range vs {
		var n float64
		for _, f := range v {
			n += float64(f) * float64(f)
		}
		if math.Abs(n-1) > 1e-5 {
			t.Fatalf("vektör normalize olmalı: %v", n)
		}
	}
}

func TestEmbedValidatesResponse(t *testing.T) {
	for name, mut := range map[string]func(map[string]any){
		"eksik vektör": func(o map[string]any) { o["data"] = o["data"].([]map[string]any)[:1] },
		"boş vektör":   func(o map[string]any) { o["data"].([]map[string]any)[0]["embedding"] = []float32{} },
		"boyut farkı": func(o map[string]any) {
			o["data"].([]map[string]any)[1]["embedding"] = []float32{1, 2, 3}
		},
	} {
		srv := fakeServer(t, nil, mut)
		_, err := New(srv.URL, "", "m").Embed(context.Background(), []string{"a b", "c d"})
		srv.Close()
		if err == nil {
			t.Errorf("%s reddedilmeliydi", name)
		}
	}
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "kota doldu", 429) }))
	defer bad.Close()
	if _, err := New(bad.URL, "", "m").Embed(context.Background(), []string{"x"}); err == nil || !strings.Contains(err.Error(), "429") {
		t.Fatalf("HTTP hatası bildirilmeli: %v", err)
	}
	if _, err := New("http://127.0.0.1:1", "", "m").Embed(context.Background(), []string{"x"}); err == nil {
		t.Fatal("ulaşılamayan sunucu hata vermeli")
	}
}

func TestEmbedSendsAuthAndTruncatesLongInput(t *testing.T) {
	var gotAuth string
	var gotLen int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		var req struct{ Input []string }
		json.NewDecoder(r.Body).Decode(&req)
		gotLen = len([]rune(req.Input[0]))
		fmt.Fprint(w, `{"data":[{"index":0,"embedding":[1,0]}]}`)
	}))
	defer srv.Close()
	New(srv.URL, "anahtar", "m").Embed(context.Background(), []string{strings.Repeat("ş", 5000)})
	if gotAuth != "Bearer anahtar" || gotLen != maxInputChars {
		t.Fatalf("auth=%q uzunluk=%d", gotAuth, gotLen)
	}
}

func seed(t *testing.T) *store.Store {
	t.Helper()
	st, _ := store.Open(":memory:")
	t.Cleanup(func() { st.Close() })
	var msgs []model.Message
	ts := time.Now()
	for i, text := range []string{"bu akşam film izleyelim mi", "olur sinema güzel olur", "yarın kahve içelim", "toplantı saat kaçta", "çay demledim gel", "tamam", "ok"} {
		msgs = append(msgs, model.Message{Sender: "Ben", TS: ts.Add(time.Duration(i) * time.Minute), Kind: model.KindText, Text: text})
	}
	msgs = append(msgs, model.Message{Sender: "Ben", TS: ts, Kind: model.KindMedia})
	imp := &model.Import{Platform: "whatsapp", Label: "x", OwnerName: "Ben", Stats: map[string]int{},
		Conversations: []model.Conversation{{Title: "A", Platform: "whatsapp", Participants: []string{"Ben", "A"}, Messages: msgs}}}
	if _, err := st.SaveImport(imp, "h"); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestIndexIsResumableAndSkipsShortAndMedia(t *testing.T) {
	st := seed(t)
	var calls atomic.Int32
	srv := fakeServer(t, &calls, nil)
	defer srv.Close()
	c := New(srv.URL, "", "m1")
	total, _, _ := st.EmbedStats("m1")
	if total != 6 { // 7 metin mesajından 2 harflik "ok" ve medya mesajı sayılmaz
		t.Fatalf("gömülebilir mesaj sayısı %d", total)
	}
	// İlk çalışma: ikinci partiden önce iptal
	ctx, cancel := context.WithCancel(context.Background())
	err := Index(ctx, st, c, 2, func(p Progress) {
		if p.Done >= 2 {
			cancel()
		}
	})
	if err == nil {
		t.Fatal("iptal hata döndürmeli")
	}
	_, done, _ := st.EmbedStats("m1")
	if done != 2 {
		t.Fatalf("ilk parti kaydedilmiş olmalı: %d", done)
	}
	before := calls.Load()
	var last Progress
	if err := Index(context.Background(), st, c, 2, func(p Progress) { last = p }); err != nil {
		t.Fatal(err)
	}
	if got := calls.Load() - before; got != 2 { // kalan 4 mesaj, parti 2: 2 çağrı
		t.Fatalf("devamda yalnızca eksikler gömülmeli: %d çağrı", got)
	}
	if last.Done != last.Total || last.Total != 6 {
		t.Fatalf("ilerleme: %+v", last)
	}
	// Üçüncü çalışma hiçbir çağrı yapmaz
	before = calls.Load()
	Index(context.Background(), st, c, 2, nil)
	if calls.Load() != before {
		t.Fatal("hepsi gömülüyken çağrı yapılmamalı")
	}
	// farklı model ayrı dizin sayılır
	if _, d2, _ := st.EmbedStats("m2"); d2 != 0 {
		t.Fatal("başka model için vektör olmamalı")
	}
}

func TestSemanticSearchFindsSynonymsWithoutSharedWords(t *testing.T) {
	st := seed(t)
	srv := fakeServer(t, nil, nil)
	defer srv.Close()
	c := New(srv.URL, "", "m1")
	if err := Index(context.Background(), st, c, 64, nil); err != nil {
		t.Fatal(err)
	}
	idx, err := st.LoadVectors("m1")
	if err != nil || len(idx.IDs) != 6 || idx.Dim != 64 {
		t.Fatalf("%v %+v", err, idx)
	}
	q, _ := c.Embed(context.Background(), []string{"dizi"}) // "dizi" → "film" kovası
	hits := Search(idx, q[0], 2, 0.1)
	if len(hits) == 0 {
		t.Fatal("sonuç bekleniyordu")
	}
	msgs, _ := st.MessagesByIDs([]int64{hits[0].ID})
	if !strings.Contains(msgs[hits[0].ID].Text, "film") {
		t.Fatalf("en yakın mesaj film içermeli: %q (skor %.2f)", msgs[hits[0].ID].Text, hits[0].Score)
	}
	if len(Search(idx, q[0], 5, 0.99)) > 1 {
		t.Fatal("eşik filtrelemeli")
	}
	if Search(idx, []float32{1, 2}, 3, 0) != nil {
		t.Fatal("boyut uyuşmazlığında sonuç dönmemeli")
	}
	if Search(nil, q[0], 3, 0) != nil {
		t.Fatal("boş dizin")
	}
}

func TestFuseRewardsAgreement(t *testing.T) {
	got := Fuse([]int64{1, 2, 3}, []int64{3, 4, 1}, 10)
	if got[0] != 1 && got[0] != 3 {
		t.Fatalf("iki listede de olan önce gelmeli: %v", got)
	}
	if len(got) != 4 {
		t.Fatalf("tekilleştirme: %v", got)
	}
	if len(Fuse([]int64{1, 2, 3}, nil, 2)) != 2 {
		t.Fatal("limit uygulanmalı")
	}
}

func TestRelativeKeepsOnlyScoresNearTheBest(t *testing.T) {
	hits := []Hit{{1, 0.85}, {2, 0.82}, {3, 0.78}, {4, 0.74}, {5, 0.63}}
	got := Relative(hits, 0.10, 2)
	if len(got) != 3 || got[2].ID != 3 {
		t.Fatalf("0.10 payı içindekiler kalmalı: %+v", got)
	}
	if got := Relative(hits, 0.01, 4); len(got) != 4 {
		t.Fatalf("en az min sonuç korunmalı: %+v", got)
	}
	if got := Relative(nil, 0.1, 3); len(got) != 0 {
		t.Fatal("boş girdi")
	}
	if got := Relative(hits[:1], 0.1, 3); len(got) != 1 {
		t.Fatal("tek sonuç")
	}
}

type fakeOllama struct {
	models   []map[string]any
	caps     map[string][]string
	embedOK  bool
	requests []string
}

func (f *fakeOllama) server(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requests = append(f.requests, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/api/tags":
			json.NewEncoder(w).Encode(map[string]any{"models": f.models})
		case "/api/show":
			var req struct{ Model string }
			json.NewDecoder(r.Body).Decode(&req)
			json.NewEncoder(w).Encode(map[string]any{"capabilities": f.caps[req.Model]})
		case "/v1/embeddings":
			if !f.embedOK {
				http.Error(w, "model yüklenemedi", 500)
				return
			}
			fmt.Fprint(w, `{"data":[{"index":0,"embedding":[0.1,0.2,0.3,0.4]}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
}

func model1(name, family string, size int64) map[string]any {
	return map[string]any{"name": name, "size": size, "details": map[string]any{"family": family, "parameter_size": "300M"}}
}

func TestDiscoverPicksBestEmbeddingModelAndVerifiesIt(t *testing.T) {
	f := &fakeOllama{embedOK: true,
		models: []map[string]any{
			model1("ornith:9b", "qwen35", 5_600_000_000),
			model1("nomic-embed-text:latest", "nomic-bert", 274_000_000),
			model1("embeddinggemma-2:latest", "gemma-embedding2", 1_285_000_000),
			model1("deepseek-v4-flash:cloud", "", 0),
			model1("yeni-ozel-model:latest", "bert", 100_000_000),
		},
		caps: map[string][]string{"ornith:9b": {"completion"}, "yeni-ozel-model:latest": {"embedding"}}}
	srv := f.server(t)
	defer srv.Close()
	d, err := Discover(context.Background(), srv.URL)
	if err != nil || !d.Reachable {
		t.Fatalf("%v %+v", err, d)
	}
	if d.Recommended != "embeddinggemma-2:latest" || !d.Verified || d.Dim != 4 || d.OpenAIURL != srv.URL+"/v1" {
		t.Fatalf("öneri: %+v", d)
	}
	names := []string{}
	for _, c := range d.Candidates {
		names = append(names, c.Name)
	}
	if strings.Join(names, ",") != "embeddinggemma-2:latest,nomic-embed-text:latest,yeni-ozel-model:latest" {
		t.Fatalf("adaylar tercih sırasında olmalı, sohbet ve bulut modelleri dışarıda: %v", names)
	}
	// /v1 ile verilen adres de kabul edilir
	if d2, err := Discover(context.Background(), srv.URL+"/v1/"); err != nil || d2.BaseURL != srv.URL {
		t.Fatalf("/v1 soneki temizlenmeli: %+v %v", d2, err)
	}
}

func TestDiscoverHintsWhenNoModelOrBrokenOrUnreachable(t *testing.T) {
	f := &fakeOllama{models: []map[string]any{model1("ornith:9b", "qwen35", 5_000_000_000)}}
	srv := f.server(t)
	d, err := Discover(context.Background(), srv.URL)
	srv.Close()
	if err != nil || !d.Reachable || d.Recommended != "" || !strings.Contains(d.Hint, "ollama pull") {
		t.Fatalf("gömme modeli yoksa indirme ipucu verilmeli: %+v", d)
	}
	f = &fakeOllama{embedOK: false, models: []map[string]any{model1("bge-m3:latest", "bert", 1_200_000_000)}}
	srv = f.server(t)
	d, _ = Discover(context.Background(), srv.URL)
	srv.Close()
	if d.Recommended != "bge-m3:latest" || d.Verified || !strings.Contains(d.Hint, "başarısız") {
		t.Fatalf("doğrulama başarısızsa Verified=false ve açıklama olmalı: %+v", d)
	}
	d, err = Discover(context.Background(), "http://127.0.0.1:1")
	if err != nil || d.Reachable || d.Hint == "" {
		t.Fatalf("ulaşılamayan Ollama hata değil ipucu olmalı: %+v %v", d, err)
	}
	notOllama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("<html>başka sunucu</html>")) }))
	defer notOllama.Close()
	if d, _ := Discover(context.Background(), notOllama.URL); d.Reachable {
		t.Fatal("Ollama olmayan sunucu bulunmuş sayılmamalı")
	}
}

func TestDiscoverRefusesNonLocalAddresses(t *testing.T) {
	for _, u := range []string{"http://example.com:11434", "http://192.168.1.20:11434", "http://[2001:db8::1]:11434", "ftp://127.0.0.1", "file:///etc/passwd", "http://evil.localhost.example.com"} {
		if _, err := Discover(context.Background(), u); !errors.Is(err, ErrNotLocal) {
			t.Errorf("%q reddedilmeliydi: %v", u, err)
		}
	}
	for _, u := range []string{"http://127.0.0.1:11434", "http://localhost:11434/v1", "http://[::1]:11434"} {
		if !IsLoopbackURL(u) {
			t.Errorf("%q yerel sayılmalı", u)
		}
	}
}
