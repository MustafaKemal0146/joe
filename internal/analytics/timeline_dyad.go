package analytics

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/MustafaKemal0146/joe/internal/model"
	"github.com/MustafaKemal0146/joe/internal/store"
)

// keyword bir ilişki kilometre taşı olabilecek ifade grubudur. Eşleşme SÖZCÜK eşleşmesidir: ne
// anlama geldiği (şaka, alıntı, olumsuzlama) yorumlanmaz; zaman çizelgesinde "ilk kez geçti" olarak
// gösterilir ve arayüzde bu açıkça belirtilir.
type keyword struct {
	label string
	rx    *regexp.Regexp
}

func kw(label, pat string) keyword { return keyword{label, regexp.MustCompile(`(?i)` + pat)} }

// milestoneLexicon Türkçe, ASCII'ye indirgenmiş (ı→i, ş→s…) metin üzerinde çalışır.
var milestoneLexicon = []keyword{
	kw("seni seviyorum", `seni (cok |de |halen |hala )?seviyorum`),
	kw("özledim", `ozledim`),
	kw("özür dilerim", `ozur dilerim`),
	kw("barışalım", `barisal[im]|barismak`),
	kw("ayrılalım / bitirelim", `ayrilalim|ayriliyoruz|bitirelim|bitiriyorum|bitirdim`),
	kw("görüşmeyelim / beni arama", `gorusmeyelim|beni arama|bir daha arama`),
	kw("kıskanıyorum", `kiskaniyorum|kiskandim`),
	kw("güvenmiyorum", `guvenmiyorum`),
	kw("evlenelim", `evlenelim|evlenmek istiyorum`),
	kw("engelledim", `engelledim|engelliyorum`),
}

var asciiFold = strings.NewReplacer("ı", "i", "İ", "i", "I", "i", "ş", "s", "Ş", "s", "ğ", "g", "Ğ", "g", "ü", "u", "Ü", "u", "ö", "o", "Ö", "o", "ç", "c", "Ç", "c")

func foldASCII(s string) string { return strings.ToLower(asciiFold.Replace(s)) }

var countMilestones = []int{1000, 5000, 10000, 25000, 50000, 100000}

func fmtInt(n int) string {
	s := fmt.Sprint(n)
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, '.')
		}
		out = append(out, c)
	}
	return string(out)
}

func gapText(d time.Duration) string {
	days := d.Hours() / 24
	if days >= 2 {
		return fmt.Sprintf("%.0f gün", days)
	}
	return fmt.Sprintf("%.0f saat", d.Hours())
}

// dyadTimeline kişinin bire-bir sohbetlerinden önemli anları çıkarır: ilk/son temas, mesaj sayısı
// kilometre taşları, en yoğun günler, sessizlikler (ve kimin bozduğu), gece konuşmaları, tek taraflı
// seriler, aylık yoğunluk değişimleri, ilk silinen mesaj ve anahtar sözcüklerin ilk geçişi.
func dyadTimeline(c *store.Corpus, s Subject, loc *time.Location) []TimelineItem {
	by := convMsgs(c)
	var out []TimelineItem
	dyads := DyadConversations(c, s)
	if len(dyads) > 5 {
		dyads = dyads[:5]
	}
	for _, d := range dyads {
		var msgs []store.MessageRow
		for _, m := range by[d.Conv.ID] {
			if m.Kind != model.KindSystem {
				msgs = append(msgs, m)
			}
		}
		n := len(msgs)
		if n == 0 {
			continue
		}
		src := c.Sources[d.Conv.SourceID].Platform
		who := d.Partner
		day := func(t time.Time) string { return t.In(loc).Format("02.01.2006") }

		// 1) İlk ve son temas
		out = append(out, item(msgs[0].TS, "contact", fmt.Sprintf("%s ile ilk mesaj", who),
			fmt.Sprintf("%s yazdı · %s üzerinden; toplam %s mesaj", msgs[0].Sender, platformLabel(src), fmtInt(n)), src, 3))
		if n > 1 {
			out = append(out, item(msgs[n-1].TS, "contact", fmt.Sprintf("%s ile son mesaj", who), fmt.Sprintf("%s yazdı", msgs[n-1].Sender), src, 3))
		}

		// 2) Mesaj sayısı kilometre taşları
		for _, m := range countMilestones {
			if m < n {
				out = append(out, item(msgs[m-1].TS, "milestone", fmt.Sprintf("%s. mesaj", fmtInt(m)), fmt.Sprintf("%s ile yazışmada %s mesaja ulaşıldı", who, fmtInt(m)), src, 3))
			}
		}

		// 3) En yoğun günler
		perDay := map[string][]int{}
		for i, m := range msgs {
			k := m.TS.In(loc).Format("2006-01-02")
			perDay[k] = append(perDay[k], i)
		}
		days := make([]string, 0, len(perDay))
		for k := range perDay {
			days = append(days, k)
		}
		sort.Slice(days, func(a, b int) bool {
			if len(perDay[days[a]]) != len(perDay[days[b]]) {
				return len(perDay[days[a]]) > len(perDay[days[b]])
			}
			return days[a] < days[b]
		})
		avg := float64(n) / float64(len(days))
		for k, dk := range days {
			cnt := len(perDay[dk])
			if k >= 5 || float64(cnt) < 3*avg || cnt < 100 {
				break
			}
			out = append(out, item(msgs[perDay[dk][0]].TS, "peak", fmt.Sprintf("%s ile en yoğun günlerden biri", who), fmt.Sprintf("Bir günde %s mesaj (günlük ortalamanın %.1f katı)", fmtInt(cnt), float64(cnt)/avg), src, 2))
		}

		// 4) Sessizlikler (≥ 3 gün): kim bozdu
		type gap struct {
			i int
			d time.Duration
		}
		var gaps []gap
		for i := 1; i < n; i++ {
			if dd := msgs[i].TS.Sub(msgs[i-1].TS); dd >= 72*time.Hour {
				gaps = append(gaps, gap{i, dd})
			}
		}
		sort.SliceStable(gaps, func(a, b int) bool { return gaps[a].d > gaps[b].d })
		for k, g := range gaps {
			if k >= 8 {
				break
			}
			w := 2
			if g.d >= 5*24*time.Hour {
				w = 3
			}
			out = append(out, item(msgs[g.i-1].TS, "silence", fmt.Sprintf("%s ile %s sessizlik başladı", who, gapText(g.d)),
				fmt.Sprintf("Son yazan %s; %s tarihinde %s yeniden yazdı", msgs[g.i-1].Sender, day(msgs[g.i].TS), msgs[g.i].Sender), src, w))
		}

		// 5) Gece konuşmaları: oturum (6 saatlik boşlukla) içinde gece yarısından sonra en çok mesaj
		type sess struct{ from, to int }
		var sessions []sess
		st := 0
		for i := 1; i <= n; i++ {
			if i == n || msgs[i].TS.Sub(msgs[i-1].TS) > sessionGap {
				sessions = append(sessions, sess{st, i})
				st = i
			}
		}
		type night struct{ first, count int }
		var nights []night
		for _, se := range sessions {
			first, cnt := -1, 0
			for i := se.from; i < se.to; i++ {
				if msgs[i].TS.In(loc).Hour() < 6 {
					if first < 0 {
						first = i
					}
					cnt++
				}
			}
			if cnt >= 20 {
				nights = append(nights, night{first, cnt})
			}
		}
		sort.SliceStable(nights, func(a, b int) bool { return nights[a].count > nights[b].count })
		for k, nt := range nights {
			if k >= 5 {
				break
			}
			out = append(out, item(msgs[nt.first].TS, "night", fmt.Sprintf("%s ile gece boyunca süren konuşma", who), fmt.Sprintf("Gece yarısından sonra %d mesaj", nt.count), src, 2))
		}

		// 6) Tek taraflı seriler (≥ 15 art arda)
		type run struct{ start, length int }
		var runs []run
		for i := 0; i < n; {
			j := i
			for j < n && model.Norm(msgs[j].Sender) == model.Norm(msgs[i].Sender) {
				j++
			}
			if j-i >= 15 {
				runs = append(runs, run{i, j - i})
			}
			i = j
		}
		sort.SliceStable(runs, func(a, b int) bool { return runs[a].length > runs[b].length })
		for k, r := range runs {
			if k >= 3 {
				break
			}
			out = append(out, item(msgs[r.start].TS, "run", fmt.Sprintf("%s art arda %d mesaj yazdı", msgs[r.start].Sender, r.length), fmt.Sprintf("%s ile yazışmada karşı taraf yanıt vermeden süren seri", who), src, 2))
		}

		// 7) Aylık yoğunluk değişimleri (önceki aya göre ≥ %50 artış ya da %40 azalış)
		monthCount := map[string]int{}
		for _, m := range msgs {
			monthCount[m.TS.In(loc).Format("2006-01")]++
		}
		months := make([]string, 0, len(monthCount))
		for k := range monthCount {
			months = append(months, k)
		}
		sort.Strings(months)
		for i := 1; i < len(months); i++ {
			prev, cur := monthCount[months[i-1]], monthCount[months[i]]
			if prev < 200 && cur < 200 {
				continue
			}
			t, _ := time.ParseInLocation("2006-01", months[i], loc)
			switch {
			case float64(cur) >= 1.5*float64(prev):
				out = append(out, item(t, "trend", fmt.Sprintf("%s ile mesajlaşma yoğunlaştı", who), fmt.Sprintf("%s ayı: %s → %s mesaj", months[i], fmtInt(prev), fmtInt(cur)), src, 3))
			case float64(cur) <= 0.6*float64(prev):
				out = append(out, item(t, "trend", fmt.Sprintf("%s ile mesajlaşma seyrekleşti", who), fmt.Sprintf("%s ayı: %s → %s mesaj", months[i], fmtInt(prev), fmtInt(cur)), src, 3))
			}
		}

		// 8) İlk silinen/geri alınan mesaj (her gönderen için)
		seen := map[string]bool{}
		for _, m := range msgs {
			if (m.Kind == model.KindDeleted || m.Kind == model.KindUnsent) && !seen[model.Norm(m.Sender)] {
				seen[model.Norm(m.Sender)] = true
				out = append(out, item(m.TS, "retract", fmt.Sprintf("%s ilk kez bir mesajı sildi/geri aldı", m.Sender), who+" ile yazışmada", src, 2))
			}
		}

		// 9) Anahtar sözcüklerin ilk geçişi
		type hit struct {
			first, last time.Time
			who         string
			count       int
		}
		hits := map[string]*hit{}
		for _, m := range msgs {
			if m.Kind != model.KindText || m.Text == "" {
				continue
			}
			f := foldASCII(m.Text)
			for _, k := range milestoneLexicon {
				if k.rx.MatchString(f) {
					h := hits[k.label]
					if h == nil {
						h = &hit{first: m.TS, who: m.Sender}
						hits[k.label] = h
					}
					h.count++
					h.last = m.TS
				}
			}
		}
		for _, k := range milestoneLexicon {
			h := hits[k.label]
			if h == nil {
				continue
			}
			detail := fmt.Sprintf("%s yazdı; toplam %d mesajda geçti", h.who, h.count)
			if h.count > 1 {
				detail += fmt.Sprintf(" (son: %s)", day(h.last))
			}
			out = append(out, item(h.first, "keyword", fmt.Sprintf("İlk kez “%s” geçti", k.label), detail+" · sözcük eşleşmesi, yorum değil", src, 2))
		}
	}
	return out
}
