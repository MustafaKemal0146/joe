package report

import (
	"strings"
	"testing"
	"time"

	"github.com/MustafaKemal0146/joe/internal/analytics"
	"github.com/MustafaKemal0146/joe/internal/store"
)

func sample() Input {
	pr := analytics.Profile{Subject: "Ben", Messages: 120, Days: 30, NightRatio: 0.25, SleepKnown: true, SleepStart: 3,
		First: time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC), Last: time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC),
		Latency: analytics.LatencyStat{Median: 120, P90: 900, N: 40},
		Notes:   []string{"Bu kişi için 200'den az mesaj var."},
		Partners: []analytics.Partner{{Name: "Ay|şe\nK", Platform: "instagram", Total: 80,
			Messages: analytics.Pair{Subject: 40, Partner: 40}, LongestSilenceDays: 9}},
		Periods: []analytics.Period{{From: "2026-02", To: "2026-02", Kind: "surge", Count: 90, Baseline: 20}},
	}
	pr.Hours[23] = 100
	pr.Hours[2] = 20
	return Input{
		Person:      store.Person{Name: "Ben", Note: "test notu", Identities: []store.Identity{{Value: "Ben"}, {Value: "ben_k"}}},
		Profile:     pr,
		Timeline:    []analytics.TimelineItem{{TS: time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC), Kind: "contact", Title: "Ayşe ile ilk mesaj", Detail: "Instagram"}},
		GeneratedAt: time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC),
		Version:     "v0.1.0",
	}
}

func TestMarkdownIsDeterministicAndHasDisclaimers(t *testing.T) {
	a, b := Markdown(sample()), Markdown(sample())
	if a != b {
		t.Fatal("aynı girdiyle aynı rapor çıkmalı")
	}
	for _, want := range []string{"Gizlilik uyarısı", "klinik tanı", "joe v0.1.0", "21.10.2026", "ben_k", "Dikkat çeken dönemler", "Zaman çizelgesi", "23:00 | 100", "03:00 – 09:00", "Konsey (yapay zekâ) yorumu içermez"} {
		if !strings.Contains(a, want) {
			t.Errorf("raporda %q yok", want)
		}
	}
}

func TestMarkdownEscapesTableBreakingCharacters(t *testing.T) {
	out := Markdown(sample())
	if strings.Contains(out, "Ay|şe") {
		t.Fatal("tablo hücresindeki | kaçışlanmalı")
	}
	if !strings.Contains(out, `Ay\|şe K`) {
		t.Fatalf("kaçışlanmış ad bulunamadı:\n%s", out)
	}
}

func TestMarkdownEmptyProfile(t *testing.T) {
	in := sample()
	in.Profile = analytics.Profile{}
	out := Markdown(in)
	if !strings.Contains(out, "eşleşen mesaj ya da etkinlik bulunamadı") || strings.Contains(out, "## Ritim") {
		t.Fatalf("boş profil sade rapor vermeli:\n%s", out)
	}
}

func TestMarkdownTimezoneApplied(t *testing.T) {
	in := sample()
	in.Loc = time.FixedZone("TRT", 3*3600)
	if !strings.Contains(Markdown(in), "21.10.2026 15:00 TRT") {
		t.Fatal("rapor saati yerel saat diliminde olmalı")
	}
}

func TestHTMLMirrorsMarkdownAndIsSafe(t *testing.T) {
	in := sample()
	in.Person.Name = `<script>alert(1)</script> Ay|şe`
	out := HTML(in)
	if strings.Contains(out, "<script>alert(1)") {
		t.Fatal("kişi adı HTML olarak çalışmamalı (kaçışlanmalı)")
	}
	if !strings.Contains(out, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Fatal("kaçışlanmış ad bulunamadı")
	}
	if HTML(sample()) != HTML(sample()) {
		t.Fatal("aynı girdiyle aynı HTML çıkmalı")
	}
	good := HTML(sample())
	for _, want := range []string{"<h1>", "<h2>Özet</h2>", "<blockquote>", "<strong>Gizlilik uyarısı.</strong>", "<table>", "<th>Ölçüt</th>", "<td>Ay|şe K</td>", "PDF olarak kaydet", `<script src="/api/report-print.js">`, "<hr>", "<li>"} {
		if !strings.Contains(good, want) {
			t.Errorf("HTML'de %q yok", want)
		}
	}
	if strings.Contains(good, "**") {
		t.Fatal("işlenmemiş ** kalmamalı")
	}
	if strings.Contains(good, "onclick") || strings.Count(good, "<script") != 1 {
		t.Fatal("satır içi betik olmamalı (CSP)")
	}
	if !strings.Contains(good, "@media print") {
		t.Fatal("yazdırma stili olmalı")
	}
}

func TestHTMLEmptyProfileAndSplitRow(t *testing.T) {
	in := sample()
	in.Profile = analytics.Profile{}
	if out := HTML(in); !strings.Contains(out, "eşleşen mesaj ya da etkinlik bulunamadı") || strings.Contains(out, "<h2>Ritim</h2>") {
		t.Fatal("boş profil sade rapor vermeli")
	}
	if got := splitRow(`| a \| b | c |`); len(got) != 2 || got[0] != "a | b" || got[1] != "c" {
		t.Fatalf("kaçışlı çizgi hücre içinde kalmalı: %q", got)
	}
	if !isSeparator("|---|:---:|") || isSeparator("| a | b |") {
		t.Fatal("ayırıcı satır tespiti")
	}
}
