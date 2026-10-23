// Package embed, mesajların vektör temsilini çıkarır ve anlamsal arama yapar. Gömme için
// OpenAI uyumlu bir /embeddings uç noktası (yerel bir bge-m3 sunucusu ya da ucuz bir API)
// kullanılır; sohbet modeli olarak yerel model gerekmez. Vektörler SQLite'ta, arama Go'da yapılır.
package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/MustafaKemal0146/joe/internal/store"
)

// Client OpenAI uyumlu bir /embeddings istemcisidir.
type Client struct {
	BaseURL string
	APIKey  string
	Model   string
	HTTP    *http.Client
}

// New varsayılan zaman aşımıyla bir istemci üretir.
func New(baseURL, apiKey, model string) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), APIKey: apiKey, Model: model, HTTP: &http.Client{Timeout: 60 * time.Second}}
}

// maxInputChars tek girdinin gönderilen en çok karakteri; çok uzun mesajlar sağlayıcı sınırına takılmasın.
const maxInputChars = 2000

// Normalize vektörü L2 birim uzunluğa getirir; böylece kosinüs benzerliği nokta çarpımına iner.
func Normalize(v []float32) []float32 {
	var n float64
	for _, f := range v {
		n += float64(f) * float64(f)
	}
	if n == 0 {
		return v
	}
	inv := float32(1 / math.Sqrt(n))
	out := make([]float32, len(v))
	for i, f := range v {
		out[i] = f * inv
	}
	return out
}

// Embed metinlerin normalize edilmiş vektörlerini döndürür; sayı ve boyut tutarlılığı doğrulanır.
func (c *Client) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	in := make([]string, len(texts))
	for i, t := range texts {
		r := []rune(t)
		if len(r) > maxInputChars {
			t = string(r[:maxInputChars])
		}
		in[i] = t
	}
	body, _ := json.Marshal(map[string]any{"model": c.Model, "input": in})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gömme sunucusuna ulaşılamadı: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		msg := strings.Join(strings.Fields(string(raw)), " ")
		if len(msg) > 200 {
			msg = msg[:200] + "…"
		}
		return nil, fmt.Errorf("gömme sunucusu HTTP %d döndürdü: %s", resp.StatusCode, msg)
	}
	var parsed struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("gömme yanıtı okunamadı: %w", err)
	}
	if len(parsed.Data) != len(texts) {
		return nil, fmt.Errorf("gömme yanıtında %d vektör var, %d bekleniyordu", len(parsed.Data), len(texts))
	}
	sort.Slice(parsed.Data, func(i, j int) bool { return parsed.Data[i].Index < parsed.Data[j].Index })
	out := make([][]float32, len(texts))
	dim := 0
	for i, d := range parsed.Data {
		if len(d.Embedding) == 0 {
			return nil, errors.New("gömme yanıtında boş vektör")
		}
		if dim == 0 {
			dim = len(d.Embedding)
		} else if len(d.Embedding) != dim {
			return nil, errors.New("gömme yanıtında tutarsız vektör boyutu")
		}
		out[i] = Normalize(d.Embedding)
	}
	return out, nil
}

// Progress dizinleme ilerlemesidir.
type Progress struct{ Done, Total int }

// Index vektörü olmayan mesajları parti parti gömer. Her parti anında kaydedilir; iptal ya da hata
// sonrası yeniden çağrıldığında yalnızca eksikler yapılır.
func Index(ctx context.Context, st *store.Store, c *Client, batch int, progress func(Progress)) error {
	if batch <= 0 {
		batch = 64
	}
	total, done, err := st.EmbedStats(c.Model)
	if err != nil {
		return err
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		pend, err := st.PendingEmbeddings(c.Model, batch)
		if err != nil {
			return err
		}
		if len(pend) == 0 {
			if progress != nil {
				progress(Progress{Done: total, Total: total})
			}
			return nil
		}
		texts := make([]string, len(pend))
		ids := make([]int64, len(pend))
		for i, p := range pend {
			texts[i], ids[i] = p.Text, p.ID
		}
		vecs, err := c.Embed(ctx, texts)
		if err != nil {
			return err
		}
		if err := st.SaveEmbeddings(c.Model, ids, vecs); err != nil {
			return err
		}
		done += len(pend)
		if progress != nil {
			progress(Progress{Done: done, Total: total})
		}
	}
}

// Hit bir anlamsal arama sonucudur.
type Hit struct {
	ID    int64
	Score float32
}

// Search sorgu vektörüne en yakın topK mesajı kaba kuvvetle bulur (vektörler normalize).
func Search(idx *store.VectorIndex, q []float32, topK int, minScore float32) []Hit {
	if idx == nil || len(idx.IDs) == 0 || len(q) != idx.Dim {
		return nil
	}
	q = Normalize(q)
	hits := make([]Hit, 0, topK+1)
	for i, v := range idx.Vecs {
		var dot float32
		for j := range v {
			dot += v[j] * q[j]
		}
		if dot < minScore {
			continue
		}
		if len(hits) < topK || dot > hits[len(hits)-1].Score {
			hits = append(hits, Hit{idx.IDs[i], dot})
			sort.Slice(hits, func(a, b int) bool { return hits[a].Score > hits[b].Score })
			if len(hits) > topK {
				hits = hits[:topK]
			}
		}
	}
	return hits
}

// Fuse iki sıralı kimlik listesini karşılıklı sıra füzyonuyla (RRF) birleştirir.
func Fuse(a, b []int64, limit int) []int64 {
	const k = 60.0
	score := map[int64]float64{}
	for r, id := range a {
		score[id] += 1 / (k + float64(r+1))
	}
	for r, id := range b {
		score[id] += 1 / (k + float64(r+1))
	}
	ids := make([]int64, 0, len(score))
	for id := range score {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		if score[ids[i]] != score[ids[j]] {
			return score[ids[i]] > score[ids[j]]
		}
		return ids[i] < ids[j]
	})
	if len(ids) > limit {
		ids = ids[:limit]
	}
	return ids
}

// Relative en iyi sonuca göre anlamlı yakınlıktaki sonuçları tutar: skor ≥ en iyi − margin. Bazı modellerde
// (ör. EmbeddingGemma) alakasız metinler bile yüksek kosinüs (0,6–0,8) alır; mutlak eşik bu yüzden anlamsızdır.
// En az min sonuç (varsa) her zaman korunur. hits skora göre azalan sıralı olmalıdır.
func Relative(hits []Hit, margin float32, min int) []Hit {
	if len(hits) == 0 {
		return hits
	}
	cut := hits[0].Score - margin
	out := hits[:0:0]
	for i, h := range hits {
		if h.Score >= cut || i < min {
			out = append(out, h)
		}
	}
	return out
}
