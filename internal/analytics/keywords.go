package analytics

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/MustafaKemal0146/joe/internal/model"
	"github.com/MustafaKemal0146/joe/internal/store"
)

var kwStop = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`ve ile ama fakat için gibi kadar çok daha bir bu şu o de da mi mı mu mü ki ne nasıl neden niye var yok evet hayır tamam olur oldu olmuş olacak ben sen biz siz onlar bana sana ona bize size onu beni seni onun benim senin bunu şunu bunun şey şeyi yani işte hani sadece belki hala hâlâ zaten artık şimdi sonra önce bugün yarın dün gibi diye değil değilim değilsin mısın misin musun müsün miyim mıyım yok yoktu vardı varsa bile hep hiç her tüm bütün kendi ama ancak çünkü yine tekrar bence sence galiba herhalde acaba lütfen sağol sağ olun tmm okey peki aynen evet`) {
		kwStop[w] = true
	}
}

// PeriodWords bir dönemin ayırt edici sözcükleridir.
type PeriodWords struct {
	From, To time.Time
	Messages int
	Words    []string
}

func kwTokens(text string) []string {
	f := strings.FieldsFunc(strings.ToLower(strings.NewReplacer("İ", "i", "I", "ı").Replace(text)), func(r rune) bool { return !unicode.IsLetter(r) })
	out := f[:0]
	for _, w := range f {
		if len([]rune(w)) >= 4 && !kwStop[w] && !strings.HasPrefix(w, "http") {
			out = append(out, w)
		}
	}
	return out
}

// PeriodKeywords mesajları zamanda eşit uzunlukta k döneme böler ve her dönemin, tüm sohbete göre ayırt edici
// sözcüklerini (log-oran) bulur. Konu zaman çizelgesinin gömmesiz, deterministik karşılığıdır: bir sözcüğün
// bir dönemde öne çıkması, o dönemde neyin konuşulduğuna dair ipucudur (anlam yorumlanmaz). Veri azsa nil.
func PeriodKeywords(msgs []store.MessageRow, k, top int) []PeriodWords {
	var texts []store.MessageRow
	for _, m := range msgs {
		if m.Kind == model.KindText && m.Text != "" {
			texts = append(texts, m)
		}
	}
	if len(texts) < 400 || k < 2 {
		return nil
	}
	first, last := texts[0].TS, texts[len(texts)-1].TS
	span := last.Sub(first)
	if span < 30*24*time.Hour {
		return nil
	}
	step := span/time.Duration(k) + 1
	counts := make([]map[string]int, k)
	totals := make([]int, k)
	msgCount := make([]int, k)
	for i := range counts {
		counts[i] = map[string]int{}
	}
	all := map[string]int{}
	allTotal := 0
	for _, m := range texts {
		i := int(m.TS.Sub(first) / step)
		if i >= k {
			i = k - 1
		}
		msgCount[i]++
		for _, w := range kwTokens(m.Text) {
			counts[i][w]++
			totals[i]++
			all[w]++
			allTotal++
		}
	}
	out := make([]PeriodWords, 0, k)
	for i := 0; i < k; i++ {
		type sc struct {
			w string
			s float64
		}
		var list []sc
		for w, c := range counts[i] {
			if c < 6 || totals[i] == 0 {
				continue
			}
			// log-oran: dönemdeki göreli sıklık / tüm sohbetteki göreli sıklık
			p := float64(c) / float64(totals[i])
			q := float64(all[w]) / float64(allTotal)
			if p <= q*1.8 {
				continue
			}
			list = append(list, sc{w, math.Log(p/q) * math.Log1p(float64(c))})
		}
		sort.SliceStable(list, func(a, b int) bool {
			if list[a].s != list[b].s {
				return list[a].s > list[b].s
			}
			return list[a].w < list[b].w
		})
		pw := PeriodWords{From: first.Add(time.Duration(i) * step), To: first.Add(time.Duration(i+1) * step), Messages: msgCount[i]}
		for j := 0; j < len(list) && j < top; j++ {
			pw.Words = append(pw.Words, list[j].w)
		}
		out = append(out, pw)
	}
	return out
}

// PeriodKeywordsText dönem sözcüklerini kanıt paketinin metrik bölümüne girecek düz metne çevirir ("" = yeterli veri yok).
func PeriodKeywordsText(msgs []store.MessageRow, loc *time.Location) string {
	if loc == nil {
		loc = time.UTC
	}
	ps := PeriodKeywords(msgs, 5, 8)
	var b strings.Builder
	n := 0
	for i, p := range ps {
		if len(p.Words) == 0 {
			continue
		}
		if n == 0 {
			b.WriteString("Dönemlere göre öne çıkan sözcükler (en sık konuşulan konulara ipucu; bir sözcüğün öne çıkması anlamını göstermez):\n")
		}
		n++
		fmt.Fprintf(&b, "- Dönem %d (%s – %s, %d mesaj): %s\n", i+1, p.From.In(loc).Format("02.01.2006"), p.To.In(loc).Format("02.01.2006"), p.Messages, strings.Join(p.Words, ", "))
	}
	return strings.TrimRight(b.String(), "\n")
}
