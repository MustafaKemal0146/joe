package analytics

import (
	"sort"
	"strings"
	"unicode"
)

var stop = func() map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(`ve ile de da ki bu şu o bir ben sen biz siz onlar için ama fakat çok daha en gibi kadar ne mi mı mu mü var yok değil evet hayır tamam ya yani şey hem veya ise olarak olan oldu olur ol bi bir iki üç ben bana beni benim sana seni senin ona onu onun bize bizi sizi bunu bunun şunu burada orada şimdi sonra önce hala hâlâ artık zaten sadece bile hiç her bütün tüm diye nasıl neden niye nerede kim hangi
the and for you your are was were with that this have has not but they them there their what when who will would can could just like get got its it's i'm you're don't can't me my we our he she him her his hers is am be been to of in on at as by or if so do did does no yes ok okay lol`) {
		m[w] = true
	}
	return m
}()

var trLower = strings.NewReplacer("İ", "i", "I", "ı")

func lowerTR(s string) string { return strings.ToLower(trLower.Replace(s)) }

// tokens bir metni küçük harfli sözcüklere böler; bağlantılar ve çok kısa sözcükler atılır.
func tokens(text string) []string {
	var out []string
	for _, f := range strings.Fields(text) {
		l := strings.ToLower(f)
		if strings.HasPrefix(l, "http") || strings.Contains(l, "www.") {
			continue
		}
		w := strings.TrimFunc(lowerTR(f), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
		if i := strings.IndexAny(w, "'’"); i > 0 {
			w = w[:i]
		}
		if len([]rune(w)) < 3 || stop[w] {
			continue
		}
		out = append(out, w)
	}
	return out
}

func isEmoji(r rune) bool {
	switch {
	case r >= 0x1F300 && r <= 0x1FAFF, r >= 0x1F000 && r <= 0x1F2FF:
		return true
	case r >= 0x2600 && r <= 0x27BF, r >= 0x2B00 && r <= 0x2BFF:
		return true
	case r == 0x203C || r == 0x2049 || r == 0x2764 || r == 0x2B50:
		return true
	}
	return false
}

// Count bir değer ve sayısıdır.
type Count struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
}

func top(m map[string]int, n int) []Count {
	out := make([]Count, 0, len(m))
	for k, v := range m {
		out = append(out, Count{k, v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Key < out[j].Key
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}

func cosine(a, b []float64) float64 {
	var dot, na, nb float64
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (sqrt(na) * sqrt(nb))
}

func sqrt(x float64) float64 {
	if x <= 0 {
		return 0
	}
	z := x
	for i := 0; i < 40; i++ {
		z = (z + x/z) / 2
	}
	return z
}

func median(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	if len(s)%2 == 1 {
		return s[len(s)/2]
	}
	return (s[len(s)/2-1] + s[len(s)/2]) / 2
}

func percentile(v []float64, p float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	i := int(p * float64(len(s)-1))
	return s[i]
}

func ratio(a, b float64) float64 {
	if a <= 0 && b <= 0 {
		return 0
	}
	if a > b {
		a, b = b, a
	}
	if b == 0 {
		return 0
	}
	return a / b
}

func jaccard(a, b map[string]bool) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 0
	}
	inter := 0
	for k := range a {
		if b[k] {
			inter++
		}
	}
	return float64(inter) / float64(len(a)+len(b)-inter)
}

// Tokens bir metnin anlamlı (bağlaç ve çok kısa sözcükleri atılmış) küçük harfli sözcüklerini döndürür.
func Tokens(text string) []string { return tokens(text) }
