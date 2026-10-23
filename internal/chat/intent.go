package chat

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

type person struct {
	id   int64
	name string
	self bool
}

func people(env Env) []person {
	var out []person
	for _, p := range env.ListPeople() {
		id, err := strconv.ParseInt(fmt.Sprint(p["id"]), 10, 64)
		if err != nil {
			continue
		}
		self, _ := p["is_self"].(bool)
		out = append(out, person{id, fmt.Sprint(p["name"]), self})
	}
	return out
}

func words(s string) []string {
	return strings.FieldsFunc(fold(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

// mentioned metinde adı geçen kişileri döndürür. Türkçe ek alan biçimleri ("Elif'i", "Elifle") için
// sözcük, adın ilk sözcüğüyle başlıyorsa eşleşir (en az 3 harf).
func mentioned(text string, ps []person) []person {
	ws := words(text)
	var out []person
	for _, p := range ps {
		if p.self {
			continue
		}
		toks := words(p.name)
		if len(toks) == 0 {
			continue
		}
		first := toks[0]
		for _, w := range ws {
			if w == first || (len([]rune(first)) >= 3 && strings.HasPrefix(w, first)) {
				out = append(out, p)
				break
			}
		}
	}
	return out
}

// RouteIntent "Elif'i analiz et", "ben ve Elif uyumu", "dosya ekle" gibi yaygın niyetleri LLM'siz
// karşılar. Analiz BURADA başlamaz: yalnızca hazırlanmış konsey sayfasına bağlantı verilir, maliyet
// onayı o sayfada alınır. Belirsizlikte (birden çok eşleşme) kullanıcıya sorar, tahmin etmez.
func RouteIntent(text string, env Env) (Reply, bool) {
	t := fold(text)
	if n := len([]rune(t)); n < 4 || n > 70 {
		return Reply{}, false
	}
	switch {
	case has(t, "dosya ekle", "veri yukle", "veri ekle", "dosya yukle", "sihirbaz", "kurulum", "sohbet ekle", "whatsapp ekle", "instagram ekle", "iceri aktar"):
		return Reply{Text: "Kurulum sihirbazını açıyorum: dosyanı sürükleyip bırakabilirsin; kimin verisi olduğunu ve kimi analiz edeceğini sırayla soracağım.", Nav: &Nav{"Sihirbazı aç", "/joe?sihirbaz=1"}}, true
	}
	analyse := has(t, "analiz", "incele", "uyum", "konsey")
	if !analyse {
		return Reply{}, false
	}
	ps := people(env)
	found := mentioned(text, ps)
	var self *person
	for i := range ps {
		if ps[i].self {
			self = &ps[i]
		}
	}
	switch len(found) {
	case 0:
		return Reply{}, false // ad yok: model sorar ya da genel yönlendirme çalışır
	case 1:
		p := found[0]
		pair := self != nil && (has(t, "ben ve ", "ben ile ", "ben ", "benimle", "ikimiz", "uyum", "iliski") && !has(t, "benim analiz"))
		if pair {
			return Reply{Text: fmt.Sprintf("%s ile senin ilişki analizini hazırladım. Maliyeti ve modele gidecek metni konsey sayfasında görüp onaylayacaksın; onaylamadan hiçbir şey gönderilmez.", p.name),
				Nav: &Nav{fmt.Sprintf("%s × %s analizini hazırla", self.name, p.name), fmt.Sprintf("/konsey?kisi=%d&ikinci=%d&tur=pair", self.id, p.id)}}, true
		}
		return Reply{Text: fmt.Sprintf("%s için analizi hazırladım. Maliyeti konsey sayfasında görüp onaylayacaksın; analiz sen onaylamadan başlamaz.", p.name),
			Nav: &Nav{fmt.Sprintf("%s analizini hazırla", p.name), fmt.Sprintf("/konsey?kisi=%d", p.id)}}, true
	default:
		var names []string
		for _, p := range found {
			names = append(names, fmt.Sprintf("#%d %s", p.id, p.name))
		}
		return Reply{Text: "Birden fazla kişi eşleşti, hangisini kastettin? " + strings.Join(names, ", ") + ". Tam adıyla yazarsan hazırlarım."}, true
	}
}
