package analytics

import (
	"strings"
	"testing"
)

func TestPhasesShowChangeOverThirds(t *testing.T) {
	c, ben := timelineFixture(t)
	subj := NewSubject(ben, c)
	partner, ps := Phases(c, subj, istanbul)
	if partner != "Ayşe" || len(ps) != 3 {
		t.Fatalf("evreler: %q %d", partner, len(ps))
	}
	if !(ps[0].PerActiveDay < ps[1].PerActiveDay) {
		t.Fatalf("ilk dönem daha seyrek olmalı: %.1f vs %.1f", ps[0].PerActiveDay, ps[1].PerActiveDay)
	}
	total := 0
	for i, p := range ps {
		total += p.Messages
		if i > 0 && !p.From.Equal(ps[i-1].To) {
			t.Fatalf("dönemler bitişik olmalı: %v %v", ps[i-1].To, p.From)
		}
		if p.SubjectShare <= 0 || p.SubjectShare >= 1 {
			t.Fatalf("pay: %+v", p)
		}
	}
	if total < 1000 {
		t.Fatalf("tüm mesajlar dönemlere dağılmalı: %d", total)
	}
	txt := PhaseText(c, subj, istanbul)
	for _, w := range []string{"Dönem karşılaştırması", "Dönem 1", "Dönem 3", "değerlendirme değil ölçüm"} {
		if !strings.Contains(txt, w) {
			t.Fatalf("metin %q içermeli:\n%s", w, txt)
		}
	}
}

func TestPhasesSkippedForShortChats(t *testing.T) {
	c, ben := groupFixture(t)
	subj := NewSubject(ben[0], c)
	if txt := PhaseText(c, subj, istanbul); txt != "" {
		t.Fatalf("kısa veride karşılaştırma üretilmemeli: %s", txt)
	}
}
