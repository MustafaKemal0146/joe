# Joe

Joe; açık kaynak araştırmasını ve çok perspektifli kuramsal metin analizini aynı
yerel çalışma alanında sunan, fakat bu iki modülü veri akışı bakımından birbirinden
ayıran web tabanlı bir uygulamadır.

Joe bir klinik tanı sistemi, kişi puanlama motoru veya otomatik kimlik doğrulama
aracı değildir. OSINT sonuçlarını **aday bulgu**, analiz çıktılarını ise
**kanıta bağlı kuramsal yorum** olarak sınıflandırır.

## İlk çalışan sürümde bulunanlar

- Yüzde 100 Türkçe, duyarlı web arayüzü
- Ayrı OSINT ve Analiz çalışma alanları
- Kullanıcı adı için Sherlock, Maigret ve WhatsMyName taramalarını aynı işte
  otomatik çalıştırma; kaynaklar arası URL tekilleştirme
- Kullanıcı adı ve tam ad için GitHub/GitLab açık profil sorguları ile elle
  doğrulanabilir arama bağlantıları
- Her OSINT araştırmasında otomatik vaka açma
- 23 hazır AI sağlayıcı profili ve özel OpenAI uyumlu endpoint desteği
- OpenAI, Anthropic, Gemini, OpenRouter, Azure OpenAI, Ollama, LM Studio ve
  vLLM protokolleri
- Yerel, şifreli API anahtarı kasası
- Kullanıcının seçebildiği 13 kuramsal persona
- Persona başına farklı AI bağlantısı yönlendirebilme
- Bağımsız görüş → çapraz sorgu → revizyon → nötr sentez akışı
- Metni `K1`, `K2` gibi değişmez kanıt parçalarına ayırma
- Model çıktısını Pydantic şemalarıyla doğrulama; hatada sahte fallback üretmeme
- Metin, JSON, ZIP, PDF, DOCX ve ekran görüntüsü yükleme
- Docker'a salt-okunur bağlanan yerel dizini ham dosyaları kopyalamadan
  indeksleme, arama ve seçili kesitleri konseye aktarma
- Türkçe ve İngilizce yerel OCR
- Güvenli ZIP inceleme ve hassas teknik alanları varsayılan olarak dışarıda bırakma
- Yeniden başlatılabilir veritabanı tabanlı iş kuyruğu
- Epistemik denetim: geçersiz kanıt kimliği, kanıtsız iddia ve teşhis dili kontrolü

## Yerel Docker kurulumu

Gereksinimler:

- Docker Desktop
- Docker Compose

Başlat:

```powershell
Copy-Item .env.example .env
docker compose up -d --build
```

Instagram/WhatsApp dışa aktarımı veya başka büyük bir klasörü yüklemeden
incelemek için `.env` içindeki `JOE_IMPORT_PATH` değerini o klasörün Windows
yoluna ayarla. Docker dizini `/imports` altında salt-okunur bağlar. Dizin toplam
boyutu HTTP yükleme sınırına tabi değildir; desteklenen dosyalar yerinde
indekslenir ve ham içerik Joe deposuna kopyalanmaz.

```dotenv
JOE_IMPORT_PATH=C:/izinli/veri/dizini
```

Arayüz:

- `http://localhost:4177`

Yerel API belgeleri:

- `http://localhost:8000/api/docs`

Servis durumu:

```powershell
docker compose ps
```

Loglar:

```powershell
docker compose logs -f api worker web
```

Tamamen yerel bir Ollama servisini de başlatmak istersen:

```powershell
docker compose --profile local-ai up -d --build
```

Ardından Joe'nun **Sağlayıcılar** ekranından Ollama bağlantısı oluştur ve daha
önce indirdiğin gerçek model kimliğini seç. Joe kendiliğinden sahte veya örnek
model kaydı oluşturmaz.

## Servisler

| Servis | Görev |
|---|---|
| `web` | Türkçe Joe arayüzü |
| `api` | Vaka, sağlayıcı, artefakt, OSINT ve analiz API'si |
| `worker` | Uzun OSINT, dizin indeksleme ve konsey görevlerini kalıcı kuyruktan işler |
| `postgres` | Vakalar, işler, kanıtlar, görüşler ve sonuçlar |
| `redis` | İleride canlı olay yayını ve görev koordinasyonu için yerel altyapı |
| `minio` | Yerel obje depolama servisi |
| `ollama` | İsteğe bağlı yerel model servisi |

Joe'nun web ve API portları yalnızca `127.0.0.1` üzerinde yayınlanır. Yerel ağ
erişimi bilinçli olarak varsayılan değildir.

## Analiz konseyi

Hazır personeller:

- Sigmund Freud
- Carl Gustav Jung
- Melanie Klein
- Alfred Adler
- Donald Winnicott
- Wilfred Bion
- Karen Horney
- Wilhelm Reich
- Erich Fromm
- Jacques Lacan
- Julia Kristeva
- Slavoj Žižek
- Michel Foucault

Personeller tarihsel kişiyi taklit etmez. Her biri yayımlanmış kuramsal çerçeveyi
uygulayan bir analiz merceğidir. Her prompt şu ortak sözleşmeye uyar:

1. Klinik tanı ve değişmez kişilik etiketi üretme.
2. Her gözlemi var olan kanıt kimliğine bağlama.
3. Doğrudan gözlem ile kuramsal yorumu ayırma.
4. Alternatif açıklama ve eksik bağlam belirtme.
5. Veri yetersizse çekimser kalma.
6. Tarihsel kişiden sahte alıntı yapmama.

Kullanıcıya hızlı/derin benzeri bir mod sunulmaz. Her çalışma bağımsız görüş,
çapraz sorgu, görüş revizyonu ve ortak sentezden oluşan tam protokolü yürütür.

Ayrıntılı konsey protokolü için [docs/PERSONA-KONSEYI.md](docs/PERSONA-KONSEYI.md)
dosyasına bak.

## AI sağlayıcıları

Sağlayıcı sistemi Hermes'teki tek-kayıt yaklaşımından esinlenen bildirimsel bir
katalog kullanır. Profil; protokolü, varsayılan adresi, anahtar ihtiyacını ve
yetenekleri tanımlar. Kullanıcı bağlantısı ise yalnız yerel veritabanında model,
endpoint ve şifreli secret referansı tutar.

Yeni OpenAI uyumlu servisler kod değişmeden **Özel OpenAI Uyumlu Sağlayıcı**
profiliyle bağlanabilir. Yeni bir özel protokol gerektiğinde
`backend/app/providers/client.py` içine adapter ve
`backend/app/providers/profiles/catalog.yaml` içine profil eklenir.

## Mock veri politikası

Repoda sahte kişi profili, sahte OSINT bulgusu veya uydurma AI yanıtı bulunmaz.

- Bağlayıcı çalışmıyorsa sonuç `unavailable/failed` olur.
- Model yapılandırılmamışsa analiz başlatılmaz.
- Model geçerli JSON döndürmezse oturum başarısız sayılır; güven puanı veya görüş
  uydurulmaz.
- Entegrasyon doğrulaması gerçek ve izinli yerel girdilerle yapılır.
- Testlerde yalnız veri modelinin yapısal kuralları sınanır; gerçek kişi temsili
  oluşturulmaz.

## Geliştirme

Web:

```powershell
npm install
npm run dev
npm test
```

API:

```powershell
py -m venv .venv
.\.venv\Scripts\python.exe -m pip install -e "backend[test]"
Set-Location backend
..\.venv\Scripts\python.exe -m pytest -q
..\.venv\Scripts\python.exe -m uvicorn app.main:app --reload
```

Worker:

```powershell
Set-Location backend
..\.venv\Scripts\python.exe -m app.worker
```

## Güvenlik sınırları

- Özel hesap engeli aşma ve credential kullanımı yoktur.
- Kullanıcı adı eşleşmesi kimlik doğrulaması sayılmaz.
- Ham dosyalar otomatik olarak dış AI sağlayıcısına gönderilmez; yalnız seçilmiş
  ve çıkarılmış metin gönderilir.
- API anahtarları yanıt gövdesinde hiçbir zaman geri dönmez.
- Arşiv ayrıştırıcı path traversal, şifreli ZIP ve aşırı genişleme kontrolleri uygular.
- Dizin tarayıcı yalnız yapılandırılmış salt-okunur kökün altındaki göreli yolları
  kabul eder; üst dizine kaçış ve sembolik bağlantılar izlenmez.
- Klinik tanı, suçluluk, tehlikelilik, işe uygunluk veya siyasi tercih tahmini
  Joe'nun çıktı sözleşmesinin dışındadır.

Daha ayrıntılı tehdit modeli için [docs/GUVENLIK.md](docs/GUVENLIK.md) dosyasına bak.

## Durum

Bu sürüm Joe'nun ilk uçtan uca omurgasıdır. Sonraki geliştirme katmanları:

- WhatsApp ve Instagram şemaları için daha ayrıntılı konuşma/medya ilişkilendirmesi
- Entity resolution grafiği ve elle birleştir/ayır arayüzü
- Yeni OSINT bağlayıcı manifestleri
- Gerçek zamanlı SSE/WebSocket görev akışı
- Sürümlemeli rapor dışa aktarma
- Kullanıcı tarafından tanımlanan persona paketleri
