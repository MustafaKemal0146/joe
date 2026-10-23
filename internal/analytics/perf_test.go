package analytics

import (
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/MustafaKemal0146/joe/internal/model"
	"github.com/MustafaKemal0146/joe/internal/store"
)

// bigFixture n mesajlık, birkaç yüz sohbetli sentetik bir kaynak üretir (gerçek veri yok).
func bigFixture(tb testing.TB, n int) (*store.Store, *store.Corpus, []store.Person) {
	tb.Helper()
	s, err := store.Open(":memory:")
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { s.Close() })
	const convs = 300
	per := n / convs
	t0 := time.Date(2024, 1, 1, 9, 0, 0, 0, time.UTC)
	words := []string{"bugün", "akşam", "film", "yarın", "görüşelim", "tamam", "olur", "çok", "güzel", "nasılsın"}
	imp := &model.Import{Platform: "instagram", Label: "perf", OwnerName: "Ben", OwnerHandle: "ben", Stats: map[string]int{}}
	for c := 0; c < convs; c++ {
		name := fmt.Sprintf("Kişi %03d", c)
		conv := model.Conversation{Title: name, Platform: "instagram", Participants: []string{"Ben", name}}
		ts := t0.Add(time.Duration(c) * time.Hour)
		for i := 0; i < per; i++ {
			sender := name
			if i%2 == 0 {
				sender = "Ben"
			}
			ts = ts.Add(time.Duration(30+(i*7)%900) * time.Second)
			conv.Messages = append(conv.Messages, model.Message{Sender: sender, TS: ts, Kind: model.KindText,
				Text: words[i%10] + " " + words[(i*3)%10] + " " + words[(i*7)%10] + " 😀"})
		}
		imp.Conversations = append(imp.Conversations, conv)
		for e := 0; e < 20; e++ {
			imp.Events = append(imp.Events, model.Event{Kind: model.EvLikePost, TS: ts, Actor: model.Owner, Counterpart: name})
		}
	}
	id, err := s.SaveImport(imp, "perf")
	if err != nil {
		tb.Fatal(err)
	}
	src, _ := s.Source(id)
	s.EnsureOwnerPerson(src)
	s.CreatePerson("Kişi 007", "", false)
	c, err := s.LoadCorpus(nil)
	if err != nil {
		tb.Fatal(err)
	}
	people, _ := s.People()
	return s, c, people
}

// TestPerfLargeCorpus büyük veride süreleri ölçer. Yavaş olduğu için yalnızca
// JOE_PERF=<mesaj sayısı> ile çalışır; örn. JOE_PERF=500000 go test -run PerfLarge -v ./internal/analytics
func TestPerfLargeCorpus(t *testing.T) {
	v := os.Getenv("JOE_PERF")
	if v == "" {
		t.Skip("JOE_PERF ayarlı değil")
	}
	n, _ := strconv.Atoi(v)
	start := time.Now()
	s, _, _ := bigFixture(t, n)
	t.Logf("%d mesaj kaydedildi: %v", n, time.Since(start))

	start = time.Now()
	c, err := s.LoadCorpus(nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("LoadCorpus: %v (%d mesaj)", time.Since(start), len(c.Messages))
	people, _ := s.People()
	var ben, other store.Person
	for _, p := range people {
		if p.IsSelf {
			ben = p
		} else {
			other = p
		}
	}
	opt := Options{Loc: istanbul}
	measure := func(name string, f func()) time.Duration {
		st := time.Now()
		f()
		d := time.Since(st)
		t.Logf("%s: %v", name, d)
		return d
	}
	profile := measure("BuildProfile(ben)", func() { BuildProfile(c, NewSubject(ben, c), opt) })
	measure("BuildGraph", func() { BuildGraph(c, people, 60) })
	measure("Compare", func() { Compare(c, NewSubject(ben, c), NewSubject(other, c), opt) })
	measure("Timeline", func() {
		sub := NewSubject(ben, c)
		BuildTimeline(c, sub, BuildProfile(c, sub, opt), istanbul)
	})
	if limit := os.Getenv("JOE_PERF_MAX_PROFILE_MS"); limit != "" {
		ms, _ := strconv.Atoi(limit)
		if profile > time.Duration(ms)*time.Millisecond {
			t.Fatalf("profil %v > %d ms", profile, ms)
		}
	}
}

func BenchmarkBuildProfile100k(b *testing.B) {
	_, c, people := bigFixture(b, 100_000)
	var ben store.Person
	for _, p := range people {
		if p.IsSelf {
			ben = p
		}
	}
	opt := Options{Loc: istanbul}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		BuildProfile(c, NewSubject(ben, c), opt)
	}
}
