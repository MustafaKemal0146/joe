package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/MustafaKemal0146/joe/internal/budget"
	"github.com/MustafaKemal0146/joe/internal/council"
	"github.com/MustafaKemal0146/joe/internal/eval"
	"github.com/MustafaKemal0146/joe/internal/llm"
	"github.com/MustafaKemal0146/joe/internal/mask"
	"github.com/MustafaKemal0146/joe/internal/personas"
)

// evalEstimate değerlendirmenin en kötü durum maliyetini hesaplar (vaka × persona çağrısı).
func evalEstimate(model string, cases []eval.Case, ids []string, ps []personas.Persona) (budget.Estimate, error) {
	var total budget.Estimate
	total.Model = model
	sec := map[string]int{}
	for _, p := range ps {
		sec[p.ID] = budget.EstimateTokens(personas.Section(p))
	}
	for _, c := range cases {
		pack, err := c.Fixture()
		if err != nil {
			return total, err
		}
		tokens := budget.EstimateTokens(pack.Render(mask.New())) + budget.EstimateTokens(personas.CommonSystem())
		for _, id := range ids {
			e := budget.Compute(budget.Plan{Model: model, SharedPrefixTokens: tokens, PerCallTokens: sec[id], Calls: 1, OutputTokensPerCall: council.PersonaMaxOut})
			total.Known = e.Known
			total.Calls++
			total.PromptTokens += e.PromptTokens
			total.OutputTokens += e.OutputTokens
			total.LowUSD += e.LowUSD
			total.HighUSD += e.HighUSD
		}
	}
	return total, nil
}

// evalCmd sentetik vakalarla persona kalitesini ölçer. Gerçek LLM çağrısı yapar ve para harcar;
// önce tahmini gösterir ve --yes olmadan çağrı yapmaz.
func evalCmd(args []string) error {
	fs := flag.NewFlagSet("eval", flag.ContinueOnError)
	list := fs.String("personas", "jung,freud,winnicott", "virgülle ayrılmış persona kimlikleri")
	yes := fs.Bool("yes", false, "tahmini maliyeti onayla ve çalıştır")
	out := fs.String("out", "", "raporu bu Markdown dosyasına yaz")
	repeat := fs.Int("repeat", 1, "her (vaka, persona) çiftini kaç kez çalıştır (şans etkisini azaltır; maliyet çarpılır)")
	legacy := fs.Bool("legacy-prompt", false, "dönem karşılaştırması kuralı olmayan eski ortak istemle çalıştır (A/B ölçümü için)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	st, cfgH, dir, err := open()
	if err != nil {
		return err
	}
	defer st.Close()
	ps, err := personas.Load(dir + "/personas")
	if err != nil {
		return err
	}
	ids := strings.Split(*list, ",")
	cases := eval.Cases()
	cfg := cfgH.Get()
	if *repeat < 1 || *repeat > 5 {
		return errors.New("--repeat 1 ile 5 arasında olmalı")
	}
	est, err := evalEstimate(cfg.ActiveModel, cases, ids, ps)
	if err != nil {
		return err
	}
	est.Calls *= *repeat
	est.LowUSD *= float64(*repeat)
	est.HighUSD *= float64(*repeat)
	if !est.Known {
		return fmt.Errorf("model %q için fiyat bilinmiyor; maliyet doğrulanamadığı için çalıştırılmaz", cfg.ActiveModel)
	}
	fmt.Printf("Değerlendirme: %d vaka × %d persona × %d tekrar = %d çağrı, model %s\n", len(cases), len(ids), *repeat, est.Calls, cfg.ActiveModel)
	fmt.Printf("Tahmini en kötü maliyet: $%.4f (alt uç $%.4f); bütçe tavanın $%.2f\n", est.HighUSD, est.LowUSD, cfg.BudgetUSD)
	if err := (budget.Guard{CapUSD: cfg.BudgetUSD}).Allow(0, est.HighUSD); err != nil {
		return err
	}
	if !*yes {
		return errors.New("çalıştırmak için --yes ekle (gerçek API çağrısı yapar ve para harcar)")
	}
	if !cfgH.Ready() {
		return errors.New("yapay zekâ sağlayıcısı hazır değil: arayüzde Ayarlar'dan anahtar ekle")
	}
	res, _ := cfgH.Resolve(cfg.ActiveProvider)
	caller := council.LLMCaller{Transport: llm.NewTransport(res.Provider.ID, res.BaseURL, res.APIKey), Model: cfg.ActiveModel}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	system := personas.CommonSystem()
	if *legacy {
		system = personas.LegacyCommonSystem()
	}
	results, err := eval.RunRepeat(ctx, caller, cfg.ActiveModel, system, cases, ids, ps, *repeat)
	if err != nil {
		return err
	}
	md := eval.Markdown(cfg.ActiveModel, personas.PromptVersion, results)
	if *out != "" {
		if err := os.WriteFile(*out, []byte(md), 0o600); err != nil {
			return err
		}
		fmt.Println("Rapor yazıldı:", *out)
	}
	fmt.Println()
	fmt.Print(md)
	return nil
}
