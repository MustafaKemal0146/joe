package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAnthropicStreamsTextThinkingAndToolCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "k" {
			t.Errorf("x-api-key = %q", r.Header.Get("x-api-key"))
		}
		if r.Header.Get("anthropic-version") != anthropicVersion {
			t.Errorf("anthropic-version = %q", r.Header.Get("anthropic-version"))
		}
		var body map[string]any
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		if body["model"] != "claude-sonnet-4-5" {
			t.Errorf("model = %v", body["model"])
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, sse(
			`{"type":"message_start","message":{"usage":{"input_tokens":21}}}`,
			`{"type":"content_block_start","index":0,"content_block":{"type":"thinking"}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"düşünüyorum"}}`,
			`{"type":"content_block_stop","index":0}`,
			`{"type":"content_block_start","index":1,"content_block":{"type":"text"}}`,
			`{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"Mer"}}`,
			`{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"haba"}}`,
			`{"type":"content_block_stop","index":1}`,
			`{"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"toolu_1","name":"shell"}}`,
			`{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"command\":"}}`,
			`{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"\"ls\"}"}}`,
			`{"type":"content_block_stop","index":2}`,
			`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":9}}`,
			`{"type":"message_stop"}`,
		))
	}))
	defer srv.Close()

	tr := NewAnthropic(srv.URL, "k")
	text, reasoning, done, err := collect(t, tr.Stream(context.Background(), Request{
		Model: "claude-sonnet-4-5", Messages: []Message{{Role: RoleUser, Content: "selam"}},
	}))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if text != "Merhaba" {
		t.Fatalf("text = %q", text)
	}
	if reasoning != "düşünüyorum" {
		t.Fatalf("reasoning = %q", reasoning)
	}
	if done == nil || done.Message == nil {
		t.Fatal("done mesajı yok")
	}
	if len(done.Message.ToolCalls) != 1 {
		t.Fatalf("tool calls = %+v", done.Message.ToolCalls)
	}
	tc := done.Message.ToolCalls[0]
	if tc.ID != "toolu_1" || tc.Name != "shell" || tc.Arguments != `{"command":"ls"}` {
		t.Fatalf("tool call = %+v", tc)
	}
	if done.Usage == nil || done.Usage.PromptTokens != 21 || done.Usage.CompletionTokens != 9 {
		t.Fatalf("usage = %+v", done.Usage)
	}
	if done.FinishReason != "tool_use" {
		t.Fatalf("finish = %q", done.FinishReason)
	}
}

func TestAnthropicErrorEvent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, sse(`{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`))
	}))
	defer srv.Close()
	tr := NewAnthropic(srv.URL, "k")
	_, _, _, err := collect(t, tr.Stream(context.Background(), Request{Model: "m", Messages: []Message{{Role: RoleUser, Content: "x"}}}))
	if err == nil {
		t.Fatal("hata beklenirdi")
	}
}

func TestAnthropicHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		io.WriteString(w, `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`)
	}))
	defer srv.Close()
	tr := NewAnthropic(srv.URL, "yanlis")
	_, _, _, err := collect(t, tr.Stream(context.Background(), Request{Model: "m", Messages: []Message{{Role: RoleUser, Content: "x"}}}))
	pe, ok := err.(*ProviderError)
	if !ok || pe.StatusCode != 401 || !pe.IsUnauthorized() {
		t.Fatalf("err = %#v", err)
	}
}

func TestAnthropicBuildBodySystemAndToolRoundTrip(t *testing.T) {
	tr := NewAnthropic("https://api.anthropic.com/v1", "k")
	raw, err := tr.buildBody(Request{
		Model: "claude-sonnet-4-5",
		Messages: []Message{
			{Role: RoleSystem, Content: "sen bir asistansın"},
			{Role: RoleUser, Content: "dosyayı oku"},
			{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "t1", Name: "read_file", Arguments: `{"path":"a.txt"}`}}},
			{Role: RoleTool, ToolCallID: "t1", Content: "merhaba"},
			{Role: RoleAssistant, Content: "dosyada 'merhaba' yazıyor"},
		},
		Tools:     []ToolDef{{Name: "read_file", Description: "oku", Parameters: json.RawMessage(`{"type":"object"}`)}},
		MaxTokens: 500,
	})
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	sysBlocks, ok := body["system"].([]any)
	if !ok || len(sysBlocks) != 1 {
		t.Fatalf("system = %v", body["system"])
	}
	sysBlock := sysBlocks[0].(map[string]any)
	if sysBlock["text"] != "sen bir asistansın" || sysBlock["type"] != "text" {
		t.Fatalf("system bloğu = %v", sysBlock)
	}
	if cc, ok := sysBlock["cache_control"].(map[string]any); !ok || cc["type"] != "ephemeral" {
		t.Fatalf("sistem istemi önbelleklenmeli: %v", sysBlock)
	}
	msgs, ok := body["messages"].([]any)
	if !ok || len(msgs) != 4 { // user, assistant(tool_use), user(tool_result), assistant(text)
		t.Fatalf("messages = %v", body["messages"])
	}
	// İkinci mesaj: assistant + tool_use bloğu.
	m1 := msgs[1].(map[string]any)
	if m1["role"] != "assistant" {
		t.Fatalf("m1 role = %v", m1["role"])
	}
	blocks1 := m1["content"].([]any)
	tu := blocks1[0].(map[string]any)
	if tu["type"] != "tool_use" || tu["id"] != "t1" || tu["name"] != "read_file" {
		t.Fatalf("tool_use bloğu = %v", tu)
	}
	// Üçüncü mesaj: user + tool_result bloğu (ayrı RoleTool mesajı birleşmiş olmalı).
	m2 := msgs[2].(map[string]any)
	if m2["role"] != "user" {
		t.Fatalf("m2 role = %v", m2["role"])
	}
	blocks2 := m2["content"].([]any)
	tr2 := blocks2[0].(map[string]any)
	if tr2["type"] != "tool_result" || tr2["tool_use_id"] != "t1" || tr2["content"] != "merhaba" {
		t.Fatalf("tool_result bloğu = %v", tr2)
	}
	// Araç tanımı Anthropic biçiminde (input_schema, "function" sarmalayıcısı yok).
	tools := body["tools"].([]any)
	tool0 := tools[0].(map[string]any)
	if tool0["name"] != "read_file" || tool0["input_schema"] == nil {
		t.Fatalf("tool = %v", tool0)
	}
	if _, bad := tool0["function"]; bad {
		t.Fatalf("OpenAI tarzı 'function' sarmalayıcısı olmamalı: %v", tool0)
	}
	if cc, ok := tool0["cache_control"].(map[string]any); !ok || cc["type"] != "ephemeral" {
		t.Fatalf("tek araç listedeki SON araç olduğundan önbelleklenmeli: %v", tool0)
	}
	// Son mesajdan BİR ÖNCEKİ mesaj (msgs[2], tool_result) önbelleklenmeli; SON mesaj (msgs[3]) değil.
	if cc, ok := tr2["cache_control"].(map[string]any); !ok || cc["type"] != "ephemeral" {
		t.Fatalf("sondan bir önceki mesaj önbelleklenmeli: %v", tr2)
	}
	lastBlocks := msgs[3].(map[string]any)["content"].([]any)
	lastBlock := lastBlocks[0].(map[string]any)
	if _, has := lastBlock["cache_control"]; has {
		t.Fatalf("SON mesaj önbelleklenmemeli (her turda değişir): %v", lastBlock)
	}
}

func TestAnthropicBuildBodySingleMessageNoCacheControl(t *testing.T) {
	// Tek mesajda (geçmiş yok) önbelleklenecek bir "önceki" mesaj olmadığından hiçbir mesaj
	// işaretlenmemeli (ama sistem/araç yine de önbelleklenir, onlar ayrı bölgeler).
	tr := NewAnthropic("https://api.anthropic.com/v1", "k")
	raw, err := tr.buildBody(Request{
		Model:    "claude-sonnet-4-5",
		Messages: []Message{{Role: RoleUser, Content: "merhaba"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	msgs := body["messages"].([]any)
	if len(msgs) != 1 {
		t.Fatalf("messages = %v", msgs)
	}
	// content düz string kalmalı (blok dizisine çevrilmemeli, önbellek işareti eklenmedi).
	if _, isString := msgs[0].(map[string]any)["content"].(string); !isString {
		t.Fatalf("tek mesajda content düz metin kalmalı: %v", msgs[0])
	}
}

func TestAnthropicUsesAdaptive(t *testing.T) {
	cases := []struct {
		model string
		want  bool
	}{
		{"claude-3-5-sonnet-20241022", false},
		{"claude-3-7-sonnet-latest", false},
		{"claude-opus-4-1", false},
		{"claude-sonnet-4-5", false},
		{"claude-sonnet-4-6", true},
		{"claude-opus-4-6", true},
		{"claude-opus-5-5", true},
		{"claude-sonnet-5-5", true},
		{"claude-haiku-5-5", true},
		{"claude-fable-5-1", true},
		{"bilinmeyen-model", true}, // çözümlenemeyen: güvenli tarafta adaptive
	}
	for _, c := range cases {
		if got := anthropicUsesAdaptive(c.model); got != c.want {
			t.Errorf("anthropicUsesAdaptive(%q) = %v, istenen %v", c.model, got, c.want)
		}
	}
}

func TestAnthropicThinkingShapes(t *testing.T) {
	// Eski (manuel) model: thinking.type=enabled + budget_tokens; max_tokens bütçeyi geçecek şekilde büyür.
	thinking, output, minMax := anthropicThinking("claude-sonnet-4-5", "high")
	if thinking["type"] != "enabled" || thinking["budget_tokens"] != 16000 || output != nil || minMax != 16000+2048 {
		t.Fatalf("manuel: thinking=%v output=%v minMax=%d", thinking, output, minMax)
	}
	// Yeni (adaptive) model: thinking.type=adaptive + output_config.effort; max_tokens'a dokunmaz.
	thinking2, output2, minMax2 := anthropicThinking("claude-sonnet-4-6", "low")
	if thinking2["type"] != "adaptive" || output2["effort"] != "low" || minMax2 != 0 {
		t.Fatalf("adaptive: thinking=%v output=%v minMax=%d", thinking2, output2, minMax2)
	}
	// Boş seviye: hiçbir alan eklenmez.
	thinking3, output3, minMax3 := anthropicThinking("claude-sonnet-4-5", "")
	if thinking3 != nil || output3 != nil || minMax3 != 0 {
		t.Fatalf("boş seviye hiçbir şey eklememeli: thinking=%v output=%v minMax=%d", thinking3, output3, minMax3)
	}
}

func TestAnthropicBuildBodyIncludesThinking(t *testing.T) {
	tr := NewAnthropic("https://api.anthropic.com/v1", "k")
	raw, err := tr.buildBody(Request{
		Model: "claude-sonnet-4-5", Messages: []Message{{Role: RoleUser, Content: "x"}},
		MaxTokens: 100, ReasoningEffort: "high",
	})
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	thinking, ok := body["thinking"].(map[string]any)
	if !ok || thinking["type"] != "enabled" {
		t.Fatalf("thinking = %v", body["thinking"])
	}
	// max_tokens, bütçeyi karşılayacak şekilde 100'den büyütülmüş olmalı.
	if mt, ok := body["max_tokens"].(float64); !ok || mt <= 100 {
		t.Fatalf("max_tokens = %v", body["max_tokens"])
	}
}
