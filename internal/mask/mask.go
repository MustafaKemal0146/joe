// Package mask, LLM sağlayıcısına gitmeden önce kişisel tanımlayıcıları tutarlı yer
// tutuculara çevirir ([TEL-1], [EPOSTA-2]…). Aynı değer her yerde aynı yer tutucuyu alır;
// geri çözme tablosu yalnızca bellekte ve yerelde kalır.
package mask

import (
	"fmt"
	"net/netip"
	"regexp"
	"sort"
	"strings"
)

// Kind maskelenen değerin türüdür.
type Kind string

const (
	Email  Kind = "EPOSTA"
	Phone  Kind = "TEL"
	IP     Kind = "IP"
	Geo    Kind = "KONUM"
	IBAN   Kind = "IBAN"
	TCKN   Kind = "TCKN"
	Handle Kind = "HESAP"
)

// AllKinds varsayılan olarak açık olan türlerdir.
var AllKinds = []Kind{Email, Phone, IP, Geo, IBAN, TCKN, Handle}

// Masker tek bir gönderim için tutarlı maskeleme yapar. Eşzamanlı kullanılmaz.
type Masker struct {
	enabled map[Kind]bool
	byValue map[string]string // "KIND|değer" → yer tutucu
	table   map[string]string // yer tutucu → değer
	counts  map[Kind]int
}

// New belirtilen türler için bir Masker üretir; boşsa tümü açıktır.
func New(kinds ...Kind) *Masker {
	if len(kinds) == 0 {
		kinds = AllKinds
	}
	m := &Masker{enabled: map[Kind]bool{}, byValue: map[string]string{}, table: map[string]string{}, counts: map[Kind]int{}}
	for _, k := range kinds {
		m.enabled[k] = true
	}
	return m
}

func (m *Masker) placeholder(k Kind, value string) string {
	key := string(k) + "|" + value
	if p, ok := m.byValue[key]; ok {
		return p
	}
	m.counts[k]++
	p := fmt.Sprintf("[%s-%d]", k, m.counts[k])
	m.byValue[key] = p
	m.table[p] = value
	return p
}

var (
	reEmail        = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9\-]+(?:\.[A-Za-z0-9\-]+)*\.[A-Za-z]{2,}`)
	reHandle       = regexp.MustCompile(`(?i)\b((?:https?://)?(?:www\.)?(?:instagram\.com|twitter\.com|x\.com|tiktok\.com/@?|facebook\.com|github\.com|gitlab\.com)/)(?:_u/)?([A-Za-z0-9._]{2,40})`)
	reIPv4         = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	reIPv6         = regexp.MustCompile(`(?i)\b(?:[0-9a-f]{1,4}:){2,7}[0-9a-f]{1,4}\b|\b(?:[0-9a-f]{1,4}:){1,7}:(?:[0-9a-f]{1,4})?\b`)
	reGeo          = regexp.MustCompile(`[-+]?\d{1,2}\.\d{4,}\s*,\s*[-+]?\d{1,3}\.\d{4,}`)
	reIBAN         = regexp.MustCompile(`\b[A-Z]{2}\d{2}(?:\s?[A-Z0-9]{4}){3,7}(?:\s?[A-Z0-9]{1,4})?\b`)
	reTCKN         = regexp.MustCompile(`\b[1-9]\d{10}\b`)
	rePhone        = regexp.MustCompile(`(?:\+|00)?\d[\d\s().\-]{7,17}\d`)
	reDateAnywhere = regexp.MustCompile(`\d{1,4}[./\-]\d{1,2}[./\-]\d{2,4}`)
)

func digitsOf(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func tcknValid(d string) bool {
	if len(d) != 11 || d[0] == '0' {
		return false
	}
	var odd, even, sum int
	for i := 0; i < 9; i++ {
		v := int(d[i] - '0')
		sum += v
		if i%2 == 0 {
			odd += v
		} else {
			even += v
		}
	}
	d10 := (((odd*7)-even)%10 + 10) % 10
	sum += int(d[9] - '0')
	return d10 == int(d[9]-'0') && sum%10 == int(d[10]-'0')
}

func ibanValid(s string) bool {
	s = strings.ReplaceAll(s, " ", "")
	if len(s) < 15 || len(s) > 34 {
		return false
	}
	r := s[4:] + s[:4]
	rem := 0
	for _, c := range r {
		var v int
		switch {
		case c >= '0' && c <= '9':
			v = int(c - '0')
			rem = (rem*10 + v) % 97
		case c >= 'A' && c <= 'Z':
			v = int(c-'A') + 10
			rem = (rem*100 + v) % 97
		default:
			return false
		}
	}
	return rem == 1
}

func publicOrAny(s string) bool {
	_, err := netip.ParseAddr(s)
	return err == nil
}

// Apply metindeki tanımlayıcıları maskeler.
func (m *Masker) Apply(text string) string {
	if m.enabled[Email] {
		text = reEmail.ReplaceAllStringFunc(text, func(s string) string { return m.placeholder(Email, strings.ToLower(s)) })
	}
	if m.enabled[Handle] {
		text = reHandle.ReplaceAllStringFunc(text, func(s string) string {
			sub := reHandle.FindStringSubmatch(s)
			return sub[1] + m.placeholder(Handle, strings.ToLower(sub[2]))
		})
	}
	if m.enabled[Geo] {
		text = reGeo.ReplaceAllStringFunc(text, func(s string) string { return m.placeholder(Geo, strings.Join(strings.Fields(s), "")) })
	}
	if m.enabled[IP] {
		text = reIPv4.ReplaceAllStringFunc(text, func(s string) string {
			if publicOrAny(s) {
				return m.placeholder(IP, s)
			}
			return s
		})
		text = reIPv6.ReplaceAllStringFunc(text, func(s string) string {
			if publicOrAny(s) {
				return m.placeholder(IP, strings.ToLower(s))
			}
			return s
		})
	}
	if m.enabled[IBAN] {
		text = reIBAN.ReplaceAllStringFunc(text, func(s string) string {
			if ibanValid(s) {
				return m.placeholder(IBAN, strings.ReplaceAll(s, " ", ""))
			}
			return s
		})
	}
	if m.enabled[TCKN] {
		text = reTCKN.ReplaceAllStringFunc(text, func(s string) string {
			if tcknValid(s) {
				return m.placeholder(TCKN, s)
			}
			return s
		})
	}
	if m.enabled[Phone] {
		text = replaceWithContext(text, rePhone, func(s, before, after string) string {
			d := digitsOf(s)
			if len(d) < 10 || len(d) > 15 || reDateAnywhere.MatchString(s) || strings.Contains(s, ":") ||
				strings.HasPrefix(after, ":") || strings.HasSuffix(before, ":") {
				return s
			}
			// Biçimlendirilmemiş uzun sayılar (zaman damgası, kimlik) telefon sayılmaz:
			// ya ayırıcı içermeli ya da + / 0 / 90 ile başlamalı.
			plain := strings.Trim(s, " ")
			if !strings.ContainsAny(plain, " ()-.+") && !(strings.HasPrefix(d, "0") || strings.HasPrefix(d, "90") || strings.HasPrefix(d, "5")) {
				return s
			}
			lead, trail := s[:len(s)-len(strings.TrimLeft(s, " "))], s[len(strings.TrimRight(s, " ")):]
			return lead + m.placeholder(Phone, d) + trail
		})
	}
	return text
}

// Unmask yer tutucuları orijinal değerlere çevirir (yalnızca yerelde, arayüz için).
func (m *Masker) Unmask(text string) string {
	if len(m.table) == 0 {
		return text
	}
	keys := make([]string, 0, len(m.table))
	for k := range m.table {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	pairs := make([]string, 0, len(keys)*2)
	for _, k := range keys {
		pairs = append(pairs, k, m.table[k])
	}
	return strings.NewReplacer(pairs...).Replace(text)
}

// Report hangi türden kaç farklı değerin maskelendiğini döndürür.
func (m *Masker) Report() map[Kind]int {
	out := map[Kind]int{}
	for k, v := range m.counts {
		out[k] = v
	}
	return out
}

// replaceWithContext eşleşmenin önündeki ve sonundaki karakteri de görerek değiştirir.
func replaceWithContext(text string, re *regexp.Regexp, f func(match, before, after string) string) string {
	idx := re.FindAllStringIndex(text, -1)
	if len(idx) == 0 {
		return text
	}
	var b strings.Builder
	last := 0
	for _, ix := range idx {
		b.WriteString(text[last:ix[0]])
		b.WriteString(f(text[ix[0]:ix[1]], text[:ix[0]], text[ix[1]:]))
		last = ix[1]
	}
	b.WriteString(text[last:])
	return b.String()
}
