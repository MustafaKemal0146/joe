package council

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/MustafaKemal0146/joe/internal/audit"
	"github.com/MustafaKemal0146/joe/internal/personas"
)

var reItemHead = regexp.MustCompile(`(?m)^## (K\d+) — `)

// kBlocks paket metnini "## K<n> — …" bölümlerine ayırır: kimlik → bölüm metni.
func kBlocks(packText string) map[string]string {
	locs := reItemHead.FindAllStringSubmatchIndex(packText, -1)
	out := make(map[string]string, len(locs))
	for i, l := range locs {
		end := len(packText)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		out[packText[l[2]:l[3]]] = packText[l[0]:end]
	}
	return out
}

// normQuote karşılaştırma için metni sadeleştirir: Türkçe uyumlu küçük harf, harf/rakam dışındakiler boşluk.
func normQuote(s string) string {
	s = strings.NewReplacer("İ", "i", "I", "ı").Replace(s)
	var b strings.Builder
	space := true
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			space = false
		} else if !space {
			b.WriteByte(' ')
			space = true
		}
	}
	return strings.TrimSpace(b.String())
}

var reEllipsis = regexp.MustCompile(`…|\.\.\.|\[\.\.\.\]|\(\.\.\.\)`)

// quoteParts alıntıyı üç noktayla bölünmüş parçalara ayırır; en az iki sözcüklü parçalar doğrulanır.
func quoteParts(q string) []string {
	var out []string
	for _, part := range reEllipsis.Split(q, -1) {
		n := normQuote(part)
		if len(strings.Fields(n)) >= 2 {
			out = append(out, n)
		}
	}
	return out
}

// quoteChecker bir paketin alıntı doğrulayıcısıdır; paket bir kez normalleştirilir.
type quoteChecker struct {
	blocks map[string]string // kimlik → normalleştirilmiş bölüm
	all    string
}

func newQuoteChecker(packText string) *quoteChecker {
	q := &quoteChecker{blocks: map[string]string{}}
	var all []string
	for id, txt := range kBlocks(packText) {
		n := normQuote(txt)
		q.blocks[id] = n
		all = append(all, n)
	}
	q.all = strings.Join(all, " | ")
	return q
}

// check alıntının durumunu döndürür: "ok", "elsewhere" (pakette var ama atıf yapılan kesitte değil),
// "missing" (pakette yok) ya da "none" (doğrulanacak parça yok: boş ya da çok kısa).
func (q *quoteChecker) check(quote string, evidence []string) string {
	parts := quoteParts(quote)
	if len(parts) == 0 {
		return "none"
	}
	status := "ok"
	for _, p := range parts {
		inCited := false
		for _, id := range evidence {
			if strings.Contains(q.blocks[id], p) {
				inCited = true
				break
			}
		}
		switch {
		case inCited:
		case strings.Contains(q.all, p):
			if status == "ok" {
				status = "elsewhere"
			}
		default:
			return "missing"
		}
	}
	return status
}

// quoteViolations persona çıktısındaki alıntıları paketle karşılaştırır. Uydurma alıntı çıktıyı geçersiz kılmaz;
// ekranda uyarı olarak görünür. Gözlemlerin yarısından fazlasında alıntı yoksa tek bir özet uyarı eklenir.
func quoteViolations(o personas.Output, packText string) []audit.Violation {
	if o.Abstain {
		return nil
	}
	q := newQuoteChecker(packText)
	var vs []audit.Violation
	noQuote := 0
	for i, ob := range o.Observations {
		if strings.TrimSpace(ob.Quote) == "" {
			noQuote++
			continue
		}
		switch q.check(ob.Quote, ob.Evidence) {
		case "missing":
			vs = append(vs, audit.Violation{Rule: audit.RuleQuoteMissing, Claim: i, Match: ob.Quote,
				Message: fmt.Sprintf("Gözlem %d: alıntı kanıt paketinde bulunamadı; bu gözlemi temkinle oku.", i+1)})
		case "elsewhere":
			vs = append(vs, audit.Violation{Rule: audit.RuleQuoteElsewhere, Claim: i, Match: ob.Quote,
				Message: fmt.Sprintf("Gözlem %d: alıntı pakette var ama atıf yapılan kesitte değil.", i+1)})
		}
	}
	for i, h := range o.Hypotheses {
		if strings.TrimSpace(h.Quote) == "" {
			continue
		}
		if q.check(h.Quote, h.Evidence) == "missing" {
			vs = append(vs, audit.Violation{Rule: audit.RuleQuoteMissing, Claim: len(o.Observations) + i, Match: h.Quote,
				Message: fmt.Sprintf("Hipotez %d: alıntı kanıt paketinde bulunamadı.", i+1)})
		}
	}
	if n := len(o.Observations); n > 0 && noQuote*2 > n {
		vs = append(vs, audit.Violation{Rule: audit.RuleNoQuotes, Claim: -1,
			Message: fmt.Sprintf("%d gözlemden %d'inde alıntı yok; bu gözlemler ölçüme dayanıyor olabilir ama doğrudan kesite bağlanmamış.", n, noQuote)})
	}
	return vs
}
