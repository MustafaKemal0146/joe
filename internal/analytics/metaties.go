package analytics

import (
	"sort"
	"time"

	"github.com/MustafaKemal0146/joe/internal/model"
	"github.com/MustafaKemal0146/joe/internal/store"
)

// metaWeight bir etkileşim türünün "ilgi" ağırlığıdır; mesaj içeriğine hiç bakmadan hesaplanır.
var metaWeight = map[string]float64{
	model.EvStoryView: 1, model.EvLikePost: 1, model.EvLikeComment: 1, model.EvStoryLike: 1.5,
	model.EvSaved: 1.5, model.EvStoryInteraction: 2, model.EvComment: 3, model.EvSearchAccount: 3,
}

// MetaTie bir hesapla (mesajlaşma olmadan bile) kurulan meta etkileşimdir.
type MetaTie struct {
	Handle string         `json:"handle"`
	Weight float64        `json:"weight"`
	Counts map[string]int `json:"counts"`
	First  time.Time      `json:"first"`
	Last   time.Time      `json:"last"`
	// DM*: görünen ad ile kullanıcı adı benzerliğinden kurulan, KESİN OLMAYAN sohbet eşleşmesi.
	DMName  string  `json:"dm_name,omitempty"`
	DMScore float64 `json:"dm_score,omitempty"`
	DMTotal int     `json:"dm_total,omitempty"`
	DMConv  int64   `json:"dm_conv,omitempty"`
}

// FollowSplit takip dengesi (kullanıcı adı eşitliğiyle).
type FollowSplit struct {
	Following        int `json:"following"`
	Followers        int `json:"followers"`
	Mutual           int `json:"mutual"`
	NotFollowingBack int `json:"not_following_back"` // sen takip ediyorsun, o etmiyor
	FansNotFollowed  int `json:"fans_not_followed"`  // seni takip ediyor, sen etmiyorsun
}

// MetaView mesaj içeriğine bakmadan, yalnızca hesap etkinliği meta verisinden çıkan bağlar ve ritimdir.
type MetaView struct {
	Ties    []MetaTie    `json:"ties"`
	Follow  FollowSplit  `json:"follow"`
	Hours   [24]int      `json:"hours"`
	Weekday [7]int       `json:"weekday"`
	Monthly []MonthCount `json:"monthly"`
	Events  int          `json:"events"`
}

// BuildMetaView öznenin kendi hesap etkinliğinden (hikâye izleme, beğeni, yorum, kaydetme, arama,
// takip, giriş) bağları ve etkinlik saatlerini çıkarır. own, öznenin kendi hesaplarının kaynak
// kimlikleridir (nil: hepsi). guide, görünen ad eşleştirmesi içindir.
func BuildMetaView(c *store.Corpus, own map[int64]bool, guide []GuideRow, loc *time.Location, maxTies int) MetaView {
	if loc == nil {
		loc = time.UTC
	}
	ties := map[string]*MetaTie{}
	names := map[string]string{} // hesap → görünen ad (olay metninden)
	following, followers := map[string]bool{}, map[string]bool{}
	var v MetaView
	monthly := map[string]int{}
	rhythmKinds := map[string]bool{model.EvLogin: true}
	for k := range metaWeight {
		rhythmKinds[k] = true
	}
	for _, ev := range c.Events {
		if own != nil && !own[ev.SourceID] {
			continue // başka hesabın etkinliği
		}
		switch ev.Kind {
		case model.EvFollowing:
			following[ev.Counterpart] = true
		case model.EvFollower:
			followers[ev.Actor] = true
		}
		if rhythmKinds[ev.Kind] && !ev.TS.IsZero() {
			t := ev.TS.In(loc)
			v.Hours[t.Hour()]++
			v.Weekday[(int(t.Weekday())+6)%7]++
			monthly[t.Format("2006-01")]++
			v.Events++
		}
		w, ok := metaWeight[ev.Kind]
		if !ok || ev.Counterpart == "" || ev.Counterpart == model.Owner {
			continue
		}
		if ev.Text != "" && metaWeight[ev.Kind] > 0 && names[ev.Counterpart] == "" && ev.Kind != model.EvStoryInteraction && ev.Kind != model.EvComment && ev.Kind != model.EvSearchAccount {
			names[ev.Counterpart] = ev.Text
		}
		t := ties[ev.Counterpart]
		if t == nil {
			t = &MetaTie{Handle: ev.Counterpart, Counts: map[string]int{}}
			ties[ev.Counterpart] = t
		}
		t.Counts[ev.Kind]++
		t.Weight += w
		if !ev.TS.IsZero() {
			if t.First.IsZero() || ev.TS.Before(t.First) {
				t.First = ev.TS
			}
			if ev.TS.After(t.Last) {
				t.Last = ev.TS
			}
		}
	}
	for h := range following {
		v.Follow.Following++
		if followers[h] {
			v.Follow.Mutual++
		} else {
			v.Follow.NotFollowingBack++
		}
	}
	for h := range followers {
		v.Follow.Followers++
		if !following[h] {
			v.Follow.FansNotFollowed++
		}
	}
	list := make([]*MetaTie, 0, len(ties))
	for _, t := range ties {
		list = append(list, t)
	}
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].Weight != list[j].Weight {
			return list[i].Weight > list[j].Weight
		}
		return list[i].Handle < list[j].Handle
	})
	if maxTies > 0 && len(list) > maxTies {
		list = list[:maxTies]
	}
	type pb struct {
		m map[string]int
		n int
	}
	pbs := make([]pb, len(guide))
	for i, g := range guide {
		pbs[i].m, pbs[i].n = bigramSet(g.Key)
	}
	for _, t := range list {
		hb, hn := bigramSet(model.Norm(t.Handle))
		best, score := -1, 0.0
		if n := model.Norm(names[t.Handle]); n != "" { // görünen ad kesin eşleşirse benzerliğe gerek yok
			for i := range guide {
				if guide[i].Key == n {
					best, score = i, 1
					break
				}
			}
		}
		if best < 0 && len([]rune(model.Norm(t.Handle))) >= 4 {
			for i := range guide {
				if d := diceSets(hb, hn, pbs[i].m, pbs[i].n); d > score {
					best, score = i, d
				}
			}
		}
		if best >= 0 && score >= metaThreshold {
			t.DMName, t.DMScore, t.DMTotal, t.DMConv = guide[best].Name, score, guide[best].Total, guide[best].ConvID
		}
		v.Ties = append(v.Ties, *t)
	}
	months := make([]string, 0, len(monthly))
	for m := range monthly {
		months = append(months, m)
	}
	sort.Strings(months)
	for _, m := range months {
		v.Monthly = append(v.Monthly, MonthCount{Month: m, Count: monthly[m]})
	}
	return v
}
