package llm

import (
	"context"
	"strings"
)

// NewTransport sağlayıcı kimliğine uygun aktarımı döndürür: Anthropic kendi protokolünü,
// diğerleri OpenAI uyumlu protokolü konuşur.
func NewTransport(providerID, baseURL, apiKey string) Transport {
	if providerID == "anthropic" {
		return NewAnthropic(baseURL, apiKey)
	}
	return NewOpenAI(baseURL, apiKey)
}

// Complete tek bir istek gönderip yanıtın tamamını bekler; kullanım bilgisini de döndürür.
func Complete(ctx context.Context, t Transport, req Request) (string, Usage, error) {
	var sb strings.Builder
	var usage Usage
	for ch := range t.Stream(ctx, req) {
		switch ch.Kind {
		case ChunkText:
			sb.WriteString(ch.Text)
		case ChunkError:
			return sb.String(), usage, ch.Err
		case ChunkDone:
			if ch.Usage != nil {
				usage = *ch.Usage
			}
			if ch.Message != nil && sb.Len() == 0 {
				sb.WriteString(ch.Message.Content)
			}
		}
	}
	return sb.String(), usage, nil
}
