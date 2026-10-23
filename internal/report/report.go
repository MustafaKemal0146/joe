// Package report, bir kişi dosyasından denetlenebilir Markdown raporu üretir. Çıktı yalnızca
// verilen girdilerin işlevidir (zaman damgası dışarıdan verilir), bu yüzden aynı veriyle
// tekrarlanabilir. Rapor deterministik metrikleri içerir; konsey yorumu yalnızca Input.Councils doluysa eklenir.
package report

import (
	"fmt"
	"strings"
	"time"

	"github.com/MustafaKemal0146/joe/internal/analytics"
	"github.com/MustafaKemal0146/joe/internal/store"
)

// Input raporun girdileridir.
type Input struct {
	Person   store.Person
	Profile  analytics.Profile
	Timeline []analytics.TimelineItem
	// Councils kişiye ait en güncel tamamlanmış konsey oturumlarıdır (boşsa rapor konsey içermez).
	Councils    []CouncilInput
	GeneratedAt time.Time
	Version     string
	Loc         *time.Location
}

func esc(s string) string {
	s = strings.NewReplacer("|", "\\|", "\n", " ", "\r", " ").Replace(s)
	return strings.TrimSpace(s)
}

func pct(v float64) string { return fmt.Sprintf("%%%.0f", v*100) }

func dur(sec float64) string {
	switch {
	case sec <= 0:
		return "—"
	case sec < 90:
		return fmt.Sprintf("%.0f sn", sec)
	case sec < 5400:
		return fmt.Sprintf("%.0f dk", sec/60)
	case sec < 86400*2:
		return fmt.Sprintf("%.1f sa", sec/3600)
	}
	return fmt.Sprintf("%.1f gün", sec/86400)
}

func day(t time.Time, loc *time.Location) string {
	if t.IsZero() {
		return "—"
	}
	return t.In(loc).Format("02.01.2006")
}

// Markdown raporu üretir.
func Markdown(in Input) string {
	loc := in.Loc
	if loc == nil {
		loc = time.UTC
	}
	p, pr := in.Person, in.Profile
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format+"\n", a...) }

	w("# %s — kişi dosyası raporu", esc(p.Name))
	w("")
	w("> **Gizlilik uyarısı.** Bu rapor üçüncü kişilere ait kişisel veriden türetilmiştir. Yalnızca kendi verin ya da rızasıyla paylaşılan veriler üzerinde kullan ve izinsiz paylaşma.")
	w("> **Kapsam.** Aşağıdakiler eldeki veriden hesaplanan davranış metrikleridir; klinik tanı, kişilik etiketi ya da tehlikelilik değerlendirmesi değildir.")
	w("")
	w("- Oluşturulma: %s", in.GeneratedAt.In(loc).Format("02.01.2006 15:04 MST"))
	w("- Yazılım sürümü: joe %s", in.Version)
	ids := make([]string, len(p.Identities))
	for i, id := range p.Identities {
		ids[i] = esc(id.Value)
	}
	w("- Kimlikler: %s", strings.Join(ids, ", "))
	if p.Note != "" {
		w("- Not: %s", esc(p.Note))
	}
	w("")

	w("## Özet")
	w("")
	if pr.Messages == 0 && pr.Activity == nil {
		w("Bu kişinin kimlikleriyle eşleşen mesaj ya da etkinlik bulunamadı.")
		return b.String()
	}
	w("| Ölçüt | Değer |")
	w("|---|---|")
	w("| Mesaj | %d |", pr.Messages)
	w("| İlk – son mesaj | %s – %s |", day(pr.First, loc), day(pr.Last, loc))
	w("| Aktif gün | %d |", pr.Active())
	if pr.Latency.N >= 5 {
		w("| Medyan yanıt süresi | %s (%%90: %s, n=%d) |", dur(pr.Latency.Median), dur(pr.Latency.P90), pr.Latency.N)
	}
	w("| Gece (00–06) payı | %s |", pct(pr.NightRatio))
	if pr.SleepKnown {
		w("| Tahmini uyku penceresi | %02d:00 – %02d:00 |", pr.SleepStart, (pr.SleepStart+6)%24)
	}
	w("")
	for _, n := range pr.Notes {
		w("> ⚠ %s", esc(n))
	}
	if len(pr.Notes) > 0 {
		w("")
	}

	if pr.Messages > 0 {
		w("## Ritim")
		w("")
		w("Saate göre mesaj sayısı (yerel saat):")
		w("")
		w("| Saat | Mesaj |")
		w("|---|---|")
		for h, n := range pr.Hours {
			if n > 0 {
				w("| %02d:00 | %d |", h, n)
			}
		}
		w("")
		days := []string{"Pazartesi", "Salı", "Çarşamba", "Perşembe", "Cuma", "Cumartesi", "Pazar"}
		w("| Gün | Mesaj |")
		w("|---|---|")
		for i, n := range pr.Weekday {
			w("| %s | %d |", days[i], n)
		}
		w("")
	}

	if l := pr.Language; l.TextMessages > 0 {
		w("## Yazış biçimi")
		w("")
		w("- Metin mesajı: %d, ortalama %.1f sözcük", l.TextMessages, l.AvgWords)
		w("- Soru oranı %s, ünlem oranı %s, üç nokta oranı %s", pct(l.QuestionRatio), pct(l.ExclaimRatio), pct(l.EllipsisRatio))
		w("- Mesaj başına emoji: %.2f, söz dağarcığı çeşitliliği: %s", l.EmojiPerMsg, pct(l.Richness))
		if len(l.TopEmoji) > 0 {
			e := make([]string, len(l.TopEmoji))
			for i, c := range l.TopEmoji {
				e[i] = fmt.Sprintf("%s×%d", c.Key, c.Count)
			}
			w("- En sık emoji: %s", strings.Join(e, " "))
		}
		if len(l.TopWords) > 0 {
			ws := make([]string, 0, 15)
			for i, c := range l.TopWords {
				if i >= 15 {
					break
				}
				ws = append(ws, fmt.Sprintf("%s (%d)", esc(c.Key), c.Count))
			}
			w("- En sık sözcükler: %s", strings.Join(ws, ", "))
		}
		w("")
	}

	if len(pr.Partners) > 0 {
		w("## Bire-bir sohbetler")
		w("")
		w("Her satırda ilk değer **%s**, ikinci değer karşı taraftır.", esc(p.Name))
		w("")
		w("| Karşı taraf | Platform | Mesaj | Konuşma başlatma | Bitirme | Medyan yanıt | Geri alınan | En uzun sessizlik |")
		w("|---|---|---|---|---|---|---|---|")
		for _, pt := range pr.Partners {
			w("| %s | %s | %d / %d | %d / %d | %d / %d | %s / %s | %d / %d | %.0f gün |", esc(pt.Name), pt.Platform,
				pt.Messages.Subject, pt.Messages.Partner, pt.Starts.Subject, pt.Starts.Partner, pt.Ends.Subject, pt.Ends.Partner,
				dur(pt.Latency.Subject.Median), dur(pt.Latency.Partner.Median), pt.Unsent.Subject, pt.Unsent.Partner, pt.LongestSilenceDays)
		}
		w("")
	}

	if len(pr.Periods) > 0 {
		w("## Dikkat çeken dönemler")
		w("")
		for _, per := range pr.Periods {
			kind := "Yoğun"
			if per.Kind == "lull" {
				kind = "Sessiz"
			}
			w("- %s: %s – %s (%d mesaj; olağan düzey ≈ %.0f/ay)", kind, per.From, per.To, per.Count, per.Baseline)
		}
		w("")
	}

	if a := pr.Activity; a != nil {
		w("## Instagram etkinliği")
		w("")
		f := a.Follow
		w("- Takipçi %d, takip edilen %d, karşılıklı %d, takip edip edilmeyen %d, takipten çıkılan %d", f.Followers, f.Following, f.Mutual, f.NotFollowingBack, f.Unfollowed)
		w("- Yakın arkadaş %d, engellenen %d, kısıtlanan %d", f.CloseFriends, f.Blocked, f.Restricted)
		if a.Posts.Posts+a.Posts.Stories+a.Posts.Reels > 0 {
			w("- Paylaşım: %d gönderi, %d hikâye, %d reels", a.Posts.Posts, a.Posts.Stories, a.Posts.Reels)
		}
		if a.Logins.Total > 0 {
			w("- Giriş: %d kayıt, %d farklı IP", a.Logins.Total, a.Logins.DistinctIPs)
		}
		list := func(title string, items []analytics.Count) {
			if len(items) == 0 {
				return
			}
			w("")
			w("**%s**", title)
			w("")
			for i, c := range items {
				if i >= 10 {
					break
				}
				w("- %s — %d", esc(c.Key), c.Count)
			}
		}
		if len(a.Searched) > 0 {
			w("")
			w("**En çok aranan hesaplar**")
			w("")
			for i, s := range a.Searched {
				if i >= 10 {
					break
				}
				w("- %s — %d kez (%s – %s)", esc(s.Key), s.Count, day(s.First, loc), day(s.Last, loc))
			}
		}
		list("En çok beğenilen hesaplar", a.Liked)
		list("Yorum yapılan hesaplar", a.Commented)
		list("Hikâyesiyle etkileşilen hesaplar", a.StoryTouched)
		if len(a.ProfileChanges) > 0 {
			w("")
			w("**Profil değişiklikleri**")
			w("")
			for _, c := range a.ProfileChanges {
				w("- %s: %s → %s (%s)", esc(c.What), esc(c.From), esc(c.To), day(c.TS, loc))
			}
		}
		w("")
	}

	if len(in.Timeline) > 0 {
		w("## Zaman çizelgesi")
		w("")
		for _, it := range in.Timeline {
			line := fmt.Sprintf("- **%s** — %s", day(it.TS, loc), esc(it.Title))
			if it.Detail != "" {
				line += " (" + esc(it.Detail) + ")"
			}
			w("%s", line)
		}
		w("")
	}

	if len(in.Councils) > 0 {
		w("## Konsey yorumları (yapay zekâ)")
		w("")
		w("> Aşağıdakiler bir yapay zekâ konseyinin kanıta bağlı **hipotezleridir**; klinik tanı, tehlike değerlendirmesi ya da kesin hüküm değildir. Yukarıdaki metriklerden farklı olarak yorum içerirler; paylaşmadan önce bu bölümü çıkarmak istersen raporu konseysiz indir.")
		w("")
		for _, c := range in.Councils {
			kind := "kişi analizi"
			if c.Session.Kind == "pair" {
				kind = "iki kişilik analiz"
			}
			w("### Oturum %s — %s, %s", c.Session.CreatedAt.In(loc).Format("02.01.2006"), kind, esc(c.Session.Model))
			w("")
			councilBody(&b, c, loc, 4)
		}
	}

	w("---")
	if len(in.Councils) > 0 {
		w("Bu rapor `joe %s` ile oluşturuldu. Metrik bölümleri deterministiktir; \"Konsey yorumları\" bölümü yapay zekâ çıktısıdır.", in.Version)
	} else {
		w("Bu rapor `joe %s` ile oluşturuldu. Konsey (yapay zekâ) yorumu içermez.", in.Version)
	}
	return b.String()
}
