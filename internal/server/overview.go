package server

import (
	"net/http"
	"sort"
	"time"

	"github.com/MustafaKemal0146/joe/internal/analytics"
)

// PersonOverview kişiler listesindeki kartın deterministik özetidir; LLM kullanmaz.
type PersonOverview struct {
	ID            int64     `json:"id"`
	Messages      int       `json:"messages"`
	First         time.Time `json:"first"`
	Last          time.Time `json:"last"`
	ActiveDays    int       `json:"active_days"`
	Platforms     []string  `json:"platforms"`
	TopPartner    string    `json:"top_partner,omitempty"`
	TopShare      float64   `json:"top_share,omitempty"` // en yoğun sohbetin toplam içindeki payı
	Conversations int       `json:"conversations"`
	NightRatio    float64   `json:"night_ratio"`
	ReplyMedian   float64   `json:"reply_median_sec"`
	PeakHour      int       `json:"peak_hour"`
	Notes         int       `json:"notes"`
}

func (s *Server) peopleOverview(w http.ResponseWriter, r *http.Request) {
	ps, err := s.Store.People()
	if err != nil {
		serverErr(w, err)
		return
	}
	out := make([]PersonOverview, 0, len(ps))
	if len(ps) == 0 {
		writeJSON(w, 200, out)
		return
	}
	c, err := s.loadCorpus()
	if err != nil {
		serverErr(w, err)
		return
	}
	for _, p := range ps {
		subj := analytics.NewSubject(p, c)
		pr := s.profileFor(p, c, subj)
		o := PersonOverview{ID: p.ID, Messages: pr.Messages, First: pr.First, Last: pr.Last,
			ActiveDays: pr.Days, NightRatio: pr.NightRatio, ReplyMedian: pr.Latency.Median, Conversations: len(pr.Partners)}
		seen := map[string]bool{}
		best, sum := 0, 0
		for _, pt := range pr.Partners {
			if !seen[pt.Platform] {
				seen[pt.Platform] = true
				o.Platforms = append(o.Platforms, pt.Platform)
			}
			sum += pt.Total
			if pt.Total > best {
				best, o.TopPartner = pt.Total, pt.Name
			}
		}
		sort.Strings(o.Platforms)
		if sum > 0 {
			o.TopShare = float64(best) / float64(sum)
		}
		for h, n := range pr.Hours {
			if n > pr.Hours[o.PeakHour] {
				o.PeakHour = h
			}
		}
		if ns, err := s.Store.Notes(p.ID); err == nil {
			o.Notes = len(ns)
		}
		out = append(out, o)
	}
	writeJSON(w, 200, out)
}
