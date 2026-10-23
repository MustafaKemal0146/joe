package llm

import "strings"

// Model kataloğu: bilinen modeller için bağlam penceresi ve yetenek meta verisi. Amaç, arayüzün
// bağlam sınırını göstermesi ve 413 (istek çok büyük) hatalarını önceden kestirebilmesidir.
// Tam liste tutmak yerine model kimliğinde geçen ipuçlarıyla (substring) eşleştirilir; böylece
// sürüm ekleri (-20241022 gibi) değişse de eşleşme bozulmaz.

// ModelInfo bir modelin meta verisidir.
type ModelInfo struct {
	// ContextWindow modelin toplam bağlam penceresi (token). 0 ise bilinmiyor.
	ContextWindow int `json:"context_window"`
	// MaxOutput önerilen azami çıktı token'ı. 0 ise bilinmiyor.
	MaxOutput int `json:"max_output"`
	// Reasoning model düşünme/akıl yürütme çıktısı üretebiliyor mu?
	Reasoning bool `json:"reasoning"`
	// CostPer1MIn/CostPer1MOut, milyon token başına YAKLAŞIK USD fiyatıdır (girdi/çıktı).
	// Fiyatlar sık değişir ve kaynaklar arasında farklılık gösterebilir; 0 ise bilinmiyor
	// demektir (arayüz bu durumda hiçbir rakam göstermez, tahmin yürütmez).
	CostPer1MIn  float64 `json:"cost_per_1m_in,omitempty"`
	CostPer1MOut float64 `json:"cost_per_1m_out,omitempty"`
}

type catalogEntry struct {
	hint string // model kimliğinde aranan ipucu (küçük harf)
	info ModelInfo
}

// catalog, ipucu uzunluğuna göre (uzun önce) sıralı tutulur ki en özgün eşleşme kazansın.
var catalog = []catalogEntry{
	// Anthropic Claude. Fiyatlar (milyon token, USD) yaklaşıktır ve sağlayıcının resmî fiyat
	// sayfasıyla sık doğrulanmalıdır; katman bazlı (Haiku/Sonnet/Opus) kullanılır.
	{"claude-opus-5", ModelInfo{ContextWindow: 200_000, MaxOutput: 32_000, Reasoning: true, CostPer1MIn: 5, CostPer1MOut: 25}},
	{"claude-sonnet-5", ModelInfo{ContextWindow: 200_000, MaxOutput: 64_000, Reasoning: true, CostPer1MIn: 3, CostPer1MOut: 15}},
	{"claude-haiku-5", ModelInfo{ContextWindow: 200_000, MaxOutput: 32_000, Reasoning: true, CostPer1MIn: 1, CostPer1MOut: 5}},
	{"claude-opus-4", ModelInfo{ContextWindow: 200_000, MaxOutput: 32_000, Reasoning: true, CostPer1MIn: 5, CostPer1MOut: 25}},
	{"claude-sonnet-4", ModelInfo{ContextWindow: 200_000, MaxOutput: 64_000, Reasoning: true, CostPer1MIn: 3, CostPer1MOut: 15}},
	{"claude-3-7-sonnet", ModelInfo{ContextWindow: 200_000, MaxOutput: 64_000, Reasoning: true, CostPer1MIn: 3, CostPer1MOut: 15}},
	{"claude-3-5-sonnet", ModelInfo{ContextWindow: 200_000, MaxOutput: 8_192, CostPer1MIn: 3, CostPer1MOut: 15}},
	{"claude-3-5-haiku", ModelInfo{ContextWindow: 200_000, MaxOutput: 8_192, CostPer1MIn: 1, CostPer1MOut: 5}},
	{"claude", ModelInfo{ContextWindow: 200_000, MaxOutput: 8_192}},
	// Google Gemini
	{"gemini-2.5-pro", ModelInfo{ContextWindow: 1_048_576, MaxOutput: 65_536, Reasoning: true}},
	{"gemini-2.5-flash", ModelInfo{ContextWindow: 1_048_576, MaxOutput: 65_536, Reasoning: true}},
	{"gemini-2.0-flash", ModelInfo{ContextWindow: 1_048_576, MaxOutput: 8_192}},
	{"gemini-1.5-pro", ModelInfo{ContextWindow: 2_097_152, MaxOutput: 8_192}},
	{"gemini", ModelInfo{ContextWindow: 1_048_576, MaxOutput: 8_192}},
	// OpenAI
	{"gpt-5", ModelInfo{ContextWindow: 400_000, MaxOutput: 128_000, Reasoning: true}},
	{"gpt-4.1", ModelInfo{ContextWindow: 1_047_576, MaxOutput: 32_768}},
	{"gpt-4o", ModelInfo{ContextWindow: 128_000, MaxOutput: 16_384}},
	{"o3", ModelInfo{ContextWindow: 200_000, MaxOutput: 100_000, Reasoning: true}},
	{"o4-mini", ModelInfo{ContextWindow: 200_000, MaxOutput: 100_000, Reasoning: true}},
	// Groq / açık modeller
	{"gpt-oss-120b", ModelInfo{ContextWindow: 131_072, MaxOutput: 32_768, Reasoning: true, CostPer1MIn: 0.15, CostPer1MOut: 0.6}},
	{"gpt-oss-20b", ModelInfo{ContextWindow: 131_072, MaxOutput: 32_768, Reasoning: true, CostPer1MIn: 0.075, CostPer1MOut: 0.3}},
	{"llama-4", ModelInfo{ContextWindow: 131_072, MaxOutput: 8_192}},
	{"llama-3.3-70b", ModelInfo{ContextWindow: 131_072, MaxOutput: 32_768}},
	{"qwen", ModelInfo{ContextWindow: 131_072, MaxOutput: 32_768, Reasoning: true}},
	// DeepSeek'in doğrudan API'si (api.deepseek.com): deepseek-flash ve deepseek-v4-pro.
	// Doğrulanmış kaynak: https://api-docs.deepseek.com/quick_start/pricing ve
	// https://api-docs.deepseek.com/api/create-chat-completion (max_tokens üst sınırı 384K/393216).
	// Fiyatlar (girdi önbellek-ıskası, çıktı) "peak" (üst) saat dilimine göre — bütçe takibinde
	// olduğundan düşük göstermemek için tepe fiyat kullanılır; off-peak'te gerçek maliyet yarısı.
	{"deepseek-flash", ModelInfo{ContextWindow: 1_000_000, MaxOutput: 393_216, Reasoning: true, CostPer1MIn: 0.3, CostPer1MOut: 1.2}},
	{"deepseek-v4-pro", ModelInfo{ContextWindow: 1_000_000, MaxOutput: 393_216, Reasoning: true, CostPer1MIn: 1.32, CostPer1MOut: 3.96}},
	// Groq üzerinden barındırılan DeepSeek damıtmaları (ör. deepseek-r1-distill-llama-70b).
	{"deepseek-r1", ModelInfo{ContextWindow: 131_072, MaxOutput: 32_768, Reasoning: true}},
	{"deepseek", ModelInfo{ContextWindow: 131_072, MaxOutput: 8_192}},
	{"mistral", ModelInfo{ContextWindow: 32_768, MaxOutput: 8_192}},
}

// knownModels sağlayıcı kimliğine göre bilinen güncel model kimlikleridir. Canlı /models ucu
// başarısız olursa (ör. ağ/başlık sorunu) ya da güncel modelleri öne çıkarmak için kullanılır.
// Canlı liste geldiğinde bunlarla birleştirilir; böylece hem güncel hem tam liste görünür.
var knownModels = map[string][]string{
	"anthropic": {
		"claude-opus-4-1", "claude-sonnet-4-5", "claude-sonnet-4-0",
		"claude-3-7-sonnet-latest", "claude-3-5-sonnet-latest", "claude-3-5-haiku-latest",
	},
	"gemini": {
		"gemini-2.5-pro", "gemini-2.5-flash", "gemini-2.5-flash-lite", "gemini-2.0-flash",
	},
	"openai": {
		"gpt-5", "gpt-5-mini", "gpt-4.1", "gpt-4.1-mini", "gpt-4o", "gpt-4o-mini", "o3", "o4-mini",
	},
	"groq": {
		"openai/gpt-oss-120b", "openai/gpt-oss-20b", "llama-3.3-70b-versatile",
		"deepseek-r1-distill-llama-70b", "qwen-2.5-32b",
	},
	"deepseek": {"deepseek-flash", "deepseek-v4-pro"},
}

// KnownModels bir sağlayıcı için bilinen güncel model kimliklerini döndürür (yoksa nil).
func KnownModels(providerID string) []string {
	ids := knownModels[providerID]
	if len(ids) == 0 {
		return nil
	}
	return append([]string(nil), ids...)
}

// ModelMeta bir model kimliği için bilinen meta veriyi döndürür. En özgün (en uzun) ipucu kazanır.
func ModelMeta(id string) (ModelInfo, bool) {
	l := strings.ToLower(strings.TrimSpace(id))
	if l == "" {
		return ModelInfo{}, false
	}
	// Sağlayıcı önekini at (ör. "openai/gpt-oss-120b" -> "gpt-oss-120b" de denenir).
	candidates := []string{l}
	if i := strings.LastIndex(l, "/"); i >= 0 && i+1 < len(l) {
		candidates = append(candidates, l[i+1:])
	}
	best := ModelInfo{}
	bestLen := -1
	for _, e := range catalog {
		for _, c := range candidates {
			if strings.Contains(c, e.hint) && len(e.hint) > bestLen {
				best = e.info
				bestLen = len(e.hint)
			}
		}
	}
	if bestLen < 0 {
		return ModelInfo{}, false
	}
	return best, true
}

// EstimateCost bir modelin bilinen fiyatına göre yaklaşık USD maliyetini hesaplar. Fiyat
// bilinmiyorsa (CostPer1MIn/Out ikisi de 0) known=false döner; arayüz bu durumda rakam
// göstermemeli (tahmin yürütmek yerine).
func EstimateCost(model string, promptTokens, completionTokens int) (usd float64, known bool) {
	info, ok := ModelMeta(model)
	if !ok || (info.CostPer1MIn == 0 && info.CostPer1MOut == 0) {
		return 0, false
	}
	usd = float64(promptTokens)/1_000_000*info.CostPer1MIn + float64(completionTokens)/1_000_000*info.CostPer1MOut
	return usd, true
}
