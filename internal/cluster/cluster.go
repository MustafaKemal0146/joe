// Package cluster, gömülmüş mesajları deterministik k-means ile konulara ayırır: her küme için
// boyutu, merkeze en yakın temsilci mesajı ve ayırt edici anahtar sözcükleri verir. LLM kullanmaz.
package cluster

import (
	"sort"

	"github.com/MustafaKemal0146/joe/internal/analytics"
	"github.com/MustafaKemal0146/joe/internal/embed"
	"github.com/MustafaKemal0146/joe/internal/store"
)

// Topic bir konu kümesidir.
type Topic struct {
	Size     int      `json:"size"`
	Share    float64  `json:"share"`
	RepID    int64    `json:"rep_id"`
	Keywords []string `json:"keywords"`
}

func dot(a, b []float32) float32 {
	var s float32
	for i := range a {
		s += a[i] * b[i]
	}
	return s
}

// Topics verilen mesajları (idx'te vektörü olanlar) k konuya ayırır. Sonuç, aynı girdiyle her zaman aynıdır:
// başlangıç merkezleri en uzak-nokta yöntemiyle, ilk mesajdan başlayarak seçilir.
func Topics(idx *store.VectorIndex, ids []int64, texts map[int64]string, k int) []Topic {
	pos := make(map[int64]int, len(idx.IDs))
	for i, id := range idx.IDs {
		pos[id] = i
	}
	var items []int64
	var vecs [][]float32
	for _, id := range ids {
		if p, ok := pos[id]; ok {
			items = append(items, id)
			vecs = append(vecs, idx.Vecs[p])
		}
	}
	n := len(items)
	if n == 0 || k < 1 {
		return nil
	}
	if k > n {
		k = n
	}
	// en uzak-nokta başlangıcı
	centers := [][]float32{vecs[0]}
	for len(centers) < k {
		best, bestD := -1, float32(2)
		for i, v := range vecs {
			maxSim := float32(-2)
			for _, c := range centers {
				if s := dot(v, c); s > maxSim {
					maxSim = s
				}
			}
			if maxSim < bestD {
				best, bestD = i, maxSim
			}
		}
		centers = append(centers, vecs[best])
	}
	assign := make([]int, n)
	for iter := 0; iter < 20; iter++ {
		changed := false
		for i, v := range vecs {
			bi, bs := 0, float32(-2)
			for ci, c := range centers {
				if s := dot(v, c); s > bs {
					bi, bs = ci, s
				}
			}
			if assign[i] != bi || iter == 0 {
				changed = changed || assign[i] != bi
				assign[i] = bi
			}
		}
		next := make([][]float32, k)
		for ci := range next {
			next[ci] = make([]float32, idx.Dim)
		}
		cnt := make([]int, k)
		for i, v := range vecs {
			cnt[assign[i]]++
			for j := range v {
				next[assign[i]][j] += v[j]
			}
		}
		for ci := range next {
			if cnt[ci] == 0 {
				next[ci] = centers[ci]
				continue
			}
			next[ci] = embed.Normalize(next[ci])
		}
		centers = next
		if !changed && iter > 0 {
			break
		}
	}

	// genel sözcük sıklığı (ayırt edicilik için)
	global := map[string]int{}
	totalTok := 0
	for _, id := range items {
		for _, w := range analytics.Tokens(texts[id]) {
			global[w]++
			totalTok++
		}
	}
	out := make([]Topic, 0, k)
	for ci := 0; ci < k; ci++ {
		var members []int
		for i := range items {
			if assign[i] == ci {
				members = append(members, i)
			}
		}
		if len(members) == 0 {
			continue
		}
		rep, repS := members[0], float32(-2)
		local := map[string]int{}
		localTok := 0
		for _, i := range members {
			if s := dot(vecs[i], centers[ci]); s > repS || (s == repS && items[i] < items[rep]) {
				rep, repS = i, s
			}
			for _, w := range analytics.Tokens(texts[items[i]]) {
				local[w]++
				localTok++
			}
		}
		type kw struct {
			w    string
			lift float64
			n    int
		}
		var kws []kw
		for w, c := range local {
			if c < 2 && len(members) > 3 {
				continue
			}
			lift := (float64(c) / float64(localTok)) / (float64(global[w]) / float64(totalTok))
			kws = append(kws, kw{w, lift * float64(c), c})
		}
		sort.Slice(kws, func(a, b int) bool {
			if kws[a].lift != kws[b].lift {
				return kws[a].lift > kws[b].lift
			}
			return kws[a].w < kws[b].w
		})
		t := Topic{Size: len(members), Share: float64(len(members)) / float64(n), RepID: items[rep]}
		for i := 0; i < len(kws) && i < 5; i++ {
			t.Keywords = append(t.Keywords, kws[i].w)
		}
		out = append(out, t)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Size != out[j].Size {
			return out[i].Size > out[j].Size
		}
		return out[i].RepID < out[j].RepID
	})
	return out
}

// KFor mesaj sayısına göre makul bir küme sayısı seçer (en çok max).
func KFor(n, max int) int {
	k := n / 40
	if k < 1 {
		k = 1
	}
	if k > max {
		k = max
	}
	return k
}
