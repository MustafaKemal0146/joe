package analytics

import (
	"sort"
	"time"
)

// Period etkinliğin olağandan belirgin biçimde yoğun ya da sessiz olduğu dönemdir. Konsey için
// kanıt seçiminde ilgi çekici kesitleri işaretler.
type Period struct {
	From     string  `json:"from"` // 2026-03
	To       string  `json:"to"`
	Kind     string  `json:"kind"` // surge | lull
	Count    int     `json:"count"`
	Baseline float64 `json:"baseline"`
}

// fillMonths eksik ayları sıfırla doldurur.
func fillMonths(in []MonthCount) []MonthCount {
	if len(in) == 0 {
		return nil
	}
	start, _ := time.Parse("2006-01", in[0].Month)
	end, _ := time.Parse("2006-01", in[len(in)-1].Month)
	have := map[string]int{}
	for _, m := range in {
		have[m.Month] = m.Count
	}
	var out []MonthCount
	for t := start; !t.After(end); t = t.AddDate(0, 1, 0) {
		k := t.Format("2006-01")
		out = append(out, MonthCount{k, have[k]})
	}
	return out
}

func detectPeriods(monthly []MonthCount) []Period {
	ms := fillMonths(monthly)
	if len(ms) < 6 {
		return nil
	}
	type flag struct {
		i    int
		kind string
		base float64
	}
	var flags []flag
	for i, m := range ms {
		var win []float64
		for j := i - 3; j <= i+3; j++ {
			if j != i && j >= 0 && j < len(ms) {
				win = append(win, float64(ms[j].Count))
			}
		}
		if len(win) < 4 {
			continue
		}
		med := median(win)
		dev := make([]float64, len(win))
		for k, w := range win {
			d := w - med
			if d < 0 {
				d = -d
			}
			dev[k] = d
		}
		mad := median(dev)
		if mad < 1 {
			mad = 1
		}
		c := float64(m.Count)
		switch {
		case c >= 20 && c > med+3*mad && c >= 2*med:
			flags = append(flags, flag{i, "surge", med})
		case med >= 40 && c < med*0.25:
			flags = append(flags, flag{i, "lull", med})
		}
	}
	sort.Slice(flags, func(a, b int) bool { return flags[a].i < flags[b].i })
	var out []Period
	for _, f := range flags {
		n := len(out)
		if n > 0 && out[n-1].Kind == f.kind && monthIndex(ms, out[n-1].To)+1 == f.i {
			out[n-1].To = ms[f.i].Month
			out[n-1].Count += ms[f.i].Count
			continue
		}
		out = append(out, Period{From: ms[f.i].Month, To: ms[f.i].Month, Kind: f.kind, Count: ms[f.i].Count, Baseline: f.base})
	}
	return out
}

func monthIndex(ms []MonthCount, key string) int {
	for i, m := range ms {
		if m.Month == key {
			return i
		}
	}
	return -1
}
