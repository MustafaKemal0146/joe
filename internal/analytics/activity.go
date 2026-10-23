package analytics

import (
	"math"
	"sort"
	"strings"
	"time"

	"github.com/MustafaKemal0146/joe/internal/model"
	"github.com/MustafaKemal0146/joe/internal/store"
)

// SearchStat bir aranan hesap/ifade ve tekrar sayısıdır.
type SearchStat struct {
	Key   string    `json:"key"`
	Count int       `json:"count"`
	First time.Time `json:"first"`
	Last  time.Time `json:"last"`
}

// FollowMonth aylık takip hareketidir.
type FollowMonth struct {
	Month      string `json:"month"`
	Following  int    `json:"following"`
	Followers  int    `json:"followers"`
	Unfollowed int    `json:"unfollowed"`
}

// Activity kişinin kendi hesabındaki (Instagram) mesaj dışı etkinlik özetidir.
type Activity struct {
	Counts         []Count         `json:"counts"`
	Searched       []SearchStat    `json:"searched"`
	Keywords       []SearchStat    `json:"keywords"`
	Liked          []Count         `json:"liked"`
	Commented      []Count         `json:"commented"`
	StoryTouched   []Count         `json:"story"`
	Saved          []Count         `json:"saved"`
	Follow         FollowSummary   `json:"follow"`
	FollowMonthly  []FollowMonth   `json:"follow_monthly"`
	Logins         LoginSummary    `json:"logins"`
	Posts          PostSummary     `json:"posts"`
	Media          MediaSummary    `json:"media"`
	Interests      []string        `json:"interests"`
	ProfileChanges []ProfileChange `json:"profile_changes"`
}

// Place yaklaşık (≈1 km) bir konum kümesidir.
type Place struct {
	Lat   float64   `json:"lat"`
	Lon   float64   `json:"lon"`
	Count int       `json:"count"`
	First time.Time `json:"first"`
	Last  time.Time `json:"last"`
}

// MediaSummary fotoğraf EXIF özetidir: cihazlar ve konumlar. Konum hassastır; yalnızca yerelde gösterilir.
type MediaSummary struct {
	WithMeta int       `json:"with_meta"`
	WithGPS  int       `json:"with_gps"`
	Devices  []Count   `json:"devices"`
	Places   []Place   `json:"places"`
	First    time.Time `json:"first"`
	Last     time.Time `json:"last"`
}

// PostSummary kendi paylaşımlarının özetidir.
type PostSummary struct {
	Posts   int          `json:"posts"`
	Stories int          `json:"stories"`
	Reels   int          `json:"reels"`
	Monthly []MonthCount `json:"monthly"`
	Hours   [24]int      `json:"hours"`
	First   time.Time    `json:"first"`
	Last    time.Time    `json:"last"`
}

// ProfileChange kullanıcı adı, e-posta, telefon ya da biyografi değişimidir.
type ProfileChange struct {
	TS   time.Time `json:"ts"`
	What string    `json:"what"`
	From string    `json:"from"`
	To   string    `json:"to"`
}

// FollowSummary takip ilişkisi sayılarıdır.
type FollowSummary struct {
	Followers        int `json:"followers"`
	Following        int `json:"following"`
	Mutual           int `json:"mutual"`
	NotFollowingBack int `json:"not_following_back"` // takip ettiği ama onu takip etmeyen
	Fans             int `json:"fans"`               // onu takip eden ama takip etmediği
	Unfollowed       int `json:"unfollowed"`
	Pending          int `json:"pending"`
	CloseFriends     int `json:"close_friends"`
	Blocked          int `json:"blocked"`
	Restricted       int `json:"restricted"`
}

// LoginSummary giriş etkinliği özetidir.
type LoginSummary struct {
	Total       int       `json:"total"`
	DistinctIPs int       `json:"distinct_ips"`
	Devices     []Count   `json:"devices"`
	Hours       [24]int   `json:"hours"`
	First       time.Time `json:"first"`
	Last        time.Time `json:"last"`
}

// deviceOf kullanıcı aracısından kısa bir cihaz etiketi çıkarır.
func deviceOf(ua string) string {
	l := strings.ToLower(ua)
	var os, app string
	switch {
	case strings.Contains(l, "iphone"):
		os = "iPhone"
	case strings.Contains(l, "ipad"):
		os = "iPad"
	case strings.Contains(l, "android"):
		os = "Android"
	case strings.Contains(l, "windows"):
		os = "Windows"
	case strings.Contains(l, "mac os") || strings.Contains(l, "macintosh"):
		os = "macOS"
	case strings.Contains(l, "linux"):
		os = "Linux"
	}
	switch {
	case strings.Contains(l, "instagram"):
		app = "Instagram uygulaması"
	case strings.Contains(l, "firefox"):
		app = "Firefox"
	case strings.Contains(l, "edg"):
		app = "Edge"
	case strings.Contains(l, "chrome"):
		app = "Chrome"
	case strings.Contains(l, "safari"):
		app = "Safari"
	}
	switch {
	case os != "" && app != "":
		return os + " · " + app
	case os != "":
		return os
	case app != "":
		return app
	}
	return "bilinmeyen"
}

func buildActivity(c *store.Corpus, s Subject, loc *time.Location) *Activity {
	if len(s.OwnSources) == 0 {
		return nil
	}
	a := &Activity{}
	kinds := map[string]int{}
	liked, commented, story, saved := map[string]int{}, map[string]int{}, map[string]int{}, map[string]int{}
	type agg struct {
		n           int
		first, last time.Time
	}
	acc, kw := map[string]*agg{}, map[string]*agg{}
	followers, following := map[string]bool{}, map[string]bool{}
	fm := map[string]*FollowMonth{}
	devices := map[string]int{}
	ips := map[string]bool{}
	postMonth := map[string]int{}
	mediaDevices := map[string]int{}
	var places []*Place
	var placeSums [][2]float64
	interests := map[string]bool{}
	any := false

	bump := func(m map[string]*agg, key string, t time.Time) {
		g := m[key]
		if g == nil {
			g = &agg{first: t, last: t}
			m[key] = g
		}
		g.n++
		if !t.IsZero() && (g.first.IsZero() || t.Before(g.first)) {
			g.first = t
		}
		if t.After(g.last) {
			g.last = t
		}
	}
	month := func(t time.Time) *FollowMonth {
		if t.IsZero() {
			return nil
		}
		k := t.Format("2006-01")
		if fm[k] == nil {
			fm[k] = &FollowMonth{Month: k}
		}
		return fm[k]
	}

	for _, e := range c.Events {
		if !s.OwnSources[e.SourceID] {
			continue
		}
		any = true
		kinds[e.Kind]++
		key := model.Norm(e.Counterpart)
		switch e.Kind {
		case model.EvSearchAccount:
			bump(acc, e.Counterpart, e.TS)
		case model.EvSearchKeyword:
			bump(kw, e.Text, e.TS)
		case model.EvLikePost, model.EvLikeComment:
			liked[e.Counterpart]++
		case model.EvComment:
			commented[e.Counterpart]++
		case model.EvStoryLike, model.EvStoryInteraction:
			story[e.Counterpart]++
		case model.EvSaved:
			saved[e.Counterpart]++
		case model.EvFollower:
			followers[model.Norm(e.Actor)] = true
			if m := month(e.TS); m != nil {
				m.Followers++
			}
		case model.EvFollowing:
			following[key] = true
			if m := month(e.TS); m != nil {
				m.Following++
			}
		case model.EvUnfollowed:
			a.Follow.Unfollowed++
			if m := month(e.TS); m != nil {
				m.Unfollowed++
			}
		case model.EvFollowRequest:
			a.Follow.Pending++
		case model.EvCloseFriend:
			a.Follow.CloseFriends++
		case model.EvBlocked:
			a.Follow.Blocked++
		case model.EvRestricted:
			a.Follow.Restricted++
		case model.EvPost, model.EvStory, model.EvReel:
			switch e.Kind {
			case model.EvPost:
				a.Posts.Posts++
			case model.EvStory:
				a.Posts.Stories++
			default:
				a.Posts.Reels++
			}
			if !e.TS.IsZero() {
				t := e.TS.In(loc)
				postMonth[t.Format("2006-01")]++
				a.Posts.Hours[t.Hour()]++
				if a.Posts.First.IsZero() || e.TS.Before(a.Posts.First) {
					a.Posts.First = e.TS
				}
				if e.TS.After(a.Posts.Last) {
					a.Posts.Last = e.TS
				}
			}
		case model.EvMediaMeta:
			a.Media.WithMeta++
			if e.Text != "" {
				mediaDevices[e.Text]++
			}
			if !e.TS.IsZero() {
				if a.Media.First.IsZero() || e.TS.Before(a.Media.First) {
					a.Media.First = e.TS
				}
				if e.TS.After(a.Media.Last) {
					a.Media.Last = e.TS
				}
			}
			if lat, ok := e.Meta["lat"].(float64); ok {
				lon, _ := e.Meta["lon"].(float64)
				a.Media.WithGPS++
				// Açgözlü yarıçap kümeleme (~1 km): sınır etkisi olan ızgara yerine merkezden uzaklık.
				idx := -1
				for i, pl := range places {
					if distKM(pl.Lat, pl.Lon, lat, lon) < 1.0 {
						idx = i
						break
					}
				}
				if idx < 0 {
					places = append(places, &Place{Lat: lat, Lon: lon, First: e.TS, Last: e.TS})
					placeSums = append(placeSums, [2]float64{})
					idx = len(places) - 1
				}
				pl := places[idx]
				pl.Count++
				placeSums[idx][0] += lat
				placeSums[idx][1] += lon
				pl.Lat, pl.Lon = placeSums[idx][0]/float64(pl.Count), placeSums[idx][1]/float64(pl.Count)
				if !e.TS.IsZero() && (pl.First.IsZero() || e.TS.Before(pl.First)) {
					pl.First = e.TS
				}
				if e.TS.After(pl.Last) {
					pl.Last = e.TS
				}
			}
		case model.EvProfileChange:
			from, _ := e.Meta["from"].(string)
			to, _ := e.Meta["to"].(string)
			a.ProfileChanges = append(a.ProfileChanges, ProfileChange{TS: e.TS, What: e.Text, From: from, To: to})
		case model.EvInterest:
			if e.Text != "" {
				interests[e.Text] = true
			}
		case model.EvLogin:
			a.Logins.Total++
			if ua, _ := e.Meta["user_agent"].(string); ua != "" {
				devices[deviceOf(ua)]++
			}
			if ip, _ := e.Meta["ip"].(string); ip != "" {
				ips[ip] = true
			}
			if !e.TS.IsZero() {
				a.Logins.Hours[e.TS.In(loc).Hour()]++
				if a.Logins.First.IsZero() || e.TS.Before(a.Logins.First) {
					a.Logins.First = e.TS
				}
				if e.TS.After(a.Logins.Last) {
					a.Logins.Last = e.TS
				}
			}
		}
	}
	if !any {
		return nil
	}
	a.Counts = top(kinds, 20)
	a.Liked, a.Commented, a.StoryTouched, a.Saved = top(liked, 15), top(commented, 15), top(story, 15), top(saved, 15)
	toStats := func(m map[string]*agg) []SearchStat {
		out := make([]SearchStat, 0, len(m))
		for k, g := range m {
			out = append(out, SearchStat{k, g.n, g.first, g.last})
		}
		sort.Slice(out, func(i, j int) bool {
			if out[i].Count != out[j].Count {
				return out[i].Count > out[j].Count
			}
			return out[i].Key < out[j].Key
		})
		if len(out) > 20 {
			out = out[:20]
		}
		return out
	}
	a.Searched, a.Keywords = toStats(acc), toStats(kw)
	a.Follow.Followers, a.Follow.Following = len(followers), len(following)
	for k := range following {
		if followers[k] {
			a.Follow.Mutual++
		} else {
			a.Follow.NotFollowingBack++
		}
	}
	for k := range followers {
		if !following[k] {
			a.Follow.Fans++
		}
	}
	keys := make([]string, 0, len(fm))
	for k := range fm {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		a.FollowMonthly = append(a.FollowMonthly, *fm[k])
	}
	pm := make([]string, 0, len(postMonth))
	for k := range postMonth {
		pm = append(pm, k)
	}
	sort.Strings(pm)
	for _, k := range pm {
		a.Posts.Monthly = append(a.Posts.Monthly, MonthCount{k, postMonth[k]})
	}
	a.Media.Devices = top(mediaDevices, 8)
	for _, pl := range places {
		pl.Lat, pl.Lon = math.Round(pl.Lat*1000)/1000, math.Round(pl.Lon*1000)/1000
		a.Media.Places = append(a.Media.Places, *pl)
	}
	sort.Slice(a.Media.Places, func(i, j int) bool {
		if a.Media.Places[i].Count != a.Media.Places[j].Count {
			return a.Media.Places[i].Count > a.Media.Places[j].Count
		}
		return a.Media.Places[i].Lat < a.Media.Places[j].Lat
	})
	if len(a.Media.Places) > 10 {
		a.Media.Places = a.Media.Places[:10]
	}
	for k := range interests {
		a.Interests = append(a.Interests, k)
	}
	sort.Strings(a.Interests)
	sort.Slice(a.ProfileChanges, func(i, j int) bool { return a.ProfileChanges[i].TS.Before(a.ProfileChanges[j].TS) })
	a.Logins.DistinctIPs = len(ips)
	a.Logins.Devices = top(devices, 8)
	return a
}

// distKM iki nokta arasındaki yaklaşık uzaklıktır (kısa mesafeler için denk dikdörtgen yaklaşımı).
func distKM(lat1, lon1, lat2, lon2 float64) float64 {
	dy := (lat2 - lat1) * 111.2
	dx := (lon2 - lon1) * 111.2 * math.Cos((lat1+lat2)/2*math.Pi/180)
	return math.Sqrt(dx*dx + dy*dy)
}
