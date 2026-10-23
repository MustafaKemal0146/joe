package chat

import (
	"fmt"
	"strings"
)

// Reply deterministik yönlendirmenin sonucudur: LLM çağrısı yapılmaz, maliyet sıfırdır.
type Reply struct {
	Text string
	Nav  *Nav
}

func fold(s string) string {
	r := strings.NewReplacer("İ", "i", "I", "ı", "ı", "i", "ş", "s", "ğ", "g", "ü", "u", "ö", "o", "ç", "c", "Ş", "s", "Ğ", "g", "Ü", "u", "Ö", "o", "Ç", "c")
	return strings.TrimSpace(strings.ToLower(r.Replace(s)))
}

func has(s string, subs ...string) bool {
	for _, x := range subs {
		if strings.Contains(s, x) {
			return true
		}
	}
	return false
}

// Help yardım metnidir.
const Help = `Şunları yapabilirim:
• Kişi dosyalarını, kaynakları ve harcamayı listelemek
• Bir kişinin metriklerini özetlemek, iki kişiyi karşılaştırmak
• Kişi dosyası açmak, kimlik eklemek, hesap sahibini belirlemek, not almak (hepsi için önce onayını isterim)
• Konsey analizini hazırlamak (maliyeti gösterip onayını alırım; analiz sen onaylamadan başlamaz)

Kısa komutlar para harcamaz: "kişiler", "kaynaklar", "harcama", "harita", "karşılaştır", "ayarlar", "konsey".`

// Route kısa, yaygın komutları LLM'siz karşılar. Eşleşme yoksa ok=false döner.
// Bütçe dolsa bile bu komutlar çalışmaya devam eder.
func Route(text string, env Env, spent, cap float64) (Reply, bool) {
	t := fold(text)
	if len([]rune(t)) > 60 { // uzun cümleler model işidir
		return Reply{}, false
	}
	switch {
	case has(t, "yardim", "ne yapabilirsin", "neler yapabilirsin"):
		return Reply{Text: Help}, true
	case t == "kisiler" || has(t, "kimler var", "kisi listesi", "kisileri goster", "kisileri listele"):
		ps := env.ListPeople()
		if len(ps) == 0 {
			return Reply{Text: "Henüz kişi dosyası yok. Bir kaynak içe aktar ya da bana \"X için kişi dosyası aç\" de.", Nav: &Nav{"Kaynaklara git", "/kaynaklar"}}, true
		}
		var b strings.Builder
		fmt.Fprintf(&b, "%d kişi dosyası var:\n", len(ps))
		for _, p := range ps {
			me := ""
			if s, _ := p["is_self"].(bool); s {
				me = " (ben)"
			}
			fmt.Fprintf(&b, "• #%v %v%s\n", p["id"], p["name"], me)
		}
		return Reply{Text: strings.TrimRight(b.String(), "\n"), Nav: &Nav{"Kişileri aç", "/kisiler"}}, true
	case t == "kaynaklar" || has(t, "hangi veriler", "kaynaklari goster", "kaynak listesi"):
		ss := env.ListSources()
		if len(ss) == 0 {
			return Reply{Text: "Henüz kaynak yok. Instagram ZIP'ini ya da WhatsApp sohbetini Kaynaklar sayfasından ekleyebilirsin.", Nav: &Nav{"Kaynaklara git", "/kaynaklar"}}, true
		}
		var b strings.Builder
		fmt.Fprintf(&b, "%d kaynak var:\n", len(ss))
		for _, s := range ss {
			fmt.Fprintf(&b, "• #%v %v — sahip: %v, %v mesaj\n", s["id"], s["platform"], s["owner"], s["messages"])
		}
		return Reply{Text: strings.TrimRight(b.String(), "\n"), Nav: &Nav{"Kaynakları aç", "/kaynaklar"}}, true
	case has(t, "harcama", "ne kadar harcadim", "maliyet", "butce"):
		return Reply{Text: fmt.Sprintf("Bu sohbette şimdiye kadar $%.4f harcandı (tavan $%.2f). Toplam harcamayı Ayarlar'da görebilirsin.", spent, cap), Nav: &Nav{"Ayarlar", "/ayarlar"}}, true
	case has(t, "harita"):
		return Reply{Text: "İlişki haritası burada:", Nav: &Nav{"Haritayı aç", "/harita"}}, true
	case t == "karsilastir" || has(t, "karsilastirma"):
		return Reply{Text: "Karşılaştırma sayfası burada:", Nav: &Nav{"Karşılaştır", "/karsilastir"}}, true
	case t == "ayarlar":
		return Reply{Text: "Ayarlar burada:", Nav: &Nav{"Ayarlar", "/ayarlar"}}, true
	case t == "konsey" || has(t, "psikolog"):
		return Reply{Text: "Konsey sayfasında kişiyi seçip önce maliyeti ve gönderilecek metni görür, sonra onaylarsın:", Nav: &Nav{"Konsey", "/konsey"}}, true
	}
	return Reply{}, false
}
