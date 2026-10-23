package eval

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/MustafaKemal0146/joe/internal/audit"
	"github.com/MustafaKemal0146/joe/internal/council"
	"github.com/MustafaKemal0146/joe/internal/llm"
	"github.com/MustafaKemal0146/joe/internal/mask"
	"github.com/MustafaKemal0146/joe/internal/personas"
)

// Result bir (vaka, persona) çalıştırmasının ölçümüdür.
type Result struct {
	Case, Persona string
	SchemaValid   bool
	SchemaError   string
	// EvidenceValid geçerli kanıt atıflarının tüm atıflara oranı (atıf yoksa 0).
	EvidenceValid  float64
	CitedTotal     int
	CitedInvalid   int
	Unsupported    int // kanıtsız iddia sayısı
	Diagnosis      int // teşhis dili ihlali
	Danger         int // tehlikelilik/suçlama dili ihlali
	Certainty      int // kesinlik dili ihlali
	PatternHit     bool
	WantPhases     bool
	PeriodHit      bool // WantPhases vakasında çıktı dönemleri kıyasladı
	Abstained      bool
	AbstainCorrect bool // WantAbstain ise çekimser ya da yalnızca düşük güven; değilse çekimser OLMAMALI
	HasCounter     bool // her hipotezde karşı hipotez + çürütme koşulu var (şema zorunlu kılar)
	PromptTokens   int
	OutputTokens   int
	CostUSD        float64
}

// Summary persona/vaka kümesinin özetidir.
type Summary struct {
	Runs                                        int
	SchemaValidRate, EvidenceValid, PatternRate float64
	AbstainCorrectRate                          float64
	Unsupported, Diagnosis, Danger, Certainty   int
	CostUSD                                     float64
	PeriodRate                                  float64 // dönem kıyası beklenen vakalarda isabet oranı
}

// Caller çağrı arayüzüdür (council.Caller ile aynı).
type Caller = council.Caller

func outputText(o personas.Output) string {
	var b strings.Builder
	for _, ob := range o.Observations {
		b.WriteString(ob.Text + " ")
	}
	for _, h := range o.Hypotheses {
		b.WriteString(h.Claim + " " + h.Counter + " ")
	}
	for _, q := range append(append([]string{}, o.OpenQuestions...), o.DataNeeded...) {
		b.WriteString(q + " ")
	}
	return strings.ToLower(b.String())
}

// Score bir ham model çıktısını vaka beklentilerine göre ölçer.
func Score(c Case, personaID, raw string, ids map[string]bool) Result {
	r := Result{Case: c.Name, Persona: personaID, WantPhases: c.WantPhases}
	o, err := personas.ParseOutput(raw)
	if err != nil {
		r.SchemaError = err.Error()
		// Şemaya uymayan çıktı da yasak dil için taranır: ham metindeki ihlaller yine sayılır.
		rep := audit.Check(nil, raw, ids)
		r.Diagnosis, r.Danger, r.Certainty = count(rep, audit.RuleDiagnosis), count(rep, audit.RuleDanger), count(rep, audit.RuleCertainty)
		return r
	}
	r.SchemaValid = true
	rep := o.Audit(ids)
	r.Unsupported = count(rep, audit.RuleNoEvidence)
	r.CitedInvalid = count(rep, audit.RuleUnknownEvidence)
	r.Diagnosis, r.Danger, r.Certainty = count(rep, audit.RuleDiagnosis), count(rep, audit.RuleDanger), count(rep, audit.RuleCertainty)
	for _, cl := range o.Claims() {
		r.CitedTotal += len(cl.Evidence)
	}
	if r.CitedTotal > 0 {
		valid := r.CitedTotal - r.CitedInvalid
		if valid < 0 {
			valid = 0
		}
		r.EvidenceValid = float64(valid) / float64(r.CitedTotal)
	}
	r.Abstained = o.Abstain
	low := true
	for _, h := range o.Hypotheses {
		if h.Confidence != "low" {
			low = false
		}
	}
	if c.WantAbstain {
		r.AbstainCorrect = o.Abstain || (len(o.Hypotheses) == 0 || low)
	} else {
		r.AbstainCorrect = !o.Abstain
	}
	r.HasCounter = true
	for _, h := range o.Hypotheses {
		if strings.TrimSpace(h.Counter) == "" || strings.TrimSpace(h.Falsified) == "" {
			r.HasCounter = false
		}
	}
	text := outputText(o)
	if c.WantPhases {
		r.PeriodHit = comparesPeriods(text)
	}
	if c.WantAbstain {
		r.PatternHit = o.Abstain
	} else {
		for _, k := range c.Keywords {
			if strings.Contains(text, strings.ToLower(k)) {
				r.PatternHit = true
				break
			}
		}
	}
	return r
}

func count(r audit.Report, rule audit.Rule) int {
	n := 0
	for _, v := range r.Violations {
		if v.Rule == rule {
			n++
		}
	}
	return n
}

// Run her (vaka, persona) çifti için bir çağrı yapar. Önbellek ve onarım kullanılmaz: ham kaliteyi ölçer.
func Run(ctx context.Context, caller Caller, model string, cases []Case, personaIDs []string, ps []personas.Persona) ([]Result, error) {
	return RunWith(ctx, caller, model, personas.CommonSystem(), cases, personaIDs, ps)
}

// RunWith Run gibi çalışır, ortak sistem istemini dışarıdan alır (istem sürümlerini kıyaslamak için).
func RunWith(ctx context.Context, caller Caller, model, system string, cases []Case, personaIDs []string, ps []personas.Persona) ([]Result, error) {
	return RunRepeat(ctx, caller, model, system, cases, personaIDs, ps, 1)
}

// RunRepeat her (vaka, persona) çiftini repeat kez çalıştırır: tek çalıştırmadaki şans etkisini azaltır.
func RunRepeat(ctx context.Context, caller Caller, model, system string, cases []Case, personaIDs []string, ps []personas.Persona, repeat int) ([]Result, error) {
	if repeat < 1 {
		repeat = 1
	}
	byID := map[string]personas.Persona{}
	for _, p := range ps {
		byID[p.ID] = p
	}
	var out []Result
	for _, c := range cases {
		pack, err := c.Fixture()
		if err != nil {
			return out, fmt.Errorf("%s: %w", c.Name, err)
		}
		text := pack.Render(mask.New())
		for _, pid := range personaIDs {
			for rep := 0; rep < repeat; rep++ {
				p, ok := byID[pid]
				if !ok {
					return out, fmt.Errorf("bilinmeyen persona: %s", pid)
				}
				raw, usage, err := caller.Complete(ctx, system, text+"\n\n"+personas.Section(p), council.PersonaMaxOut)
				if err != nil {
					return out, fmt.Errorf("%s/%s: %w", c.Name, pid, err)
				}
				r := Score(c, pid, raw, pack.IDs())
				r.PromptTokens, r.OutputTokens = usage.PromptTokens, usage.CompletionTokens
				r.CostUSD, _ = llm.EstimateCost(model, usage.PromptTokens, usage.CompletionTokens)
				out = append(out, r)
			}
		}
	}
	return out, nil
}

// Summarize sonuçları özetler.
func Summarize(rs []Result) Summary {
	var s Summary
	s.Runs = len(rs)
	if len(rs) == 0 {
		return s
	}
	var schema, pat, abst, evSum float64
	evN, phN, phHit := 0, 0, 0
	for _, r := range rs {
		if r.SchemaValid {
			schema++
		}
		if r.PatternHit {
			pat++
		}
		if r.WantPhases {
			phN++
			if r.PeriodHit {
				phHit++
			}
		}
		if r.AbstainCorrect {
			abst++
		}
		if r.CitedTotal > 0 {
			evSum += r.EvidenceValid
			evN++
		}
		s.Unsupported += r.Unsupported
		s.Diagnosis += r.Diagnosis
		s.Danger += r.Danger
		s.Certainty += r.Certainty
		s.CostUSD += r.CostUSD
	}
	n := float64(len(rs))
	s.SchemaValidRate, s.PatternRate, s.AbstainCorrectRate = schema/n, pat/n, abst/n
	if evN > 0 {
		s.EvidenceValid = evSum / float64(evN)
	}
	if phN > 0 {
		s.PeriodRate = float64(phHit) / float64(phN)
	}
	return s
}

// Markdown sonuç tablosunu ve özeti üretir.
func Markdown(model, promptVersion string, rs []Result) string {
	var b strings.Builder
	s := Summarize(rs)
	fmt.Fprintf(&b, "# Değerlendirme raporu\n\nModel: `%s` · İstem sürümü: `%s` · %d çalıştırma · maliyet $%.4f\n\n", model, promptVersion, s.Runs, s.CostUSD)
	fmt.Fprintf(&b, "| Ölçüt | Değer |\n|---|---|\n")
	fmt.Fprintf(&b, "| Geçerli şema oranı | %%%.0f |\n| Geçerli kanıt atfı oranı | %%%.0f |\n| Gömülü örüntüyü yakalama | %%%.0f |\n| Doğru çekimserlik davranışı | %%%.0f |\n| Dönem kıyası (dönem vakası) | %%%.0f |\n",
		s.SchemaValidRate*100, s.EvidenceValid*100, s.PatternRate*100, s.AbstainCorrectRate*100, s.PeriodRate*100)
	fmt.Fprintf(&b, "| Kanıtsız iddia | %d |\n| Teşhis dili | %d |\n| Tehlikelilik/suçlama dili | %d |\n| Kesinlik dili | %d |\n\n", s.Unsupported, s.Diagnosis, s.Danger, s.Certainty)
	b.WriteString("| Vaka | Persona | Şema | Kanıt | Örüntü | Çekimserlik | Kanıtsız | Tanı | Tehlike |\n|---|---|---|---|---|---|---|---|---|\n")
	sort.SliceStable(rs, func(i, j int) bool {
		if rs[i].Case != rs[j].Case {
			return rs[i].Case < rs[j].Case
		}
		return rs[i].Persona < rs[j].Persona
	})
	yn := func(v bool) string {
		if v {
			return "✓"
		}
		return "✗"
	}
	for _, r := range rs {
		fmt.Fprintf(&b, "| %s | %s | %s | %%%.0f | %s | %s | %d | %d | %d |\n", r.Case, r.Persona, yn(r.SchemaValid), r.EvidenceValid*100, yn(r.PatternHit), yn(r.AbstainCorrect), r.Unsupported, r.Diagnosis, r.Danger)
	}
	return b.String()
}

// Compare iki çalıştırmanın özetini kıyaslar (ör. iki istem sürümü).
func Compare(a, b Summary) string {
	d := func(x, y float64) string { return fmt.Sprintf("%+.0f puan", (y-x)*100) }
	return fmt.Sprintf("şema %s, kanıt atfı %s, örüntü %s, çekimserlik %s, dönem kıyası %s; teşhis dili %+d, tehlike dili %+d, kanıtsız iddia %+d",
		d(a.SchemaValidRate, b.SchemaValidRate), d(a.EvidenceValid, b.EvidenceValid), d(a.PatternRate, b.PatternRate),
		d(a.AbstainCorrectRate, b.AbstainCorrectRate), d(a.PeriodRate, b.PeriodRate), b.Diagnosis-a.Diagnosis, b.Danger-a.Danger, b.Unsupported-a.Unsupported)
}

var (
	firstPeriod = []string{"ilk dönem", "birinci dönem", "1. dönem", "dönem 1", "başlangıç dönem", "ilk üçte bir", "ilk üçüncü"}
	lastPeriod  = []string{"son dönem", "üçüncü dönem", "3. dönem", "dönem 3", "son üçte bir", "son üçüncü"}
	changeWords = []string{"azal", "arta", "artış", "düş", "seyrek", "yoğun", "değiş", "kay", "ters", "fark", "gerile", "yüksel", "dönüş"}
)

// comparesPeriods çıktının dönemleri GERÇEKTEN kıyasladığını söyler: ilk ve son dönemi açıkça anar
// ve bir değişim sözcüğü kullanır. Yalnızca "dönem" sözcüğünü geçirmek yetmez.
func comparesPeriods(text string) bool {
	has := func(words []string) bool {
		for _, w := range words {
			if strings.Contains(text, w) {
				return true
			}
		}
		return false
	}
	return has(firstPeriod) && has(lastPeriod) && has(changeWords)
}
