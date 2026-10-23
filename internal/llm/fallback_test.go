package llm

import (
	"context"
	"testing"
)

// chunkTransport verilen chunk dizisini olduğu gibi yayımlayan sahte bir aktarımdır; gelen
// isteği de kaydeder (hangi model/parametrelerle çağrıldığını sınamak için).
type chunkTransport struct {
	chunks []Chunk
	got    *Request
}

func (c *chunkTransport) Stream(_ context.Context, req Request) <-chan Chunk {
	c.got = &req
	out := make(chan Chunk, len(c.chunks))
	for _, ch := range c.chunks {
		out <- ch
	}
	close(out)
	return out
}

func drainKinds(ch <-chan Chunk) []ChunkKind {
	var kinds []ChunkKind
	for c := range ch {
		kinds = append(kinds, c.Kind)
	}
	return kinds
}

func TestFallbackSwitchesOnRateLimitBeforeAnyContent(t *testing.T) {
	primary := &chunkTransport{chunks: []Chunk{{Kind: ChunkError, Err: NewProviderError(429, "too many requests")}}}
	secondary := &chunkTransport{chunks: []Chunk{
		{Kind: ChunkText, Text: "ikincil"},
		{Kind: ChunkDone, Message: &Message{Role: RoleAssistant, Content: "ikincil"}},
	}}
	f := &Fallback{Primary: primary, Secondary: secondary, SecondaryModel: "ikincil-model", SecondaryLabel: "İkincil Sağlayıcı"}

	kinds := drainKinds(f.Stream(context.Background(), Request{Model: "birincil-model"}))
	want := []ChunkKind{ChunkFallback, ChunkText, ChunkDone}
	if len(kinds) != len(want) {
		t.Fatalf("kinds = %v", kinds)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("kinds[%d] = %v, istenen %v (tam: %v)", i, kinds[i], want[i], kinds)
		}
	}
	if secondary.got == nil || secondary.got.Model != "ikincil-model" {
		t.Fatalf("ikincil isteğin modeli değiştirilmeli: %+v", secondary.got)
	}
}

func TestFallbackDoesNotSwitchOnAuthError(t *testing.T) {
	primary := &chunkTransport{chunks: []Chunk{{Kind: ChunkError, Err: NewProviderError(401, "invalid api key")}}}
	secondary := &chunkTransport{chunks: []Chunk{{Kind: ChunkText, Text: "hiç çağrılmamalı"}}}
	f := &Fallback{Primary: primary, Secondary: secondary}

	kinds := drainKinds(f.Stream(context.Background(), Request{Model: "m"}))
	if len(kinds) != 1 || kinds[0] != ChunkError {
		t.Fatalf("kimlik hatasında geçiş yapılmamalı: %v", kinds)
	}
	if secondary.got != nil {
		t.Fatal("ikincil sağlayıcı hiç çağrılmamalı")
	}
}

func TestFallbackDoesNotSwitchAfterContentAlreadySent(t *testing.T) {
	primary := &chunkTransport{chunks: []Chunk{
		{Kind: ChunkText, Text: "yarım yanıt"},
		{Kind: ChunkError, Err: NewProviderError(429, "too many requests")},
	}}
	secondary := &chunkTransport{chunks: []Chunk{{Kind: ChunkText, Text: "hiç çağrılmamalı"}}}
	f := &Fallback{Primary: primary, Secondary: secondary}

	kinds := drainKinds(f.Stream(context.Background(), Request{Model: "m"}))
	if len(kinds) != 2 || kinds[0] != ChunkText || kinds[1] != ChunkError {
		t.Fatalf("içerik akmaya başladıktan sonra olduğu gibi iletilmeli: %v", kinds)
	}
	if secondary.got != nil {
		t.Fatal("içerik akmaya başladıysa ikincil sağlayıcı çağrılmamalı")
	}
}

func TestFallbackNoSecondaryIsTransparent(t *testing.T) {
	primary := &chunkTransport{chunks: []Chunk{{Kind: ChunkText, Text: "x"}}}
	f := &Fallback{Primary: primary}
	kinds := drainKinds(f.Stream(context.Background(), Request{Model: "m"}))
	if len(kinds) != 1 || kinds[0] != ChunkText {
		t.Fatalf("ikincil yoksa şeffaf olmalı: %v", kinds)
	}
}
