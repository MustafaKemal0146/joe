package personas

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/MustafaKemal0146/joe/internal/audit"
)

// Output bir persona çağrısının doğrulanmış çıktısıdır.
type Output struct {
	Abstain       bool          `json:"abstain"`
	AbstainReason string        `json:"abstain_reason"`
	Observations  []Observation `json:"observations"`
	Hypotheses    []Hypothesis  `json:"hypotheses"`
	OpenQuestions []string      `json:"open_questions"`
	DataNeeded    []string      `json:"data_needed"`
}

// Observation pakette görünen doğrudan gözlemdir.
type Observation struct {
	Text     string   `json:"text"`
	Evidence []string `json:"evidence"`
	// Quote, atıf yapılan kesitten BİREBİR alınmış kısa bir parçadır (≤15 kelime). Kod, alıntının pakette gerçekten
	// geçtiğini doğrular; sayısal/ölçüm kaynaklı gözlemlerde boş olabilir.
	Quote string `json:"quote,omitempty"`
	// About iki kişilik analizde gözlemin kimin hakkında olduğunu belirtir: "a", "b" ya da "ikisi" (isteğe bağlı).
	About string `json:"about,omitempty"`
}

// Hypothesis kuramsal okumadır; karşı hipotez ve çürütme koşulu zorunludur.
type Hypothesis struct {
	Claim      string   `json:"claim"`
	Evidence   []string `json:"evidence"`
	Counter    string   `json:"counter_hypothesis"`
	Confidence string   `json:"confidence"`
	Falsified  string   `json:"falsified_by"`
	Quote      string   `json:"quote,omitempty"`
	About      string   `json:"about,omitempty"`
}

const (
	maxItems = 12
	maxText  = 3000
)

// ErrInvalidOutput modelin şemaya uymayan çıktısını bildirir; uydurma çıktıya dönüştürülmez.
var ErrInvalidOutput = errors.New("persona çıktısı şemaya uymuyor")

// extractJSON yanıttaki ilk JSON nesnesini çıkarır (kod çiti ya da ön/son metin olabilir).
func extractJSON(raw string) (string, error) {
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return "", fmt.Errorf("%w: JSON nesnesi bulunamadı", ErrInvalidOutput)
	}
	return raw[start : end+1], nil
}

// decodeOutput JSON'u katı biçimde okur (bilinmeyen alan yok); madde doğrulaması ayrı yapılır.
func decodeOutput(raw string) (Output, error) {
	js, err := extractJSON(raw)
	if err != nil {
		return Output{}, err
	}
	var o Output
	dec := json.NewDecoder(strings.NewReader(js))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&o); err != nil {
		return Output{}, fmt.Errorf("%w: %v", ErrInvalidOutput, err)
	}
	return o, nil
}

func validAbout(a string) bool { return a == "" || a == "a" || a == "b" || a == "ikisi" }

// itemProblems tek tek madde hatalarını (1'den başlayan sıra, ileti) döndürür; geçerli maddeleri ayırır.
func itemProblems(o Output) (okObs []Observation, okHyp []Hypothesis, obsProblems, hypProblems []string) {
	long := func(s string) bool { return len([]rune(s)) > maxText }
	for i, ob := range o.Observations {
		switch {
		case strings.TrimSpace(ob.Text) == "" || len(ob.Evidence) == 0:
			obsProblems = append(obsProblems, fmt.Sprintf("gözlem %d: metin ve en az bir kanıt gerekir", i+1))
		case long(ob.Text) || long(ob.Quote):
			obsProblems = append(obsProblems, fmt.Sprintf("gözlem %d çok uzun", i+1))
		case !validAbout(ob.About):
			obsProblems = append(obsProblems, fmt.Sprintf("gözlem %d: about a/b/ikisi olmalı", i+1))
		default:
			okObs = append(okObs, ob)
		}
	}
	for i, h := range o.Hypotheses {
		switch {
		case strings.TrimSpace(h.Claim) == "" || len(h.Evidence) == 0:
			hypProblems = append(hypProblems, fmt.Sprintf("hipotez %d: iddia ve en az bir kanıt gerekir", i+1))
		case strings.TrimSpace(h.Counter) == "":
			hypProblems = append(hypProblems, fmt.Sprintf("hipotez %d: karşı hipotez zorunlu", i+1))
		case strings.TrimSpace(h.Falsified) == "":
			hypProblems = append(hypProblems, fmt.Sprintf("hipotez %d: çürütme koşulu zorunlu", i+1))
		case h.Confidence != "low" && h.Confidence != "medium" && h.Confidence != "high":
			hypProblems = append(hypProblems, fmt.Sprintf("hipotez %d: güven düzeyi low/medium/high olmalı, %q geldi", i+1, h.Confidence))
		case long(h.Claim) || long(h.Counter) || long(h.Falsified) || long(h.Quote):
			hypProblems = append(hypProblems, fmt.Sprintf("hipotez %d çok uzun", i+1))
		case !validAbout(h.About):
			hypProblems = append(hypProblems, fmt.Sprintf("hipotez %d: about a/b/ikisi olmalı", i+1))
		default:
			okHyp = append(okHyp, h)
		}
	}
	return
}

// ParseOutput model yanıtını ayrıştırır ve doğrular: tek bir geçersiz madde bile çıktıyı reddeder.
func ParseOutput(raw string) (Output, error) {
	o, err := decodeOutput(raw)
	if err != nil {
		return Output{}, err
	}
	bad := func(format string, a ...any) (Output, error) {
		return Output{}, fmt.Errorf("%w: %s", ErrInvalidOutput, fmt.Sprintf(format, a...))
	}
	if len(o.Observations) > maxItems || len(o.Hypotheses) > maxItems {
		return bad("en çok %d gözlem ve %d hipotez olabilir", maxItems, maxItems)
	}
	if o.Abstain {
		if strings.TrimSpace(o.AbstainReason) == "" {
			return bad("çekimser kalındıysa gerekçe zorunlu")
		}
		return o, nil
	}
	if len(o.Observations) == 0 {
		return bad("en az bir gözlem gerekir (veri yetersizse abstain kullan)")
	}
	_, _, op, hp := itemProblems(o)
	if len(op) > 0 {
		return bad("%s", op[0])
	}
	if len(hp) > 0 {
		return bad("%s", hp[0])
	}
	return o, nil
}

// Minimum kalan madde sayıları: ayıklamadan sonra bunların altına inen çıktı reddedilir.
const (
	minKeptObservations = 3
	minKeptHypotheses   = 1
)

// ParseOutputLenient ParseOutput'un ayıklayan sürümüdür: geçersiz maddeleri çıkarıp kalanı kabul eder (7.500 token'lık
// bir çıktı, tek bir kanıtsız gözlem yüzünden tümüyle çöpe gitmesin). Yeterli geçerli madde kalmazsa hata döner.
// Dönen ikinci değer, ayıklanan maddelerin açıklamasıdır (boşsa hiçbir şey ayıklanmadı). Sayıları değiştirilmiş ya
// da uydurulmuş madde EKLENMEZ.
func ParseOutputLenient(raw string) (Output, []string, error) {
	o, err := decodeOutput(raw)
	if err != nil {
		return Output{}, nil, err
	}
	if o.Abstain {
		out, perr := ParseOutput(raw)
		return out, nil, perr
	}
	ok, hk, op, hp := itemProblems(o)
	if len(ok) < minKeptObservations || len(hk) < minKeptHypotheses {
		return Output{}, nil, fmt.Errorf("%w: ayıklamadan sonra yeterli geçerli madde kalmıyor (%d gözlem, %d hipotez)", ErrInvalidOutput, len(ok), len(hk))
	}
	o.Observations, o.Hypotheses = ok, hk
	if len(o.Observations) > maxItems {
		o.Observations = o.Observations[:maxItems]
	}
	if len(o.Hypotheses) > maxItems {
		o.Hypotheses = o.Hypotheses[:maxItems]
	}
	return o, append(op, hp...), nil
}

// Claims çıktıyı epistemik denetim için iddialara çevirir. Gözlemler kanıtı zorunlu "gözlem",
// hipotezler "kuramsal yorum"dur; karşı hipotez ve çürütme koşulu da denetlenir.
func (o Output) Claims() []audit.Claim {
	var cs []audit.Claim
	for _, ob := range o.Observations {
		cs = append(cs, audit.Claim{Text: ob.Text, Evidence: ob.Evidence, Observation: true})
	}
	for _, h := range o.Hypotheses {
		cs = append(cs, audit.Claim{Text: h.Claim, Evidence: h.Evidence})
		cs = append(cs, audit.Claim{Text: h.Counter, Evidence: h.Evidence})
		cs = append(cs, audit.Claim{Text: h.Falsified, Evidence: h.Evidence})
	}
	return cs
}

// Audit çıktıyı geçerli kanıt kimlikleriyle denetler.
func (o Output) Audit(valid map[string]bool) audit.Report {
	free := strings.Join(append(append([]string{}, o.OpenQuestions...), o.DataNeeded...), " ")
	return audit.Check(o.Claims(), free, valid)
}
