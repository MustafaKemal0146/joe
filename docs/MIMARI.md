# Joe mimarisi

## Temel ayrım

Joe iki bağımsız üst düzey bounded context kullanır:

1. `OSINT`: sorgu, bağlayıcı çalışması, aday bulgu ve manuel doğrulama.
2. `Analiz`: kullanıcı tarafından seçilen içerik, kuramsal persona görüşleri,
   çapraz sorgu, revizyon ve sentez.

OSINT bulgusu kendiliğinden Analiz bağlamına girmez. İleride eklenecek
`Analize aktar` işlemi, seçilen kanıtların yeni bir immutable evidence pack
içine kopyalanmasını ve kullanıcı onayını gerektirir.

## Katmanlar

```text
Tarayıcı
  └─ Joe Web (React / Next.js standalone)
       └─ Joe API (FastAPI)
            ├─ Vaka ve kanıt kayıtları
            ├─ Sağlayıcı kayıt defteri ve secret kasası
            ├─ Artefakt ayrıştırıcıları
            ├─ Salt-okunur dizin gezgini ve corpus araması
            └─ Kalıcı iş kayıtları
                 └─ Joe Worker
                      ├─ OSINT bağlayıcıları
                      ├─ Dizin indeksleyici
                      └─ Konsey motoru
                           └─ AI sağlayıcı adapterleri
```

## Kalıcı işler

Uzun çalışmalar HTTP isteği açık tutularak yürütülmez. API, işi `queued`
durumuyla PostgreSQL'e yazar. Worker `FOR UPDATE SKIP LOCKED` ile tek işi sahiplenir,
`running` durumuna geçirir ve aşama kalp atışlarını kaydeder. Otuz dakikadan uzun
süre kalp atışı vermeyen çalışma yeniden sıraya alınır.

Bu tercih şu özellikleri sağlar:

- Sayfa kapansa da işin kaybolmaması
- Worker yeniden başladığında sıranın korunması
- Aynı işin iki worker tarafından sahiplenilmemesi
- Çalışmanın aşama ve hata durumunun denetlenebilmesi

## Salt-okunur dizin indeksi

Docker yalnız yapılandırılmış `JOE_IMPORT_PATH` dizinini `/imports` altında
salt-okunur görür. API göreli yol dışında giriş kabul etmez. Worker desteklenen
dosyalardan metin ve şema bilgisi çıkarıp `CorpusDocument` kayıtlarına yazar;
ham dosyayı Joe veri alanına kopyalamaz. Arama sonucu seçilen kesitler ancak
kullanıcı `Sonuçları konseye aktar` dediğinde Analiz kaynağı olur.

## OSINT bağlayıcı zinciri

Kullanıcı adı sorgusunda uyumlu bağlayıcılar seçim ekranı olmadan otomatik ve
eşzamanlı çalışır. Hızlı bağlayıcıların sonuçları uzun Maigret taraması sürerken
iş kaydına kademeli yazılır. Profil URL'leri normalize edilip tekilleştirilir;
aynı bağlantıyı bulan bütün bağlayıcıların kaynak bilgisi korunur.

## Sağlayıcı kayıt defteri

`ProviderProfile`, sağlayıcının ürün kimliğini değil çalışma protokolünü tanımlar:

- `openai-chat`
- `anthropic`
- `gemini`
- `ollama`
- `azure-openai`

OpenAI uyumlu sağlayıcıların tamamı ortak adapteri kullanır. Kullanıcı bağlantısı
profil kimliği, model, isteğe bağlı endpoint, şifreli API anahtarı ve özel ayarlar
taşır. Bir persona varsayılan bağlantıyı kullanabilir veya persona bazında başka
bir bağlantıya yönlendirilebilir.

## Kanıt modeli

Analiz kaynağı normalize edilir fakat özgün artefakt değiştirilmez. Metin
parçaları sıralı `K1`, `K2` kimlikleri ve SHA-256 hash'i kazanır. Model yalnız bu
kimliklere atıf yapabilir. Sentezden sonra deterministik denetçi:

- olmayan kanıt kimliklerini,
- kanıtsız gözlem/yorum iddialarını,
- teşhis ve tehlikelilik dilini

işaretler.

## Hata ilkesi

Joe eksik veya bozuk harici yanıtı başarıya dönüştürmez. Geçersiz JSON,
sağlayıcı zaman aşımı, bağlayıcı eksikliği ve kota hataları açık hata durumlarıdır.
Bu hatalarda varsayılan güven puanı, görüş veya OSINT bulgusu üretilmez.
