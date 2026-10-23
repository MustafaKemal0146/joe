package council

import (
	"github.com/MustafaKemal0146/joe/internal/budget"
	"github.com/MustafaKemal0146/joe/internal/personas"
)

// perPersonaSummaryTokens bir persona çıktısının moderatör/sentez girdisindeki yaklaşık boyutudur.
const perPersonaSummaryTokens = 700

// repairHeadroom onarım çağrıları için eklenen güvenlik payıdır.
const repairHeadroom = 0.10

// PlanInput bir konsey çalışmasının maliyet hesabı girdileridir.
type PlanInput struct {
	Model       string // persona aşaması modeli
	StrongModel string // moderatör, sentez ve doğrulama modeli ("" = Model)
	PackTokens  int    // kanıt paketinin (maskeli) token sayısı
	Section     int    // persona bölümünün ortalama token sayısı
	Personas    int
	Items       int  // paketteki kesit sayısı (dönem özetleri için)
	Deep        bool // "derin" mod: persona aşamasından önce dönem özetleri
}

// Moderatör, sentez ve doğrulama girdisine eklenen atıf yapılan kesitlerin tahmini üst sınırı (token).
const citedTokensCap = 25_000

// verifyCalls sentezdeki iddia sayısına göre (≈22 iddia, 8'erli gruplar) doğrulama çağrısı sayısıdır.
const verifyCalls = 3

// DigestGroups derin modda kesitlerin kaç döneme bölüneceğini verir (3–10).
func DigestGroups(items int) int {
	g := items / 8
	if g < 3 {
		g = 3
	}
	if g > 10 {
		g = 10
	}
	return g
}

// PlanEstimate N persona + (derin modda dönem özetleri) + moderatör + sentez + doğrulama çalışmasının maliyet
// aralığını hesaplar. Güçlü model tanımlıysa moderatör, sentez ve doğrulama onun fiyatıyla hesaplanır.
func PlanEstimate(in PlanInput) budget.Estimate {
	strong := in.StrongModel
	if strong == "" {
		strong = in.Model
	}
	sys := budget.EstimateTokens(personas.CommonSystem())
	ps := budget.Compute(budget.Plan{Model: in.Model, SharedPrefixTokens: in.PackTokens + sys, PerCallTokens: in.Section, Calls: in.Personas, OutputTokensPerCall: PersonaMaxOut})
	total := ps
	total.Calls = in.Personas
	add := func(model string, prompt, out, calls int) {
		e := budget.Compute(budget.Plan{Model: model, SharedPrefixTokens: prompt, Calls: calls, OutputTokensPerCall: out})
		total.Calls += calls
		total.PromptTokens += e.PromptTokens
		total.OutputTokens += e.OutputTokens
		total.LowUSD += e.LowUSD
		total.HighUSD += e.HighUSD
		total.Known = total.Known && e.Known
	}
	if in.Deep {
		g := DigestGroups(in.Items)
		add(in.Model, in.PackTokens/g+budget.EstimateTokens(digestSystem), DigestMaxOut, g)
	}
	cited := in.PackTokens
	if cited > citedTokensCap {
		cited = citedTokensCap
	}
	if in.Personas >= 2 {
		add(strong, in.Personas*perPersonaSummaryTokens+budget.EstimateTokens(moderatorSystem)+cited, ModeratorMaxOut, 1)
	}
	add(strong, in.Personas*perPersonaSummaryTokens+ModeratorMaxOut+budget.EstimateTokens(synthesisSystem)+1500+cited, SynthesisMaxOut, 1)
	add(strong, budget.EstimateTokens(verifySystem)+6000, 1500, verifyCalls)
	total.HighUSD *= 1 + repairHeadroom
	total.LowUSD *= 1 + repairHeadroom
	return total
}

// AskEstimate tek soru-cevap çağrısının maliyet aralığıdır.
func AskEstimate(model string, packTokens, extraTokens int) budget.Estimate {
	e := budget.Compute(budget.Plan{Model: model, SharedPrefixTokens: packTokens + budget.EstimateTokens(askSystem), PerCallTokens: extraTokens, Calls: 1, OutputTokensPerCall: AskMaxOut})
	e.HighUSD *= 1 + repairHeadroom
	return e
}
