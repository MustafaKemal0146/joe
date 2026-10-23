package eval

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/MustafaKemal0146/joe/internal/llm"
	"github.com/MustafaKemal0146/joe/internal/personas"
)

func good(text string) string {
	return fmt.Sprintf(`{"abstain":false,"abstain_reason":"","observations":[{"text":%q,"evidence":["K1"]}],
"hypotheses":[{"claim":"Bir yakınlık arayışı olabilir.","evidence":["K1"],"counter_hypothesis":"Yalnızca program değişmiş olabilir.","confidence":"low","falsified_by":"Eski saatlere dönülürse."}],
"open_questions":[],"data_needed":["Hafta sonu verisi"]}`, text)
}

const abstain = `{"abstain":true,"abstain_reason":"Yalnızca 12 mesaj var, örüntü çıkarılamaz."}`

func TestFixturesBuildPacksWithExpectedShape(t *testing.T) {
	for _, c := range Cases() {
		pack, err := c.Fixture()
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		if len(pack.Items) == 0 || pack.Metrics == "" {
			t.Errorf("%s: paket boş", c.Name)
		}
		if again, _ := c.Fixture(); again.Hash(nil) != pack.Hash(nil) {
			t.Errorf("%s: vaka deterministik olmalı", c.Name)
		}
	}
	// gömülü örüntüler metriklerde görünmeli
	byName := map[string]string{}
	for _, c := range Cases() {
		p, _ := c.Fixture()
		byName[c.Name] = p.Metrics
	}
	if !strings.Contains(byName["ani_sessizlik"], "en uzun sessizlik 46 gün") {
		t.Errorf("sessizlik metriklerde görünmeli:\n%s", byName["ani_sessizlik"])
	}
	if !strings.Contains(byName["tek_tarafli_baslatma"], "başlatma 135/15") {
		t.Errorf("başlatma asimetrisi metriklerde görünmeli:\n%s", byName["tek_tarafli_baslatma"])
	}
	if !strings.Contains(byName["gece_kaymasi"], "Dönem: yoğun") {
		t.Errorf("gece kayması yoğun dönem olarak görünmeli:\n%s", byName["gece_kaymasi"])
	}
}

func TestScoreGoodOutputCapturesPattern(t *testing.T) {
	c := Cases()[0]
	r := Score(c, "jung", good("Üçüncü aydan sonra mesajlar gece 01:00 civarına kaymış."), map[string]bool{"K1": true, "K2": true})
	if !r.SchemaValid || !r.PatternHit || r.EvidenceValid != 1 || r.Unsupported != 0 || r.Diagnosis+r.Danger+r.Certainty != 0 || !r.AbstainCorrect || !r.HasCounter {
		t.Fatalf("iyi çıktı: %+v", r)
	}
	miss := Score(c, "jung", good("Mesajlar düzenli."), map[string]bool{"K1": true})
	if miss.PatternHit {
		t.Fatal("örüntüden söz etmeyen çıktı yakalanmış sayılmamalı")
	}
}

func TestScoreBadOutputsAreCounted(t *testing.T) {
	c := Cases()[0]
	bad := `{"abstain":false,"observations":[{"text":"Kişi borderline kişilik bozukluğu gösteriyor.","evidence":["K9"]}],"hypotheses":[]}`
	r := Score(c, "freud", bad, map[string]bool{"K1": true})
	if !r.SchemaValid || r.Diagnosis != 1 || r.CitedInvalid != 1 || r.EvidenceValid != 0 {
		t.Fatalf("kötü çıktı: %+v", r)
	}
	broken := Score(c, "freud", "Özetim: bu kişi psikopat biri.", map[string]bool{})
	if broken.SchemaValid || broken.SchemaError == "" || broken.Diagnosis != 1 {
		t.Fatalf("şemasız çıktı da yasak dil için taranmalı: %+v", broken)
	}
}

func TestAbstainExpectations(t *testing.T) {
	var little Case
	for _, c := range Cases() {
		if c.WantAbstain {
			little = c
		}
	}
	if r := Score(little, "jung", abstain, map[string]bool{"K1": true}); !r.AbstainCorrect || !r.PatternHit || !r.Abstained {
		t.Fatalf("çekimserlik doğru: %+v", r)
	}
	over := `{"abstain":false,"observations":[{"text":"Güçlü bir örüntü var.","evidence":["K1"]}],"hypotheses":[{"claim":"x olabilir.","evidence":["K1"],"counter_hypothesis":"y","confidence":"high","falsified_by":"z"}]}`
	if r := Score(little, "jung", over, map[string]bool{"K1": true}); r.AbstainCorrect {
		t.Fatal("yetersiz veride yüksek güvenle iddia doğru davranış değil")
	}
	// veri yeterliyken çekimser kalmak doğru değil
	if r := Score(Cases()[0], "jung", abstain, map[string]bool{"K1": true}); r.AbstainCorrect {
		t.Fatal("yeterli veride çekimserlik doğru sayılmamalı")
	}
}

type script struct {
	i    int
	outs []string
}

func (s *script) Complete(ctx context.Context, system, user string, max int) (string, llm.Usage, error) {
	o := s.outs[s.i%len(s.outs)]
	s.i++
	return o, llm.Usage{PromptTokens: 4000, CompletionTokens: 500}, nil
}

func TestRunSummarizeAndReport(t *testing.T) {
	ps, _ := personas.Load("")
	cases := Cases()
	sc := &script{outs: []string{
		good("Mesajlar gece yarısı saatlerine kaymış."), good("Başlatma tek taraflı, yanıt gecikmeli."),
		good("Uzun bir sessizlik ve dönüş var."), good("Son dönemde yazışma belirgin biçimde azalmış; ilk dönemle kıyaslanınca değişim görülüyor."), good("Son dönemde mesajlar gece saatlerine kaymış; ilk dönemde akşam yazışması vardı."), good("İki gün boyunca sitem ve çatışma yoğunlaşmış."), good("Ayşe'nin yanıtları seyrekleşmiş."), abstain,
	}}
	rs, err := Run(context.Background(), sc, "deepseek-flash", cases, []string{"jung"}, ps)
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 8 || sc.i != 8 {
		t.Fatalf("8 çalıştırma beklenir: %d", len(rs))
	}
	s := Summarize(rs)
	if s.SchemaValidRate != 1 || s.PatternRate != 1 || s.AbstainCorrectRate != 1 || s.EvidenceValid != 1 || s.PeriodRate != 1 || s.CostUSD <= 0 {
		t.Fatalf("özet: %+v", s)
	}
	md := Markdown("deepseek-flash", personas.PromptVersion, rs)
	for _, want := range []string{"# Değerlendirme raporu", "Geçerli şema oranı | %100", "gece_kaymasi", "donem_degisimi", "yetersiz_veri", "deepseek-flash"} {
		if !strings.Contains(md, want) {
			t.Errorf("raporda %q yok", want)
		}
	}
	worse := Summarize([]Result{{SchemaValid: true, Diagnosis: 2, PatternHit: false, AbstainCorrect: true}})
	if cmp := Compare(s, worse); !strings.Contains(cmp, "örüntü -100 puan") || !strings.Contains(cmp, "teşhis dili +2") {
		t.Fatalf("karşılaştırma: %s", cmp)
	}
	if _, err := Run(context.Background(), sc, "m", cases[:1], []string{"yok"}, ps); err == nil {
		t.Fatal("bilinmeyen persona hata vermeli")
	}
}

func TestPeriodCaseFixtureCarriesPhaseComparisonAndLegacyPromptDropsRule(t *testing.T) {
	var c Case
	for _, x := range Cases() {
		if x.WantPhases {
			c = x
		}
	}
	pack, err := c.Fixture()
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{"Dönem karşılaştırması", "Dönem 1", "Dönem 3"} {
		if !strings.Contains(pack.Metrics, w) {
			t.Fatalf("paket metriklerinde %q olmalı:\n%s", w, pack.Metrics)
		}
	}
	if !strings.Contains(personas.CommonSystem(), "Dönem karşılaştırması") || strings.Contains(personas.LegacyCommonSystem(), "Dönem karşılaştırması") {
		t.Fatal("eski istem dönem kuralını içermemeli, yeni istem içermeli")
	}
	if !strings.Contains(personas.LegacyCommonSystem(), "10. Yanıtın yalnızca") {
		t.Fatal("eski istemde numaralandırma düzgün kalmalı")
	}
	// kıyas yapmayan çıktı dönem vakasında isabet sayılmaz
	r := Score(c, "jung", good("Gece saatlerinde yoğunluk var."), pack.IDs())
	if r.PeriodHit {
		t.Fatal("'dönem' geçmeyen çıktı dönem kıyası sayılmamalı")
	}
}

func TestComparesPeriodsNeedsBothPeriodsAndAChangeWord(t *testing.T) {
	cases := map[string]bool{
		"İlk dönemde yazışma yoğundu, son dönemde azaldı.": true,
		"1. dönem ile 3. dönem arasında fark var.":         true,
		"Bu dönem ilişkide önemli bir dönem.":              false, // yalnızca sözcük
		"İlk dönemde yoğun yazışma vardı.":                 false, // son dönem anılmıyor
		"İlk dönem ve son dönem aynı saatlerde yazılmış.":  false, // değişim sözcüğü yok
		"Gece yoğunluğu artmış.":                           false,
	}
	for text, want := range cases {
		if got := comparesPeriods(strings.ToLower(text)); got != want {
			t.Errorf("%q: %v, beklenen %v", text, got, want)
		}
	}
}
