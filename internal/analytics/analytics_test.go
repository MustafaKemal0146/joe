package analytics

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/MustafaKemal0146/joe/internal/model"
	"github.com/MustafaKemal0146/joe/internal/store"
)

var istanbul = func() *time.Location {
	l, err := time.LoadLocation("Europe/Istanbul")
	if err != nil {
		return time.FixedZone("TRT", 3*3600)
	}
	return l
}()

// fixture: 40 gün, her gün 10 mesajlık bir konuşma. Ayşe konuşmaların %75'ini başlatır;
// Ben 60 sn'de, Ayşe 600 sn'de yanıt verir. Mesajlar gece 23:00 civarındadır.
func fixture(t *testing.T) (*store.Store, *store.Corpus, []store.Person) {
	t.Helper()
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	day0 := time.Date(2026, 1, 1, 23, 0, 0, 0, istanbul)
	var msgs []model.Message
	for d := 0; d < 40; d++ {
		start := day0.AddDate(0, 0, d)
		first, second := "Ayşe", "Ben Sahip"
		if d%4 == 3 {
			first, second = second, first
		}
		cur, other := first, second
		ts := start
		for i := 0; i < 10; i++ {
			msgs = append(msgs, model.Message{Sender: cur, TS: ts, Kind: model.KindText, Text: fmt.Sprintf("bugün film izledik mi? 😀 mesaj %d", i)})
			if cur == "Ayşe" {
				ts = ts.Add(60 * time.Second) // Ben'in yanıtı 60 sn sonra
			} else {
				ts = ts.Add(600 * time.Second)
			}
			cur, other = other, cur
		}
	}
	msgs = append(msgs, model.Message{Sender: "Ayşe", TS: day0.AddDate(0, 0, 60), Kind: model.KindUnsent})
	imp := &model.Import{
		Platform: "instagram", Label: "ig.zip", OwnerName: "Ben Sahip", OwnerHandle: "benim", Stats: map[string]int{},
		Conversations: []model.Conversation{{Title: "Ayşe", Platform: "instagram", Participants: []string{"Ben Sahip", "Ayşe"}, Messages: msgs}},
		Events: []model.Event{
			{Kind: model.EvSearchAccount, TS: day0, Actor: model.Owner, Counterpart: "ayse_k", Text: "ayse_k"},
			{Kind: model.EvSearchAccount, TS: day0.AddDate(0, 0, 3), Actor: model.Owner, Counterpart: "ayse_k", Text: "ayse_k"},
			{Kind: model.EvFollowing, TS: day0, Actor: model.Owner, Counterpart: "ayse_k"},
			{Kind: model.EvFollower, TS: day0, Actor: "ayse_k", Counterpart: model.Owner},
			{Kind: model.EvFollowing, TS: day0, Actor: model.Owner, Counterpart: "tek_yonlu"},
			{Kind: model.EvLikePost, TS: day0, Actor: model.Owner, Counterpart: "ayse_k"},
			{Kind: model.EvLogin, TS: day0, Actor: model.Owner, Meta: map[string]any{"ip": "203.0.113.7", "user_agent": "Instagram 300 Android"}},
			{Kind: model.EvPost, TS: day0, Actor: model.Owner, Text: "gönderi"},
			{Kind: model.EvPost, TS: day0.AddDate(0, 0, 1), Actor: model.Owner},
			{Kind: model.EvStory, TS: day0.AddDate(0, 1, 0), Actor: model.Owner},
			{Kind: model.EvReel, TS: day0.AddDate(0, 1, 1), Actor: model.Owner},
			{Kind: model.EvProfileChange, TS: day0.AddDate(0, 0, 5), Actor: model.Owner, Text: "Username", Meta: map[string]any{"from": "eski", "to": "yeni"}},
			{Kind: model.EvMediaMeta, TS: day0, Actor: model.Owner, Text: "Apple iPhone 13", Meta: map[string]any{"lat": 41.0051, "lon": 28.9751}},
			{Kind: model.EvMediaMeta, TS: day0.AddDate(0, 0, 2), Actor: model.Owner, Text: "Apple iPhone 13", Meta: map[string]any{"lat": 41.0049, "lon": 28.9749}},
			{Kind: model.EvMediaMeta, TS: day0.AddDate(0, 0, 3), Actor: model.Owner, Text: "Samsung SM-G991B"},
			{Kind: model.EvInterest, Actor: model.Owner, Text: "Müzik"},
			{Kind: model.EvInterest, Actor: model.Owner, Text: "Film"},
		},
	}
	id, err := s.SaveImport(imp, "sha")
	if err != nil {
		t.Fatal(err)
	}
	src, _ := s.Source(id)
	me, _ := s.EnsureOwnerPerson(src)
	ayse, err := s.CreatePerson("Ayşe", "", false, "ayse_k")
	if err != nil {
		t.Fatal(err)
	}
	_ = me
	c, err := s.LoadCorpus(nil)
	if err != nil {
		t.Fatal(err)
	}
	people, _ := s.People()
	_ = ayse
	return s, c, people
}

func person(ps []store.Person, name string) store.Person {
	for _, p := range ps {
		if p.Name == name {
			return p
		}
	}
	panic("kişi yok: " + name)
}

func TestProfileRhythmAndReciprocity(t *testing.T) {
	_, c, ps := fixture(t)
	opt := Options{Loc: istanbul}
	ben := BuildProfile(c, NewSubject(person(ps, "Ben Sahip"), c), opt)
	if ben.Messages != 200 {
		t.Fatalf("Ben mesaj sayısı %d, beklenen 200", ben.Messages)
	}
	// Mesajlar 23:00 ile 23:xx arasında → gece oranı yüksek değil, 23. saat baskın.
	if ben.Hours[23] != ben.Messages {
		t.Errorf("tüm mesajlar 23. saatte olmalı: %v", ben.Hours)
	}
	if !ben.SleepKnown {
		t.Fatal("uyku penceresi tahmin edilmeli")
	}
	if ben.SleepStart == 23 || ben.SleepStart == 22 {
		t.Errorf("uyku penceresi etkin saati içermemeli: %d", ben.SleepStart)
	}
	if len(ben.Partners) != 1 {
		t.Fatalf("partner sayısı %d", len(ben.Partners))
	}
	p := ben.Partners[0]
	if p.Name != "Ayşe" || p.Sessions != 41 {
		t.Fatalf("partner: %s oturum %d", p.Name, p.Sessions)
	}
	// 40 gün: Ayşe 30, Ben 10 kez başlatır; 60. gündeki geri alınan mesaj Ayşe'nin 31. başlatması.
	if p.Starts.Subject != 10 || p.Starts.Partner != 31 {
		t.Errorf("başlatma: %+v", p.Starts)
	}
	if got := p.Latency.Subject.Median; math.Abs(got-60) > 1 {
		t.Errorf("Ben yanıt medyanı %.0f, beklenen 60", got)
	}
	if got := p.Latency.Partner.Median; math.Abs(got-600) > 1 {
		t.Errorf("Ayşe yanıt medyanı %.0f, beklenen 600", got)
	}
	if p.Unsent.Partner != 1 {
		t.Errorf("Ayşe'nin geri alınanı: %+v", p.Unsent)
	}
	if p.LongestSilenceDays < 19 {
		t.Errorf("son mesajdan önceki sessizlik: %.1f", p.LongestSilenceDays)
	}
	if ben.Language.TopEmoji[0].Key != "😀" {
		t.Errorf("emoji: %+v", ben.Language.TopEmoji)
	}
}

func TestActivityFollowAndSearch(t *testing.T) {
	_, c, ps := fixture(t)
	ben := BuildProfile(c, NewSubject(person(ps, "Ben Sahip"), c), Options{Loc: istanbul})
	if ben.Activity == nil {
		t.Fatal("kendi hesabı için etkinlik olmalı")
	}
	a := ben.Activity
	if a.Searched[0].Key != "ayse_k" || a.Searched[0].Count != 2 {
		t.Errorf("arama: %+v", a.Searched)
	}
	if a.Follow.Mutual != 1 || a.Follow.NotFollowingBack != 1 || a.Follow.Fans != 0 {
		t.Errorf("takip: %+v", a.Follow)
	}
	if a.Logins.DistinctIPs != 1 || a.Logins.Devices[0].Key != "Android · Instagram uygulaması" {
		t.Errorf("giriş: %+v", a.Logins)
	}
	// Ayşe'nin kendi hesabı yok → etkinlik nil.
	ay := BuildProfile(c, NewSubject(person(ps, "Ayşe"), c), Options{Loc: istanbul})
	if ay.Activity != nil {
		t.Error("kendi dışa aktarımı olmayan kişide etkinlik olmamalı")
	}
	if ay.Messages != 201 {
		t.Errorf("Ayşe mesaj sayısı %d", ay.Messages)
	}
}

func TestGraphMergesHandleIntoPerson(t *testing.T) {
	_, c, ps := fixture(t)
	g := BuildGraph(c, ps, 50)
	if len(g.Nodes) != 3 { // Ben, Ayşe (ad+kullanıcı adı birleşik), tek_yonlu
		t.Fatalf("düğüm sayısı %d: %+v", len(g.Nodes), g.Nodes)
	}
	var aysePID int64
	for _, n := range g.Nodes {
		if n.Label == "Ayşe" {
			aysePID = n.PersonID
		}
	}
	if aysePID == 0 {
		t.Fatal("Ayşe kişi dosyasına çözülmeli")
	}
	top := g.Edges[0]
	if top.Breakdown["dm"] == 0 || top.Breakdown[model.EvSearchAccount] != 2 {
		t.Errorf("en güçlü kenar: %+v", top)
	}
	small := BuildGraph(c, ps, 2)
	if len(small.Nodes) != 2 || small.Total != 3 {
		t.Errorf("sınır: %d düğüm, toplam %d", len(small.Nodes), small.Total)
	}
}

func TestSuggestLinksNameToHandle(t *testing.T) {
	s, _, _ := fixture(t)
	// "Ayşe" kişi dosyasına bağlı olduğu için öneri çıkmaz; yeni bir görünen ad ekleyip dene.
	imp := &model.Import{Platform: "whatsapp", Label: "w", OwnerName: "Ben Sahip", Stats: map[string]int{},
		Conversations: []model.Conversation{{Title: "Mehmet Demir", Platform: "whatsapp", Participants: []string{"Ben Sahip", "Mehmet Demir"},
			Messages: []model.Message{{Sender: "Mehmet Demir", TS: time.Now(), Kind: model.KindText, Text: "x"}}}},
		Events: []model.Event{{Kind: model.EvLikePost, TS: time.Now(), Actor: model.Owner, Counterpart: "mehmet_dem"}}}
	if _, err := s.SaveImport(imp, "sha2"); err != nil {
		t.Fatal(err)
	}
	c, _ := s.LoadCorpus(nil)
	people, _ := s.People()
	sug := Suggest(c, people, 0.6)
	if len(sug) == 0 || sug[0].Name != "Mehmet Demir" || sug[0].Handle != "mehmet_dem" || sug[0].Score < 0.7 {
		t.Fatalf("öneri: %+v", sug)
	}
}

func TestCompareDirectBalance(t *testing.T) {
	_, c, ps := fixture(t)
	a, b := NewSubject(person(ps, "Ben Sahip"), c), NewSubject(person(ps, "Ayşe"), c)
	cmp := Compare(c, a, b, Options{Loc: istanbul})
	if cmp.Direct == nil {
		t.Fatal("doğrudan sohbet bulunmalı")
	}
	got := map[string]float64{}
	for _, d := range cmp.Dimensions {
		got[d.Key] = d.Score
	}
	if got["direct_volume"] < 0.99 {
		t.Errorf("mesaj dengesi %v", got["direct_volume"])
	}
	// Başlatma 10'a 31 → 1-|10/41-0.5|*2 ≈ 0.488
	if math.Abs(got["direct_initiative"]-0.488) > 0.01 {
		t.Errorf("başlatma dengesi %v", got["direct_initiative"])
	}
	// Yanıt 60 vs 600 → oran 0.1
	if math.Abs(got["direct_reply"]-0.1) > 0.02 {
		t.Errorf("yanıt hızı %v", got["direct_reply"])
	}
	if got["rhythm"] < 0.99 {
		t.Errorf("aynı saatlerde yazan iki kişinin ritim benzerliği %v", got["rhythm"])
	}
	if cmp.Caveat == "" || cmp.Overall <= 0 || cmp.Overall > 1 {
		t.Errorf("genel: %v", cmp.Overall)
	}
}

func TestCompareWithoutDataReportsMissing(t *testing.T) {
	s, _, _ := fixture(t)
	ghost, _ := s.CreatePerson("Hiç Veri Yok", "", false)
	c, _ := s.LoadCorpus(nil)
	people, _ := s.People()
	cmp := Compare(c, NewSubject(person(people, "Ben Sahip"), c), NewSubject(ghost, c), Options{Loc: istanbul})
	if len(cmp.Missing) == 0 {
		t.Fatal("eksik veri açıkça bildirilmeli")
	}
	if cmp.Direct != nil {
		t.Fatal("doğrudan sohbet uydurulmamalı")
	}
	for _, d := range cmp.Dimensions {
		if d.Key == "rhythm" {
			t.Fatal("veri yokken ritim boyutu üretilmemeli")
		}
	}
}

func TestPeriodsDetectSurgeAndLull(t *testing.T) {
	var ms []MonthCount
	for i := 0; i < 12; i++ {
		n := 100
		if i == 6 {
			n = 600
		}
		if i == 10 {
			n = 5
		}
		ms = append(ms, MonthCount{fmt.Sprintf("2026-%02d", i+1), n})
	}
	ps := detectPeriods(ms)
	if len(ps) != 2 || ps[0].Kind != "surge" || ps[0].From != "2026-07" || ps[1].Kind != "lull" || ps[1].From != "2026-11" {
		t.Fatalf("dönemler: %+v", ps)
	}
	flat := make([]MonthCount, 12)
	for i := range flat {
		flat[i] = MonthCount{fmt.Sprintf("2026-%02d", i+1), 100}
	}
	if len(detectPeriods(flat)) != 0 {
		t.Fatal("düz seride dönem işaretlenmemeli")
	}
}

func TestTimelineHasContactSilenceAndFollow(t *testing.T) {
	_, c, ps := fixture(t)
	subj := NewSubject(person(ps, "Ben Sahip"), c)
	items := BuildTimeline(c, subj, BuildProfile(c, subj, Options{Loc: istanbul}), istanbul)
	kinds := map[string]int{}
	for i, it := range items {
		kinds[it.Kind]++
		if i > 0 && items[i].TS.Before(items[i-1].TS) {
			t.Fatal("zaman çizelgesi sıralı olmalı")
		}
	}
	for _, k := range []string{"contact", "silence", "follow"} {
		if kinds[k] == 0 {
			t.Errorf("%s öğesi yok: %v", k, kinds)
		}
	}
}

func TestDedupeAcrossSources(t *testing.T) {
	s, _, _ := fixture(t)
	ts := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	mk := func(label string) *model.Import {
		return &model.Import{Platform: "whatsapp", Label: label, OwnerName: "Ben Sahip", Stats: map[string]int{},
			Conversations: []model.Conversation{{Title: "Can", Platform: "whatsapp", Participants: []string{"Ben Sahip", "Can"},
				Messages: []model.Message{{Sender: "Can", TS: ts, Kind: model.KindText, Text: "aynı mesaj"}}}}}
	}
	s.SaveImport(mk("a"), "h1")
	s.SaveImport(mk("b"), "h2")
	can, _ := s.CreatePerson("Can", "", false)
	c, _ := s.LoadCorpus(nil)
	if n := len(NewSubject(can, c).Msgs(c)); n != 1 {
		t.Fatalf("iki kaynaktaki aynı mesaj bir kez sayılmalı, %d", n)
	}
}

func TestActivityPostsInterestsAndProfileChanges(t *testing.T) {
	_, c, ps := fixture(t)
	ben := BuildProfile(c, NewSubject(person(ps, "Ben Sahip"), c), Options{Loc: istanbul})
	a := ben.Activity
	if a.Posts.Posts != 2 || a.Posts.Stories != 1 || a.Posts.Reels != 1 {
		t.Errorf("paylaşım sayıları: %+v", a.Posts)
	}
	if len(a.Posts.Monthly) != 2 || a.Posts.Hours[23] != 4 {
		t.Errorf("aylık/saatlik paylaşım: %+v", a.Posts)
	}
	if len(a.Interests) != 2 || a.Interests[0] != "Film" {
		t.Errorf("ilgi alanları sıralı olmalı: %v", a.Interests)
	}
	if len(a.ProfileChanges) != 1 || a.ProfileChanges[0].From != "eski" || a.ProfileChanges[0].To != "yeni" {
		t.Errorf("profil değişimi: %+v", a.ProfileChanges)
	}
	subj := NewSubject(person(ps, "Ben Sahip"), c)
	found := false
	for _, it := range BuildTimeline(c, subj, ben, istanbul) {
		if it.Kind == "profile" && it.Detail == "eski → yeni" {
			found = true
		}
	}
	if !found {
		t.Error("profil değişimi zaman çizelgesinde olmalı")
	}
}

func groupFixture(t *testing.T) (*store.Corpus, []store.Person) {
	t.Helper()
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	t0 := time.Date(2026, 2, 1, 21, 0, 0, 0, istanbul)
	var msgs []model.Message
	ts := t0
	// 20 tur: Ayşe yazar, 1 dk sonra Ben yanıtlar; ardından 10 dk sonra Can yazar, 1 dk sonra Ayşe yanıtlar
	for i := 0; i < 20; i++ {
		msgs = append(msgs, model.Message{Sender: "Ayşe", TS: ts, Kind: model.KindText, Text: "grup mesajı"})
		msgs = append(msgs, model.Message{Sender: "Ben", TS: ts.Add(time.Minute), Kind: model.KindText, Text: "yanıt"})
		ts = ts.Add(10 * time.Minute)
		msgs = append(msgs, model.Message{Sender: "Can", TS: ts, Kind: model.KindText, Text: "başka konu"})
		msgs = append(msgs, model.Message{Sender: "Ayşe", TS: ts.Add(time.Minute), Kind: model.KindText, Text: "tamam can"})
		msgs = append(msgs, model.Message{Sender: "Can", TS: ts.Add(2 * time.Minute), Kind: model.KindText, Text: "ek"})
		ts = ts.Add(20 * time.Minute)
	}
	msgs = append(msgs, model.Message{Sender: "Deniz", TS: ts, Kind: model.KindText, Text: "merhaba"})
	imp := &model.Import{Platform: "whatsapp", Label: "g", OwnerName: "Ben", Stats: map[string]int{},
		Conversations: []model.Conversation{{Title: "Hafta sonu planı", Platform: "whatsapp", IsGroup: true,
			Participants: []string{"Ben", "Ayşe", "Can", "Deniz"}, Messages: msgs}}}
	id, err := s.SaveImport(imp, "g")
	if err != nil {
		t.Fatal(err)
	}
	src, _ := s.Source(id)
	s.EnsureOwnerPerson(src)
	c, _ := s.LoadCorpus(nil)
	people, _ := s.People()
	return c, people
}

func TestGroupStatsShareRankAndReplyPatterns(t *testing.T) {
	c, ps := groupFixture(t)
	ben := BuildProfile(c, NewSubject(person(ps, "Ben"), c), Options{Loc: istanbul})
	if len(ben.Groups) != 1 {
		t.Fatalf("grup sayısı %d", len(ben.Groups))
	}
	g := ben.Groups[0]
	if g.Title != "Hafta sonu planı" || g.Members != 4 || g.SubjectMsgs != 20 || g.Total != 101 {
		t.Fatalf("grup: %+v", g)
	}
	// Ayşe 40, Can 40, Ben 20, Deniz 1 → Ben 3. sırada
	if g.Rank != 3 || math.Abs(g.SubjectShare-20.0/101) > 1e-9 {
		t.Fatalf("sıra/pay: %d %.3f", g.Rank, g.SubjectShare)
	}
	if len(g.RepliesTo) != 1 || g.RepliesTo[0].Key != "Ayşe" || g.RepliesTo[0].Count != 20 {
		t.Fatalf("Ben yalnızca Ayşe'den sonra yazıyor: %+v", g.RepliesTo)
	}
	if g.Hours[21] == 0 || len(g.Top) != 4 {
		t.Fatalf("saat/üyeler: %v %+v", g.Hours[21], g.Top)
	}
	// gruplar bire-bir metriklere karışmamalı
	if len(ben.Partners) != 0 {
		t.Fatalf("grup, bire-bir sohbet sayılmamalı: %+v", ben.Partners)
	}
}

func TestGroupReplyEdgesLinkMembersAmongThemselves(t *testing.T) {
	c, ps := groupFixture(t)
	g := BuildGraph(c, ps, 50)
	label := map[string]string{}
	for _, n := range g.Nodes {
		label[n.ID] = n.Label
	}
	found := false
	for _, e := range g.Edges {
		a, b := label[e.A], label[e.B]
		if (a == "Ayşe" && b == "Can" || a == "Can" && b == "Ayşe") && e.Breakdown["group_reply"] > 0 {
			found = true
		}
	}
	if !found {
		t.Fatalf("Ayşe–Can arasında grup içi yanıt kenarı olmalı: %+v", g.Edges)
	}
}

func TestMediaSummaryClustersPlacesAndDevices(t *testing.T) {
	_, c, ps := fixture(t)
	a := BuildProfile(c, NewSubject(person(ps, "Ben Sahip"), c), Options{Loc: istanbul}).Activity
	m := a.Media
	if m.WithMeta != 3 || m.WithGPS != 2 {
		t.Fatalf("sayılar: %+v", m)
	}
	if len(m.Devices) != 2 || m.Devices[0].Key != "Apple iPhone 13" || m.Devices[0].Count != 2 {
		t.Fatalf("cihazlar: %+v", m.Devices)
	}
	if len(m.Places) != 1 || m.Places[0].Count != 2 || math.Abs(m.Places[0].Lat-41.005) > 1e-9 || math.Abs(m.Places[0].Lon-28.975) > 1e-9 {
		t.Fatalf("yakın iki konum tek kümede toplanmalı: %+v", m.Places)
	}
}

func TestDistantPlacesStaySeparate(t *testing.T) {
	if d := distKM(41.0, 29.0, 39.9, 32.8); d < 300 || d > 400 {
		t.Fatalf("İstanbul–Ankara ≈ 350 km olmalı: %.0f", d)
	}
	if distKM(41.005, 28.975, 41.0049, 28.9749) > 0.05 {
		t.Fatal("yakın noktalar yakın çıkmalı")
	}
}

func TestDedupeKeepsIdenticalMessagesWithinOneSourceButCollapsesAcrossSources(t *testing.T) {
	s, _, _ := fixture(t)
	ts := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC) // dakika çözünürlüğü: saniye 0
	mk := func(label string, n int) *model.Import {
		var msgs []model.Message
		for i := 0; i < n; i++ {
			msgs = append(msgs, model.Message{Sender: "Can", TS: ts, Kind: model.KindText, Text: "😂"})
		}
		return &model.Import{Platform: "whatsapp", Label: label, OwnerName: "Ben Sahip", Stats: map[string]int{},
			Conversations: []model.Conversation{{Title: "Can", Platform: "whatsapp", Participants: []string{"Ben Sahip", "Can"}, Messages: msgs}}}
	}
	s.SaveImport(mk("a", 3), "d1") // aynı dakikada 3 özdeş mesaj: üçü de gerçek
	can, _ := s.CreatePerson("Can", "", false)
	c, _ := s.LoadCorpus(nil)
	if n := len(NewSubject(can, c).Msgs(c)); n != 3 {
		t.Fatalf("aynı kaynaktaki özdeş mesajlar korunmalı: %d", n)
	}
	s.SaveImport(mk("b", 3), "d2") // ikinci kaynakta aynı 3 mesaj: çift sayılmamalı
	c, _ = s.LoadCorpus(nil)
	if n := len(NewSubject(can, c).Msgs(c)); n != 3 {
		t.Fatalf("iki kaynaktaki aynı mesajlar bir kez sayılmalı: %d", n)
	}
	s.SaveImport(mk("c", 5), "d3") // üçüncü kaynakta 5 tekrar var: fazlası (4. ve 5.) yeni mesajlardır
	c, _ = s.LoadCorpus(nil)
	if n := len(NewSubject(can, c).Msgs(c)); n != 5 {
		t.Fatalf("beklenen 5, %d", n)
	}
}

func TestDeletedWhatsAppMessagesCountAsRetractedInDyad(t *testing.T) {
	s, _ := store.Open(":memory:")
	defer s.Close()
	ts := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	msgs := []model.Message{
		{Sender: "Ben", TS: ts, Kind: model.KindText, Text: "selam"},
		{Sender: "Ayşe", TS: ts.Add(time.Minute), Kind: model.KindDeleted},
		{Sender: "Ayşe", TS: ts.Add(2 * time.Minute), Kind: model.KindDeleted},
	}
	s.SaveImport(&model.Import{Platform: "whatsapp", Label: "w", OwnerName: "Ben", Stats: map[string]int{},
		Conversations: []model.Conversation{{Title: "Ayşe", Platform: "whatsapp", Participants: []string{"Ben", "Ayşe"}, Messages: msgs}}}, "w")
	ben, _ := s.CreatePerson("Ben", "", false)
	c, _ := s.LoadCorpus(nil)
	pr := BuildProfile(c, NewSubject(ben, c), Options{})
	if len(pr.Partners) != 1 || pr.Partners[0].Unsent.Partner != 2 {
		t.Fatalf("silinen mesajlar geri alınan sayılmalı: %+v", pr.Partners)
	}
}

func TestMinuteResolutionNoteAppearsOnlyForWholeMinuteTimestamps(t *testing.T) {
	build := func(sec int) Profile {
		s, _ := store.Open(":memory:")
		defer s.Close()
		var msgs []model.Message
		ts := time.Date(2026, 5, 1, 12, 0, sec, 0, time.UTC)
		for i := 0; i < 520; i++ {
			who := "Ben"
			if i%2 == 1 {
				who = "Ayşe"
			}
			msgs = append(msgs, model.Message{Sender: who, TS: ts.Add(time.Duration(i) * time.Minute), Kind: model.KindText, Text: fmt.Sprintf("m%d", i)})
		}
		s.SaveImport(&model.Import{Platform: "whatsapp", Label: "w", OwnerName: "Ben", Stats: map[string]int{},
			Conversations: []model.Conversation{{Title: "Ayşe", Platform: "whatsapp", Participants: []string{"Ben", "Ayşe"}, Messages: msgs}}}, "w")
		ben, _ := s.CreatePerson("Ben", "", false)
		c, _ := s.LoadCorpus(nil)
		return BuildProfile(c, NewSubject(ben, c), Options{})
	}
	has := func(p Profile) bool {
		for _, n := range p.Notes {
			if strings.Contains(n, "dakika çözünürlüğünde") {
				return true
			}
		}
		return false
	}
	if !has(build(0)) {
		t.Fatal("tüm zaman damgaları tam dakikaysa not düşülmeli")
	}
	if has(build(37)) {
		t.Fatal("saniyeli zaman damgalarında not olmamalı")
	}
}

// timelineFixture ilişkinin önemli anlarını içeren 4 aylık bir sohbet kurar.
func timelineFixture(t *testing.T) (*store.Corpus, store.Person) {
	t.Helper()
	s, _ := store.Open(":memory:")
	t.Cleanup(func() { s.Close() })
	var msgs []model.Message
	add := func(who string, ts time.Time, text string) {
		msgs = append(msgs, model.Message{Sender: who, TS: ts, Kind: model.KindText, Text: text})
	}
	start := time.Date(2026, 1, 1, 20, 0, 0, 0, istanbul)
	for d := 0; d < 120; d++ {
		day := start.AddDate(0, 0, d)
		if d >= 40 && d < 46 { // ≈7 günlük sessizlik (son mesaj 39. gün 20:29, dönüş 46. gün 20:00); Ayşe bozar
			continue
		}
		n := 6 // Ocak: hafif
		if d >= 31 {
			n = 30 // Şubat'tan itibaren yoğun → yoğunlaşma
		}
		if d == 70 {
			n = 200 // en yoğun gün
		}
		for i := 0; i < n; i++ {
			who := "Ayşe"
			if i%2 == 1 {
				who = "Ben"
			}
			text := fmt.Sprintf("gündelik mesaj %d", i)
			if d == 5 && i == 0 {
				text = "Seni seviyorum" // ilk geçiş (büyük harf + ASCII dışı yok)
			}
			if d == 20 && i == 1 {
				text = "Çok özledim seni" // "özledim": ö/ç Türkçe karakter eşleşmesi
			}
			if d == 46 && i == 0 {
				text = "bu sabah özledim"
			}
			add(who, day.Add(time.Duration(i)*time.Minute), text)
		}
		if d == 50 { // gece konuşması: 02:00'de 30 mesaj
			ts := time.Date(2026, 2, 20, 2, 0, 0, 0, istanbul)
			for i := 0; i < 30; i++ {
				add("Ben", ts.Add(time.Duration(i)*time.Minute), "uyuyamadım")
				if i%2 == 0 {
					add("Ayşe", ts.Add(time.Duration(i)*time.Minute+30*time.Second), "ben de")
				}
			}
		}
		if d == 60 { // tek taraflı seri: Ayşe art arda 20 mesaj
			ts := day.Add(5 * time.Hour)
			for i := 0; i < 20; i++ {
				add("Ayşe", ts.Add(time.Duration(i)*time.Minute), "yanıt ver")
			}
		}
		if d == 80 {
			msgs = append(msgs, model.Message{Sender: "Ayşe", TS: day.Add(7 * time.Hour), Kind: model.KindDeleted})
		}
	}
	sort.SliceStable(msgs, func(i, j int) bool { return msgs[i].TS.Before(msgs[j].TS) })
	s.SaveImport(&model.Import{Platform: "whatsapp", Label: "w", OwnerName: "Ben", Stats: map[string]int{},
		Conversations: []model.Conversation{{Title: "Ayşe", Platform: "whatsapp", Participants: []string{"Ben", "Ayşe"}, Messages: msgs}}}, "tl")
	ben, _ := s.CreatePerson("Ben", "", true)
	c, _ := s.LoadCorpus(nil)
	return c, ben
}

func TestTimelineHasImportantMomentsNotJustFirstAndLast(t *testing.T) {
	c, ben := timelineFixture(t)
	subj := NewSubject(ben, c)
	items := BuildTimeline(c, subj, BuildProfile(c, subj, Options{Loc: istanbul}), istanbul)
	byKind := map[string][]TimelineItem{}
	for i, it := range items {
		byKind[it.Kind] = append(byKind[it.Kind], it)
		if i > 0 && it.TS.Before(items[i-1].TS) {
			t.Fatal("kronolojik sırada olmalı")
		}
		if it.Weight < 1 || it.Weight > 3 {
			t.Fatalf("ağırlık 1–3 olmalı: %+v", it)
		}
	}
	for _, k := range []string{"contact", "milestone", "peak", "silence", "night", "run", "trend", "retract", "keyword"} {
		if len(byKind[k]) == 0 {
			t.Errorf("%s olayı yok; var olanlar: %v", k, func() []string {
				var ks []string
				for kk := range byKind {
					ks = append(ks, kk)
				}
				return ks
			}())
		}
	}
	if len(items) < 12 {
		t.Fatalf("önemli olayların hepsi görünmeli: %d öğe", len(items))
	}
	// 1.000. mesaj kilometre taşı
	if m := byKind["milestone"]; len(m) == 0 || m[0].Title != "1.000. mesaj" || m[0].Weight != 3 {
		t.Errorf("1.000. mesaj kilometre taşı: %+v", m)
	}
	// en yoğun gün 200 mesaj
	if p := byKind["peak"]; len(p) == 0 || !strings.Contains(p[0].Detail, "200") {
		t.Errorf("en yoğun gün: %+v", p)
	}
	// 6 günlük sessizlik, kimin bozduğu detayda
	sil := byKind["silence"]
	if len(sil) == 0 || !strings.Contains(sil[0].Title, "7 gün") || !strings.Contains(sil[0].Detail, "Ayşe yeniden yazdı") || sil[0].Weight != 3 {
		t.Errorf("sessizlik: %+v", sil)
	}
	// yoğunlaşma: Ocak → Şubat
	tr := byKind["trend"]
	if len(tr) == 0 || !strings.Contains(tr[0].Title, "yoğunlaştı") {
		t.Errorf("yoğunlaşma: %+v", tr)
	}
	// gece konuşması ve tek taraflı seri
	if n := byKind["night"]; len(n) == 0 || !strings.Contains(n[0].Detail, "Gece yarısından sonra") {
		t.Errorf("gece: %+v", n)
	}
	if r := byKind["run"]; len(r) == 0 || !strings.Contains(r[0].Title, "Ayşe art arda 21") {
		t.Errorf("seri: %+v", r)
	}
	// silinen mesaj
	if r := byKind["retract"]; len(r) != 1 || !strings.Contains(r[0].Title, "Ayşe") {
		t.Errorf("silinen: %+v", r)
	}
	// anahtar sözcükler: ilk geçiş doğru gün ve kişide; Türkçe karakterli ve büyük harfli yazım yakalanır
	got := map[string]TimelineItem{}
	for _, k := range byKind["keyword"] {
		got[k.Title] = k
	}
	love, ok := got["İlk kez “seni seviyorum” geçti"]
	if !ok || !strings.Contains(love.Detail, "Ayşe yazdı") || love.TS.In(istanbul).Format("2006-01-02") != "2026-01-06" || !strings.Contains(love.Detail, "yorum değil") {
		t.Errorf("seni seviyorum: %+v", love)
	}
	miss, ok := got["İlk kez “özledim” geçti"]
	if !ok || !strings.Contains(miss.Detail, "toplam 2 mesajda") || miss.TS.In(istanbul).Format("2006-01-02") != "2026-01-21" {
		t.Errorf("özledim (ilk geçiş 21 Ocak, toplam 2): %+v", miss)
	}
	if _, ok := got["İlk kez “ayrılalım / bitirelim” geçti"]; ok {
		t.Error("geçmeyen ifade için olay üretilmemeli")
	}
}

func TestTimelineIsCappedKeepingHeavyItems(t *testing.T) {
	c, ben := timelineFixture(t)
	subj := NewSubject(ben, c)
	items := BuildTimeline(c, subj, BuildProfile(c, subj, Options{Loc: istanbul}), istanbul)
	if len(items) > maxTimeline {
		t.Fatalf("üst sınır aşıldı: %d", len(items))
	}
	if foldASCII("Çok ÖZLEDİM, Şaşırdım ığ") != "cok ozledim, sasirdim ig" {
		t.Fatalf("ASCII indirgeme: %q", foldASCII("Çok ÖZLEDİM, Şaşırdım ığ"))
	}
	if fmtInt(1234567) != "1.234.567" || fmtInt(999) != "999" {
		t.Fatal("binlik ayırıcı")
	}
}
