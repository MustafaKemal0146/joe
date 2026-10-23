package budget

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

func TestEstimateTokensIsConservative(t *testing.T) {
	if EstimateTokens("") != 0 {
		t.Fatal("boş metin 0 token")
	}
	if got := EstimateTokens(strings.Repeat("ş", 300)); got != 120 {
		t.Fatalf("300 karakter ≈ 120 token, 2,5 karakter/token (rune sayılır, bayt değil): %d", got)
	}
}

func TestComputeRangeUsesCatalogPrices(t *testing.T) {
	e := Compute(Plan{Model: "deepseek-flash", SharedPrefixTokens: 10_000, PerCallTokens: 500, Calls: 15, OutputTokensPerCall: 1500})
	if !e.Known {
		t.Fatalf("deepseek-flash fiyatı kataloğda: %+v", e)
	}
	if e.PromptTokens != 15*10_500 || e.OutputTokens != 15*1500 {
		t.Fatalf("token toplamları: %+v", e)
	}
	// Yüksek uç elle: 157.500 * 0.3/1M + 22.500 * 1.2/1M
	wantHigh := 157_500*0.3/1e6 + 22_500*1.2/1e6
	if math.Abs(e.HighUSD-wantHigh) > 1e-9 {
		t.Fatalf("üst uç %v, beklenen %v", e.HighUSD, wantHigh)
	}
	if !(e.LowUSD < e.HighUSD) || e.LowUSD <= 22_500*1.2/1e6 {
		t.Fatalf("alt uç üst uçtan küçük ama çıktı maliyetinden büyük olmalı: %+v", e)
	}
}

func TestComputeUnknownModelInventsNothing(t *testing.T) {
	e := Compute(Plan{Model: "bilinmeyen-model-x", SharedPrefixTokens: 1000, Calls: 3, OutputTokensPerCall: 100})
	if e.Known || e.HighUSD != 0 || e.LowUSD != 0 || e.Note == "" {
		t.Fatalf("bilinmeyen modelde tutar uydurulmamalı: %+v", e)
	}
	if e.PromptTokens != 3000 {
		t.Fatal("token sayıları yine de verilmeli")
	}
}

func TestComputeZeroCalls(t *testing.T) {
	if e := Compute(Plan{Model: "deepseek-flash"}); e.HighUSD != 0 || e.PromptTokens != 0 {
		t.Fatalf("%+v", e)
	}
}

func TestGuardHardCap(t *testing.T) {
	g := Guard{CapUSD: 0.25}
	if err := g.Allow(0.10, 0.10); err != nil {
		t.Fatalf("tavan altında izin verilmeli: %v", err)
	}
	if err := g.Allow(0.20, 0.10); !errors.Is(err, ErrOverBudget) {
		t.Fatalf("tavanı aşan reddedilmeli: %v", err)
	}
	if err := (Guard{}).Allow(0, 0); !errors.Is(err, ErrOverBudget) {
		t.Fatal("tavan tanımsızsa hiçbir çağrıya izin verilmemeli")
	}
	if g.Remaining(1) != 0 || math.Abs(g.Remaining(0.1)-0.15) > 1e-9 {
		t.Fatal("kalan bütçe hatalı")
	}
}

func TestApprovalIsSingleUseAndExpires(t *testing.T) {
	a := NewApprovals()
	now := time.Now()
	a.now = func() time.Time { return now }
	ap := a.Grant(Estimate{Calls: 15}, 0.25, "b1")
	if _, err := a.Consume("yok"); !errors.Is(err, ErrNoApproval) {
		t.Fatal("olmayan onay reddedilmeli")
	}
	got, err := a.Consume(ap.ID)
	if err != nil || got.Estimate.Calls != 15 || got.CapUSD != 0.25 || got.Binding != "b1" {
		t.Fatalf("onay tüketilemedi: %+v %v", got, err)
	}
	if _, err := a.Consume(ap.ID); !errors.Is(err, ErrNoApproval) {
		t.Fatal("onay yalnızca bir kez kullanılabilmeli")
	}
	ap2 := a.Grant(Estimate{}, 1, "")
	a.now = func() time.Time { return now.Add(16 * time.Minute) }
	if _, err := a.Consume(ap2.ID); !errors.Is(err, ErrNoApproval) {
		t.Fatal("süresi dolan onay reddedilmeli")
	}
	if len(ap.ID) != 32 || ap.ID == ap2.ID {
		t.Fatal("kimlikler rastgele ve benzersiz olmalı")
	}
}
