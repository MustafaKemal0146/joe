package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Candidate Ollama'da bulunan, gömme için kullanılabilir bir modeldir.
type Candidate struct {
	Name   string `json:"name"`
	Family string `json:"family"`
	Params string `json:"params,omitempty"`
	SizeMB int    `json:"size_mb"`
	Score  int    `json:"score"`
	Reason string `json:"reason"`
}

// Discovery Ollama taramasının sonucudur.
type Discovery struct {
	Reachable   bool        `json:"reachable"`
	BaseURL     string      `json:"base_url"`   // http://127.0.0.1:11434
	OpenAIURL   string      `json:"openai_url"` // http://127.0.0.1:11434/v1
	Candidates  []Candidate `json:"candidates"`
	Recommended string      `json:"recommended"`
	Dim         int         `json:"dim,omitempty"`
	Verified    bool        `json:"verified"`
	Hint        string      `json:"hint,omitempty"`
}

// DefaultOllama varsayılan yerel Ollama adresidir.
const DefaultOllama = "http://127.0.0.1:11434"

// ErrNotLocal otomatik keşfin yalnızca yerel (loopback) Ollama için yapıldığını bildirir: uzak adreslere
// keşif isteği atmak SSRF yüzeyi açar ve mesaj metinlerini istemeden dışarı yollayabilir.
var ErrNotLocal = errors.New("otomatik keşif yalnızca yerel Ollama için çalışır (127.0.0.1 / localhost)")

// IsLoopbackURL adresin loopback bir konuma işaret edip etmediğini söyler.
func IsLoopbackURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	h := u.Hostname()
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

// Gömme modelleri için tercih sırası (Türkçe başarısı ve çok dillilik öne alınır). Adın içinde geçen ipucuna göre.
var preference = []struct {
	hint  string
	score int
	why   string
}{
	{"embeddinggemma", 100, "çok dilli, küçük ve hızlı"},
	{"bge-m3", 95, "çok dilli, uzun bağlam"},
	{"multilingual-e5", 90, "çok dilli"},
	{"snowflake-arctic-embed2", 85, "çok dilli"},
	{"jina-embeddings-v3", 80, "çok dilli"},
	{"mxbai-embed", 60, "ağırlıklı İngilizce"},
	{"nomic-embed", 50, "ağırlıklı İngilizce"},
	{"all-minilm", 40, "İngilizce, küçük"},
	{"embed", 30, "gömme modeli"},
	{"bge", 30, "gömme modeli"},
	{"gte", 30, "gömme modeli"},
	{"e5", 30, "gömme modeli"},
}

var embedFamilies = []string{"bert", "nomic-bert", "gemma-embedding", "gemma3-embedding", "embeddinggemma", "xlm-roberta", "jina-bert"}

func looksLikeEmbedding(name, family string, caps []string) (bool, int, string) {
	low := strings.ToLower(name)
	score, why := 0, ""
	for _, p := range preference {
		if strings.Contains(low, p.hint) {
			score, why = p.score, p.why
			break
		}
	}
	for _, c := range caps {
		if c == "embedding" {
			if score == 0 {
				score, why = 35, "Ollama bu modeli gömme yeteneğli olarak bildiriyor"
			}
			return true, score, why
		}
	}
	if score > 0 {
		return true, score, why
	}
	fl := strings.ToLower(family)
	for _, f := range embedFamilies {
		if strings.Contains(fl, f) {
			return true, 25, "model ailesi gömme modeli gibi görünüyor"
		}
	}
	return false, 0, ""
}

// Discover yerel Ollama'yı tarar, gömme modellerini bulur, en uygununu önerir ve (varsa) çalıştığını
// tek bir gerçek istekle doğrular. Ollama'ya ulaşılamazsa hata değil Reachable=false döner.
func Discover(ctx context.Context, raw string) (Discovery, error) {
	if raw == "" {
		raw = DefaultOllama
	}
	raw = strings.TrimRight(raw, "/")
	raw = strings.TrimSuffix(raw, "/v1")
	if !IsLoopbackURL(raw) {
		return Discovery{}, ErrNotLocal
	}
	d := Discovery{BaseURL: raw, OpenAIURL: raw + "/v1", Candidates: []Candidate{}}
	cl := &http.Client{Timeout: 4 * time.Second}

	var tags struct {
		Models []struct {
			Name    string `json:"name"`
			Size    int64  `json:"size"`
			Details struct {
				Family string `json:"family"`
				Params string `json:"parameter_size"`
			} `json:"details"`
		} `json:"models"`
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, raw+"/api/tags", nil)
	resp, err := cl.Do(req)
	if err != nil {
		d.Hint = "Ollama'ya ulaşılamadı. Çalıştığından emin ol (`ollama serve`) ve adresi kontrol et."
		return d, nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&tags) != nil {
		d.Hint = "Bu adres bir Ollama sunucusu gibi yanıt vermedi."
		return d, nil
	}
	d.Reachable = true
	for _, m := range tags.Models {
		if m.Size == 0 { // bulut modelleri yerelde gömme için kullanılamaz
			continue
		}
		var caps []string
		body, _ := json.Marshal(map[string]string{"model": m.Name})
		sreq, _ := http.NewRequestWithContext(ctx, http.MethodPost, raw+"/api/show", bytes.NewReader(body))
		sreq.Header.Set("Content-Type", "application/json")
		if sresp, err := cl.Do(sreq); err == nil {
			var show struct {
				Capabilities []string `json:"capabilities"`
			}
			_ = json.NewDecoder(io.LimitReader(sresp.Body, 4<<20)).Decode(&show)
			sresp.Body.Close()
			caps = show.Capabilities
		}
		if ok, score, why := looksLikeEmbedding(m.Name, m.Details.Family, caps); ok {
			d.Candidates = append(d.Candidates, Candidate{Name: m.Name, Family: m.Details.Family, Params: m.Details.Params, SizeMB: int(m.Size / 1e6), Score: score, Reason: why})
		}
	}
	sort.SliceStable(d.Candidates, func(i, j int) bool {
		if d.Candidates[i].Score != d.Candidates[j].Score {
			return d.Candidates[i].Score > d.Candidates[j].Score
		}
		return d.Candidates[i].Name < d.Candidates[j].Name
	})
	if len(d.Candidates) == 0 {
		d.Hint = "Ollama'da gömme modeli bulunamadı. Örneğin `ollama pull embeddinggemma` ya da `ollama pull bge-m3` ile indir."
		return d, nil
	}
	d.Recommended = d.Candidates[0].Name
	vecs, err := New(d.OpenAIURL, "", d.Recommended).Embed(ctx, []string{"deneme"})
	if err != nil || len(vecs) != 1 {
		d.Hint = fmt.Sprintf("%s bulundu ama gömme isteği başarısız oldu: %v", d.Recommended, err)
		return d, nil
	}
	d.Verified, d.Dim = true, len(vecs[0])
	return d, nil
}
