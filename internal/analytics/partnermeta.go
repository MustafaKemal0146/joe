package analytics

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/MustafaKemal0146/joe/internal/model"
	"github.com/MustafaKemal0146/joe/internal/store"
)

// partnerMetaThreshold görünen ad ↔ kullanıcı adı benzerliğinin kabul eşiğidir (olay metnindeki görünen ad
// kesin eşleşirse eşik aranmaz).
const partnerMetaThreshold = 0.82

// PartnerMetaText öznenin kendi hesap etkinliğinden, karşı tarafla ilgili davranışları düz metne çevirir:
// takip ilişkisi, yakın arkadaş listesi, hikâye izleme, beğeni, yorum, kaydetme, arama ve aylık seyri.
// names karşı tarafın adlarıdır (kişi dosyasındaki ad ve kimlikler). Hesap eşleşmesi görünen ad benzerliğine
// dayanır ve kesin değildir; metin bunu belirtir. Eşleşen etkinlik yoksa "" döner.
func PartnerMetaText(c *store.Corpus, own map[int64]bool, names []string, loc *time.Location) string {
	if loc == nil {
		loc = time.UTC
	}
	want := map[string]bool{}
	for _, n := range names {
		if k := model.Norm(n); k != "" {
			want[k] = true
		}
	}
	if len(want) == 0 {
		return ""
	}
	type nb struct {
		m map[string]int
		n int
	}
	var wantBi []nb
	for k := range want {
		m, n := bigramSet(k)
		wantBi = append(wantBi, nb{m, n})
	}
	matchCache := map[string]bool{}
	matches := func(handle, display string) bool {
		if want[model.Norm(display)] && display != "" {
			return true
		}
		key := handle
		if v, ok := matchCache[key]; ok {
			return v
		}
		h := model.Norm(handle)
		ok := false
		if len([]rune(h)) >= 4 {
			hb, hn := bigramSet(h)
			for _, w := range wantBi {
				if diceSets(hb, hn, w.m, w.n) >= partnerMetaThreshold {
					ok = true
					break
				}
			}
		}
		matchCache[key] = ok
		return ok
	}

	type agg struct {
		n           int
		first, last time.Time
	}
	by := map[string]*agg{}
	monthly := map[string]int{}
	handles := map[string]bool{}
	var flags []string
	seenFlag := map[string]bool{}
	for _, ev := range c.Events {
		if own != nil && !own[ev.SourceID] {
			continue
		}
		handle := ev.Counterpart
		if ev.Kind == model.EvFollower {
			handle = ev.Actor
		}
		if handle == "" || handle == model.Owner || !matches(handle, ev.Text) {
			continue
		}
		handles[handle] = true
		switch ev.Kind {
		case model.EvCloseFriend, model.EvFollowing, model.EvFollower, model.EvBlocked, model.EvUnfollowed, model.EvFollowRequest:
			if !seenFlag[ev.Kind] {
				seenFlag[ev.Kind] = true
				flags = append(flags, ev.Kind)
			}
			continue
		}
		if _, ok := metaWeight[ev.Kind]; !ok {
			continue
		}
		a := by[ev.Kind]
		if a == nil {
			a = &agg{}
			by[ev.Kind] = a
		}
		a.n++
		if !ev.TS.IsZero() {
			if a.first.IsZero() || ev.TS.Before(a.first) {
				a.first = ev.TS
			}
			if ev.TS.After(a.last) {
				a.last = ev.TS
			}
			monthly[ev.TS.In(loc).Format("2006-01")]++
		}
	}
	if len(handles) == 0 {
		return ""
	}
	label := map[string]string{
		model.EvStoryView: "hikâye izleme", model.EvLikePost: "gönderi beğenisi", model.EvLikeComment: "yorum beğenisi", model.EvStoryLike: "hikâye beğenisi",
		model.EvSaved: "kaydetme", model.EvStoryInteraction: "hikâye etkileşimi", model.EvComment: "yorum", model.EvSearchAccount: "hesap arama",
	}
	var hs []string
	for h := range handles {
		hs = append(hs, "@"+h)
	}
	sort.Strings(hs)
	var b strings.Builder
	fmt.Fprintf(&b, "Instagram davranış verisi (kullanıcının kendi hesabından, mesaj içeriği değil; hesap eşleşmesi ad benzerliğine dayanır ve kesin değildir; eşleşen: %s):\n", strings.Join(hs, ", "))
	rel := map[string]string{model.EvCloseFriend: "yakın arkadaş listesinde", model.EvBlocked: "engellenmiş", model.EvUnfollowed: "takipten çıkılmış", model.EvFollowRequest: "takip isteği gönderilmiş"}
	var rels []string
	_, fing := seenFlag[model.EvFollowing]
	_, fer := seenFlag[model.EvFollower]
	switch {
	case fing && fer:
		rels = append(rels, "karşılıklı takip")
	case fing:
		rels = append(rels, "kullanıcı onu takip ediyor (o etmiyor)")
	case fer:
		rels = append(rels, "o kullanıcıyı takip ediyor (kullanıcı etmiyor)")
	}
	for _, f := range flags {
		if r, ok := rel[f]; ok {
			rels = append(rels, r)
		}
	}
	if len(rels) > 0 {
		fmt.Fprintf(&b, "- Takip/ilişki durumu: %s.\n", strings.Join(rels, "; "))
	}
	var kinds []string
	for k := range by {
		kinds = append(kinds, k)
	}
	sort.Slice(kinds, func(i, j int) bool {
		if by[kinds[i]].n != by[kinds[j]].n {
			return by[kinds[i]].n > by[kinds[j]].n
		}
		return kinds[i] < kinds[j]
	})
	for _, k := range kinds {
		a := by[k]
		fmt.Fprintf(&b, "- %s: %d kez (%s – %s).\n", label[k], a.n, a.first.In(loc).Format("01.2006"), a.last.In(loc).Format("01.2006"))
	}
	if len(monthly) >= 3 {
		months := make([]string, 0, len(monthly))
		for m := range monthly {
			months = append(months, m)
		}
		sort.Strings(months)
		if len(months) > 18 {
			months = months[len(months)-18:]
		}
		parts := make([]string, len(months))
		for i, m := range months {
			parts[i] = fmt.Sprintf("%s:%d", m, monthly[m])
		}
		fmt.Fprintf(&b, "- Aylık toplam etkileşim (izleme, beğeni, yorum…): %s.\n", strings.Join(parts, " "))
	}
	return strings.TrimRight(b.String(), "\n")
}
