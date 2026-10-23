package llm

import "testing"

func TestProviderErrorIsUnauthorized(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{"openai 401", 401, `{"error":{"message":"Invalid API Key"}}`, true},
		{"anthropic 401", 401, `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`, true},
		{"groq 403", 403, `{"error":"forbidden"}`, true},
		{"gemini geçersiz anahtar 400", 400, `{"error":{"code":400,"message":"API key not valid. Please pass a valid API key.","status":"INVALID_ARGUMENT"}}`, true},
		{"deepseek 401", 401, `{"error":{"message":"Authentication Fails, Your api key: ****fake is invalid"}}`, true},
		{"gemini normal 400 (anahtarla ilgisiz)", 400, `{"error":{"code":400,"message":"Invalid request: missing field 'contents'","status":"INVALID_ARGUMENT"}}`, false},
		{"sunucu hatası", 500, `internal error`, false},
		{"rate limit", 429, `too many requests`, false},
		{"istek çok büyük", 413, `payload too large`, false},
	}
	for _, c := range cases {
		e := NewProviderError(c.status, c.body)
		if got := e.IsUnauthorized(); got != c.want {
			t.Errorf("%s: IsUnauthorized() = %v, istenen %v", c.name, got, c.want)
		}
	}
}
