# Güvenlik ve kötüye kullanım sınırları

## Veri sınırı

- Docker portları varsayılan olarak yalnız `127.0.0.1` üzerinde yayınlanır.
- Vaka verisi PostgreSQL volume'ünde, dosyalar Joe data volume'ünde kalır.
- Şifreleme anahtarı veritabanından ayrı, kalıcı secret volume'ünde tutulur.
- API anahtarının düz metni API yanıtına veya loga yazılmaz.
- Dış AI seçildiğinde yalnız seçilmiş/çıkarılmış analiz metni sağlayıcıya gider.

## Dosya güvenliği

- Yükleme boyutu varsayılan olarak 75 MB ile sınırlıdır.
- Dosya adı path bileşenlerinden arındırılır.
- Artefakt SHA-256 adıyla immutable biçimde saklanır.
- ZIP içindeki mutlak yollar ve `..` bileşenleri reddedilir.
- Şifreli üyeler atlanır.
- Dosya sayısı ve toplam açılmış boyut sınırlandırılır.
- IP, cookie, parola, token ve oturum alanları JSON metin çıkarımından varsayılan
  olarak dışlanır.
- Büyük klasörler HTTP ile yüklenmez. Yalnız `JOE_IMPORT_PATH` ile izin verilen
  kök Docker'a salt-okunur bağlanır; ham dosya kopyalanmadan indekslenir.
- Dizin API'si mutlak yol, üst dizine kaçış ve sembolik bağlantı izlemesini reddeder.

## OSINT sınırı

- Yalnız açık kaynaklar ve kullanıcı tarafından başlatılan sorgular.
- Özel hesap erişimi, parola kurtarma istismarı veya oturum çerezi kullanımı yok.
- Bulunan kullanıcı adı `aday_hesap` olarak saklanır.
- Sherlock, Maigret, WhatsMyName ve açık profil API'leri aynı arka plan işinde
  çalışır; başarısız kaynaklar sonuçtan saklanmaz.
- Kimlik doğrulama, bağımsız kanıt ve insan kararı gerektirir.
- Minörlere yönelik profilleme, doxxing ve takip amaçlı kullanım desteklenmez.

## Analiz sınırı

- Klinik tanı ve tedavi önerisi yok.
- Suçluluk, tehlikelilik, işe/krediye uygunluk veya siyasi tercih puanı yok.
- Yüz görüntüsünden kişilik veya duygu çıkarımı yok.
- Kuramsal yorumlar doğrudan gözlemden ayrı tutulur.
- Yetersiz veri çekimserlikle sonuçlanabilir.

## Bilinen ilk sürüm sınırları

- Tek kullanıcı yerel çalışma modeli vardır; LAN açılmadan önce kimlik doğrulama
  katmanı eklenmelidir.
- Secret dosyası volume ile korunur ancak işletim sistemi hesabı ele geçirilirse
  uygulama düzeyi şifreleme tek başına yeterli değildir.
- Sağlayıcıların kendi veri saklama şartları Joe tarafından değiştirilemez.
- MinIO servisi ilk sürümde hazırdır; artefaktlar şu an Joe data volume'ünde
  tutulur. MinIO adapteri sonraki katmandır.
