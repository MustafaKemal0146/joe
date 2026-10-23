package llm

import "testing"

func TestModelMeta(t *testing.T) {
	cases := []struct {
		id     string
		ok     bool
		ctx    int
		reason bool
	}{
		{"claude-sonnet-4-5", true, 200_000, true},
		{"claude-opus-5-5-20260101", true, 200_000, true},
		{"gemini-2.5-flash", true, 1_048_576, true},
		{"openai/gpt-oss-120b", true, 131_072, true},
		{"gpt-4o-mini", true, 128_000, false},
		{"gpt-5-mini", true, 400_000, true},
		{"deepseek-flash", true, 1_000_000, true},
		{"deepseek-v4-pro", true, 1_000_000, true},
		{"deepseek-r1-distill-llama-70b", true, 131_072, true}, // Groq barındırmalı damıtma
		{"bilinmeyen-model-xyz", false, 0, false},
		{"", false, 0, false},
	}
	for _, c := range cases {
		info, ok := ModelMeta(c.id)
		if ok != c.ok {
			t.Errorf("ModelMeta(%q) ok=%v, istenen %v", c.id, ok, c.ok)
			continue
		}
		if !ok {
			continue
		}
		if info.ContextWindow != c.ctx {
			t.Errorf("ModelMeta(%q) ctx=%d, istenen %d", c.id, info.ContextWindow, c.ctx)
		}
		if info.Reasoning != c.reason {
			t.Errorf("ModelMeta(%q) reasoning=%v, istenen %v", c.id, info.Reasoning, c.reason)
		}
	}
}

func TestKnownModelsDeepSeek(t *testing.T) {
	ids := KnownModels("deepseek")
	if len(ids) != 2 || ids[0] != "deepseek-flash" || ids[1] != "deepseek-v4-pro" {
		t.Fatalf("deepseek bilinen modelleri beklenmedik: %v", ids)
	}
	if KnownModels("yok-saglayici") != nil {
		t.Fatal("bilinmeyen sağlayıcı nil dönmeli")
	}
}

func TestModelMetaSpecificityWins(t *testing.T) {
	// "claude-3-5-sonnet" daha özgün; genel "claude" yerine onu seçmeli (Reasoning false).
	info, ok := ModelMeta("claude-3-5-sonnet-20241022")
	if !ok || info.Reasoning {
		t.Fatalf("en özgün eşleşme kazanmalı: %+v ok=%v", info, ok)
	}
}
