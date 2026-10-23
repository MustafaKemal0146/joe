package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// OpenAI, OpenAI uyumlu /chat/completions uç noktasını (OpenAI, Groq, OpenRouter, Ollama,
// LM Studio, vLLM...) SSE akışıyla konuşan aktarımdır.
type OpenAI struct {
	HTTP    *http.Client
	BaseURL string // örn. https://api.groq.com/openai/v1
	APIKey  string
	// IncludeUsage stream_options.include_usage göndermeyi açar (çoğu sağlayıcı destekler).
	IncludeUsage bool
}

// NewOpenAI makul varsayılanlarla bir aktarım üretir.
func NewOpenAI(baseURL, apiKey string) *OpenAI {
	return &OpenAI{
		HTTP:         &http.Client{Timeout: 0}, // akış uzun sürebilir; iptal ctx ile yapılır
		BaseURL:      strings.TrimRight(baseURL, "/"),
		APIKey:       apiKey,
		IncludeUsage: true,
	}
}

type wireToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

func (o *OpenAI) buildBody(req Request) ([]byte, error) {
	msgs := make([]map[string]any, 0, len(req.Messages))
	for _, m := range req.Messages {
		wm := map[string]any{"role": string(m.Role)}
		switch m.Role {
		case RoleAssistant:
			if m.Content != "" {
				wm["content"] = m.Content
			} else {
				wm["content"] = nil
			}
			if len(m.ToolCalls) > 0 {
				tcs := make([]wireToolCall, 0, len(m.ToolCalls))
				for _, c := range m.ToolCalls {
					var w wireToolCall
					w.ID, w.Type = c.ID, "function"
					w.Function.Name, w.Function.Arguments = c.Name, c.Arguments
					tcs = append(tcs, w)
				}
				wm["tool_calls"] = tcs
			}
		case RoleTool:
			wm["content"] = m.Content
			wm["tool_call_id"] = m.ToolCallID
		default:
			wm["content"] = m.Content
		}
		msgs = append(msgs, wm)
	}
	body := map[string]any{"model": req.Model, "messages": msgs, "stream": true}
	if o.IncludeUsage {
		body["stream_options"] = map[string]any{"include_usage": true}
	}
	if len(req.Tools) > 0 {
		tools := make([]map[string]any, 0, len(req.Tools))
		for _, t := range req.Tools {
			params := t.Parameters
			if len(params) == 0 {
				params = json.RawMessage(`{"type":"object","properties":{}}`)
			}
			tools = append(tools, map[string]any{
				"type": "function",
				"function": map[string]any{
					"name": t.Name, "description": t.Description, "parameters": params,
				},
			})
		}
		body["tools"] = tools
	}
	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	}
	if req.Temperature != nil {
		body["temperature"] = *req.Temperature
	}
	// Düşünme seviyesi: yalnızca canlı olarak doğrulanmış sağlayıcılarda gönderilir (bkz.
	// ProviderKind). Doğrulanmamış bir sağlayıcıya (Gemini uyumluluk katmanı, OpenRouter, yerel
	// sunucular...) yanlış biçimli bir alan göndermektense hiç göndermemek tercih edilir.
	switch ProviderKind(o.BaseURL) {
	case "deepseek":
		// DeepSeek düşünmeyi varsayılan açık tutar; kullanıcı kapattıysa açıkça "disabled" gönderilir,
		// açtıysa hem "thinking" nesnesi hem reasoning_effort (resmî örnekler ikisini birlikte kullanır).
		if req.ReasoningEffort == "" {
			body["thinking"] = map[string]any{"type": "disabled"}
		} else {
			body["thinking"] = map[string]any{"type": "enabled"}
			body["reasoning_effort"] = req.ReasoningEffort
		}
	case "groq", "openai":
		if req.ReasoningEffort != "" {
			body["reasoning_effort"] = req.ReasoningEffort
		}
	}
	return json.Marshal(body)
}

// ProviderKind, adresten sağlayıcıyı tanır (düşünme alanı gibi sağlayıcıya özgü davranışlar için).
// Bilinmeyen/özel adresler için boş döner: o durumda sağlayıcıya özgü hiçbir alan eklenmez.
func ProviderKind(baseURL string) string {
	switch {
	case strings.Contains(baseURL, "api.anthropic.com"):
		return "anthropic"
	case strings.Contains(baseURL, "api.deepseek.com"):
		return "deepseek"
	case strings.Contains(baseURL, "generativelanguage.googleapis.com"):
		return "gemini"
	case strings.Contains(baseURL, "api.groq.com"):
		return "groq"
	case strings.Contains(baseURL, "api.openai.com"):
		return "openai"
	default:
		return ""
	}
}

type sseChunk struct {
	Choices []struct {
		Delta struct {
			Content          *string `json:"content"`
			ReasoningContent *string `json:"reasoning_content"`
			Reasoning        *string `json:"reasoning"`
			ToolCalls        []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	Error json.RawMessage `json:"error"`
}

type callBuilder struct {
	id   string
	name string
	args strings.Builder
}

// Stream, isteği gönderir ve akışı kanala yazar.
func (o *OpenAI) Stream(ctx context.Context, req Request) <-chan Chunk {
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

		payload, err := o.buildBody(req)
		if err != nil {
			fail(err)
			return
		}
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, o.BaseURL+"/chat/completions", bytes.NewReader(payload))
		if err != nil {
			fail(err)
			return
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "text/event-stream")
		if o.APIKey != "" {
			httpReq.Header.Set("Authorization", "Bearer "+o.APIKey)
		}
		client := o.HTTP
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
		calls := map[int]*callBuilder{}
		var usage *Usage
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
			if data == "[DONE]" {
				break
			}
			var ch sseChunk
			if json.Unmarshal([]byte(data), &ch) != nil {
				continue
			}
			if len(ch.Error) > 0 && string(ch.Error) != "null" {
				fail(NewProviderError(0, string(ch.Error)))
				return
			}
			if ch.Usage != nil {
				usage = &Usage{ch.Usage.PromptTokens, ch.Usage.CompletionTokens}
			}
			if len(ch.Choices) == 0 {
				continue
			}
			c := ch.Choices[0]
			if c.FinishReason != nil && *c.FinishReason != "" {
				finish = *c.FinishReason
			}
			if c.Delta.Content != nil && *c.Delta.Content != "" {
				text.WriteString(*c.Delta.Content)
				if !send(Chunk{Kind: ChunkText, Text: *c.Delta.Content}) {
					return
				}
			}
			r := c.Delta.ReasoningContent
			if r == nil {
				r = c.Delta.Reasoning
			}
			if r != nil && *r != "" {
				if !send(Chunk{Kind: ChunkReasoning, Text: *r}) {
					return
				}
			}
			for _, tc := range c.Delta.ToolCalls {
				idx := tc.Index
				b := calls[idx]
				// Bazı sağlayıcılar index'i hep 0 gönderir; yeni bir id gelirse yeni çağrıdır.
				if b != nil && tc.ID != "" && b.id != "" && tc.ID != b.id {
					idx = len(calls)
					for calls[idx] != nil {
						idx++
					}
					b = nil
				}
				if b == nil {
					b = &callBuilder{}
					calls[idx] = b
				}
				if tc.ID != "" {
					b.id = tc.ID
				}
				if tc.Function.Name != "" {
					b.name = tc.Function.Name
				}
				b.args.WriteString(tc.Function.Arguments)
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
		idxs := make([]int, 0, len(calls))
		for i := range calls {
			idxs = append(idxs, i)
		}
		sort.Ints(idxs)
		for _, i := range idxs {
			b := calls[i]
			if b.name == "" {
				continue
			}
			id := b.id
			if id == "" {
				id = fmt.Sprintf("call_%d_%d", time.Now().UnixNano(), i)
			}
			args := b.args.String()
			if strings.TrimSpace(args) == "" {
				args = "{}"
			}
			msg.ToolCalls = append(msg.ToolCalls, ToolCall{ID: id, Name: b.name, Arguments: args})
		}
		send(Chunk{Kind: ChunkDone, Message: &msg, Usage: usage, FinishReason: finish})
	}()
	return out
}
