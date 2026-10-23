package analytics

import (
	"sort"
	"strings"
	"time"

	"github.com/MustafaKemal0146/joe/internal/model"
	"github.com/MustafaKemal0146/joe/internal/store"
)

// Pair iki tarafın (kişi, karşı taraf) ayrı ayrı sayılarıdır.
type Pair struct {
	Subject int `json:"subject"`
	Partner int `json:"partner"`
}

// PairF iki tarafın ondalık değerleridir.
type PairF struct {
	Subject float64 `json:"subject"`
	Partner float64 `json:"partner"`
}

// DyadMonth bir ilişkinin aylık dengesi.
type DyadMonth struct {
	Month   string  `json:"month"`
	Subject int     `json:"subject"`
	Partner int     `json:"partner"`
	LatSubj float64 `json:"latency_subject"` // saniye medyan
}

// Partner bir kişinin tek bir bire-bir sohbetteki karşılıklılık özetidir.
type Partner struct {
	Name         string  `json:"name"`
	Platform     string  `json:"platform"`
	ConvID       int64   `json:"conv_id"`
	SourceID     int64   `json:"source_id"`
	Total        int     `json:"total"`
	Messages     Pair    `json:"messages"`
	SubjectShare float64 `json:"subject_share"`
	Sessions     int     `json:"sessions"`
	Starts       Pair    `json:"starts"`
	Ends         Pair    `json:"ends"`
	DoubleTexts  Pair    `json:"double_texts"`
	Latency      struct {
		Subject LatencyStat `json:"subject"`
		Partner LatencyStat `json:"partner"`
	} `json:"latency"`
	AvgWords PairF     `json:"avg_words"`
	Unsent   Pair      `json:"unsent"`
	Media    Pair      `json:"media"`
	First    time.Time `json:"first"`
	Last     time.Time `json:"last"`
	// LongestSilenceDays en uzun sessizlik; SilenceFrom/To tarihleridir.
	LongestSilenceDays float64     `json:"longest_silence_days"`
	SilenceFrom        time.Time   `json:"silence_from"`
	Monthly            []DyadMonth `json:"monthly"`
}

func (p *Pair) add(subject bool) {
	if subject {
		p.Subject++
	} else {
		p.Partner++
	}
}

// PartnerOf, dış paketler için partnerOf'un dışa açık adıdır.
func PartnerOf(conv store.Conversation, s Subject) (string, bool) { return partnerOf(conv, s) }

// partnerOf, bire-bir sohbette karşı tarafın adını verir (özne katılımcı değilse boş).
func partnerOf(conv store.Conversation, s Subject) (string, bool) {
	if conv.IsGroup || len(conv.Participants) != 2 {
		return "", false
	}
	a, b := conv.Participants[0], conv.Participants[1]
	switch {
	case s.Is(a) && !s.Is(b):
		return b, true
	case s.Is(b) && !s.Is(a):
		return a, true
	}
	return "", false
}

func buildPartners(c *store.Corpus, s Subject) ([]Partner, LatencyStat) {
	by := convMsgs(c)
	var out []Partner
	var allLat []float64
	for _, conv := range c.Convs {
		name, ok := partnerOf(conv, s)
		if !ok {
			continue
		}
		p := analyseDyad(by[conv.ID], s, name)
		if p.Total == 0 {
			continue
		}
		p.Platform = c.Sources[conv.SourceID].Platform
		p.ConvID, p.SourceID = conv.ID, conv.SourceID
		out = append(out, p.Partner)
		allLat = append(allLat, p.subjLat...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Total > out[j].Total })
	if len(out) > 25 {
		out = out[:25]
	}
	return out, latencyOf(allLat)
}

func latencyOf(v []float64) LatencyStat {
	return LatencyStat{Median: median(v), P90: percentile(v, 0.9), N: len(v)}
}

type dyadResult struct {
	Partner
	subjLat []float64
}

func analyseDyad(msgs []store.MessageRow, s Subject, partnerName string) dyadResult {
	var r dyadResult
	r.Name = partnerName
	var subjLat, partLat []float64
	var wordsS, wordsP, textS, textP int
	monthSubj, monthPart := map[string]int{}, map[string]int{}
	monthLat := map[string][]float64{}

	var prev *store.MessageRow
	var last *store.MessageRow
	for i := range msgs {
		m := &msgs[i]
		if m.Kind == model.KindSystem {
			continue
		}
		fromSubject := s.Is(m.Sender)
		r.Total++
		r.Messages.add(fromSubject)
		if r.First.IsZero() {
			r.First = m.TS
		}
		r.Last = m.TS
		mk := m.TS.Format("2006-01")
		if fromSubject {
			monthSubj[mk]++
		} else {
			monthPart[mk]++
		}
		switch m.Kind {
		case model.KindText, model.KindShare:
			n := len(strings.Fields(m.Text))
			if fromSubject {
				wordsS += n
				textS++
			} else {
				wordsP += n
				textP++
			}
		case model.KindUnsent, model.KindDeleted: // Instagram'da geri alınan, WhatsApp'ta silinen
			r.Unsent.add(fromSubject)
		case model.KindMedia:
			r.Media.add(fromSubject)
		}
		newSession := prev == nil || m.TS.Sub(prev.TS) > sessionGap
		if prev != nil {
			gap := m.TS.Sub(prev.TS)
			if gap > r.LongestSilence() {
				r.LongestSilenceDays = gap.Hours() / 24
				r.SilenceFrom = prev.TS
			}
			prevSubject := s.Is(prev.Sender)
			if !newSession {
				if prevSubject != fromSubject {
					sec := gap.Seconds()
					if fromSubject {
						subjLat = append(subjLat, sec)
						monthLat[mk] = append(monthLat[mk], sec)
					} else {
						partLat = append(partLat, sec)
					}
				} else if gap >= 10*time.Minute {
					r.DoubleTexts.add(fromSubject)
				}
			}
			if newSession && last != nil {
				r.Ends.add(s.Is(last.Sender))
			}
		}
		if newSession {
			r.Sessions++
			r.Starts.add(fromSubject)
		}
		prev, last = m, m
	}
	if last != nil {
		r.Ends.add(s.Is(last.Sender))
	}
	if r.Total > 0 {
		r.SubjectShare = float64(r.Messages.Subject) / float64(r.Total)
	}
	if textS > 0 {
		r.AvgWords.Subject = float64(wordsS) / float64(textS)
	}
	if textP > 0 {
		r.AvgWords.Partner = float64(wordsP) / float64(textP)
	}
	r.Latency.Subject, r.Latency.Partner = latencyOf(subjLat), latencyOf(partLat)
	r.subjLat = subjLat

	months := map[string]bool{}
	for k := range monthSubj {
		months[k] = true
	}
	for k := range monthPart {
		months[k] = true
	}
	keys := make([]string, 0, len(months))
	for k := range months {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		r.Monthly = append(r.Monthly, DyadMonth{Month: k, Subject: monthSubj[k], Partner: monthPart[k], LatSubj: median(monthLat[k])})
	}
	return r
}

// LongestSilence mevcut en uzun sessizliği süre olarak verir.
func (p *dyadResult) LongestSilence() time.Duration {
	return time.Duration(p.LongestSilenceDays * 24 * float64(time.Hour))
}

// DyadConv bir kişinin bire-bir sohbetinin kaydıdır (kanıt seçimi gibi dış paketler için).
type DyadConv struct {
	Partner string
	Conv    store.Conversation
}

// DyadConversations kişinin katıldığı bire-bir sohbetleri, mesaj sayısına göre azalan döndürür.
func DyadConversations(c *store.Corpus, s Subject) []DyadConv {
	var out []DyadConv
	for _, conv := range c.Convs {
		if name, ok := partnerOf(conv, s); ok {
			out = append(out, DyadConv{name, conv})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Conv.Count > out[j].Conv.Count })
	return out
}

// ConvMessages mesajları sohbet kimliğine göre gruplar (zamana göre sıralı).
func ConvMessages(c *store.Corpus) map[int64][]store.MessageRow { return convMsgs(c) }
