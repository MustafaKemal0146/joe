package analytics

import (
	"fmt"
	"sort"
	"time"

	"github.com/MustafaKemal0146/joe/internal/model"
	"github.com/MustafaKemal0146/joe/internal/store"
)

// Highlight bir sohbetteki dikkat çeken andır. MessageID, arayüzde sohbetin o noktasına gitmek içindir.
// Hiçbiri yorum değildir: ölçülen bir örüntünün adıdır (ifade eşleşmeleri yalnızca sözcük sayımıdır).
type Highlight struct {
	Kind      string    `json:"kind"`
	Title     string    `json:"title"`
	Detail    string    `json:"detail,omitempty"`
	TS        time.Time `json:"ts"`
	MessageID int64     `json:"message_id"`
	Weight    int       `json:"weight"` // 1–3
}

const maxHighlights = 60

// ConversationHighlights tek bir bire-bir sohbetin önemli anlarını çıkarır: ilk/son mesaj, en yoğun
// günler, uzun sessizlikler ve yoğun yazışmadan sonra kopuşlar, çatışma ifadesi kümeleri, yakınlık
// ifadesi zirveleri, gece oturumları, tek taraflı seriler, geri alınan mesaj kümeleri.
func ConversationHighlights(c *store.Corpus, convID int64, s Subject, partner string, loc *time.Location) []Highlight {
	if loc == nil {
		loc = time.UTC
	}
	var msgs []store.MessageRow
	for _, m := range c.Messages {
		if m.ConvID == convID && m.Kind != model.KindSystem {
			msgs = append(msgs, m)
		}
	}
	if len(msgs) == 0 {
		return nil
	}
	who := func(m store.MessageRow) string {
		if s.Is(m.Sender) {
			return "sen"
		}
		return partner
	}
	day := func(t time.Time) string { return t.In(loc).Format("02.01.2006") }
	var out []Highlight
	add := func(kind, title, detail string, m store.MessageRow, w int) {
		out = append(out, Highlight{Kind: kind, Title: title, Detail: detail, TS: m.TS, MessageID: m.ID, Weight: w})
	}

	add("contact", "İlk mesaj", who(msgs[0])+" yazdı", msgs[0], 3)
	if n := len(msgs); n > 1 {
		add("contact", "Son mesaj", who(msgs[n-1])+" yazdı", msgs[n-1], 3)
	}

	// günlük sayımlar
	type dayInfo struct {
		first      store.MessageRow
		n, aff, un int
	}
	days := map[string]*dayInfo{}
	var order []string
	for _, m := range msgs {
		k := m.TS.In(loc).Format("2006-01-02")
		d := days[k]
		if d == nil {
			d = &dayInfo{first: m}
			days[k] = d
			order = append(order, k)
		}
		d.n++
		if m.Kind == model.KindText && affectionIn(m.Text) {
			d.aff++
		}
		if m.Kind == model.KindUnsent || m.Kind == model.KindDeleted {
			d.un++
		}
	}
	top := func(less func(a, b *dayInfo) bool, min func(*dayInfo) bool, n int) []*dayInfo {
		var l []*dayInfo
		for _, k := range order {
			if min(days[k]) {
				l = append(l, days[k])
			}
		}
		sort.SliceStable(l, func(i, j int) bool { return less(l[i], l[j]) })
		if len(l) > n {
			l = l[:n]
		}
		return l
	}
	for i, d := range top(func(a, b *dayInfo) bool { return a.n > b.n }, func(d *dayInfo) bool { return d.n >= 30 }, 3) {
		add("peak", fmt.Sprintf("En yoğun günlerden biri: %d mesaj", d.n), "", d.first, 3-min(i, 1))
	}
	for _, d := range top(func(a, b *dayInfo) bool { return a.aff > b.aff }, func(d *dayInfo) bool { return d.aff >= 5 }, 3) {
		add("affection", fmt.Sprintf("Yakınlık ifadelerinin yoğun olduğu gün (%d mesaj)", d.aff), "ifade geçen mesaj sayısı; anlam yorumlanmaz", d.first, 2)
	}
	for _, d := range top(func(a, b *dayInfo) bool { return a.un > b.un }, func(d *dayInfo) bool { return d.un >= 3 }, 2) {
		add("retract", fmt.Sprintf("Bir günde %d mesaj geri alındı ya da silindi", d.un), "", d.first, 2)
	}

	// sessizlikler ve kopuşlar
	type gap struct {
		i int
		d time.Duration
	}
	var gaps []gap
	for i := 1; i < len(msgs); i++ {
		if d := msgs[i].TS.Sub(msgs[i-1].TS); d >= 7*24*time.Hour {
			gaps = append(gaps, gap{i, d})
		}
	}
	sort.SliceStable(gaps, func(a, b int) bool { return gaps[a].d > gaps[b].d })
	if len(gaps) > 4 {
		gaps = gaps[:4]
	}
	for _, g := range gaps {
		prev, next := msgs[g.i-1], msgs[g.i]
		days := int(g.d.Hours() / 24)
		before := 0 // sessizlikten önceki 14 gündeki mesaj sayısı
		for j := g.i - 1; j >= 0 && prev.TS.Sub(msgs[j].TS) <= 14*24*time.Hour; j-- {
			before++
		}
		detail := fmt.Sprintf("son mesajı %s yazmıştı; sessizliği %s bozdu", who(prev), who(next))
		if before >= 60 && days >= 14 {
			add("rupture", fmt.Sprintf("Yoğun yazışmanın ardından %d günlük sessizlik", days), fmt.Sprintf("öncesindeki 14 günde %d mesaj; %s", before, detail), next, 3)
		} else {
			add("silence", fmt.Sprintf("%d günlük sessizlik", days), detail, next, 2)
		}
	}

	// çatışma ifadesi kümeleri: aralarında en çok 30 dk olan, en az 3 mesaj
	type cluster struct {
		first store.MessageRow
		n     int
		last  time.Time
	}
	var cls []cluster
	for _, m := range msgs {
		if m.Kind != model.KindText || !conflictIn(m.Text) {
			continue
		}
		if n := len(cls); n > 0 && m.TS.Sub(cls[n-1].last) <= 30*time.Minute {
			cls[n-1].n++
			cls[n-1].last = m.TS
		} else {
			cls = append(cls, cluster{m, 1, m.TS})
		}
	}
	sort.SliceStable(cls, func(a, b int) bool { return cls[a].n > cls[b].n })
	shown := 0
	for _, cl := range cls {
		if cl.n < 3 || shown >= 5 {
			break
		}
		add("conflict", fmt.Sprintf("Çatışma ifadelerinin yoğunlaştığı an (%d mesaj)", cl.n), "ifade geçen mesaj sayısı; şaka, alıntı ya da olumsuzlama ayrımı yapılmaz", cl.first, 3)
		shown++
	}

	// gece oturumu: gece 00–06 arası bir günde ≥ 20 mesaj
	night := map[string][]store.MessageRow{}
	var nOrder []string
	for _, m := range msgs {
		if m.TS.In(loc).Hour() < 6 {
			k := m.TS.In(loc).Format("2006-01-02")
			if night[k] == nil {
				nOrder = append(nOrder, k)
			}
			night[k] = append(night[k], m)
		}
	}
	sort.SliceStable(nOrder, func(a, b int) bool { return len(night[nOrder[a]]) > len(night[nOrder[b]]) })
	for i, k := range nOrder {
		if i >= 3 || len(night[k]) < 20 {
			break
		}
		add("night", fmt.Sprintf("Gece yarısından sonra %d mesaj", len(night[k])), "", night[k][0], 2)
	}

	// tek taraflı seri: aynı kişiden art arda ≥ 12 mesaj (aralarında en çok 24 saat)
	runStart := 0
	type runInfo struct {
		first store.MessageRow
		n     int
	}
	var runs []runInfo
	flush := func(end int) {
		if n := end - runStart; n >= 12 {
			runs = append(runs, runInfo{msgs[runStart], n})
		}
	}
	for i := 1; i <= len(msgs); i++ {
		if i == len(msgs) || s.Is(msgs[i].Sender) != s.Is(msgs[i-1].Sender) || msgs[i].TS.Sub(msgs[i-1].TS) > 24*time.Hour {
			flush(i)
			runStart = i
		}
	}

	sort.SliceStable(runs, func(a, b int) bool { return runs[a].n > runs[b].n })
	for i, r := range runs {
		if i >= 3 { // en uzun üç seri
			break
		}
		add("run", fmt.Sprintf("%s art arda %d mesaj yazdı", who(r.first), r.n), "karşılık gelmeden", r.first, 2)
	}

	// ilk kez geçen ifadeler
	for _, m := range msgs {
		if m.Kind == model.KindText && affectionIn(m.Text) {
			add("first", "Yakınlık ifadesinin ilk geçtiği mesaj", who(m)+" yazdı; "+day(m.TS), m, 2)
			break
		}
	}
	for _, m := range msgs {
		if m.Kind == model.KindText && conflictIn(m.Text) {
			add("first", "Çatışma ifadesinin ilk geçtiği mesaj", who(m)+" yazdı; "+day(m.TS), m, 2)
			break
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].TS.Equal(out[j].TS) {
			return out[i].TS.Before(out[j].TS)
		}
		return out[i].Title < out[j].Title
	})
	if len(out) > maxHighlights {
		sort.SliceStable(out, func(i, j int) bool { return out[i].Weight > out[j].Weight })
		out = out[:maxHighlights]
		sort.SliceStable(out, func(i, j int) bool { return out[i].TS.Before(out[j].TS) })
	}
	return out
}
