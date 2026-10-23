package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func sse(lines ...string) string {
	var b strings.Builder
	for _, l := range lines {
		b.WriteString("data: " + l + "\n\n")
	}
	return b.String()
}

func collect(t *testing.T, ch <-chan Chunk) (text string, reasoning string, done *Chunk, err error) {
	t.Helper()
	for c := range ch {
		switch c.Kind {
		case ChunkText:
			text += c.Text
		case ChunkReasoning:
			reasoning += c.Text
		case ChunkDone:
			cc := c
			done = &cc
		case ChunkError:
			err = c.Err
		}
	}
	return
}

func TestStreamsTextAndUsage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer k" {
			t.Errorf("auth header = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, sse(
			`{"choices":[{"delta":{"reasoning":"düşünüyor"}}]}`,
			`{"choices":[{"delta":{"content":"Mer"}}]}`,
			`{"choices":[{"delta":{"content":"haba"},"finish_reason":"stop"}]}`,
			`{"choices":[],"usage":{"prompt_tokens":11,"completion_tokens":3}}`,
			`[DONE]`,
		))
	}))
	defer srv.Close()

	tr := NewOpenAI(srv.URL+"/v1/", "k")
	tr.BaseURL = srv.URL // sondaki eğik çizgiyi kırpma davranışından bağımsız
	text, reasoning, done, err := collect(t, tr.Stream(context.Background(), Request{
		Model: "m", Messages: []Message{{Role: RoleUser, Content: "selam"}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if text != "Merhaba" || reasoning != "düşünüyor" {
		t.Fatalf("text=%q reasoning=%q", text, reasoning)
	}
	if done == nil || done.Message.Content != "Merhaba" || done.FinishReason != "stop" {
		t.Fatalf("done = %+v", done)
	}
	if done.Usage == nil || done.Usage.Total() != 14 {
		t.Fatalf("usage = %+v", done.Usage)
	}
}

func TestAccumulatesToolCallsAcrossChunks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["stream"] != true {
			t.Errorf("stream flag missing: %v", body["stream"])
		}
		if _, ok := body["tools"]; !ok {
			t.Errorf("tools missing")
		}
		io.WriteString(w, sse(
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"read_file","arguments":"{\"pa"}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"th\":\"a.txt\"}"}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":1,"id":"c2","function":{"name":"list_dir","arguments":""}}]},"finish_reason":"tool_calls"}]}`,
			`[DONE]`,
		))
	}))
	defer srv.Close()

	tr := NewOpenAI(srv.URL, "")
	_, _, done, err := collect(t, tr.Stream(context.Background(), Request{
		Model:    "m",
		Messages: []Message{{Role: RoleUser, Content: "x"}},
		Tools:    []ToolDef{{Name: "read_file", Description: "d"}},
	}))
	if err != nil || done == nil {
		t.Fatalf("err=%v done=%v", err, done)
	}
	calls := done.Message.ToolCalls
	if len(calls) != 2 {
		t.Fatalf("calls = %+v", calls)
	}
	if calls[0].ID != "c1" || calls[0].Name != "read_file" || calls[0].Arguments != `{"path":"a.txt"}` {
		t.Fatalf("call0 = %+v", calls[0])
	}
	if calls[1].Name != "list_dir" || calls[1].Arguments != "{}" {
		t.Fatalf("call1 = %+v (boş argüman {} olmalı)", calls[1])
	}
}

func TestNewIDWithSameIndexStartsNewCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, sse(
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"a","function":{"name":"t1","arguments":"{}"}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"b","function":{"name":"t2","arguments":"{}"}}]}}]}`,
			`[DONE]`,
		))
	}))
	defer srv.Close()
	_, _, done, err := collect(t, NewOpenAI(srv.URL, "").Stream(context.Background(), Request{Model: "m"}))
	if err != nil || done == nil || len(done.Message.ToolCalls) != 2 {
		t.Fatalf("err=%v done=%+v", err, done)
	}
}

func TestRequestTooLargeIsParsed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(413)
		io.WriteString(w, `{"error":{"message":"Request too large for model on tokens per minute (TPM): Limit 8000, Requested 16079, please reduce","code":"rate_limit_exceeded"}}`)
	}))
	defer srv.Close()
	_, _, done, err := collect(t, NewOpenAI(srv.URL, "").Stream(context.Background(), Request{Model: "m"}))
	if done != nil || err == nil {
		t.Fatalf("hata bekleniyordu: done=%v err=%v", done, err)
	}
	pe, ok := err.(*ProviderError)
	if !ok {
		t.Fatalf("ProviderError bekleniyordu: %T", err)
	}
	if pe.StatusCode != 413 || pe.Limit != 8000 || pe.Requested != 16079 || !pe.IsRequestTooLarge() {
		t.Fatalf("pe = %+v", pe)
	}
}

func TestRateLimitIsFlagged(t *testing.T) {
	pe := NewProviderError(429, `slow down`)
	if !pe.IsRateLimited() || pe.IsRequestTooLarge() {
		t.Fatalf("pe = %+v", pe)
	}
}

func TestBuildBodyShapesAssistantToolCallsAndToolResults(t *testing.T) {
	o := NewOpenAI("http://x", "")
	b, err := o.buildBody(Request{
		Model: "m",
		Messages: []Message{
			{Role: RoleUser, Content: "u"},
			{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "c1", Name: "n", Arguments: `{"a":1}`}}},
			{Role: RoleTool, ToolCallID: "c1", Content: "sonuç"},
		},
		MaxTokens: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Messages []map[string]any `json:"messages"`
		Max      int              `json:"max_tokens"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.Max != 100 || len(got.Messages) != 3 {
		t.Fatalf("body = %s", b)
	}
	if got.Messages[1]["content"] != nil {
		t.Errorf("araç çağıran asistan mesajında content null olmalı: %v", got.Messages[1]["content"])
	}
	tcs := got.Messages[1]["tool_calls"].([]any)
	fn := tcs[0].(map[string]any)["function"].(map[string]any)
	if fn["name"] != "n" || fn["arguments"] != `{"a":1}` {
		t.Errorf("tool_calls = %v", tcs)
	}
	if got.Messages[2]["tool_call_id"] != "c1" || got.Messages[2]["role"] != "tool" {
		t.Errorf("tool mesajı = %v", got.Messages[2])
	}
}

func TestProviderKind(t *testing.T) {
	cases := map[string]string{
		"https://api.anthropic.com/v1":                            "anthropic",
		"https://api.deepseek.com":                                "deepseek",
		"https://generativelanguage.googleapis.com/v1beta/openai": "gemini",
		"https://api.groq.com/openai/v1":                          "groq",
		"https://api.openai.com/v1":                               "openai",
		"https://openrouter.ai/api/v1":                            "",
		"http://127.0.0.1:11434/v1":                               "",
	}
	for url, want := range cases {
		if got := ProviderKind(url); got != want {
			t.Errorf("ProviderKind(%q) = %q, istenen %q", url, got, want)
		}
	}
}

func TestBuildBodyReasoningEffortPerProvider(t *testing.T) {
	// Groq/OpenAI: düz reasoning_effort alanı.
	o := NewOpenAI("https://api.groq.com/openai/v1", "k")
	b, _ := o.buildBody(Request{Model: "openai/gpt-oss-120b", Messages: []Message{{Role: RoleUser, Content: "x"}}, ReasoningEffort: "high"})
	var got map[string]any
	_ = json.Unmarshal(b, &got)
	if got["reasoning_effort"] != "high" {
		t.Fatalf("groq reasoning_effort = %v", got["reasoning_effort"])
	}
	if _, has := got["thinking"]; has {
		t.Fatalf("groq'a thinking nesnesi eklenmemeli: %v", got)
	}

	// DeepSeek açık: thinking.enabled + reasoning_effort.
	d := NewOpenAI("https://api.deepseek.com", "k")
	b2, _ := d.buildBody(Request{Model: "deepseek-flash", Messages: []Message{{Role: RoleUser, Content: "x"}}, ReasoningEffort: "medium"})
	var got2 map[string]any
	_ = json.Unmarshal(b2, &got2)
	thinking := got2["thinking"].(map[string]any)
	if thinking["type"] != "enabled" || got2["reasoning_effort"] != "medium" {
		t.Fatalf("deepseek açık = %v", got2)
	}

	// DeepSeek kapalı (boş efor): thinking.disabled, reasoning_effort alanı YOK.
	b3, _ := d.buildBody(Request{Model: "deepseek-flash", Messages: []Message{{Role: RoleUser, Content: "x"}}})
	var got3 map[string]any
	_ = json.Unmarshal(b3, &got3)
	thinking3 := got3["thinking"].(map[string]any)
	if thinking3["type"] != "disabled" {
		t.Fatalf("deepseek kapalı = %v", got3)
	}
	if _, has := got3["reasoning_effort"]; has {
		t.Fatalf("düşünme kapalıyken reasoning_effort gönderilmemeli: %v", got3)
	}

	// Doğrulanmamış sağlayıcı (ör. Gemini uyumluluk katmanı): hiçbir alan eklenmez.
	g := NewOpenAI("https://generativelanguage.googleapis.com/v1beta/openai", "k")
	b4, _ := g.buildBody(Request{Model: "gemini-2.5-flash", Messages: []Message{{Role: RoleUser, Content: "x"}}, ReasoningEffort: "high"})
	var got4 map[string]any
	_ = json.Unmarshal(b4, &got4)
	if _, has := got4["reasoning_effort"]; has {
		t.Fatalf("doğrulanmamış sağlayıcıya reasoning_effort gönderilmemeli: %v", got4)
	}
	if _, has := got4["thinking"]; has {
		t.Fatalf("doğrulanmamış sağlayıcıya thinking gönderilmemeli: %v", got4)
	}
}

func TestCancelStopsStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fl := w.(http.Flusher)
		io.WriteString(w, sse(`{"choices":[{"delta":{"content":"a"}}]}`))
		fl.Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	ch := NewOpenAI(srv.URL, "").Stream(ctx, Request{Model: "m"})
	first := <-ch
	if first.Kind != ChunkText {
		t.Fatalf("first = %+v", first)
	}
	cancel()
	for range ch { // kanal kapanmalı (takılmamalı)
	}
}
