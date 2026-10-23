package analytics

import (
	"sort"
	"time"

	"github.com/MustafaKemal0146/joe/internal/model"
	"github.com/MustafaKemal0146/joe/internal/store"
)

// Moment bir sohbetteki ölçülebilir bir andır (indeksler msgs diliminde, From dahil To hariç değil: ikisi dahildir).
// Önemli anlar listesi (Highlights) ve konsey kanıt paketi aynı hesabı kullanır; böylece arayüzde gördüğün an
// ile psikologların okuduğu kesit tutarlıdır. Hiçbiri yorum değildir.
type Moment struct {
	Kind  string // conflict | affection | rupture
	From  int    // ilk mesajın indeksi
	To    int    // son mesajın indeksi
	Count int    // ifade geçen mesaj sayısı (conflict/affection) ya da öncesindeki yoğunluk (rupture)
	Gap   time.Duration
	Day   string // yyyy-mm-dd (yerel saat)
}

// Moments filtrelenmiş (sistem mesajları atılmış), zamana göre sıralı mesajlardan an adaylarını döndürür; her tür
// kendi içinde önem sırasına göredir (çatışma: kümedeki mesaj sayısı; yakınlık: günlük sayı; kopuş: sessizlik süresi).
func Moments(msgs []store.MessageRow, loc *time.Location) []Moment {
	if loc == nil {
		loc = time.UTC
	}
	var out []Moment

	// çatışma ifadesi kümeleri: aralarında en çok 30 dk olan, en az 3 mesaj
	type cl struct {
		from, to, n int
		last        time.Time
	}
	var cls []cl
	for i, m := range msgs {
		if m.Kind != model.KindText || !conflictIn(m.Text) {
			continue
		}
		if k := len(cls); k > 0 && m.TS.Sub(cls[k-1].last) <= 30*time.Minute {
			cls[k-1].to, cls[k-1].n, cls[k-1].last = i, cls[k-1].n+1, m.TS
		} else {
			cls = append(cls, cl{i, i, 1, m.TS})
		}
	}
	sort.SliceStable(cls, func(a, b int) bool { return cls[a].n > cls[b].n })
	for _, c := range cls {
		if c.n >= 3 {
			out = append(out, Moment{Kind: "conflict", From: c.from, To: c.to, Count: c.n, Day: msgs[c.from].TS.In(loc).Format("2006-01-02")})
		}
	}

	// yakınlık ifadesi zirveleri: günde en az 5 mesaj; pencere o günün ilk yakınlık mesajından başlar
	type dayAff struct {
		first, last, n int
		day            string
	}
	days := map[string]*dayAff{}
	var order []string
	for i, m := range msgs {
		if m.Kind != model.KindText || !affectionIn(m.Text) {
			continue
		}
		k := m.TS.In(loc).Format("2006-01-02")
		d := days[k]
		if d == nil {
			d = &dayAff{first: i, day: k}
			days[k] = d
			order = append(order, k)
		}
		d.n++
		d.last = i
	}
	sort.SliceStable(order, func(a, b int) bool { return days[order[a]].n > days[order[b]].n })
	for _, k := range order {
		if d := days[k]; d.n >= 5 {
			out = append(out, Moment{Kind: "affection", From: d.first, To: d.last, Count: d.n, Day: d.day})
		}
	}

	// kopuş: en az 14 günlük sessizlikten önceki 14 günde ≥ 60 mesaj
	type gp struct {
		i      int
		d      time.Duration
		before int
	}
	var gaps []gp
	for i := 1; i < len(msgs); i++ {
		d := msgs[i].TS.Sub(msgs[i-1].TS)
		if d < 14*24*time.Hour {
			continue
		}
		before := 0
		for j := i - 1; j >= 0 && msgs[i-1].TS.Sub(msgs[j].TS) <= 14*24*time.Hour; j-- {
			before++
		}
		if before >= 60 {
			gaps = append(gaps, gp{i, d, before})
		}
	}
	sort.SliceStable(gaps, func(a, b int) bool { return gaps[a].d > gaps[b].d })
	for _, g := range gaps {
		out = append(out, Moment{Kind: "rupture", From: g.i - 1, To: g.i, Count: g.before, Gap: g.d, Day: msgs[g.i].TS.In(loc).Format("2006-01-02")})
	}
	return out
}
