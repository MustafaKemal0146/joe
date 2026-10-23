package analytics

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/MustafaKemal0146/joe/internal/model"
	"github.com/MustafaKemal0146/joe/internal/store"
)

// Phase bir sohbetin eşit uzunluktaki üç zaman diliminden birinin ölçümleridir. Amaç, yorumlayanın
// "ilişki zamanla değişti mi?" sorusunu sayıyla görmesidir; evre adı ya da hüküm üretilmez.
type Phase struct {
	From, To       time.Time
	Messages       int
	PerActiveDay   float64
	SubjectShare   float64 // mesajların kişiye ait payı
	StartsSubject  float64 // oturum başlatmalarında kişinin payı (-1: oturum yok)
	NightShare     float64 // 00:00–05:59
	AvgWordsSubj   float64
	AvgWordsOther  float64
	Retracted      int
	LongestGapDays float64
}

// Phases kişinin en yoğun bire-bir sohbetini zaman olarak üçe böler. Sohbet 60 günden kısa ya da 300
// mesajdan azsa nil döner: bu durumda karşılaştırma gürültüdür.
func Phases(c *store.Corpus, s Subject, loc *time.Location) (partner string, ps []Phase) {
	dyads := DyadConversations(c, s)
	if len(dyads) == 0 {
		return "", nil
	}
	d := dyads[0]
	var msgs []store.MessageRow
	for _, m := range convMsgs(c)[d.Conv.ID] {
		if m.Kind != model.KindSystem {
			msgs = append(msgs, m)
		}
	}
	if len(msgs) < 300 {
		return "", nil
	}
	first, last := msgs[0].TS, msgs[len(msgs)-1].TS
	span := last.Sub(first)
	if span < 60*24*time.Hour {
		return "", nil
	}
	third := span / 3
	bounds := []time.Time{first, first.Add(third), first.Add(2 * third), last.Add(time.Second)}
	for i := 0; i < 3; i++ {
		var seg []store.MessageRow
		for _, m := range msgs {
			if !m.TS.Before(bounds[i]) && m.TS.Before(bounds[i+1]) {
				seg = append(seg, m)
			}
		}
		ps = append(ps, phaseOf(seg, s, loc, bounds[i], bounds[i+1]))
	}
	return d.Partner, ps
}

func phaseOf(seg []store.MessageRow, s Subject, loc *time.Location, from, to time.Time) Phase {
	p := Phase{From: from, To: to, Messages: len(seg), StartsSubject: -1}
	if len(seg) == 0 {
		return p
	}
	days := map[string]bool{}
	var subj, night, starts, startsSubj, wS, nS, wO, nO int
	prev := time.Time{}
	var gap time.Duration
	for _, m := range seg {
		days[m.TS.In(loc).Format("2006-01-02")] = true
		mine := s.Is(m.Sender)
		if mine {
			subj++
		}
		if m.TS.In(loc).Hour() < 6 {
			night++
		}
		if m.Kind == model.KindUnsent || m.Kind == model.KindDeleted {
			p.Retracted++
		}
		if prev.IsZero() || m.TS.Sub(prev) >= sessionGap {
			starts++
			if mine {
				startsSubj++
			}
		}
		if !prev.IsZero() && m.TS.Sub(prev) > gap {
			gap = m.TS.Sub(prev)
		}
		prev = m.TS
		if m.Kind == model.KindText && m.Text != "" {
			w := len(strings.FieldsFunc(m.Text, unicode.IsSpace))
			if mine {
				wS += w
				nS++
			} else {
				wO += w
				nO++
			}
		}
	}
	p.PerActiveDay = float64(len(seg)) / float64(len(days))
	p.SubjectShare = float64(subj) / float64(len(seg))
	p.NightShare = float64(night) / float64(len(seg))
	if starts > 0 {
		p.StartsSubject = float64(startsSubj) / float64(starts)
	}
	if nS > 0 {
		p.AvgWordsSubj = float64(wS) / float64(nS)
	}
	if nO > 0 {
		p.AvgWordsOther = float64(wO) / float64(nO)
	}
	p.LongestGapDays = gap.Hours() / 24
	return p
}

// PhaseText evreleri konsey paketinin metrik bölümüne girecek düz metne çevirir ("" = yeterli veri yok).
func PhaseText(c *store.Corpus, s Subject, loc *time.Location) string {
	partner, ps := Phases(c, s, loc)
	if len(ps) != 3 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Dönem karşılaştırması (%s ile sohbet, zamanın eşit üçte biri; değerlendirme değil ölçüm; önce=%s):\n", partner, s.Name)
	for i, p := range ps {
		starts := "—"
		if p.StartsSubject >= 0 {
			starts = fmt.Sprintf("%%%.0f", p.StartsSubject*100)
		}
		fmt.Fprintf(&b, "- Dönem %d (%s – %s): %d mesaj, aktif günde %.0f; kişinin payı %%%.0f; konuşma başlatmada kişinin payı %s; gece payı %%%.0f; ort. sözcük kişi %.1f / karşı %.1f; geri alınan %d; en uzun sessizlik %.1f gün\n",
			i+1, p.From.In(loc).Format("02.01.2006"), p.To.In(loc).Format("02.01.2006"), p.Messages, p.PerActiveDay, p.SubjectShare*100, starts, p.NightShare*100, p.AvgWordsSubj, p.AvgWordsOther, p.Retracted, p.LongestGapDays)
	}
	return strings.TrimRight(b.String(), "\n")
}
