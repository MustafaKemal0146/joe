package analytics

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/MustafaKemal0146/joe/internal/model"
	"github.com/MustafaKemal0146/joe/internal/store"
)

// minCalls bu kadar arama kaydından azsa arama metriği üretilmez (yorum için çok az).
const minCalls = 3

// CallsText bir sohbetteki sesli/görüntülü arama kayıtlarını düz metne çevirir ("" = yeterli kayıt yok).
// Yalnızca sayıdır: kim kaç kez aradı, kaçı cevapsız kaldı, süreler, gece payı ve aylık seyir.
// Arama kaydını başlatan taraf mesajın göndereni olarak okunur.
func CallsText(msgs []store.MessageRow, loc *time.Location) string {
	if loc == nil {
		loc = time.UTC
	}
	type side struct{ answered, missed int }
	by := map[string]*side{}
	var secs []int64
	var total int64
	night, calls := 0, 0
	months := map[string]int{}
	var first, last time.Time
	for _, m := range msgs {
		if m.Kind != model.KindCall && m.Kind != model.KindMissedCall {
			continue
		}
		calls++
		s := by[m.Sender]
		if s == nil {
			s = &side{}
			by[m.Sender] = s
		}
		if m.Kind == model.KindMissedCall {
			s.missed++
		} else {
			s.answered++
			if v, ok := m.Meta["seconds"].(float64); ok {
				secs = append(secs, int64(v))
				total += int64(v)
			} else if v, ok := m.Meta["seconds"].(int64); ok {
				secs = append(secs, v)
				total += v
			}
		}
		t := m.TS.In(loc)
		if t.Hour() < 6 {
			night++
		}
		months[t.Format("2006-01")]++
		if first.IsZero() || t.Before(first) {
			first = t
		}
		if t.After(last) {
			last = t
		}
	}
	if calls < minCalls {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Arama kayıtları (%s – %s; yalnızca sayı, anlam yüklenmez):\n", first.Format("02.01.2006"), last.Format("02.01.2006"))
	names := make([]string, 0, len(by))
	for n := range by {
		names = append(names, n)
	}
	sort.Strings(names)
	answered, missed := 0, 0
	for _, n := range names {
		s := by[n]
		answered += s.answered
		missed += s.missed
		fmt.Fprintf(&b, "- %s başlattı: %d arama (%d cevaplandı, %d cevapsız kaldı)\n", n, s.answered+s.missed, s.answered, s.missed)
	}
	fmt.Fprintf(&b, "- Toplam: %d kayıt, %d cevaplanan, %d cevapsız; gece (00–06) payı %%%d\n", calls, answered, missed, night*100/calls)
	if len(secs) > 0 {
		sort.Slice(secs, func(i, j int) bool { return secs[i] < secs[j] })
		short := 0
		for _, v := range secs {
			if v < 60 {
				short++
			}
		}
		fmt.Fprintf(&b, "- Cevaplanan aramalarda toplam %s, ortanca %s, en uzun %s; 1 dakikadan kısa: %d\n", callDur(total), callDur(secs[len(secs)/2]), callDur(secs[len(secs)-1]), short)
	}
	keys := make([]string, 0, len(months))
	for k := range months {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) > 1 {
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("%s:%d", k, months[k]))
		}
		fmt.Fprintf(&b, "- Aylık arama sayısı: %s\n", strings.Join(parts, ", "))
	}
	return strings.TrimRight(b.String(), "\n")
}

func callDur(s int64) string {
	if s >= 3600 {
		return fmt.Sprintf("%d sa %d dk", s/3600, (s%3600)/60)
	}
	if s >= 60 {
		return fmt.Sprintf("%d dk %d sn", s/60, s%60)
	}
	return fmt.Sprintf("%d sn", s)
}
