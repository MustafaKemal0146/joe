package analytics

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/MustafaKemal0146/joe/internal/model"
	"github.com/MustafaKemal0146/joe/internal/store"
)

// instagramEgoFixture sentetik bir Instagram hesabı: sahip "Ben Sahip" (@bensahip); karşılıklı takip,
// yakın arkadaş, tek yönlü takip, yalnızca beğeni ve yoğun DM bağlantıları.
func instagramEgoFixture(t *testing.T) (*store.Store, *store.Corpus, []store.Person) {
	t.Helper()
	s, _ := store.Open(":memory:")
	t.Cleanup(func() { s.Close() })
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	ev := func(kind, cp string, n int) []model.Event {
		var out []model.Event
		for i := 0; i < n; i++ {
			actor := model.Owner
			if kind == model.EvFollower {
				actor = cp
			}
			out = append(out, model.Event{Kind: kind, TS: now.Add(time.Duration(i) * time.Hour), Actor: actor, Counterpart: cp})
		}
		return out
	}
	var events []model.Event
	events = append(events, ev(model.EvFollowing, "karsilikli", 1)...)
	events = append(events, ev(model.EvFollower, "karsilikli", 1)...)
	events = append(events, ev(model.EvFollowing, "yakin_arkadas", 1)...)
	events = append(events, ev(model.EvCloseFriend, "yakin_arkadas", 1)...)
	events = append(events, ev(model.EvFollowing, "tek_yon", 1)...)
	events = append(events, ev(model.EvFollower, "takipci", 1)...)
	events = append(events, ev(model.EvLikePost, "begenen", 3)...)
	var msgs []model.Message
	for i := 0; i < 120; i++ {
		who := "Yoğun Dm"
		if i%2 == 1 {
			who = "Ben Sahip"
		}
		msgs = append(msgs, model.Message{Sender: who, TS: now.Add(time.Duration(i) * time.Minute), Kind: model.KindText, Text: fmt.Sprintf("mesaj %d", i)})
	}
	imp := &model.Import{Platform: "instagram", Label: "ig", OwnerName: "Ben Sahip", OwnerHandle: "bensahip", Stats: map[string]int{},
		Conversations: []model.Conversation{{Title: "Yoğun Dm", Platform: "instagram", Participants: []string{"Ben Sahip", "Yoğun Dm"}, Messages: msgs}}, Events: events}
	id, err := s.SaveImport(imp, "ego")
	if err != nil {
		t.Fatal(err)
	}
	src, _ := s.Source(id)
	if _, err := s.EnsureOwnerPerson(src); err != nil {
		t.Fatal(err)
	}
	c, _ := s.LoadCorpus(nil)
	ps, _ := s.People()
	return s, c, ps
}

func TestEgoNetworkLabelsFriendshipsFromInstagramActivity(t *testing.T) {
	_, c, ps := instagramEgoFixture(t)
	var me store.Person
	for _, p := range ps {
		if p.IsSelf {
			me = p
		}
	}
	e := EgoNetwork(c, ps, me.ID, nil, 40)
	if e.Center.PersonID != me.ID || !e.Center.IsOwner || !e.OwnData || len(e.Neighbors) != 6 || e.Total != 6 {
		t.Fatalf("merkez/bağlantılar: %+v (%d)", e.Center, len(e.Neighbors))
	}
	by := map[string]EgoNeighbor{}
	for _, n := range e.Neighbors {
		by[n.Label] = n
	}
	has := func(name, rel string) bool {
		for _, r := range by[name].Relations {
			if r == rel {
				return true
			}
		}
		return false
	}
	if !has("karsilikli", "karşılıklı takip") || by["karsilikli"].Circle != "yakin" {
		t.Errorf("karşılıklı takip yakın çevre olmalı: %+v", by["karsilikli"])
	}
	if !has("yakin_arkadas", "yakın arkadaş") || by["yakin_arkadas"].Circle != "yakin" {
		t.Errorf("yakın arkadaş: %+v", by["yakin_arkadas"])
	}
	if !has("tek_yon", "takip ediyor") || by["tek_yon"].Circle != "genis" || has("tek_yon", "karşılıklı takip") {
		t.Errorf("tek yönlü takip karşılıklı sayılmamalı: %+v", by["tek_yon"])
	}
	if !has("takipci", "takipçisi") || !has("begenen", "beğeni") || by["begenen"].Circle != "genis" {
		t.Errorf("takipçi/beğeni: %+v %+v", by["takipci"], by["begenen"])
	}
	if !has("Yoğun Dm", "mesajlaşıyor") || by["Yoğun Dm"].Circle != "yakin" { // karşı taraftan 60 mesaj
		t.Errorf("yoğun DM: %+v", by["Yoğun Dm"])
	}
	// bağlantılar güce göre sıralı; sınır uygulanır
	for i := 1; i < len(e.Neighbors); i++ {
		if e.Neighbors[i].Strength > e.Neighbors[i-1].Strength {
			t.Fatal("bağlantılar güce göre azalan olmalı")
		}
	}
	if lim := EgoNetwork(c, ps, me.ID, nil, 2); len(lim.Neighbors) != 2 || lim.Total != 6 {
		t.Errorf("sınır: %d / %d", len(lim.Neighbors), lim.Total)
	}
	// gizlenen düğüm çevreden çıkar; sahip gizlenemez
	hidden := map[string]bool{"nbegenen": true, "p" + fmt.Sprint(me.ID): true}
	h := EgoNetwork(c, ps, me.ID, hidden, 40)
	if len(h.Neighbors) != 5 || !h.Center.IsOwner {
		t.Errorf("gizleme: %d bağlantı, sahip %v", len(h.Neighbors), h.Center.IsOwner)
	}
	// verisi olmayan kişi: boş çevre, hata yok
	if x := EgoNetwork(c, ps, 9999, nil, 40); len(x.Neighbors) != 0 {
		t.Errorf("olmayan kişi boş dönmeli: %+v", x)
	}
}

func TestMetaViewRanksAccountsWithoutMessagesAndSplitsFollows(t *testing.T) {
	_, c, ps := instagramEgoFixture(t)
	var me store.Person
	for _, p := range ps {
		if p.IsSelf {
			me = p
		}
	}
	subj := NewSubject(me, c)
	guide := BuildGuide(c, subj, time.UTC)
	v := BuildMetaView(c, subj.OwnSources, guide, time.UTC, 10)
	by := map[string]MetaTie{}
	for _, t := range v.Ties {
		by[t.Handle] = t
	}
	if by["begenen"].Counts[model.EvLikePost] != 3 || by["begenen"].Weight != 3 || by["begenen"].DMName != "" {
		t.Errorf("DM'siz hesap: %+v", by["begenen"])
	}
	if len(v.Ties) != 1 {
		t.Errorf("yalnızca beğeni alan hesap bağ olmalı (takip ve DM bağ sayılmaz): %+v", v.Ties)
	}
	// takip dengesi: following={karsilikli, yakin_arkadas, tek_yon}, followers={karsilikli, takipci}
	f := v.Follow
	if f.Following != 3 || f.Followers != 2 || f.Mutual != 1 || f.NotFollowingBack != 2 || f.FansNotFollowed != 1 {
		t.Errorf("takip dengesi: %+v", f)
	}
	if v.Events != 3 || v.Hours[12] != 1 || v.Hours[13] != 1 || v.Hours[14] != 1 {
		t.Errorf("ritim: %d olay, saat dağılımı %v", v.Events, v.Hours)
	}
}

func TestPartnerMetaTextUsesDisplayNameAndShowsRelationsAndMonthlySeries(t *testing.T) {
	_, c, ps := instagramEgoFixture(t)
	var me store.Person
	for _, p := range ps {
		if p.IsSelf {
			me = p
		}
	}
	subj := NewSubject(me, c)
	// "Yoğun Dm" hesabı fikstürde yalnızca DM; etkinlik yok → boş
	if txt := PartnerMetaText(c, subj.OwnSources, []string{"Yoğun Dm"}, time.UTC); txt != "" {
		t.Fatalf("etkinlik yoksa boş olmalı: %s", txt)
	}
	// "begenen" ve "karsilikli" hesapları: görünen adı olmayan olaylar kullanıcı adı benzerliğiyle eşleşir
	txt := PartnerMetaText(c, subj.OwnSources, []string{"Karşılıklı", "karsilikli"}, time.UTC)
	if !strings.Contains(txt, "karşılıklı takip") || !strings.Contains(txt, "kesin değildir") {
		t.Fatalf("takip durumu ve sınır notu olmalı: %s", txt)
	}
	txt = PartnerMetaText(c, subj.OwnSources, []string{"begenen"}, time.UTC)
	if !strings.Contains(txt, "gönderi beğenisi: 3 kez") {
		t.Fatalf("beğeni sayısı: %s", txt)
	}
	if PartnerMetaText(c, subj.OwnSources, []string{"Kimse"}, time.UTC) != "" || PartnerMetaText(c, subj.OwnSources, nil, time.UTC) != "" {
		t.Fatal("eşleşme yoksa boş")
	}
	// başka hesabın etkinliği sayılmaz
	if PartnerMetaText(c, map[int64]bool{999: true}, []string{"begenen"}, time.UTC) != "" {
		t.Fatal("kendi hesabı olmayan kaynak sayılmamalı")
	}
}
