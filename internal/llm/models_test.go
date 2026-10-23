package llm

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestListModelsParsesOpenAIShapeAndSorts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer k" {
			t.Errorf("path=%s auth=%q", r.URL.Path, r.Header.Get("Authorization"))
		}
		io.WriteString(w, `{"object":"list","data":[{"id":"zeta"},{"id":"alfa"},{"id":"alfa"},{"id":""}]}`)
	}))
	defer srv.Close()
	ids, err := ListModels(context.Background(), nil, srv.URL+"/v1/", "k")
	if err != nil || strings.Join(ids, ",") != "alfa,zeta" {
		t.Fatalf("ids=%v err=%v", ids, err)
	}
}

func TestListModelsAcceptsOtherShapes(t *testing.T) {
	for name, body := range map[string]string{
		"dizi":         `["b","a"]`,
		"models/name":  `{"models":[{"name":"b"},{"name":"a"}]}`,
		"models/model": `{"models":[{"model":"b"},{"model":"a"}]}`,
	} {
		ids, err := parseModels([]byte(body))
		if err != nil || strings.Join(ids, ",") != "a,b" {
			t.Errorf("%s: ids=%v err=%v", name, ids, err)
		}
	}
	if _, err := parseModels([]byte("html değil json")); err == nil {
		t.Error("bozuk gövde hata vermeli")
	}
}

func TestListModelsUnauthorizedIsProviderError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		io.WriteString(w, `{"error":{"message":"Invalid API Key"}}`)
	}))
	defer srv.Close()
	_, err := ListModels(context.Background(), nil, srv.URL, "yanlis")
	pe, ok := err.(*ProviderError)
	if !ok || pe.StatusCode != 401 {
		t.Fatalf("err = %#v", err)
	}
}

func TestListModelsSendsAnthropicVersionHeaderOnlyForAnthropic(t *testing.T) {
	var gotVersion string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotVersion = r.Header.Get("anthropic-version")
		io.WriteString(w, `{"data":[{"id":"m"}]}`)
	}))
	defer srv.Close()
	// Anthropic olmayan bir adrese başlık gönderilmemeli.
	if _, err := ListModels(context.Background(), nil, srv.URL, "k"); err != nil {
		t.Fatal(err)
	}
	if gotVersion != "" {
		t.Fatalf("anthropic olmayan adrese anthropic-version gönderilmemeli: %q", gotVersion)
	}
}

func TestWantsAnthropicVersion(t *testing.T) {
	cases := map[string]bool{
		"https://api.anthropic.com/v1":                            true,
		"https://api.anthropic.com":                               true,
		"https://api.groq.com/openai/v1":                          false,
		"https://api.deepseek.com":                                false,
		"http://127.0.0.1:11434/v1":                               false,
		"https://generativelanguage.googleapis.com/v1beta/openai": false,
	}
	for url, want := range cases {
		if got := wantsAnthropicVersion(url); got != want {
			t.Errorf("wantsAnthropicVersion(%q) = %v, istenen %v", url, got, want)
		}
	}
}

func TestIsChatModel(t *testing.T) {
	for id, want := range map[string]bool{
		"openai/gpt-oss-120b": true, "llama-3.3-70b-versatile": true, "gpt-5-mini": true,
		"whisper-large-v3": false, "text-embedding-3-small": false, "meta-llama/llama-guard-4-12b": false,
		"playai-tts": false, "canopylabs/orpheus-v1": false,
	} {
		if IsChatModel(id) != want {
			t.Errorf("IsChatModel(%q) != %v", id, want)
		}
	}
}
