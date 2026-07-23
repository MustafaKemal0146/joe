# Persona konseyi

## Persona tanımı

Persona, tarihsel kişinin diliyle rol yapma promptu değildir. Aşağıdaki alanları
taşıyan kuramsal analiz bileşenidir:

- kimlik ve okul,
- kısa kullanıcı açıklaması,
- kavram seti,
- ayrıntılı sistem promptu,
- yönlendirici analiz soruları,
- diğer görüşlere yönelteceği özel eleştiri görevi,
- ortak kanıt ve güven sözleşmesi.

Tanımlar `backend/app/personas/catalog.py` dosyasındadır.

## Konsey aşamaları

### 1. Bağımsız görüş

Her persona diğerlerinin yanıtını görmeden aynı evidence pack üzerinde çalışır.
Çıktı; tez, gözlemler, kanıt kimlikleri, alternatifler, gerilimler, bilinmeyenler
ve çekimser kalınan alanlardan oluşur.

### 2. Çapraz sorgu

Persona diğer ilk görüşleri görür ve en fazla dört somut itiraz üretir. İtirazın
hedefi kuramsal üstünlük yarışı değil:

- kanıt yetersizliği,
- bağlam atlama,
- alternatif açıklamayı dışlama,
- aşırı kesinlik,
- kuramın yanlış kullanımıdır.

### 3. Revizyon

Persona kendisine yöneltilen itirazları değerlendirir. İtirazı otomatik kabul
etmez; değişen ve korunan görüşlerini gerekçelendirir. Yeni kanıt kimliği üretemez.

### 4. Nötr sentez

Moderatör çoğunluk görüşünü otomatik doğru saymadan:

- doğrudan gözlemleri,
- kuramsal yorumları,
- karşı hipotezleri,
- belirsizlikleri,
- uzlaşı ve giderilmemiş ayrılıkları

ayrı alanlarda birleştirir.

### 5. Epistemik denetim

Sentez deterministik doğrulayıcıdan geçer. Denetçi yeni bir AI persona değildir;
kanıt kimliklerini ve çıktı sözleşmesini kodla kontrol eder.

## Modlar

Joe kullanıcıya analiz modu seçtirmez. Her oturum aynı tam protokolü uygular:
bağımsız görüş, çapraz sorgu, görüş revizyonu ve kanıtlı ortak sentez.

## Güven

Güven, klinik veya istatistiksel olasılık değildir. `düşük`, `orta`, `yüksek`
seviyeleri yalnız kanıt kapsamı, tekrar, bağlam ve karşı hipotezlerle birlikte
yorumlanır. Modelin tek başına verdiği yüzde güven skoru kullanılmaz.
