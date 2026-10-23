package cluster

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/MustafaKemal0146/joe/internal/embed"
	"github.com/MustafaKemal0146/joe/internal/store"
)

// iki iyi ayrışmış küme: ilk eksende "film" mesajları, ikinci eksende "kahve" mesajları
func fixture() (*store.VectorIndex, []int64, map[int64]string) {
	idx := &store.VectorIndex{Dim: 4}
	texts := map[int64]string{}
	var ids []int64
	add := func(id int64, v []float32, text string) {
		idx.IDs = append(idx.IDs, id)
		idx.Vecs = append(idx.Vecs, embed.Normalize(v))
		texts[id] = text
		ids = append(ids, id)
	}
	for i := 0; i < 12; i++ {
		add(int64(i+1), []float32{1, 0.1 * float32(i%3), 0, 0}, "akşam film izleyelim sinema bileti")
	}
	for i := 0; i < 8; i++ {
		add(int64(100+i), []float32{0, 0, 1, 0.1 * float32(i%2)}, "yarın kahve içelim kafe randevu")
	}
	return idx, ids, texts
}

func TestTopicsSeparatesClustersWithKeywordsAndRepresentative(t *testing.T) {
	idx, ids, texts := fixture()
	topics := Topics(idx, ids, texts, 2)
	if len(topics) != 2 || topics[0].Size != 12 || topics[1].Size != 8 {
		t.Fatalf("boyutlar 12 ve 8 olmalı: %+v", topics)
	}
	if math.Abs(topics[0].Share-0.6) > 1e-9 {
		t.Fatalf("pay: %v", topics[0].Share)
	}
	if topics[0].RepID < 1 || topics[0].RepID > 12 || topics[1].RepID < 100 {
		t.Fatalf("temsilci kendi kümesinden olmalı: %+v", topics)
	}
	if !strings.Contains(strings.Join(topics[0].Keywords, " "), "film") || !strings.Contains(strings.Join(topics[1].Keywords, " "), "kahve") {
		t.Fatalf("anahtar sözcükler: %v | %v", topics[0].Keywords, topics[1].Keywords)
	}
}

func TestTopicsDeterministicAndEdgeCases(t *testing.T) {
	idx, ids, texts := fixture()
	a := fmt.Sprint(Topics(idx, ids, texts, 3))
	for i := 0; i < 5; i++ {
		if fmt.Sprint(Topics(idx, ids, texts, 3)) != a {
			t.Fatal("aynı girdiyle aynı sonuç çıkmalı")
		}
	}
	if Topics(idx, nil, texts, 2) != nil || Topics(idx, []int64{999}, texts, 2) != nil {
		t.Fatal("vektörü olmayan mesajlarla sonuç olmamalı")
	}
	if got := Topics(idx, ids[:3], texts, 10); len(got) == 0 || len(got) > 3 {
		t.Fatalf("k, mesaj sayısını aşamaz: %+v", got)
	}
	if got := Topics(idx, ids, texts, 1); len(got) != 1 || got[0].Size != 20 {
		t.Fatalf("tek küme: %+v", got)
	}
	if KFor(10, 6) != 1 || KFor(400, 6) != 6 || KFor(120, 6) != 3 {
		t.Fatal("KFor")
	}
}
