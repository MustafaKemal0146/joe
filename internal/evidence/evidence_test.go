package evidence

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/MustafaKemal0146/joe/internal/analytics"
	"github.com/MustafaKemal0146/joe/internal/audit"
	"github.com/MustafaKemal0146/joe/internal/mask"
	"github.com/MustafaKemal0146/joe/internal/model"
	"github.com/MustafaKemal0146/joe/internal/store"
)

var tz = time.FixedZone("TRT", 3*3600)

// fixture: 8 ay, her gün birkaç mesaj; 5. ayda yoğunluk, 3. ayda 20 günlük sessizlik, 2. ayda geri alınan mesaj.
func fixture(t *testing.T) (*store.Corpus, analytics.Subject, analytics.Profile) {
	t.Helper()
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	start := time.Date(2026, 1, 1, 21, 0, 0, 0, tz)
	var msgs []model.Message
	for d := 0; d < 240; d++ {
		day := start.AddDate(0, 0, d)
		if m := day.Month(); m == time.March && day.Day() > 5 && day.Day() <= 25 {
			continue // sessizlik
		}
		n := 4
		if day.Month() == time.May {
			n = 40 // yoğun dönem
		}
		ts := day
		for i := 0; i < n; i++ {
			sender, text := "Ayşe", fmt.Sprintf("mesaj %d gün %d", i, d)
			if i%2 == 1 {
				sender = "Ben"
			}
			if d == 1 && i == 0 {
				text = "beni ara: 0532 123 45 67"
			}
			msgs = append(msgs, model.Message{Sender: sender, TS: ts, Kind: model.KindText, Text: text})
			ts = ts.Add(2 * time.Minute)
		}
		if d == 45 {
			msgs = append(msgs, model.Message{Sender: "Ayşe", TS: ts, Kind: model.KindUnsent})
		}
	}
	imp := &model.Import{Platform: "instagram", Label: "x", OwnerName: "Ben", OwnerHandle: "ben", Stats: map[string]int{},
		Conversations: []model.Conversation{{Title: "Ayşe", Platform: "instagram", Participants: []string{"Ben", "Ayşe"}, Messages: msgs}}}
	id, err := s.SaveImport(imp, "sha")
	if err != nil {
		t.Fatal(err)
	}
	src, _ := s.Source(id)
	me, _ := s.EnsureOwnerPerson(src)
	c, _ := s.LoadCorpus(nil)
	subj := analytics.NewSubject(me, c)
	return c, subj, analytics.BuildProfile(c, subj, analytics.Options{Loc: tz})
}

func TestPackIsDeterministicWithChronologicalIDs(t *testing.T) {
	c, subj, pr := fixture(t)
	a := Build(c, subj, pr, Options{Loc: tz})
	b := Build(c, subj, pr, Options{Loc: tz})
	if a.Hash(nil) != b.Hash(nil) {
		t.Fatal("aynı girdiyle aynı paket çıkmalı")
	}
	if len(a.Items) < 4 {
		t.Fatalf("en az 4 kesit beklenir: %d", len(a.Items))
	}
	for i, it := range a.Items {
		if it.ID != fmt.Sprintf("K%d", i+1) {
			t.Errorf("kimlik sırası: %s, beklenen K%d", it.ID, i+1)
		}
		if i > 0 && it.From.Before(a.Items[i-1].From) {
			t.Error("kimlikler kronolojik sırada atanmalı")
		}
		if len(it.Messages) == 0 || it.Why == "" {
			t.Errorf("%s: boş kesit ya da gerekçe yok", it.ID)
		}
	}
}

func TestPackCoversTriggersAndRespectsBudget(t *testing.T) {
	c, subj, pr := fixture(t)
	p := Build(c, subj, pr, Options{Loc: tz})
	kinds := map[string]bool{}
	for _, it := range p.Items {
		kinds[it.Kind] = true
	}
	for _, k := range []string{KindFirstContact, KindSilence, KindSurge, KindUnsent, KindLatest} {
		if !kinds[k] {
			t.Errorf("%s kesiti yok (var olanlar: %v, atlanan: %v)", k, kinds, p.Skipped)
		}
	}
	if p.Tokens > DefaultMaxTokens {
		t.Fatalf("paket bütçeyi aşıyor: %d > %d", p.Tokens, DefaultMaxTokens)
	}
	tiny := Build(c, subj, pr, Options{Loc: tz, MaxTokens: 2500})
	if tiny.Tokens > 2500 || len(tiny.Skipped) == 0 || len(tiny.Items) >= len(p.Items) {
		t.Fatalf("küçük bütçede kırpılmalı ve raporlanmalı: %d token, %d kesit, atlanan %v", tiny.Tokens, len(tiny.Items), tiny.Skipped)
	}
	if len(tiny.Items) > 0 && tiny.Items[0].Kind != KindFirstContact {
		t.Fatalf("kırpmada öncelik ilk temasta olmalı: %s", tiny.Items[0].Kind)
	}
}

func TestPackMessagesTraceBackToRealMessages(t *testing.T) {
	c, subj, pr := fixture(t)
	p := Build(c, subj, pr, Options{Loc: tz})
	real := map[int64]store.MessageRow{}
	for _, m := range c.Messages {
		real[m.ID] = m
	}
	for _, it := range p.Items {
		for _, m := range it.Messages {
			r, ok := real[m.ID]
			if !ok || !r.TS.Equal(m.TS) || r.Sender != m.Sender || r.ConvID != it.ConvID {
				t.Fatalf("%s: mesaj %d orijinaline geri izlenemiyor", it.ID, m.ID)
			}
		}
	}
	unsent := false
	for _, it := range p.Items {
		if it.Kind == KindUnsent {
			for _, m := range it.Messages {
				if strings.Contains(m.Text, "geri alındı") {
					unsent = true
				}
			}
		}
	}
	if !unsent {
		t.Fatal("geri alınan mesaj kesitte görünmeli")
	}
}

func TestRenderMasksIdentifiersAndKeepsIDs(t *testing.T) {
	c, subj, pr := fixture(t)
	p := Build(c, subj, pr, Options{Loc: tz})
	raw := p.Render(nil)
	if !strings.Contains(raw, "0532 123 45 67") {
		t.Fatal("test verisindeki telefon ham paketde görünmeli")
	}
	masked := p.Render(mask.New())
	if strings.Contains(masked, "0532 123 45 67") || !strings.Contains(masked, "[TEL-1]") {
		t.Fatalf("telefon maskelenmeli:\n%s", masked[:600])
	}
	if !strings.Contains(masked, "## K1 —") || !strings.Contains(masked, "VERİDİR") {
		t.Fatal("kimlikler ve veri uyarısı korunmalı")
	}
	if p.Hash(mask.New()) == p.Hash(nil) {
		t.Fatal("maskelenmiş ve ham paketin özeti farklı olmalı")
	}
	if p.Hash(mask.New()) != p.Hash(mask.New()) {
		t.Fatal("maskeli özet de deterministik olmalı")
	}
}

func TestIDsFeedTheAudit(t *testing.T) {
	c, subj, pr := fixture(t)
	p := Build(c, subj, pr, Options{Loc: tz})
	ids := p.IDs()
	if !ids["K1"] || ids[fmt.Sprintf("K%d", len(p.Items)+1)] {
		t.Fatal("geçerli kimlik kümesi yanlış")
	}
	rep := audit.Check([]audit.Claim{{Text: "Bir şey.", Evidence: []string{"K1", fmt.Sprintf("K%d", len(p.Items)+5)}, Observation: true}}, "", ids)
	if !rep.Has(audit.RuleUnknownEvidence) {
		t.Fatal("pakette olmayan kimlik yakalanmalı")
	}
}

func TestExcludeTitlesRemovesItem(t *testing.T) {
	c, subj, pr := fixture(t)
	p := Build(c, subj, pr, Options{Loc: tz})
	title := p.Items[0].Title
	q := Build(c, subj, pr, Options{Loc: tz, ExcludeTitles: map[string]bool{title: true}})
	for _, it := range q.Items {
		if it.Title == title {
			t.Fatalf("çıkarılan kesit pakette: %s", title)
		}
	}
	if len(q.Items) != len(p.Items)-1 {
		t.Fatalf("tam bir kesit eksilmeli: %d → %d", len(p.Items), len(q.Items))
	}
}

func TestNoPartnersGivesMetricsOnly(t *testing.T) {
	s, _ := store.Open(":memory:")
	defer s.Close()
	p, _ := s.CreatePerson("Kimse", "", false)
	c, _ := s.LoadCorpus(nil)
	subj := analytics.NewSubject(p, c)
	pk := Build(c, subj, analytics.BuildProfile(c, subj, analytics.Options{}), Options{})
	if len(pk.Items) != 0 || pk.Metrics == "" || pk.Tokens == 0 {
		t.Fatalf("yalnızca metrik beklenir: %+v", pk)
	}
}

// richFixture 4 ay: her ay normal günler, bir yoğun gün (60 mesaj), gece 02:00 oturumu, tek taraflı 12'lik seri ve iki uzun sessizlik.
func richFixture(t *testing.T) (*store.Corpus, analytics.Subject, analytics.Profile) {
	t.Helper()
	s, _ := store.Open(":memory:")
	t.Cleanup(func() { s.Close() })
	start := time.Date(2026, 1, 1, 20, 0, 0, 0, tz)
	var msgs []model.Message
	add := func(who string, ts time.Time, text string) {
		msgs = append(msgs, model.Message{Sender: who, TS: ts, Kind: model.KindText, Text: text})
	}
	for d := 0; d < 120; d++ {
		day := start.AddDate(0, 0, d)
		if (d >= 20 && d < 32) || (d >= 70 && d < 80) { // iki sessizlik
			continue
		}
		n := 6
		if d == 50 {
			n = 60 // yoğun gün
		}
		for i := 0; i < n; i++ {
			who := "Ayşe"
			if i%2 == 1 {
				who = "Ben"
			}
			add(who, day.Add(time.Duration(i)*2*time.Minute), fmt.Sprintf("gün %d mesaj %d", d, i))
		}
		if d == 40 { // gece oturumu: 02:00'de 15 mesaj
			ts := time.Date(2026, 2, 11, 2, 0, 0, 0, tz)
			for i := 0; i < 15; i++ {
				who := "Ayşe"
				if i%2 == 1 {
					who = "Ben"
				}
				add(who, ts.Add(time.Duration(i)*time.Minute), fmt.Sprintf("gece %d", i))
			}
		}
		if d == 60 { // tek taraflı seri: Ayşe art arda 12 mesaj
			ts := day.Add(3 * time.Hour)
			for i := 0; i < 12; i++ {
				add("Ayşe", ts.Add(time.Duration(i)*time.Minute), fmt.Sprintf("seri %d", i))
			}
		}
	}
	imp := &model.Import{Platform: "whatsapp", Label: "x", OwnerName: "Ben", Stats: map[string]int{},
		Conversations: []model.Conversation{{Title: "Ayşe", Platform: "whatsapp", Participants: []string{"Ben", "Ayşe"}, Messages: msgs}}}
	id, _ := s.SaveImport(imp, "rich")
	src, _ := s.Source(id)
	me, _ := s.EnsureOwnerPerson(src)
	c, _ := s.LoadCorpus(nil)
	subj := analytics.NewSubject(me, c)
	return c, subj, analytics.BuildProfile(c, subj, analytics.Options{Loc: tz})
}

func TestRichPackCoversRelationshipOverTimeNotJustEdges(t *testing.T) {
	c, subj, pr := richFixture(t)
	p := Build(c, subj, pr, Options{Loc: tz})
	count := map[string]int{}
	titles := map[string]bool{}
	for _, it := range p.Items {
		count[it.Kind]++
		if titles[it.Title] {
			t.Errorf("kesit başlıkları benzersiz olmalı (sepetten çıkarma başlığa göre): %q", it.Title)
		}
		titles[it.Title] = true
	}
	want := map[string]int{KindFirstContact: 1, KindLatest: 1, KindSilence: 2, KindPeakDay: 1, KindNight: 1, KindRun: 1}
	for k, n := range want {
		if count[k] < n {
			t.Errorf("%s en az %d olmalı: %v", k, n, count)
		}
	}
	if count[KindMonth] < 3 {
		t.Errorf("her ay için örnek olmalı (4 ay): %v", count)
	}
	if len(p.Items) < 10 {
		t.Fatalf("zengin veride paket ince kalmamalı: %d kesit (%v)", len(p.Items), count)
	}
	if p.Tokens > DefaultMaxTokens {
		t.Fatalf("bütçe aşıldı: %d", p.Tokens)
	}
	// seri ve gece kesiti doğru içeriği taşıyor
	for _, it := range p.Items {
		if it.Kind == KindRun && !strings.Contains(it.Messages[len(it.Messages)/2].Text, "seri") {
			t.Errorf("seri kesiti seri mesajlarını içermeli: %+v", it.Messages[:3])
		}
		if it.Kind == KindNight && !strings.Contains(it.Messages[0].Text, "gece") {
			t.Errorf("gece kesiti gece mesajlarıyla başlamalı: %+v", it.Messages[0])
		}
	}
	// küçük bütçede öncelik: ilk temas ve son durum ay örneklerinden önce gelir
	small := Build(c, subj, pr, Options{Loc: tz, MaxTokens: 2600})
	kinds := map[string]bool{}
	for _, it := range small.Items {
		kinds[it.Kind] = true
	}
	if !kinds[KindFirstContact] || !kinds[KindLatest] || kinds[KindMonth] {
		t.Fatalf("dar bütçede önce ilk temas/son durum alınmalı, ay örnekleri değil: %v", kinds)
	}
}

// momentFixture: çatışma ifadesi kümesi, yoğun yazışma sonrası uzun sessizlik ve yakınlık günü olan 6 aylık sohbet.
func momentFixture(t *testing.T) (*store.Corpus, analytics.Subject, analytics.Profile) {
	t.Helper()
	s, _ := store.Open(":memory:")
	t.Cleanup(func() { s.Close() })
	start := time.Date(2026, 1, 1, 20, 0, 0, 0, tz)
	var msgs []model.Message
	add := func(who string, ts time.Time, text string) {
		msgs = append(msgs, model.Message{Sender: who, TS: ts, Kind: model.KindText, Text: text})
	}
	for d := 0; d < 180; d++ {
		if d >= 100 && d < 130 { // 30 günlük sessizlik (öncesi yoğun)
			continue
		}
		day := start.AddDate(0, 0, d)
		n := 8
		if d >= 86 && d < 100 {
			n = 20 // yoğun yazışma → sessizlik
		}
		for i := 0; i < n; i++ {
			who := "Ayşe"
			if i%2 == 1 {
				who = "Ben"
			}
			text := fmt.Sprintf("gün %d mesaj %d", d, i)
			if d == 60 && i >= 2 && i < 8 {
				text = "yeter artık bıktım" // çatışma kümesi: 6 mesaj, dakikalar içinde
			}
			if d == 150 && i < 7 {
				text = "canım çok özledim" // yakınlık günü
			}
			add(who, day.Add(time.Duration(i)*3*time.Minute), text)
		}
	}
	imp := &model.Import{Platform: "whatsapp", Label: "m", OwnerName: "Ben", Stats: map[string]int{},
		Conversations: []model.Conversation{{Title: "Ayşe", Platform: "whatsapp", Participants: []string{"Ben", "Ayşe"}, Messages: msgs}}}
	id, _ := s.SaveImport(imp, "moments")
	src, _ := s.Source(id)
	me, _ := s.EnsureOwnerPerson(src)
	c, _ := s.LoadCorpus(nil)
	subj := analytics.NewSubject(me, c)
	return c, subj, analytics.BuildProfile(c, subj, analytics.Options{Loc: tz})
}

func TestPackIncludesConflictAffectionAndRuptureWindows(t *testing.T) {
	c, subj, pr := momentFixture(t)
	p := Build(c, subj, pr, Options{Loc: tz})
	kinds := map[string]int{}
	for _, it := range p.Items {
		kinds[it.Kind]++
	}
	for _, k := range []string{KindConflict, KindAffection, KindRupture, KindCoverage} {
		if kinds[k] == 0 {
			t.Errorf("%s kesiti pakette olmalı; var olanlar: %v", k, kinds)
		}
	}
	// çatışma kesiti, kümedeki mesajları ve çevresini gerçekten içermeli
	for _, it := range p.Items {
		if it.Kind == KindConflict {
			hit := 0
			for _, m := range it.Messages {
				if strings.Contains(m.Text, "yeter artık") {
					hit++
				}
			}
			if hit < 5 || !strings.Contains(it.Why, "anlam yorumlanmaz") {
				t.Errorf("çatışma kesiti kümeyi içermeli ve sınırını belirtmeli: %d isabet, %q", hit, it.Why)
			}
		}
	}
}

func TestBiggerBudgetAddsCoverageAndSmallBudgetStaysEvenlySpread(t *testing.T) {
	c, subj, pr := momentFixture(t)
	small := Build(c, subj, pr, Options{Loc: tz, MaxTokens: 5000})
	big := Build(c, subj, pr, Options{Loc: tz, MaxTokens: DefaultMaxTokens})
	if len(big.Items) <= len(small.Items)*2 {
		t.Fatalf("büyük bütçe çok daha fazla kesit taşımalı: %d vs %d", len(big.Items), len(small.Items))
	}
	if small.Tokens > 5000+200 || big.Tokens > DefaultMaxTokens+200 {
		t.Fatalf("bütçe aşılmamalı: %d %d", small.Tokens, big.Tokens)
	}
	// dar bütçede bile örnek pencereler tüm zamana yayılır: ilk ve son çeyrekten en az birer kesit
	first, last := small.Items[0].From, small.Items[len(small.Items)-1].To
	span := last.Sub(first)
	early, late := 0, 0
	for _, it := range small.Items {
		if it.From.Sub(first) < span/4 {
			early++
		}
		if last.Sub(it.To) < span/4 {
			late++
		}
	}
	if early == 0 || late == 0 {
		t.Fatalf("seçim zamana yayılmalı: ilk çeyrek %d, son çeyrek %d", early, late)
	}
	// örtüşen kesitler tekrar alınmaz
	seen := map[int64]int{}
	for _, it := range big.Items {
		for _, m := range it.Messages {
			seen[m.ID]++
		}
	}
	dup := 0
	for _, n := range seen {
		if n > 1 {
			dup++
		}
	}
	total := 0
	for _, it := range big.Items {
		total += len(it.Messages)
	}
	if float64(dup) > 0.25*float64(total) {
		t.Fatalf("çok fazla tekrar eden mesaj: %d / %d", dup, total)
	}
}

func TestMaxTokensForRespectsContextWindow(t *testing.T) {
	cases := map[int]int{0: DefaultMaxTokens, 1_000_000: DefaultMaxTokens, 128_000: 44800, 32_000: MinMaxTokens, 8_000: MinMaxTokens}
	for ctx, want := range cases {
		if got := MaxTokensFor(ctx); got != want {
			t.Errorf("bağlam %d: %d, beklenen %d", ctx, got, want)
		}
	}
}
