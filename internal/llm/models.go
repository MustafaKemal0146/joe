package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// wantsAnthropicVersion, adresin Anthropic'in resmî uç noktası olup olmadığını söyler (bkz.
// ListModels: Anthropic /models'ı Bearer ile kabul eder ama anthropic-version başlığını zorunlu ister).
func wantsAnthropicVersion(baseURL string) bool {
	return strings.Contains(baseURL, "api.anthropic.com")
}

// ListModels, OpenAI uyumlu bir sağlayıcının GET {base}/models uç noktasından model kimliklerini alır.
// 401/403 gibi durumlar *ProviderError olarak döner (anahtar doğrulaması için de kullanılır).
func ListModels(ctx context.Context, client *http.Client, baseURL, apiKey string) ([]string, error) {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	// Anthropic'in /models ucu Bearer'ı kabul eder ama anthropic-version başlığını zorunlu ister.
	if wantsAnthropicVersion(baseURL) {
		req.Header.Set("anthropic-version", "2023-06-01")
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, NewProviderError(resp.StatusCode, string(body))
	}
	return parseModels(body)
}

func parseModels(body []byte) ([]string, error) {
	var env struct {
		Data   []json.RawMessage `json:"data"`
		Models []json.RawMessage `json:"models"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		// Bazı sunucular doğrudan dizi döndürür.
		var arr []json.RawMessage
		if err2 := json.Unmarshal(body, &arr); err2 != nil {
			return nil, err
		}
		env.Data = arr
	}
	raw := env.Data
	if len(raw) == 0 {
		raw = env.Models
	}
	seen := map[string]bool{}
	var ids []string
	for _, r := range raw {
		var s string
		if json.Unmarshal(r, &s) == nil && s != "" {
			if !seen[s] {
				seen[s] = true
				ids = append(ids, s)
			}
			continue
		}
		var o struct {
			ID    string `json:"id"`
			Name  string `json:"name"`
			Model string `json:"model"`
		}
		if json.Unmarshal(r, &o) != nil {
			continue
		}
		id := o.ID
		if id == "" {
			id = o.Model
		}
		if id == "" {
			id = o.Name
		}
		if id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids, nil
}

// Sohbet dışı (ses, gömme, güvenlik süzgeci...) modelleri tanıyan ipuçları.
var nonChatHints = []string{
	"whisper", "tts", "text-to-speech", "speech", "embed", "moderation", "guard", "orpheus",
	"dall-e", "image", "rerank", "transcribe", "audio", "realtime", "babbage", "davinci",
}

// IsChatModel kimliğin bir sohbet modeli olma ihtimalini sezgisel olarak söyler.
func IsChatModel(id string) bool {
	l := strings.ToLower(id)
	for _, h := range nonChatHints {
		if strings.Contains(l, h) {
			return false
		}
	}
	return true
}
