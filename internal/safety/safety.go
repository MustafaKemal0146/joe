// Package safety, metinde kendine zarar ya da başkasına zarar ifadelerini deterministik
// kalıplarla tespit eder ve sorumlu, teşhis koymayan yanıt metinleri sağlar. Tespit bir
// değerlendirme ya da tanı değildir: yalnızca "bu ifade geçiyor, dikkatle ele al" işaretidir.
//
// Kalıplar bilerek dar tutulur ("ölüyorum gülmekten" gibi deyimler işaretlenmez); kaçırmak
// ile yanlış alarm arasındaki dengede, uyarı ucuz olduğu için kaçırmama yönünde ayarlanmıştır.
package safety

import (
	"regexp"
	"strings"

	"github.com/MustafaKemal0146/joe/internal/rx"
)

// Level risk düzeyidir.
type Level int

const (
	None    Level = iota
	Concern       // düşünce/istek ifadesi
	Acute         // niyet ve/veya zaman/yöntem ifadesi
)

func (l Level) String() string {
	switch l {
	case Concern:
		return "concern"
	case Acute:
		return "acute"
	}
	return "none"
}

// Category riskin türüdür.
const (
	SelfHarm = "self_harm"
	Violence = "violence_to_others"
)

// Result tespit sonucudur.
type Result struct {
	Level      Level    `json:"level"`
	Categories []string `json:"categories"`
}

type rule struct {
	cat   string
	level Level
	rx    *regexp.Regexp
}

func r(cat string, lvl Level, pat string) rule {
	return rule{cat, lvl, rx.Compile(pat)}
}

var rules = []rule{
	// Kendine zarar — niyet / yöntem / zaman (acil)
	r(SelfHarm, Acute, `\bkendimi\s+öldür(?:eceğim|ecem|mek\s+üzereyim|düm|ürüm|eyim|ücem)\b`),
	r(SelfHarm, Acute, `\bintihar\s+(?:edeceğim|edecem|edicem|etmeye\s+karar|planı|mektubu)`),
	r(SelfHarm, Acute, `\b(?:bu\s+gece|bugün|yarın)\s+(?:kendimi\s+)?(?:öl(?:eceğim|ecem)|intihar)`),
	r(SelfHarm, Acute, `\bhaplar(?:ı|ı\s+içtim|ı\s+yuttum)\b.{0,40}\b(?:ölmek|bitir)`),
	r(SelfHarm, Acute, `\b(?:i(?:'| a)?m\s+going\s+to|i\s+will|i(?:'| a)?m\s+about\s+to)\s+(?:kill\s+myself|end\s+(?:my\s+life|it\s+all))\b`),
	r(SelfHarm, Acute, `\bsuicide\s+(?:note|plan|tonight)\b`),
	// Kendine zarar — düşünce / istek (endişe)
	r(SelfHarm, Concern, `\b(?:ölmek|ölüp\s+gitmek|yok\s+olmak)\s+istiyorum\b`),
	r(SelfHarm, Concern, `\bya?şamak\s+istemiyorum\b`),
	r(SelfHarm, Concern, `\bhayatıma\s+son\s+ver(?:mek|eceğim|sem)\b`),
	r(SelfHarm, Concern, `\bkendime\s+zarar\s+ver(?:mek|iyorum|diyorum|eceğim|ebilirim)\b`),
	r(SelfHarm, Concern, `\bintihar(?:ı|\s+etmeyi|\s+etmek)?\s+(?:düşünüyorum|aklımdan|geçiyor|istiyorum)\b`),
	r(SelfHarm, Concern, `\b(?:kendimi\s+)?kes(?:iyorum|tim)\s+kendimi\b`),
	r(SelfHarm, Concern, `\bher\s+şey(?:i)?\s+bitirmek\s+istiyorum\b`),
	r(SelfHarm, Concern, `\b(?:i\s+)?want\s+to\s+die\b`),
	r(SelfHarm, Concern, `\bkill\s+myself\b`),
	r(SelfHarm, Concern, `\b(?:hurt|harm|cut)\s+myself\b`),
	r(SelfHarm, Concern, `\bend\s+my\s+life\b`),
	r(SelfHarm, Concern, `\bsuicidal\b`),
	r(SelfHarm, Concern, `\bdon'?t\s+want\s+to\s+(?:live|be\s+alive)\b`),
	// Başkasına zarar
	r(Violence, Acute, `\b(?:seni|onu|hepinizi|onları)\s+öldüreceğim\b`),
	r(Violence, Acute, `\b(?:seni|onu)\s+(?:gebertece|öldürecem|öldüricem)\w*`),
	r(Violence, Concern, `\b(?:sana|ona)\s+zarar\s+vereceğim\b`),
	r(Violence, Concern, `\b(?:seni|onu)\s+(?:döveceğim|bulup\s+(?:döv|öldür))\w*`),
	r(Violence, Acute, `\bi(?:'| a)?m\s+going\s+to\s+kill\s+(?:you|him|her|them)\b`),
	r(Violence, Concern, `\bi(?:'| wi)ll\s+(?:hurt|beat|get)\s+(?:you|him|her|them)\b`),
}

var negation = rx.Compile(`\b(?:değil|değilim|asla|hiç(?:bir\s+zaman)?|olmaz|yapmam|yapmayacağım|etmeyeceğim|düşünmüyorum|istemiyorum\s+ki|never|not|don'?t|won'?t|no\s+longer|would\s+never)\b`)

// quoted, örnek ya da alıntı gibi görünüyorsa (tırnak içinde) işaretlenmez; "film replikleri" gibi.
func negatedOrIdiom(low string, start, end int) bool {
	lo := start - 25
	if lo < 0 {
		lo = 0
	}
	hi := end + 25
	if hi > len(low) {
		hi = len(low)
	}
	return negation.MatchString(low[lo:start]) || negation.MatchString(low[end:hi])
}

func fold(s string) string { return strings.ToLower(strings.ReplaceAll(s, "İ", "i")) }

// Detect metni tarar. Boş sonuç "risk ifadesi bulunmadı" demektir, "risk yok" demek değildir.
func Detect(text string) Result {
	low := fold(text)
	var res Result
	seen := map[string]bool{}
	for _, ru := range rules {
		for _, ix := range ru.rx.FindAllStringIndex(low, -1) {
			if negatedOrIdiom(low, ix[0], ix[1]) {
				continue
			}
			if ru.level > res.Level {
				res.Level = ru.level
			}
			if !seen[ru.cat] {
				seen[ru.cat] = true
				res.Categories = append(res.Categories, ru.cat)
			}
		}
	}
	return res
}

// Has kategori sonuçta var mı?
func (r Result) Has(cat string) bool {
	for _, c := range r.Categories {
		if c == cat {
			return true
		}
	}
	return false
}

// UserResponse kullanıcının kendi mesajında kendine zarar işareti varsa gösterilecek yanıttır:
// kısa, sıcak, teşhissiz, acil durumda 112'ye yönlendiren. Yalnızca 112 (Türkiye acil çağrı
// numarası) yazılır; başka hat numaraları resmî kaynaktan doğrulanmadan eklenmez.
func UserResponse(l Level) string {
	switch l {
	case Acute:
		return "Yazdıkların beni gerçekten önemsetti ve yalnız olmanı istemiyorum. Şu an kendine zarar verme riskin varsa lütfen hemen **112**'yi ara ya da en yakın acil servise git; mümkünse yanına güvendiğin birini çağır. Ben bir uzman değilim ve tanı koyamam, ama bu anda bir insanın yanında olması en değerlisi.\n\nİstersen bu konuşmayı burada durdurup sadece seninle ilgilenelim."
	case Concern:
		return "Bunu paylaştığın için teşekkür ederim; yazdıkların hafife alınacak şeyler değil. Ben bir uzman değilim ve tanı koymam, ama bir ruh sağlığı uzmanıyla ya da güvendiğin biriyle konuşmanın çok yardımı olabilir. Şu an kendine zarar verme riskin varsa lütfen **112**'yi ara.\n\nDilersen analize ara verip bunu konuşabiliriz."
	}
	return ""
}

// AnalysisNote, analiz edilen metinde (üçüncü kişinin ifadesinde) risk işareti bulunduğunda
// arayüzde gösterilen nottur. Tanı, tehlikelilik ya da niyet hükmü içermez.
func AnalysisNote(res Result) string {
	if res.Level == None {
		return ""
	}
	n := "Bu yazışmada kendine zarar ya da şiddet içeren ifadeler geçiyor. Bu bir değerlendirme ya da tanı değildir; ifadenin bağlamı (şaka, alıntı, öfke anı, gerçek bir sıkıntı) veriden çıkarılamaz. "
	n += "Konu endişe veriyorsa kişinin kendisiyle ya da bir ruh sağlığı uzmanıyla konuşulması önerilir; acil bir tehlike varsa 112 aranmalıdır. Joe bu kişi hakkında tehlikelilik ya da risk hükmü üretmez."
	return n
}
