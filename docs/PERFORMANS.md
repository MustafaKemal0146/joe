# Performans ölçümü

500 bin mesajlık sentetik veriyle (300 sohbet, kişi başına yaklaşık 1.700 mesaj, 6.000 olay) ölçüm.
Gerçek kişisel veri kullanılmaz; fikstür `internal/analytics/perf_test.go` içinde üretilir.

```bash
JOE_PERF=500000 go test -run PerfLarge -v ./internal/analytics
JOE_PERF=500000 JOE_PERF_MAX_PROFILE_MS=3000 go test -run PerfLarge ./internal/analytics   # eşik denetimi
go test -bench BuildProfile100k -run xxx ./internal/analytics
```

| İşlem | Önce | Sonra |
|---|---|---|
| Kaydetme (içe aktarma, SQLite bellek içi) | 10 sn | 10 sn |
| `LoadCorpus` | 0,77 sn | 0,77 sn |
| `BuildProfile` | 3,33 sn | **0,75 sn** |
| `Compare` (iki profil + doğrudan sohbet) | 4,01 sn | **0,84 sn** |
| `Timeline` | 3,33 sn | **0,74 sn** |
| `BuildGraph` | 0,05 sn | 0,04 sn |

Ölçüm geliştirme makinesinde, tek çalıştırma; mutlak değerler makineye göre değişir, oran önemlidir.

## Yapılan iyileştirme

Sıcak nokta, her mesajda yeniden oluşturulan `strings.NewReplacer` ve aynı gönderen adı için
tekrar tekrar yapılan `Norm` hesabıydı:

- `model.Norm` ve `lowerTR` içindeki değiştiriciler paket düzeyine alındı.
- `Subject.Is` sonuçları ad bazında önbelleğe alınır (bir `Subject` tek goroutine'de kullanılır).

## Önbellek ve geçersiz kılma

Sunucu veri yığınını (`Corpus`) ve kişi profillerini bellekte tutar:

- İçe aktarma, kaynak silme ve sahip değişimi: hepsi sıfırlanır.
- Kişi/kimlik değişimi: yalnızca profil önbelleği sıfırlanır.

## Kalan sınırlar

- İçe aktarma ve `LoadCorpus` doğrusal ölçeklenir; birkaç milyon mesajın üstünde tüm mesajların
  belleğe yüklenmesi sorun olur. O noktada kişi bazlı SQL sorgusuna geçilmelidir.
- Profil hesabı tek çekirdeklidir; birden çok kişi eşzamanlı istenirse sunucu bunları paralel çalıştırır.
