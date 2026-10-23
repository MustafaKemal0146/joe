package analytics

import (
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/MustafaKemal0146/joe/internal/model"
	"github.com/MustafaKemal0146/joe/internal/store"
)

// Profile bir kişinin deterministik davranış özetidir.
type Profile struct {
	Subject  string    `json:"subject"`
	Messages int       `json:"messages"`
	First    time.Time `json:"first"`
	Last     time.Time `json:"last"`
	Days     int       `json:"active_days"`

	Hours   [24]int      `json:"hours"`
	Weekday [7]int       `json:"weekday"` // 0 = Pazartesi
	Heat    [7][24]int   `json:"heat"`
	Monthly []MonthCount `json:"monthly"`

	NightRatio float64 `json:"night_ratio"` // 00:00–05:59
	// SleepStart tahmini uyku penceresinin başlangıç saatidir; Confident=false ise veri azdır.
	SleepStart int  `json:"sleep_start"`
	SleepKnown bool `json:"sleep_known"`

	Language Language    `json:"language"`
	Partners []Partner   `json:"partners"`
	Periods  []Period    `json:"periods"`
	Groups   []GroupStat `json:"groups"`
	Activity *Activity   `json:"activity,omitempty"`
	Notes    []string    `json:"notes"`
	Latency  LatencyStat `json:"latency"`
}

// MonthCount bir aydaki mesaj sayısıdır.
type MonthCount struct {
	Month string `json:"month"` // 2026-03
	Count int    `json:"count"`
}

// Language dil ve ifade izleridir.
type Language struct {
	TextMessages  int     `json:"text_messages"`
	AvgWords      float64 `json:"avg_words"`
	AvgChars      float64 `json:"avg_chars"`
	QuestionRatio float64 `json:"question_ratio"`
	ExclaimRatio  float64 `json:"exclaim_ratio"`
	LinkRatio     float64 `json:"link_ratio"`
	CapsRatio     float64 `json:"caps_ratio"`
	EllipsisRatio float64 `json:"ellipsis_ratio"`
	EmojiPerMsg   float64 `json:"emoji_per_msg"`
	Richness      float64 `json:"richness"` // ilk 5000 sözcükte tekil/toplam
	TopWords      []Count `json:"top_words"`
	TopEmoji      []Count `json:"top_emoji"`
	Kinds         []Count `json:"kinds"` // medya, silinen, geri alınan, cevapsız arama...
}

// LatencyStat yanıt gecikmesi özetidir (saniye).
type LatencyStat struct {
	Median float64 `json:"median"`
	P90    float64 `json:"p90"`
	N      int     `json:"n"`
}

const sessionGap = 6 * time.Hour

func isCaps(s string) bool {
	letters, upper := 0, 0
	for _, r := range s {
		if unicode.IsLetter(r) {
			letters++
			if unicode.IsUpper(r) {
				upper++
			}
		}
	}
	return letters >= 5 && float64(upper)/float64(letters) > 0.7
}

// BuildProfile bir kişinin tüm profilini hesaplar.
func BuildProfile(c *store.Corpus, s Subject, opt Options) Profile {
	loc := opt.loc()
	p := Profile{Subject: s.Name, Notes: []string{}}
	msgs := s.Msgs(c)
	p.Messages = len(msgs)

	days := map[string]bool{}
	month := map[string]int{}
	words := map[string]int{}
	emoji := map[string]int{}
	kinds := map[string]int{}
	var textN, wordTotal, charTotal, q, ex, link, caps, ell, emojiTotal int
	var firstWords []string

	for _, m := range msgs {
		t := m.TS.In(loc)
		if p.First.IsZero() || m.TS.Before(p.First) {
			p.First = m.TS
		}
		if m.TS.After(p.Last) {
			p.Last = m.TS
		}
		days[t.Format("2006-01-02")] = true
		month[t.Format("2006-01")]++
		p.Hours[t.Hour()]++
		wd := (int(t.Weekday()) + 6) % 7
		p.Weekday[wd]++
		p.Heat[wd][t.Hour()]++

		switch m.Kind {
		case model.KindText, model.KindShare:
			if m.Text == "" {
				continue
			}
			textN++
			toks := tokens(m.Text)
			wordTotal += len(strings.Fields(m.Text))
			charTotal += len([]rune(m.Text))
			for _, w := range toks {
				words[w]++
				if len(firstWords) < 5000 {
					firstWords = append(firstWords, w)
				}
			}
			if strings.Contains(m.Text, "?") {
				q++
			}
			if strings.Contains(m.Text, "!") {
				ex++
			}
			if strings.Contains(m.Text, "http") {
				link++
			}
			if isCaps(m.Text) {
				caps++
			}
			if strings.Contains(m.Text, "...") || strings.Contains(m.Text, "…") {
				ell++
			}
			for _, r := range m.Text {
				if isEmoji(r) {
					emoji[string(r)]++
					emojiTotal++
				}
			}
			if e, _ := m.Meta["edited"].(bool); e {
				kinds["düzenlenen"]++
			}
		case model.KindMedia:
			label := "medya"
			if mt, _ := m.Meta["media"].(string); mt != "" {
				label = "medya:" + mt
			}
			kinds[label]++
		case model.KindUnsent:
			kinds["geri alınan"]++
		case model.KindDeleted:
			kinds["silinen"]++
		case model.KindMissedCall:
			kinds["cevapsız arama"]++
		case model.KindCall:
			kinds["arama"]++
		case model.KindReaction:
			kinds["tepki mesajı"]++
		}
	}
	p.Days = len(days)

	if p.Messages > 0 {
		night := p.Hours[0] + p.Hours[1] + p.Hours[2] + p.Hours[3] + p.Hours[4] + p.Hours[5]
		p.NightRatio = float64(night) / float64(p.Messages)
	}
	p.SleepStart, p.SleepKnown = sleepWindow(p.Hours, p.Messages)

	keys := make([]string, 0, len(month))
	for k := range month {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		p.Monthly = append(p.Monthly, MonthCount{k, month[k]})
	}

	if textN > 0 {
		f := float64(textN)
		p.Language = Language{TextMessages: textN, AvgWords: float64(wordTotal) / f, AvgChars: float64(charTotal) / f,
			QuestionRatio: float64(q) / f, ExclaimRatio: float64(ex) / f, LinkRatio: float64(link) / f,
			CapsRatio: float64(caps) / f, EllipsisRatio: float64(ell) / f, EmojiPerMsg: float64(emojiTotal) / f}
	}
	if len(firstWords) > 0 {
		uniq := map[string]bool{}
		for _, w := range firstWords {
			uniq[w] = true
		}
		p.Language.Richness = float64(len(uniq)) / float64(len(firstWords))
	}
	p.Language.TopWords = top(words, 30)
	p.Language.TopEmoji = top(emoji, 12)
	p.Language.Kinds = top(kinds, 12)

	p.Partners, p.Latency = buildPartners(c, s)
	p.Periods = detectPeriods(p.Monthly)
	p.Groups = BuildGroups(c, s, opt)
	p.Activity = buildActivity(c, s, loc)
	if p.Messages >= 200 {
		whole := 0
		for _, m := range msgs {
			if m.TS.Second() == 0 && m.TS.Nanosecond() == 0 {
				whole++
			}
		}
		if float64(whole)/float64(len(msgs)) >= 0.98 {
			p.Notes = append(p.Notes, "Bu kaynakta zaman damgaları dakika çözünürlüğünde (saniye yok): yanıt süreleri ve aynı dakikadaki mesajların sırası kesin değildir; 0 sn medyan, \"aynı dakika içinde\" demektir.")
		}
	}
	if p.Messages < 200 {
		p.Notes = append(p.Notes, "Bu kişi için 200'den az mesaj var; ritim ve dil metrikleri düşük güvenlidir.")
	}
	if p.Messages == 0 && p.Activity == nil {
		p.Notes = append(p.Notes, "Bu kişinin kimlikleriyle eşleşen mesaj ya da etkinlik bulunamadı. Kimlikleri kontrol et.")
	}
	return p
}

// sleepWindow, etkinliğin en düşük olduğu 6 saatlik dairesel pencerenin başlangıç saatini bulur.
func sleepWindow(h [24]int, total int) (int, bool) {
	if total < 100 {
		return 0, false
	}
	best, bestSum := 0, 1<<62
	for s := 0; s < 24; s++ {
		sum := 0
		for k := 0; k < 6; k++ {
			sum += h[(s+k)%24]
		}
		if sum < bestSum {
			best, bestSum = s, sum
		}
	}
	return best, true
}

// Active etkin gün sayısını döndürür.
func (p Profile) Active() int { return p.Days }
