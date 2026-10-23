// Package analytics, yüklenmiş kaynak verisinden LLM kullanmadan deterministik metrikler üretir:
// ritim, karşılıklılık, dil izleri, dönemler, ilişki ağı, zaman çizelgesi ve iki kişinin
// karşılaştırması. Buradaki hiçbir hesap para harcamaz; konseye gidecek kanıtın temelidir.
package analytics

import (
	"sort"
	"time"

	"github.com/MustafaKemal0146/joe/internal/model"
	"github.com/MustafaKemal0146/joe/internal/store"
)

// Options analiz ayarlarıdır.
type Options struct {
	// Loc saat dağılımları için yerel saat dilimidir.
	Loc *time.Location
}

func (o Options) loc() *time.Location {
	if o.Loc != nil {
		return o.Loc
	}
	return time.UTC
}

// Subject analizin odağındaki kişidir: kimlik kümesi ve kendi dışa aktarımları.
type Subject struct {
	ID    int64
	Name  string
	Norms map[string]bool
	// OwnSources bu kişinin kendi hesabından alınan kaynaklardır (etkinlik verisi içerir).
	OwnSources map[int64]bool
	// memo, ad → ait mi sonuçlarını önbellekler: yüz binlerce mesajda aynı gönderen adı
	// tekrar tekrar sorulur. Bir Subject tek goroutine'de kullanılır.
	memo map[string]bool
}

// NewSubject bir kişi kaydından analiz öznesi kurar.
func NewSubject(p store.Person, c *store.Corpus) Subject {
	s := Subject{ID: p.ID, Name: p.Name, Norms: map[string]bool{}, OwnSources: map[int64]bool{}, memo: map[string]bool{}}
	for _, id := range p.Identities {
		s.Norms[id.Norm] = true
	}
	for id, src := range c.Sources {
		if s.Norms[model.Norm(src.OwnerName)] && src.OwnerName != "" || s.Norms[model.Norm(src.OwnerHandle)] && src.OwnerHandle != "" {
			s.OwnSources[id] = true
		}
	}
	return s
}

// Is bir ad/kullanıcı adının bu kişiye ait olup olmadığını söyler.
func (s Subject) Is(name string) bool {
	if s.memo != nil {
		if v, ok := s.memo[name]; ok {
			return v
		}
	}
	n := model.Norm(name)
	v := n != "" && s.Norms[n]
	if s.memo != nil {
		s.memo[name] = v
	}
	return v
}

// ownerOf bir olayın aktörünü çözümler: $owner ise kaynağın sahibine bakar.
func (s Subject) actorIs(e store.EventRow) bool {
	if e.Actor == model.Owner {
		return s.OwnSources[e.SourceID]
	}
	return s.Is(e.Actor)
}

func (s Subject) counterpartIs(e store.EventRow) bool {
	if e.Counterpart == model.Owner {
		return s.OwnSources[e.SourceID]
	}
	return s.Is(e.Counterpart)
}

// dedupe, aynı mesajın birden fazla KAYNAKTA (ör. iki tarafın dışa aktarımı) bulunmasını önler. Aynı kaynak
// içindeki özdeş mesajlar korunur: WhatsApp zaman damgası dakika çözünürlüğündedir ve aynı dakikada
// art arda gönderilen aynı metinler ("😂", medya satırı) gerçek ayrı mesajlardır. Bunun için her özdeş
// mesaj, kaynağı içindeki sıra numarasıyla (n. tekrar) anahtarlanır; iki kaynaktaki n. tekrar aynı mesajdır.
func dedupe(msgs []store.MessageRow) []store.MessageRow {
	type base struct {
		sender string
		ts     int64
		text   string
		kind   string
	}
	type key struct {
		b   base
		occ int
	}
	perSource := map[int64]map[base]int{}
	seen := make(map[key]bool, len(msgs))
	out := msgs[:0:0]
	for _, m := range msgs {
		b := base{model.Norm(m.Sender), m.TS.UnixMilli(), m.Text, m.Kind}
		ps := perSource[m.SourceID]
		if ps == nil {
			ps = map[base]int{}
			perSource[m.SourceID] = ps
		}
		k := key{b, ps[b]}
		ps[b]++
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, m)
	}
	return out
}

// Msgs bu kişinin yazdığı (sistem dışı) mesajlar, zamana göre sıralı ve tekilleştirilmiş.
func (s Subject) Msgs(c *store.Corpus) []store.MessageRow {
	var out []store.MessageRow
	for _, m := range c.Messages {
		if m.Kind != model.KindSystem && s.Is(m.Sender) {
			out = append(out, m)
		}
	}
	out = dedupe(out)
	sort.SliceStable(out, func(i, j int) bool { return out[i].TS.Before(out[j].TS) })
	return out
}

// convMsgs sohbet kimliğine göre gruplanmış mesajlar.
func convMsgs(c *store.Corpus) map[int64][]store.MessageRow {
	m := map[int64][]store.MessageRow{}
	for _, r := range c.Messages {
		m[r.ConvID] = append(m[r.ConvID], r)
	}
	return m
}
