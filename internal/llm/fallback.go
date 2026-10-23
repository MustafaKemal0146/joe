package llm

import (
	"context"
	"errors"
)

// Fallback, birincil aktarımı dener; henüz hiçbir içerik göndermeden (kullanıcıya yarım bir
// yanıt göstermeden) hız sınırı ya da ağ/sunucu hatasıyla karşılaşırsa SESSİZCE ikincil bir
// sağlayıcıya geçer. Zaten içerik akmaya başladıysa (mid-stream kopma gibi nadir bir durum)
// güvenlik için ikinci bir sağlayıcıyla "baştan karışık" bir yanıt oluşturmamak adına hatayı
// olduğu gibi iletir — bu durumda geçiş yapılmaz.
type Fallback struct {
	Primary Transport
	// Secondary nil ise Fallback sadece Primary'yi şeffafça sarar (hiçbir değişiklik olmaz).
	Secondary Transport
	// SecondaryModel ayarlıysa, ikincil sağlayıcıya geçerken isteğin Model alanının yerini alır
	// (sağlayıcılar arasında model adları örtüşmez; birincilin modeli ikincilde anlamsız olabilir).
	SecondaryModel string
	// SecondaryLabel, ChunkFallback'in Text alanına konan kısa açıklamadır (ör. sağlayıcı adı).
	SecondaryLabel string
}

// isFallbackWorthy, bu hatanın ikinci bir sağlayıcı denemeye değip değmeyeceğini söyler: hız
// sınırı, sunucu tarafı hata (5xx) ya da bağlantı kurulamadı (StatusCode==0, ağ hatası). Kimlik
// doğrulama, geçersiz istek gibi "tekrar denense de aynı sonucu verecek" hatalar denenmez.
func isFallbackWorthy(err error) bool {
	var pe *ProviderError
	if !errors.As(err, &pe) {
		return true // sarılmamış bir hata: büyük olasılıkla ağ/bağlantı sorunu
	}
	if pe.IsRateLimited() {
		return true
	}
	if pe.StatusCode == 0 || pe.StatusCode >= 500 {
		return true
	}
	return false
}

func (f *Fallback) Stream(ctx context.Context, req Request) <-chan Chunk {
	if f.Secondary == nil {
		return f.Primary.Stream(ctx, req)
	}
	out := make(chan Chunk, 16)
	go func() {
		defer close(out)
		sent := false
		var fallbackErr error
		for c := range f.Primary.Stream(ctx, req) {
			if c.Kind == ChunkError && !sent && isFallbackWorthy(c.Err) {
				fallbackErr = c.Err
				break
			}
			sent = true
			select {
			case out <- c:
			case <-ctx.Done():
				return
			}
		}
		if fallbackErr == nil {
			return
		}
		fbReq := req
		if f.SecondaryModel != "" {
			fbReq.Model = f.SecondaryModel
		}
		select {
		case out <- Chunk{Kind: ChunkFallback, Text: f.SecondaryLabel, Err: fallbackErr}:
		case <-ctx.Done():
			return
		}
		for c := range f.Secondary.Stream(ctx, fbReq) {
			select {
			case out <- c:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out
}
