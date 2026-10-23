// Package audit, konsey ve sohbet çıktılarını deterministik kurallarla denetler: geçersiz kanıt
// kimliği, kanıtsız iddia, klinik teşhis dili ve tehlikelilik/suçluluk çıkarımı. Model çıktısı
// bu denetimden geçmeden "geçerli" sayılmaz; ihlaller gizlenmez, işaretlenir.
package audit

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/MustafaKemal0146/joe/internal/rx"
)

// Rule ihlalin türüdür.
type Rule string

const (
	RuleUnknownEvidence Rule = "gecersiz_kanit"       // olmayan K* kimliği
	RuleNoEvidence      Rule = "kanitsiz_iddia"       // hiç kanıt referansı yok
	RuleDiagnosis       Rule = "teshis_dili"          // klinik tanı / bozukluk etiketi
	RuleDanger          Rule = "tehlike_suclama"      // tehlikelilik, suçluluk, suç çıkarımı
	RuleCertainty       Rule = "kesinlik_dili"        // değişmez gerçek gibi sunma
	RuleLostDivergence  Rule = "ayrisma_kayboldu"     // moderatör ayrışma buldu, sentez bunu hiç yansıtmadı
	RuleShortOverview   Rule = "kisa_degerlendirme"   // genel değerlendirme istenen uzunluktan kısa
	RuleQuoteMissing    Rule = "alinti_bulunamadi"    // alıntı kanıt paketinde hiç geçmiyor
	RuleQuoteElsewhere  Rule = "alinti_baska_kesitte" // alıntı pakette var ama atıf yapılan kesitte değil
	RulePruned          Rule = "ayiklandi"            // geçersiz maddeler ayıklandı, kalanı kabul edildi
	RuleNoQuotes        Rule = "alintisiz"            // gözlemlerin çoğunda alıntı yok
)

// Claim bir çıktıdaki tek bir iddiadır.
type Claim struct {
	Text     string   `json:"text"`
	Evidence []string `json:"evidence"`
	// Observation true ise iddia doğrudan gözlemdir; kanıt zorunludur. Kuramsal yorumda da
	// kanıt aranır, ancak yalnızca kanıtsız olması uyarıdır.
	Observation bool `json:"observation"`
}

// Violation tek bir ihlaldir.
type Violation struct {
	Rule    Rule   `json:"rule"`
	Claim   int    `json:"claim"` // iddia sırası; -1 serbest metin
	Message string `json:"message"`
	Match   string `json:"match,omitempty"`
}

// Report denetim sonucudur.
type Report struct {
	Violations []Violation `json:"violations"`
	// Cited çıktıda geçen geçerli kanıt kimlikleridir (sıralı).
	Cited []string `json:"cited"`
}

// OK ihlal yoksa true döner.
func (r Report) OK() bool { return len(r.Violations) == 0 }

// Has belirtilen kuralın ihlali var mı?
func (r Report) Has(rule Rule) bool {
	for _, v := range r.Violations {
		if v.Rule == rule {
			return true
		}
	}
	return false
}

var evidenceRx = regexp.MustCompile(`\bK(\d{1,5})\b`)

// ExtractEvidence metinden K<n> kimliklerini çıkarır.
func ExtractEvidence(text string) []string {
	var out []string
	for _, m := range evidenceRx.FindAllString(text, -1) {
		out = append(out, m)
	}
	return out
}

// Türkçe ve İngilizce kalıplar. Kuramsal çerçevenin kendi terimleri ("narsisistik savunma")
// yasak değildir; yasak olan, kişiye klinik bozukluk etiketi yapıştırmak ve tehlikelilik ya da
// suçluluk çıkarmaktır.
var diagnosisPatterns = compile(
	`\b(?:teşhis|tanı)\s+(?:koy(?:du|duk|ar|uyor|uyoruz|abilir|abilirim)|konul(?:du|ur|abilir))\b`,
	`\b(?:klinik\s+)?tanısı\s+(?:var|vardır|koyulabilir)`,
	`\b(?:borderline|sınırda)\s+kişilik\s+bozukluğ\w*`,
	`\b(?:narsisistik|antisosyal|paranoid|şizoid|şizotipal|histriyonik|kaçıngan|obsesif[\s-]kompulsif|bağımlı|şizoid)\s+kişilik\s+bozukluğ\w*`,
	`\bşizofreni\w*\b`,
	`\bbipolar\s+(?:bozukluk|hastası|hastalığ)\w*`,
	`\b(?:major\s+)?depresyon(?:da|dadır|u var|a sahip)\b`,
	`\b(?:kişilik|duygudurum|anksiyete|travma|stres\s+sonrası)\s+bozukluğ\w*\s+(?:var|vardır|olduğu|geliştir)`,
	`\b(?:diagnos(?:e|is|ed)|clinically\s+(?:depressed|ill))\b`,
	`\b(?:has|suffers\s+from|is\s+a)\s+(?:a\s+)?(?:borderline|narcissistic|antisocial|paranoid|bipolar|schizophren)\w*`,
	`\bpersonality\s+disorder\b`,
	`\b(?:psikopat|sosyopat|psychopath|sociopath)\w*\b`,
)

var dangerPatterns = compile(
	`\b(?:tehlikeli|tehlike\s+arz|şiddete\s+(?:meyilli|yatkın)|şiddet\s+uygulayacak|zarar\s+verecek)\w*`,
	`\b(?:suçlu|suç\s+işle(?:miş|yecek|r)|suç\s+eğilim)\w*`,
	`\b(?:istismarcı|manipülatör|tacizci|stalker|takipçi\s+obsesyon)\w*`,
	`\b(?:dangerous|violent|abusive|criminal|guilty|will\s+(?:harm|hurt|attack))\b`,
	`\b(?:işe|krediye|güvene)\s+(?:uygun|elverişli)\s*(?:değil|dir|olmayan)\w*`,
	`\bsiyasi\s+(?:görüş|tercih|eğilim)\w*\s+(?:muhtemelen|büyük\s+olasılıkla|tahmin)`,
)

var certaintyPatterns = compile(
	`\bkesinlikle\s+(?:bir|böyle|öyle|\w+\s+biri)\b`,
	`\bdeğişmez\s+(?:kişiliği|yapısı|doğası)\w*`,
	`\bher\s+zaman\s+böyle(?:dir)?\b`,
	`\bhiçbir\s+zaman\s+değişmez\b`,
	`\b(?:definitely|always\s+been|will\s+never\s+change)\b`,
	`\b(?:gerçek\s+kişiliği|asıl\s+kimliği)\s+(?:budur|şudur)`,
)

func compile(pats ...string) []*regexp.Regexp {
	out := make([]*regexp.Regexp, len(pats))
	for i, p := range pats {
		out[i] = rx.Compile(p)
	}
	return out
}

// negated, eşleşmenin hemen öncesinde olumsuzlama ya da çerçeveleme geçiyorsa true döner:
// "teşhis koymuyorum", "tanı koymadan", "klinik tanı değildir" gibi sınırlayıcı ifadeler ihlal değildir.
var negators = rx.Compile(`(?:\b(?:değil|değildir|yok|olmadan|olmaksızın|koymadan|koymuyorum|koymam|asla|never|not|no|without|cannot|can't|don't|doesn't|isn't)\b|\b(?:tanı|teşhis)\s+(?:koymadan|değil))`)

// negatedNear eşleşmenin kendisi hariç, çevresindeki pencerede olumsuzlama arar. Eşleşmenin
// içindeki olumsuzlama sözcüğü ("işe uygun değil") ihlali ortadan kaldırmaz.
func negatedNear(text string, start, end int) bool {
	lo := start - 40
	if lo < 0 {
		lo = 0
	}
	hi := end + 60
	if hi > len(text) {
		hi = len(text)
	}
	return negators.MatchString(text[lo:start]) || negators.MatchString(text[end:hi])
}

// fold, Go'nun büyük/küçük harf eşlemesinin bilmediği "İ" harfini "i"ye indirger ki
// "İşe uygun değil" gibi büyük harfle başlayan cümleler de eşleşsin.
func fold(s string) string { return strings.ToLower(strings.ReplaceAll(s, "İ", "i")) }

func scan(text string, pats []*regexp.Regexp) (string, bool) {
	text = fold(text)
	for _, re := range pats {
		for _, ix := range re.FindAllStringIndex(text, -1) {
			if negatedNear(text, ix[0], ix[1]) {
				continue
			}
			return text[ix[0]:ix[1]], true
		}
	}
	return "", false
}

// Check iddiaları ve serbest metni denetler. valid, pakette bulunan geçerli K* kimlikleridir.
func Check(claims []Claim, free string, valid map[string]bool) Report {
	var rep Report
	cited := map[string]bool{}

	flagText := func(idx int, text string) {
		if m, ok := scan(text, diagnosisPatterns); ok {
			rep.Violations = append(rep.Violations, Violation{RuleDiagnosis, idx, "Klinik tanı ya da bozukluk etiketi içeriyor.", m})
		}
		if m, ok := scan(text, dangerPatterns); ok {
			rep.Violations = append(rep.Violations, Violation{RuleDanger, idx, "Tehlikelilik, suçluluk ya da uygunluk çıkarımı içeriyor.", m})
		}
		if m, ok := scan(text, certaintyPatterns); ok {
			rep.Violations = append(rep.Violations, Violation{RuleCertainty, idx, "Yorumu değişmez gerçek gibi sunuyor.", m})
		}
		for _, id := range ExtractEvidence(text) {
			if !valid[id] {
				rep.Violations = append(rep.Violations, Violation{RuleUnknownEvidence, idx, fmt.Sprintf("Metinde olmayan kanıt kimliği: %s", id), id})
			} else {
				cited[id] = true
			}
		}
	}

	for i, c := range claims {
		text := strings.TrimSpace(c.Text)
		if text == "" {
			continue
		}
		if len(c.Evidence) == 0 && len(ExtractEvidence(text)) == 0 {
			msg := "İddiaya hiçbir kanıt bağlanmamış."
			if !c.Observation {
				msg = "Kuramsal yoruma kanıt bağlanmamış."
			}
			rep.Violations = append(rep.Violations, Violation{RuleNoEvidence, i, msg, ""})
		}
		for _, id := range c.Evidence {
			if !valid[id] {
				rep.Violations = append(rep.Violations, Violation{RuleUnknownEvidence, i, fmt.Sprintf("Metinde olmayan kanıt kimliği: %s", id), id})
			} else {
				cited[id] = true
			}
		}
		flagText(i, text)
	}
	if strings.TrimSpace(free) != "" {
		flagText(-1, free)
	}
	for id := range cited {
		rep.Cited = append(rep.Cited, id)
	}
	sort.Slice(rep.Cited, func(i, j int) bool { return evidenceNum(rep.Cited[i]) < evidenceNum(rep.Cited[j]) })
	return rep
}

func evidenceNum(id string) int {
	n := 0
	for _, r := range id {
		if unicode.IsDigit(r) {
			n = n*10 + int(r-'0')
		}
	}
	return n
}

// ValidIDs 1..n için K<n> kümesi üretir (paket kurulumunda yardımcı).
func ValidIDs(n int) map[string]bool {
	m := make(map[string]bool, n)
	for i := 1; i <= n; i++ {
		m[fmt.Sprintf("K%d", i)] = true
	}
	return m
}
