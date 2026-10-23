package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Anthropic, Claude'un yerel Messages API'sini (POST {base}/messages, SSE akışlı) konuşan
// aktarımdır. OpenAI-uyumlu katmanın aksine genişletilmiş düşünme (extended/adaptive thinking),
// doğru sistem istemi ayrımı ve doğru tool şemasını destekler. BaseURL "https://api.anthropic.com/v1"
// biçiminde beklenir (config.go'daki "anthropic" sağlayıcı ön ayarıyla aynı).
type Anthropic struct {
	HTTP    *http.Client
	BaseURL string
	APIKey  string
}

// NewAnthropic makul varsayılanlarla bir aktarım üretir.
func NewAnthropic(baseURL, apiKey string) *Anthropic {
	return &Anthropic{
		HTTP:    &http.Client{Timeout: 0},
		BaseURL: strings.TrimRight(baseURL, "/"),
		APIKey:  apiKey,
	}
}

const anthropicVersion = "2023-06-01"
const anthropicDefaultMaxTokens = 4096

// anthropicThinkingBudgets, "low"/"medium"/"high" düşünme seviyelerini manuel (budget_tokens)
// modunda kullanılan token bütçelerine çevirir. Anthropic en az 1024 ister.
var anthropicThinkingBudgets = map[string]int{"low": 2000, "medium": 6000, "high": 16000}

// anthropicVersionNums bir model kimliğindeki ilk iki sayı grubunu (büyük.küçük sürüm) bulur.
// Hem eski adlandırmada (claude-3-7-sonnet, sürüm önce) hem yeni adlandırmada (claude-opus-4-1,
// katman önce) sürüm numaraları birbirini izleyen ilk iki sayı grubu olarak görünür.
var anthropicVersionNums = regexp.MustCompile(`\d+`)

// anthropicUsesAdaptive, modelin genişletilmiş (manuel budget_tokens) yerine uyarlanabilir
// (adaptive + output_config.effort) düşünmeyi mi kullandığını söyler. Claude 4.6 ve sonrası
// (5.x dahil) yalnızca adaptive'i destekler; 4.5 ve öncesi yalnızca manuel'i. Sürüm
// çözümlenemezse (bilinmeyen/özel bir model adı) güvenli tarafta adaptive varsayılır: yanlışsa
// sağlayıcı açık bir hata mesajıyla reddeder, manuel moddaki sessiz 400'den daha iyi teşhis edilir.
func anthropicUsesAdaptive(model string) bool {
	nums := anthropicVersionNums.FindAllString(strings.ToLower(model), 2)
	if len(nums) < 2 {
		return true
	}
	major, err1 := strconv.Atoi(nums[0])
	minor, err2 := strconv.Atoi(nums[1])
	if err1 != nil || err2 != nil {
		return true
	}
	if major > 4 {
		return true
	}
	return major == 4 && minor >= 6
}

// anthropicThinking, istek gövdesine eklenecek "thinking" (ve varsa "output_config") alanlarını
// ve bu düşünme bütçesinin gerektirdiği en az max_tokens değerini hesaplar. effort boşsa düşünme
// isteği yapılmaz (nil, 0).
func anthropicThinking(model, effort string) (thinking map[string]any, outputConfig map[string]any, minMaxTokens int) {
	if effort == "" {
		return nil, nil, 0
	}
	if anthropicUsesAdaptive(model) {
		return map[string]any{"type": "adaptive"}, map[string]any{"effort": effort}, 0
	}
	budget, ok := anthropicThinkingBudgets[effort]
	if !ok {
		budget = anthropicThinkingBudgets["medium"]
	}
	return map[string]any{"type": "enabled", "budget_tokens": budget}, nil, budget + 2048
}

func (a *Anthropic) buildBody(req Request) ([]byte, error) {
	var system strings.Builder
	var msgs []map[string]any

	i := 0
	for i < len(req.Messages) {
		m := req.Messages[i]
		switch m.Role {
		case RoleSystem:
			if system.Len() > 0 {
				system.WriteString("\n\n")
			}
			system.WriteString(m.Content)
			i++
		case RoleUser:
			msgs = append(msgs, map[string]any{"role": "user", "content": m.Content})
			i++
		case RoleAssistant:
			var blocks []map[string]any
			if m.Content != "" {
				blocks = append(blocks, map[string]any{"type": "text", "text": m.Content})
			}
			for _, tc := range m.ToolCalls {
				var input any
				if err := json.Unmarshal([]byte(tc.Arguments), &input); err != nil || input == nil {
					input = map[string]any{}
				}
				blocks = append(blocks, map[string]any{"type": "tool_use", "id": tc.ID, "name": tc.Name, "input": input})
			}
			if len(blocks) == 0 {
				// Anthropic boş içerikli mesajı reddeder; en azından boş bir metin bloğu koy.
				blocks = append(blocks, map[string]any{"type": "text", "text": ""})
			}
			msgs = append(msgs, map[string]any{"role": "assistant", "content": blocks})
			i++
		case RoleTool:
			// Anthropic'te araç sonuçları, aynı asistan turuna ait TEK bir "user" mesajında
			// tool_result bloklarının dizisi olarak gelir; ardışık RoleTool mesajları birleştirilir.
			var blocks []map[string]any
			for i < len(req.Messages) && req.Messages[i].Role == RoleTool {
				t := req.Messages[i]
				blocks = append(blocks, map[string]any{"type": "tool_result", "tool_use_id": t.ToolCallID, "content": t.Content})
				i++
			}
			msgs = append(msgs, map[string]any{"role": "user", "content": blocks})
		default:
			i++
		}
	}

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = anthropicDefaultMaxTokens
	}
	thinking, outputConfig, minMax := anthropicThinking(req.Model, req.ReasoningEffort)
	if minMax > maxTokens {
		maxTokens = minMax
	}

	body := map[string]any{"model": req.Model, "messages": msgs, "max_tokens": maxTokens, "stream": true}
	if system.Len() > 0 {
		// Sistem istemi tek metin bloğu olarak ve "ephemeral" önbellek işaretiyle gönderilir: aynı
		// oturumda turdan tura birebir aynı kaldığından sağlayıcı onu yeniden işlemez (maliyet/gecikme
		// düşer). Anthropic önbellekleme için content'in blok dizisi olmasını ister (düz metin olduğunda
		// cache_control eklenemez).
		body["system"] = []map[string]any{
			{"type": "text", "text": system.String(), "cache_control": anthropicEphemeralCache},
		}
	}
	if req.Temperature != nil {
		body["temperature"] = *req.Temperature
	}
	if thinking != nil {
		body["thinking"] = thinking
	}
	if outputConfig != nil {
		body["output_config"] = outputConfig
	}
	if len(req.Tools) > 0 {
		tools := make([]map[string]any, 0, len(req.Tools))
		for _, t := range req.Tools {
			schema := t.Parameters
			if len(schema) == 0 {
				schema = json.RawMessage(`{"type":"object","properties":{}}`)
			}
			tools = append(tools, map[string]any{"name": t.Name, "description": t.Description, "input_schema": json.RawMessage(schema)})
		}
		// Araç tanımları da turdan tura neredeyse hiç değişmez: SON araca önbellek işareti koymak,
		// Anthropic'in önbellekleme kuralı gereği tüm araç listesini (ondan önceki her şeyle birlikte)
		// tek bir önbellek bölgesi yapar.
		if len(tools) > 0 {
			tools[len(tools)-1]["cache_control"] = anthropicEphemeralCache
		}
		body["tools"] = tools
	}
	// Büyüyen konuşma geçmişini de önbellekle: SON mesajdan BİR ÖNCEKİ mesaja işaret koy. Bir ajan
	// turu sırasında (ve bir sonraki kullanıcı mesajında) geçmiş yalnızca sona ekleme yapılarak
	// büyüdüğünden, bu önbellek bölgesi art arda gelen isteklerde yeniden kullanılır. En az 2 mesaj
	// gerekir (tek mesajda önbelleklenecek bir "önceki" yok).
	if n := len(msgs); n >= 2 {
		addAnthropicCacheControl(msgs[n-2])
	}
	return json.Marshal(body)
}

// anthropicEphemeralCache, Anthropic'in "ephemeral" (TTL'li, varsayılan ~5dk) önbellek türüdür.
var anthropicEphemeralCache = map[string]any{"type": "ephemeral"}

// addAnthropicCacheControl bir mesajın content alanına (düz metin ya da blok dizisi olabilir)
// önbellek işareti ekler; düz metinse önce blok dizisine çevirir.
func addAnthropicCacheControl(m map[string]any) {
	switch c := m["content"].(type) {
	case string:
		m["content"] = []map[string]any{{"type": "text", "text": c, "cache_control": anthropicEphemeralCache}}
	case []map[string]any:
		if len(c) > 0 {
			c[len(c)-1]["cache_control"] = anthropicEphemeralCache
		}
	}
}

// anthropicEvent, native Messages API'nin SSE olaylarından ihtiyaç duyulan alanlardır.
type anthropicEvent struct {
	Type         string `json:"type"`
	Index        int    `json:"index"`
	ContentBlock *struct {
		Type string `json:"type"`
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"content_block"`
	Delta *struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		Thinking    string `json:"thinking"`
		PartialJSON string `json:"partial_json"`
		StopReason  string `json:"stop_reason"`
	} `json:"delta"`
	Usage *struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	Message *struct {
		Usage struct {
			InputTokens int `json:"input_tokens"`
		} `json:"usage"`
	} `json:"message"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

type anthropicBlockKind int

const (
	anthropicBlockText anthropicBlockKind = iota
	anthropicBlockThinking
	anthropicBlockToolUse
)

type anthropicBlock struct {
	kind anthropicBlockKind
	id   string
	name string
	args strings.Builder
}

// Stream, isteği gönderir ve akışı kanala yazar.
func (a *Anthropic) Stream(ctx context.Context, req Request) <-chan Chunk {
	out := make(chan Chunk, 16)
	go func() {
		defer close(out)
		send := func(c Chunk) bool {
			select {
			case out <- c:
				return true
			case <-ctx.Done():
				return false
			}
		}
		fail := func(err error) { send(Chunk{Kind: ChunkError, Err: err}) }

		payload, err := a.buildBody(req)
		if err != nil {
			fail(err)
			return
		}
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.BaseURL+"/messages", bytes.NewReader(payload))
		if err != nil {
			fail(err)
			return
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "text/event-stream")
		httpReq.Header.Set("anthropic-version", anthropicVersion)
		if a.APIKey != "" {
			httpReq.Header.Set("x-api-key", a.APIKey)
		}
		client := a.HTTP
		if client == nil {
			client = http.DefaultClient
		}
		resp, err := client.Do(httpReq)
		if err != nil {
			fail(err)
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
			fail(NewProviderError(resp.StatusCode, string(b)))
			return
		}

		var text strings.Builder
		blocks := map[int]*anthropicBlock{}
		var usage Usage
		finish := ""

		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
		for sc.Scan() {
			line := sc.Text()
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "" {
				continue
			}
			var ev anthropicEvent
			if json.Unmarshal([]byte(data), &ev) != nil {
				continue
			}
			switch ev.Type {
			case "error":
				msg := "anthropic hata"
				if ev.Error != nil {
					msg = ev.Error.Message
				}
				fail(NewProviderError(0, msg))
				return
			case "message_start":
				if ev.Message != nil {
					usage.PromptTokens = ev.Message.Usage.InputTokens
				}
			case "content_block_start":
				if ev.ContentBlock == nil {
					break
				}
				b := &anthropicBlock{}
				switch ev.ContentBlock.Type {
				case "thinking":
					b.kind = anthropicBlockThinking
				case "tool_use":
					b.kind = anthropicBlockToolUse
					b.id, b.name = ev.ContentBlock.ID, ev.ContentBlock.Name
				default:
					b.kind = anthropicBlockText
				}
				blocks[ev.Index] = b
			case "content_block_delta":
				if ev.Delta == nil {
					break
				}
				b := blocks[ev.Index]
				if b == nil {
					break
				}
				switch ev.Delta.Type {
				case "text_delta":
					if ev.Delta.Text != "" {
						text.WriteString(ev.Delta.Text)
						if !send(Chunk{Kind: ChunkText, Text: ev.Delta.Text}) {
							return
						}
					}
				case "thinking_delta":
					if ev.Delta.Thinking != "" {
						if !send(Chunk{Kind: ChunkReasoning, Text: ev.Delta.Thinking}) {
							return
						}
					}
				case "input_json_delta":
					b.args.WriteString(ev.Delta.PartialJSON)
				}
			case "message_delta":
				if ev.Delta != nil && ev.Delta.StopReason != "" {
					finish = ev.Delta.StopReason
				}
				if ev.Usage != nil {
					usage.CompletionTokens = ev.Usage.OutputTokens
				}
			case "message_stop":
				// turun sonu; döngü sc.Scan() ile zaten bitecek
			}
		}
		if err := sc.Err(); err != nil {
			if ctx.Err() != nil {
				return
			}
			fail(err)
			return
		}

		msg := Message{Role: RoleAssistant, Content: text.String()}
		idxs := make([]int, 0, len(blocks))
		for i := range blocks {
			idxs = append(idxs, i)
		}
		sort.Ints(idxs)
		for _, i := range idxs {
			b := blocks[i]
			if b.kind != anthropicBlockToolUse || b.name == "" {
				continue
			}
			args := b.args.String()
			if strings.TrimSpace(args) == "" {
				args = "{}"
			}
			id := b.id
			if id == "" {
				id = fmt.Sprintf("toolu_%d_%d", time.Now().UnixNano(), i)
			}
			msg.ToolCalls = append(msg.ToolCalls, ToolCall{ID: id, Name: b.name, Arguments: args})
		}
		send(Chunk{Kind: ChunkDone, Message: &msg, Usage: &usage, FinishReason: finish})
	}()
	return out
}
