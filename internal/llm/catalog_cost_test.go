package llm

import "testing"

func TestEstimateCost(t *testing.T) {
	usd, known := EstimateCost("openai/gpt-oss-120b", 1_000_000, 1_000_000)
	if !known {
		t.Fatal("gpt-oss-120b fiyatı bilinmeli")
	}
	if usd != 0.15+0.6 {
		t.Fatalf("usd = %v, istenen %v", usd, 0.15+0.6)
	}

	// Bilinmeyen bir modelde known=false ve 0 dönmeli (tahmin yürütülmemeli).
	usd2, known2 := EstimateCost("bilinmeyen-model-xyz", 1000, 1000)
	if known2 || usd2 != 0 {
		t.Fatalf("bilinmeyen model: usd=%v known=%v", usd2, known2)
	}

	// Sıfır token -> sıfır maliyet, ama yine de "biliniyor".
	usd3, known3 := EstimateCost("claude-sonnet-4-5", 0, 0)
	if !known3 || usd3 != 0 {
		t.Fatalf("usd3=%v known3=%v", usd3, known3)
	}
}
