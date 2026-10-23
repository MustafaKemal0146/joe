package analytics

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/MustafaKemal0146/joe/internal/model"
	"github.com/MustafaKemal0146/joe/internal/store"
)

// guideFixture dört bire-bir sohbet: ad ipucu, yoğun karşılıklı yakınlık ifadesi, çatışma ifadeleri, aile hitabı.
func guideFixture(t *testing.T) (*store.Corpus, Subject) {
	t.Helper()
	s, _ := store.Open(":memory:")
	t.Cleanup(func() { s.Close() })
	base := time.Date(2026, 1, 1, 20, 0, 0, 0, time.UTC)
	mk := func(partner string, n int, days int, text func(i int, mine bool) string) model.Conversation {
		var msgs []model.Message
		for i := 0; i < n; i++ {
			ts := base.Add(time.Duration(i) * time.Duration(days*24*60/n) * time.Minute)
			mine := i%2 == 0
			who := partner
			if mine {
				who = "Ben"
			}
			msgs = append(msgs, model.Message{Sender: who, TS: ts, Kind: model.KindText, Text: text(i, mine)})
		}
		return model.Conversation{Title: partner, Platform: "instagram", Participants: []string{"Ben", partner}, Messages: msgs}
	}
	convs := []model.Conversation{
		mk("Annem", 40, 30, func(i int, _ bool) string { return fmt.Sprintf("mesaj %d", i) }),
		mk("Deniz Kaya", 400, 120, func(i int, _ bool) string {
			if i%6 == 0 || i%6 == 3 {
				return "canım özledim seni ❤"
			}
			return fmt.Sprintf("gündelik %d", i)
		}),
		mk("Ali Veli", 120, 60, func(i int, mine bool) string {
			if i >= 60 && i < 70 {
				return "yeter artık bıktım senden, bir daha arama"
			}
			return fmt.Sprintf("selam %d", i)
		}),
		mk("Zeynep Demir", 60, 40, func(i int, mine bool) string {
			if mine && i%4 == 0 {
				return fmt.Sprintf("anne bak %d", i)
			}
			return fmt.Sprintf("tamam %d", i)
		}),
	}
	_, err := s.SaveImport(&model.Import{Platform: "instagram", Label: "g", OwnerName: "Ben", Stats: map[string]int{}, Conversations: convs}, "guide")
	if err != nil {
		t.Fatal(err)
	}
	ben, _ := s.CreatePerson("Ben", "", true)
	c, _ := s.LoadCorpus(nil)
	return c, NewSubject(ben, c)
}

func TestBuildGuideCountsToneAndSuggestsLabelsWithReasons(t *testing.T) {
	c, subj := guideFixture(t)
	rows := BuildGuide(c, subj, time.UTC)
	if len(rows) != 4 || !sort.SliceIsSorted(rows, func(i, j int) bool { return rows[i].Total > rows[j].Total }) {
		t.Fatalf("satırlar: %d", len(rows))
	}
	by := map[string]GuideRow{}
	for _, r := range rows {
		by[r.Name] = r
	}
	d := by["Deniz Kaya"]
	if d.Affection.Subject == 0 || d.Affection.Partner == 0 || d.Conflict.Subject+d.Conflict.Partner != 0 || d.Texts != 400 {
		t.Fatalf("yakınlık sayımı: %+v", d.Affection)
	}
	if d.Suggestion == nil || d.Suggestion.Label != "sevgili" || len(d.Suggestion.Why) == 0 {
		t.Fatalf("sevgili önerisi gerekçeli olmalı: %+v", d.Suggestion)
	}
	if a := by["Ali Veli"]; a.Conflict.Subject+a.Conflict.Partner != 10 || a.Suggestion != nil {
		t.Fatalf("çatışma sayımı/öneri: %+v %+v", a.Conflict, a.Suggestion)
	}
	if an := by["Annem"]; an.Suggestion == nil || an.Suggestion.Label != "anne" || !strings.Contains(an.Suggestion.Why[0], "annem") {
		t.Fatalf("ad ipucu: %+v", an.Suggestion)
	}
	if z := by["Zeynep Demir"]; z.Suggestion == nil || z.Suggestion.Label != "anne" || !strings.Contains(z.Suggestion.Why[0], "hitab") {
		t.Fatalf("aile hitabı: %+v", z.Suggestion)
	}
	for _, r := range rows {
		if r.Monthly != nil {
			t.Fatal("rehber yanıtı aylık dökümü taşımamalı (boyut)")
		}
	}
	if !ValidLabel("") || !ValidLabel("sevgili") || ValidLabel("toksik") {
		t.Fatal("etiket doğrulaması")
	}
}

func TestLexiconCountsPhrasesNotMeaning(t *testing.T) {
	for text, want := range map[string]bool{"Canım nasılsın": true, "özledim": true, "😘": true, "sınır tanımaz": false, "Aşkım": true, "canımı sıkma": false} {
		if got := affectionIn(text); got != want {
			t.Errorf("yakınlık %q: %v", text, got)
		}
	}
	for text, want := range map[string]bool{"Yeter artık": true, "bıktım": true, "Sinirliyim": true, "güzel gün": false} {
		if got := conflictIn(text); got != want {
			t.Errorf("çatışma %q: %v", text, got)
		}
	}
}

func TestConversationHighlightsFindRuptureConflictNightAndRuns(t *testing.T) {
	s, _ := store.Open(":memory:")
	t.Cleanup(func() { s.Close() })
	var msgs []model.Message
	add := func(ts time.Time, who, text string) {
		msgs = append(msgs, model.Message{Sender: who, TS: ts, Kind: model.KindText, Text: text})
	}
	d0 := time.Date(2026, 3, 1, 20, 0, 0, 0, time.UTC)
	// yoğun gün (80 mesaj) + 20 günlük sessizlik + dönüş
	for i := 0; i < 80; i++ {
		who := "Ben"
		if i%2 == 1 {
			who = "Deniz"
		}
		add(d0.Add(time.Duration(i)*time.Minute), who, fmt.Sprintf("sohbet %d", i))
	}
	back := d0.Add(21 * 24 * time.Hour)
	add(back, "Deniz", "selam, nasılsın")
	// çatışma kümesi: 5 mesaj, 20 dk içinde
	for i := 0; i < 5; i++ {
		add(back.Add(time.Hour+time.Duration(i)*4*time.Minute), "Ben", "yeter artık bıktım")
	}
	// gece oturumu: 25 mesaj, 01:00
	night := back.Add(48 * time.Hour).Truncate(24 * time.Hour).Add(25 * time.Hour)
	for i := 0; i < 25; i++ {
		who := "Ben"
		if i%2 == 1 {
			who = "Deniz"
		}
		add(night.Add(time.Duration(i)*time.Minute), who, "uyuyamadım")
	}
	// tek taraflı seri: Deniz 14 mesaj
	run := night.Add(48 * time.Hour)
	for i := 0; i < 14; i++ {
		add(run.Add(time.Duration(i)*time.Minute), "Deniz", "orada mısın")
	}
	// yakınlık günü: 6 mesaj aynı gün
	aff := run.Add(72 * time.Hour)
	for i := 0; i < 6; i++ {
		add(aff.Add(time.Duration(i)*time.Minute), "Deniz", "canım")
	}
	if _, err := s.SaveImport(&model.Import{Platform: "instagram", Label: "h", OwnerName: "Ben", Stats: map[string]int{},
		Conversations: []model.Conversation{{Title: "Deniz", Platform: "instagram", Participants: []string{"Ben", "Deniz"}, Messages: msgs}}}, "hl"); err != nil {
		t.Fatal(err)
	}
	ben, _ := s.CreatePerson("Ben", "", true)
	c, _ := s.LoadCorpus(nil)
	subj := NewSubject(ben, c)
	hs := ConversationHighlights(c, c.Convs[0].ID, subj, "Deniz", time.UTC)
	kinds := map[string]Highlight{}
	for i, h := range hs {
		if h.MessageID == 0 || h.Weight < 1 || h.Weight > 3 {
			t.Fatalf("geçersiz öğe: %+v", h)
		}
		if i > 0 && h.TS.Before(hs[i-1].TS) {
			t.Fatal("kronolojik sırada olmalı")
		}
		if _, ok := kinds[h.Kind]; !ok {
			kinds[h.Kind] = h
		}
	}
	for _, k := range []string{"contact", "peak", "rupture", "conflict", "night", "run", "affection", "first"} {
		if _, ok := kinds[k]; !ok {
			t.Errorf("%s anı bulunmalı; bulunanlar: %v", k, hs)
		}
	}
	if r := kinds["rupture"]; !strings.Contains(r.Title, "20 günlük") && !strings.Contains(r.Title, "21 günlük") {
		t.Errorf("kopuş süresi: %q", r.Title)
	}
	if cf := kinds["conflict"]; !strings.Contains(cf.Title, "5 mesaj") || !strings.Contains(cf.Detail, "yorumlanmaz") && !strings.Contains(cf.Detail, "ayrımı yapılmaz") {
		t.Errorf("çatışma kümesi: %+v", cf)
	}
	if rn := kinds["run"]; !strings.Contains(rn.Title, "Deniz art arda 14") {
		t.Errorf("tek taraflı seri: %q", rn.Title)
	}
	if none := ConversationHighlights(c, 9999, subj, "x", time.UTC); none != nil {
		t.Fatal("olmayan sohbet için boş dönmeli")
	}
}

func TestCloseFriendMatchesByExactDisplayNameFromEventText(t *testing.T) {
	s, _ := store.Open(":memory:")
	t.Cleanup(func() { s.Close() })
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	var msgs []model.Message
	for i := 0; i < 6; i++ {
		who := "Ben"
		if i%2 == 1 {
			who = "Zehra Çelik"
		}
		msgs = append(msgs, model.Message{Sender: who, TS: now.Add(time.Duration(i) * time.Minute), Kind: model.KindText, Text: "merhaba"})
	}
	// kullanıcı adı ile görünen ad hiç benzemiyor: yalnızca olay metnindeki görünen ad eşleştirir
	imp := &model.Import{Platform: "instagram", Label: "ig", OwnerName: "Ben", Stats: map[string]int{},
		Conversations: []model.Conversation{{Title: "Zehra Çelik", Platform: "instagram", Participants: []string{"Ben", "Zehra Çelik"}, Messages: msgs}},
		Events:        []model.Event{{Kind: model.EvCloseFriend, TS: now, Actor: model.Owner, Counterpart: "xx_9", Text: "Zehra Çelik"}}}
	if _, err := s.SaveImport(imp, "cf"); err != nil {
		t.Fatal(err)
	}
	ben, _ := s.CreatePerson("Ben", "", true)
	c, _ := s.LoadCorpus(nil)
	rows := BuildGuide(c, NewSubject(ben, c), time.UTC)
	if len(rows) != 1 || !rows[0].CloseFriendHint || rows[0].Suggestion == nil || rows[0].Suggestion.Label != "yakin_arkadas" {
		t.Fatalf("yakın arkadaş görünen adla eşleşmeli: %+v", rows)
	}
	v := BuildMetaView(c, nil, rows, time.UTC, 10)
	_ = v
}

func TestGuideKeysStayUniqueForSharedDisplayNames(t *testing.T) {
	s, _ := store.Open(":memory:")
	t.Cleanup(func() { s.Close() })
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	mk := func(partner string, n int) model.Conversation {
		var msgs []model.Message
		for i := 0; i < n; i++ {
			who := "Ben"
			if i%2 == 1 {
				who = partner
			}
			msgs = append(msgs, model.Message{Sender: who, TS: now.Add(time.Duration(i) * time.Minute), Kind: model.KindText, Text: "x"})
		}
		return model.Conversation{Title: partner, Platform: "instagram", Participants: []string{"Ben", partner}, Messages: msgs}
	}
	// iki farklı silinmiş hesap aynı adla; ayrıca tek kalan bir ad
	imp := &model.Import{Platform: "instagram", Label: "u", OwnerName: "Ben", Stats: map[string]int{},
		Conversations: []model.Conversation{mk("Instagram User", 6), mk("Instagram User", 4), mk("Tekil Kişi", 8)}}
	if _, err := s.SaveImport(imp, "uniq"); err != nil {
		t.Fatal(err)
	}
	ben, _ := s.CreatePerson("Ben", "", true)
	c, _ := s.LoadCorpus(nil)
	rows := BuildGuide(c, NewSubject(ben, c), time.UTC)
	seen := map[string]bool{}
	for _, r := range rows {
		if seen[r.Key] {
			t.Fatalf("anahtar çakıştı: %q", r.Key)
		}
		seen[r.Key] = true
		if r.Name == "Tekil Kişi" && strings.Contains(r.Key, "#") {
			t.Errorf("tekil ad değişmemeli: %q", r.Key)
		}
		if r.Name == "Instagram User" && !strings.Contains(r.Key, "#") {
			t.Errorf("paylaşılan ad sohbet kimliğiyle ayrışmalı: %q", r.Key)
		}
	}
	if len(rows) != 3 {
		t.Fatalf("satırlar: %d", len(rows))
	}
}

func TestPeriodKeywordsSurfaceTopicsThatShiftOverTime(t *testing.T) {
	s, _ := store.Open(":memory:")
	t.Cleanup(func() { s.Close() })
	base := time.Date(2026, 1, 1, 20, 0, 0, 0, time.UTC)
	var msgs []model.Message
	for i := 0; i < 900; i++ {
		who := "Ben"
		if i%2 == 1 {
			who = "Deniz"
		}
		topic := "kahve sinema"
		if i >= 450 {
			topic = "taşınma kiralık"
		}
		msgs = append(msgs, model.Message{Sender: who, TS: base.Add(time.Duration(i) * 6 * time.Hour), Kind: model.KindText, Text: fmt.Sprintf("bugün %s konuştuk %d tamam", topic, i%7)})
	}
	if _, err := s.SaveImport(&model.Import{Platform: "whatsapp", Label: "k", OwnerName: "Ben", Stats: map[string]int{},
		Conversations: []model.Conversation{{Title: "Deniz", Platform: "whatsapp", Participants: []string{"Ben", "Deniz"}, Messages: msgs}}}, "kw"); err != nil {
		t.Fatal(err)
	}
	c, _ := s.LoadCorpus(nil)
	ps := PeriodKeywords(c.Messages, 4, 5)
	if len(ps) != 4 {
		t.Fatalf("dönemler: %d", len(ps))
	}
	has := func(p PeriodWords, w string) bool {
		for _, x := range p.Words {
			if x == w {
				return true
			}
		}
		return false
	}
	if !has(ps[0], "kahve") || !has(ps[0], "sinema") || has(ps[0], "taşınma") {
		t.Errorf("ilk dönem konusu: %v", ps[0].Words)
	}
	if !has(ps[3], "taşınma") || !has(ps[3], "kiralık") || has(ps[3], "kahve") {
		t.Errorf("son dönem konusu: %v", ps[3].Words)
	}
	for _, p := range ps {
		for _, w := range p.Words {
			if w == "bugün" || w == "tamam" || w == "konuştuk" {
				t.Errorf("her dönemde geçen sözcük ayırt edici olmamalı: %q", w)
			}
		}
	}
	if txt := PeriodKeywordsText(c.Messages, time.UTC); !strings.Contains(txt, "Dönem 1") || !strings.Contains(txt, "anlamını göstermez") {
		t.Errorf("metin: %s", txt)
	}
	if PeriodKeywords(c.Messages[:100], 4, 5) != nil {
		t.Error("az veride üretilmemeli")
	}
}
