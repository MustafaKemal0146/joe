package evidence

import (
	"strings"
	"unicode"
)

var stopwords = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`neden niye nasıl hangi kimin kime kimi nedir midir miydi için gibi daha çok kadar şimdi sonra önce olarak olan olur oldu olmuş var yok mesaj mesajlar mesajı yazıyor yazdı yazar konuşuyor konuşma ilişki ilişkide ilişkinin kişi kişinin bunu şunu onun onlar ama fakat ancak veya hem ise bile ben sen biz siz bana sana bence sence acaba belki şey şeyler gerçekten genelde zaman zamanlar sohbet`) {
		stopwords[w] = true
	}
}

func lowerTR(s string) string {
	return strings.ToLower(strings.NewReplacer("İ", "i", "I", "ı").Replace(s))
}

// QueryTerms bir doğal dil sorusundan arama için anahtar sözcük kökleri çıkarır. Uzun sözcükler
// kısaltılır ki Türkçe ekler ("geceleri", "gecelerde") aynı kökle eşleşsin. En çok 6 kök döner.
func QueryTerms(q string) []string {
	var out []string
	seen := map[string]bool{}
	for _, w := range strings.FieldsFunc(lowerTR(q), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		r := []rune(w)
		if len(r) < 4 || filler(w) {
			continue
		}
		switch {
		case len(r) >= 7:
			r = r[:4]
		case len(r) == 6:
			r = r[:5]
		}
		t := string(r)
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
		if len(out) == 6 {
			break
		}
	}
	return out
}

// filler sözcüğün (ekli biçimleriyle) dolgu sözcük olup olmadığını söyler.
func filler(w string) bool {
	if stopwords[w] {
		return true
	}
	for sw := range stopwords {
		if len([]rune(sw)) >= 4 && strings.HasPrefix(w, sw) {
			return true
		}
	}
	return false
}
