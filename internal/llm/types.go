// Package llm sağlayıcıdan bağımsız mesaj tiplerini ve model aktarımlarını (tel biçimlerini) içerir.
package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Role bir mesajın sahibidir.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// ToolCall modelin istediği bir araç çağrısıdır. Arguments ham JSON metnidir.
type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Message sağlayıcıdan bağımsız tek bir konuşma mesajıdır.
type Message struct {
	Role       Role       `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ToolDef modele bildirilen bir aracın tanımıdır. Parameters bir JSON Schema'dır.
type ToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// Usage sağlayıcının bildirdiği token kullanımıdır.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

// Total toplam token sayısını döndürür.
func (u Usage) Total() int { return u.PromptTokens + u.CompletionTokens }

// Add iki kullanımı toplar.
func (u Usage) Add(o Usage) Usage {
	return Usage{u.PromptTokens + o.PromptTokens, u.CompletionTokens + o.CompletionTokens}
}

// Request bir sohbet isteğidir.
type Request struct {
	Model       string
	Messages    []Message
	Tools       []ToolDef
	MaxTokens   int
	Temperature *float64
	// ReasoningEffort modelin "düşünme" seviyesidir: "" (kapalı/varsayılan), "low", "medium", "high".
	// Her aktarım kendi sağlayıcısının gerçek alanına çevirir (ör. Groq/DeepSeek/OpenAI için
	// reasoning_effort, Anthropic için thinking nesnesi); desteklemeyen sağlayıcılar yok sayar.
	ReasoningEffort string
}

// ChunkKind akış parçasının türüdür.
type ChunkKind int

const (
	// ChunkText cevap metninin bir parçasıdır.
	ChunkText ChunkKind = iota
	// ChunkReasoning modelin düşünce parçasıdır (destekleyen sağlayıcılarda).
	ChunkReasoning
	// ChunkDone akışın bittiğini ve birleştirilmiş mesajı bildirir.
	ChunkDone
	// ChunkError akışın hatayla bittiğini bildirir.
	ChunkError
	// ChunkFallback, birincil sağlayıcı başarısız olduğu için ikincil bir sağlayıcıya
	// geçildiğini bildirir (bkz. Fallback transport). Text, geçilen sağlayıcının kısa açıklamasıdır.
	ChunkFallback
)

// Chunk akış sırasında gelen bir parçadır.
type Chunk struct {
	Kind         ChunkKind
	Text         string
	Message      *Message // ChunkDone: birleştirilmiş asistan mesajı
	Usage        *Usage   // ChunkDone: varsa kullanım
	FinishReason string   // ChunkDone
	Err          error    // ChunkError
}

// Transport bir sağlayıcı protokolünü konuşan aktarımdır. Kanal her zaman kapatılır;
// başarıda son parça ChunkDone, hatada ChunkError'dır.
type Transport interface {
	Stream(ctx context.Context, req Request) <-chan Chunk
}

// ProviderError sağlayıcı hata döndürdü (HTTP durum kodu ve gövde ile).
type ProviderError struct {
	StatusCode int
	Body       string
	// Limit ve Requested "Limit 8000, Requested 16079" gibi gövdelerden okunur; yoksa 0.
	Limit     int
	Requested int
}

var limitRx = regexp.MustCompile(`(?i)Limit\s*[:=]?\s*([\d,]+)\s*,\s*Requested\s*[:=]?\s*([\d,]+)`)

// NewProviderError gövdeyi ayrıştırarak bir ProviderError üretir.
func NewProviderError(status int, body string) *ProviderError {
	e := &ProviderError{StatusCode: status, Body: body}
	if m := limitRx.FindStringSubmatch(body); m != nil {
		e.Limit, _ = strconv.Atoi(strings.ReplaceAll(m[1], ",", ""))
		e.Requested, _ = strconv.Atoi(strings.ReplaceAll(m[2], ",", ""))
	}
	return e
}

// IsRequestTooLarge istek, modelin tek seferde kabul ettiği sınırı aşıyorsa true döner.
func (e *ProviderError) IsRequestTooLarge() bool {
	return e.StatusCode == 413 || (e.Limit > 0 && e.Requested > e.Limit)
}

// IsRateLimited sağlayıcı hız sınırı koyduysa true döner.
func (e *ProviderError) IsRateLimited() bool { return e.StatusCode == 429 }

// unauthorizedHints gövdede aranan, "anahtar geçersiz/eksik" anlamına gelen ifadelerdir.
// Çoğu sağlayıcı 401/403 döner ama Google'ın Gemini uç noktası geçersiz anahtarda bile 400
// (INVALID_ARGUMENT) döner; bu yüzden durum koduna ek olarak gövde de denetlenir.
var unauthorizedHints = []string{
	"api key not valid", "api_key_invalid", "invalid api key", "invalid_api_key",
	"invalid x-api-key", "invalid bearer token", "authentication_error", "authentication fails",
	"missing bearer authentication", "incorrect api key",
}

// IsUnauthorized anahtar geçersiz ya da eksik olduğu için reddedildiyse true döner. Çoğu sağlayıcı
// bunu 401/403 ile bildirir; Gemini gibi bazıları 400 ile ve gövdede bir ipucuyla bildirir.
func (e *ProviderError) IsUnauthorized() bool {
	if e.StatusCode == 401 || e.StatusCode == 403 {
		return true
	}
	if e.StatusCode == 400 {
		low := strings.ToLower(e.Body)
		for _, h := range unauthorizedHints {
			if strings.Contains(low, h) {
				return true
			}
		}
	}
	return false
}

func (e *ProviderError) Error() string {
	flat := strings.Join(strings.Fields(e.Body), " ")
	if len(flat) > 300 {
		flat = flat[:300] + "…"
	}
	if e.StatusCode > 0 {
		return fmt.Sprintf("HTTP %d: %s", e.StatusCode, flat)
	}
	return flat
}
