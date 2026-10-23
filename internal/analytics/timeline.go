package analytics

import (
	"fmt"
	"sort"
	"time"

	"github.com/MustafaKemal0146/joe/internal/model"
	"github.com/MustafaKemal0146/joe/internal/store"
)

// TimelineItem bir kişi dosyasının zaman çizelgesindeki bir andır.
type TimelineItem struct {
	TS     time.Time `json:"ts"`
	Kind   string    `json:"kind"` // contact, silence, follow, unfollow, interest, period, search
	Title  string    `json:"title"`
	Detail string    `json:"detail,omitempty"`
	Source string    `json:"source,omitempty"` // instagram | whatsapp
	// Weight önem düzeyidir: 3 dönüm noktası (ilk/son temas, kilometre taşı, uzun sessizlik, yoğunluk
	// değişimi), 2 belirgin olay (yoğun gün, gece konuşması, anahtar sözcük), 1 küçük ayrıntı.
	Weight int `json:"weight"`
}

const maxTimeline = 400

// BuildTimeline bir kişi için anlamlı anları tek bir eksende toplar: ilk ve son temas, uzun
// sessizlikler, takip hareketleri, kişiye yönelen ilgi (beğeni, yorum, hikâye, arama) ve
// etkinlik dönemleri. Ham mesaj akışı değildir; konseye ve gözle incelemeye uygun bir özet.
func BuildTimeline(c *store.Corpus, s Subject, p Profile, loc *time.Location) []TimelineItem {
	if loc == nil {
		loc = time.UTC
	}
	var items []TimelineItem
	items = append(items, dyadTimeline(c, s, loc)...)
	for _, e := range c.Events {
		if s.actorIs(e) {
			label := e.Counterpart
			switch e.Kind {
			case model.EvFollowing:
				items = append(items, item(e.TS, "follow", "Takip etti: "+label, "", "instagram", 2))
			case model.EvUnfollowed:
				items = append(items, item(e.TS, "unfollow", "Takibi bıraktı: "+label, "", "instagram", 2))
			case model.EvCloseFriend:
				items = append(items, item(e.TS, "follow", "Yakın arkadaş: "+label, "", "instagram", 2))
			case model.EvBlocked:
				items = append(items, item(e.TS, "unfollow", "Engelledi: "+label, "", "instagram", 2))
			case model.EvProfileChange:
				detail := ""
				if from, _ := e.Meta["from"].(string); from != "" {
					to, _ := e.Meta["to"].(string)
					detail = from + " → " + to
				}
				items = append(items, item(e.TS, "profile", "Profil değişti: "+e.Text, detail, "instagram", 2))
			}
			continue
		}
		if s.counterpartIs(e) {
			who := e.Actor
			if e.Actor == model.Owner {
				who = c.Sources[e.SourceID].OwnerName
			}
			switch e.Kind {
			case model.EvFollower, model.EvFollowing:
				items = append(items, item(e.TS, "follow", who+" takip ediyor", "", "instagram", 2))
			case model.EvUnfollowed:
				items = append(items, item(e.TS, "unfollow", who+" takibi bıraktı", "", "instagram", 2))
			case model.EvLikePost, model.EvLikeComment, model.EvComment, model.EvStoryLike, model.EvStoryInteraction, model.EvSaved:
				items = append(items, item(e.TS, "interest", who+" → "+interestLabel(e.Kind), e.Text, "instagram", 1))
			case model.EvSearchAccount:
				items = append(items, item(e.TS, "search", who+" profilini aradı", "", "instagram", 1))
			}
		}
	}
	for _, per := range p.Periods {
		t, _ := time.Parse("2006-01", per.From)
		title := "Yoğun dönem"
		if per.Kind == "lull" {
			title = "Sessiz dönem"
		}
		items = append(items, item(t, "period", fmt.Sprintf("%s: %s – %s", title, per.From, per.To),
			fmt.Sprintf("%d mesaj; olağan düzey ayda yaklaşık %.0f", per.Count, per.Baseline), "", 3))
	}
	items = dropZero(items)
	// Üst sınır aşılırsa önce en düşük önemdekiler (ve aynı önemde en eskiler) atılır; önemli anlar korunur.
	if len(items) > maxTimeline {
		sort.SliceStable(items, func(i, j int) bool {
			if items[i].Weight != items[j].Weight {
				return items[i].Weight > items[j].Weight
			}
			return items[i].TS.After(items[j].TS)
		})
		items = items[:maxTimeline]
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].TS.Before(items[j].TS) })
	return items
}

func item(ts time.Time, kind, title, detail, source string, weight int) TimelineItem {
	return TimelineItem{TS: ts, Kind: kind, Title: title, Detail: detail, Source: source, Weight: weight}
}

func dropZero(in []TimelineItem) []TimelineItem {
	out := in[:0]
	for _, it := range in {
		if !it.TS.IsZero() {
			out = append(out, it)
		}
	}
	return out
}

func platformLabel(p string) string {
	switch p {
	case "instagram":
		return "Instagram"
	case "whatsapp":
		return "WhatsApp"
	}
	return p
}

func interestLabel(kind string) string {
	switch kind {
	case model.EvLikePost:
		return "gönderisini beğendi"
	case model.EvLikeComment:
		return "yorumunu beğendi"
	case model.EvComment:
		return "gönderisine yorum yaptı"
	case model.EvStoryLike:
		return "hikâyesini beğendi"
	case model.EvStoryInteraction:
		return "hikâyesiyle etkileşti"
	case model.EvSaved:
		return "gönderisini kaydetti"
	}
	return kind
}
