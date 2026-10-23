# joe geliştirme kuralları

- Kullanıcıya görünen tüm metin Türkçe olmalı.
- Ham veri (mesaj, IP, konum) hiçbir zaman LLM'e olduğu gibi gitmez: önce deterministik metrik, sonra seçilmiş ve önizlenmiş kanıt.
- Sahte kişi, sahte veri ya da örnek LLM sonucu depoya eklenmez. Testler sentetik ama yapısal fikstürler üretir.
- Dış servis hatası başarılı yanıta çevrilmez.
- Klinik tanı, tehlikelilik, suçluluk, otomatik sosyal puanlama üretme.
- Gizli değerler (API anahtarı) API yanıtında, logda ya da istemci durumunda yer almaz.
- ZIP ve yükleme sınırlarını gevşetme; sunucu loopback dışında dinlemez.
- Yeni platform = `internal/ingest/<platform>` adaptörü + `model.Import` çıktısı; analiz katmanı platformu bilmez.
- Değişiklikten sonra: `go vet ./... && go test ./...` ve `cd web && pnpm test && pnpm build`.
