package analytics

import (
	"sort"
	"time"

	"github.com/MustafaKemal0146/joe/internal/model"
	"github.com/MustafaKemal0146/joe/internal/store"
)

// GroupStat bir kişinin tek bir grup sohbetindeki davranışıdır (bire-bir metriklerle karışmaz).
type GroupStat struct {
	Title        string       `json:"title"`
	ConvID       int64        `json:"conv_id"`
	SourceID     int64        `json:"source_id"`
	Platform     string       `json:"platform"`
	Members      int          `json:"members"`
	Total        int          `json:"total"`
	SubjectMsgs  int          `json:"subject_msgs"`
	SubjectShare float64      `json:"subject_share"`
	Rank         int          `json:"rank"` // 1 = gruptaki en aktif üye
	First        time.Time    `json:"first"`
	Last         time.Time    `json:"last"`
	Hours        [24]int      `json:"hours"`
	Monthly      []MonthCount `json:"monthly"`
	// RepliesTo kişiden hemen önce (5 dk içinde) yazan üyeler: kime yanıt veriyor gibi görünüyor.
	RepliesTo []Count `json:"replies_to"`
	// RepliedBy kişiden hemen sonra yazan üyeler.
	RepliedBy []Count `json:"replied_by"`
	Top       []Count `json:"top"` // gruptaki en çok yazan 5 üye
}

const replyWindow = 5 * time.Minute

// groupConvs kişinin katıldığı grup sohbetlerini döndürür.
func groupConvs(c *store.Corpus, s Subject) []store.Conversation {
	var out []store.Conversation
	by := convMsgs(c)
	for _, conv := range c.Convs {
		if !conv.IsGroup {
			continue
		}
		in := false
		for _, p := range conv.Participants {
			if s.Is(p) {
				in = true
			}
		}
		if !in {
			for _, m := range by[conv.ID] {
				if s.Is(m.Sender) {
					in = true
					break
				}
			}
		}
		if in {
			out = append(out, conv)
		}
	}
	return out
}

// BuildGroups kişinin grup sohbetlerindeki payını, sırasını, yanıt örüntülerini ve saat ritmini çıkarır.
func BuildGroups(c *store.Corpus, s Subject, opt Options) []GroupStat {
	loc := opt.loc()
	by := convMsgs(c)
	var out []GroupStat
	for _, conv := range groupConvs(c, s) {
		g := GroupStat{Title: conv.Title, ConvID: conv.ID, SourceID: conv.SourceID, Platform: c.Sources[conv.SourceID].Platform, Members: len(conv.Participants)}
		counts := map[string]int{}
		month := map[string]int{}
		replies, repliedBy := map[string]int{}, map[string]int{}
		var prev *store.MessageRow
		msgs := by[conv.ID]
		for i := range msgs {
			m := &msgs[i]
			if m.Kind == model.KindSystem {
				continue
			}
			g.Total++
			counts[m.Sender]++
			if g.First.IsZero() {
				g.First = m.TS
			}
			g.Last = m.TS
			mine := s.Is(m.Sender)
			if mine {
				g.SubjectMsgs++
				t := m.TS.In(loc)
				g.Hours[t.Hour()]++
				month[t.Format("2006-01")]++
			}
			if prev != nil && m.TS.Sub(prev.TS) <= replyWindow && model.Norm(prev.Sender) != model.Norm(m.Sender) {
				switch {
				case mine:
					replies[prev.Sender]++
				case s.Is(prev.Sender):
					repliedBy[m.Sender]++
				}
			}
			prev = m
		}
		if g.SubjectMsgs == 0 {
			continue
		}
		g.SubjectShare = float64(g.SubjectMsgs) / float64(g.Total)
		g.Rank = 1
		for sender, n := range counts {
			if !s.Is(sender) && n > g.SubjectMsgs {
				g.Rank++
			}
		}
		g.Top = top(counts, 5)
		g.RepliesTo, g.RepliedBy = top(replies, 3), top(repliedBy, 3)
		keys := make([]string, 0, len(month))
		for k := range month {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			g.Monthly = append(g.Monthly, MonthCount{k, month[k]})
		}
		out = append(out, g)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].SubjectMsgs > out[j].SubjectMsgs })
	if len(out) > 15 {
		out = out[:15]
	}
	return out
}
