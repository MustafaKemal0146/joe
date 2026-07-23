# Joe geliştirme kuralları

- Kullanıcıyla görünen tüm arayüz metni Türkçe olmalıdır.
- OSINT ve Analiz bounded context'lerini otomatik veri akışıyla birleştirme.
- Sahte kişi, sahte OSINT bulgusu veya örnek AI sonucu ekleme.
- Harici servis başarısızlığını başarılı fallback yanıtına dönüştürme.
- Her analiz iddiası var olan `K*` kanıt kimliklerine dayanmalıdır.
- Persona promptları tarihsel kişiyi taklit etmemeli; kuramsal mercek olarak çalışmalıdır.
- Klinik tanı, tehlikelilik ve otomatik sosyal puanlama üretme.
- Yeni AI sağlayıcılarını bildirimsel profil + protokol adapteri olarak ekle.
- Secret değerlerini API yanıtında, logda veya istemci durumunda tutma.
- ZIP ve yükleme güvenlik sınırlarını gevşetme.
- Windows yerel doğrulamada `npm run build`, Docker doğrulamada
  `docker compose up -d --build` ve `http://localhost:8000/api/v1/health`
  kontrollerini kullan.
- Değişiklikten sonra en az `npm test` ve `backend` pytest paketini çalıştır.
