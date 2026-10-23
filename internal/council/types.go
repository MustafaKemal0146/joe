// Package council, kanıt paketini persona'lara okutan, çelişkileri çıkaran ve sentez yapan
// motordur. Her çağrı sonucu anında kaydedilir (yarıda kalan iş devam eder), bütçe tavanı
// çağrı öncesinde zorlanır, çıktılar şemadan ve epistemik denetimden geçer.
package council

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/MustafaKemal0146/joe/internal/audit"
)

// ErrInvalid şemaya uymayan moderatör/sentez/cevap çıktısını bildirir.
var ErrInvalid = errors.New("çıktı şemaya uymuyor")

func extractJSON(raw string) (string, error) {
	a, b := strings.Index(raw, "{"), strings.LastIndex(raw, "}")
	if a < 0 || b <= a {
		return "", fmt.Errorf("%w: JSON nesnesi bulunamadı", ErrInvalid)
	}
	return raw[a : b+1], nil
}

func decodeStrict(raw string, v any) error {
	js, err := extractJSON(raw)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(strings.NewReader(js))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	return nil
}

// Moderator persona çıktılarındaki uzlaşı ve ayrışmaları çıkarır.
type Moderator struct {
	Agreements []struct {
		Point    string   `json:"point"`
		Personas []string `json:"personas"`
		Evidence []string `json:"evidence"`
	} `json:"agreements"`
	Disagreements []struct {
		Topic string `json:"topic"`
		// Meaning konunun sade dille "bu ne demek, neden önemli" açıklamasıdır (isteğe bağlı).
		Meaning   string `json:"meaning"`
		Positions []struct {
			Persona  string   `json:"persona"`
			View     string   `json:"view"`
			Evidence []string `json:"evidence"`
		} `json:"positions"`
	} `json:"disagreements"`
	Gaps []string `json:"gaps"`
}

// ParseModerator moderatör çıktısını doğrular.
func ParseModerator(raw string) (Moderator, error) {
	var m Moderator
	if err := decodeStrict(raw, &m); err != nil {
		return m, err
	}
	if len(m.Agreements)+len(m.Disagreements)+len(m.Gaps) == 0 {
		return m, fmt.Errorf("%w: moderatör çıktısı boş", ErrInvalid)
	}
	for i, d := range m.Disagreements {
		if strings.TrimSpace(d.Topic) == "" || len(d.Positions) < 2 {
			return m, fmt.Errorf("%w: ayrışma %d: konu ve en az iki görüş gerekir", ErrInvalid, i+1)
		}
	}
	return m, nil
}

// Claims moderatör çıktısını denetim iddialarına çevirir.
func (m Moderator) Claims() []audit.Claim {
	var cs []audit.Claim
	for _, a := range m.Agreements {
		cs = append(cs, audit.Claim{Text: a.Point, Evidence: a.Evidence})
	}
	for _, d := range m.Disagreements {
		for _, p := range d.Positions {
			cs = append(cs, audit.Claim{Text: p.View, Evidence: p.Evidence})
		}
	}
	return cs
}

// Point kanıtlı bir sentez maddesidir.
type Point struct {
	Text     string   `json:"text"`
	Evidence []string `json:"evidence"`
}

// Synthesis nihai nötr sentezdir.
// Section genel değerlendirmenin başlıklı bir bölümüdür.
type Section struct {
	Title    string   `json:"title"`
	Text     string   `json:"text"`
	Evidence []string `json:"evidence"`
}

type Synthesis struct {
	// Summary genel değerlendirmenin düz metnidir. Model "sections" döndürürse Summary bölüm metinlerinden
	// (başlıksız, boş satırla ayrılmış) kurulur; eski çıktılar yalnızca Summary taşır.
	Summary  string    `json:"summary"`
	Sections []Section `json:"sections"`
	// Evolution ilişkinin zaman içindeki gelişimini anlatan, isteğe bağlı bölümdür (eski çıktılarda yoktur).
	Evolution         string   `json:"evolution"`
	EvolutionEvidence []string `json:"evolution_evidence"`
	Convergences      []Point  `json:"convergences"`
	Divergences       []Point  `json:"divergences"`
	Alternatives      []string `json:"alternative_explanations"`
	OpenQuestion      []string `json:"open_questions"`
	DataNeeded        []string `json:"data_needed"`
	Confidence        string   `json:"confidence"`
	Limits            string   `json:"limits"`
}

// ParseSynthesis sentez çıktısını doğrular.
func ParseSynthesis(raw string) (Synthesis, error) {
	var s Synthesis
	if err := decodeStrict(raw, &s); err != nil {
		return s, err
	}
	if len(s.Sections) > 0 {
		var texts []string
		for i, sec := range s.Sections {
			if strings.TrimSpace(sec.Title) == "" || strings.TrimSpace(sec.Text) == "" {
				return s, fmt.Errorf("%w: bölüm %d: başlık ve metin gerekir", ErrInvalid, i+1)
			}
			texts = append(texts, strings.TrimSpace(sec.Text))
		}
		if strings.TrimSpace(s.Summary) == "" {
			s.Summary = strings.Join(texts, "\n\n")
		}
	}
	if strings.TrimSpace(s.Summary) == "" {
		return s, fmt.Errorf("%w: özet boş", ErrInvalid)
	}
	if s.Confidence != "low" && s.Confidence != "medium" && s.Confidence != "high" {
		return s, fmt.Errorf("%w: güven düzeyi low/medium/high olmalı, %q geldi", ErrInvalid, s.Confidence)
	}
	if len(s.Alternatives) == 0 {
		return s, fmt.Errorf("%w: en az bir alternatif açıklama gerekir", ErrInvalid)
	}
	for i, p := range append(append([]Point{}, s.Convergences...), s.Divergences...) {
		if strings.TrimSpace(p.Text) == "" || len(p.Evidence) == 0 {
			return s, fmt.Errorf("%w: madde %d: metin ve kanıt gerekir", ErrInvalid, i+1)
		}
	}
	return s, nil
}

// Claims sentezi denetim iddialarına çevirir.
func (s Synthesis) Claims() []audit.Claim {
	var union []string
	seen := map[string]bool{}
	for _, p := range append(append([]Point{}, s.Convergences...), s.Divergences...) {
		for _, e := range p.Evidence {
			if !seen[e] {
				seen[e] = true
				union = append(union, e)
			}
		}
	}
	cs := []audit.Claim{{Text: s.Summary, Evidence: union}}
	if len(s.Sections) > 0 {
		cs = cs[:0]
		for _, sec := range s.Sections {
			ev := sec.Evidence
			if len(ev) == 0 {
				ev = union
			}
			cs = append(cs, audit.Claim{Text: sec.Text, Evidence: ev})
		}
	}
	if strings.TrimSpace(s.Evolution) != "" {
		ev := s.EvolutionEvidence
		if len(ev) == 0 {
			ev = union
		}
		cs = append(cs, audit.Claim{Text: s.Evolution, Evidence: ev})
	}
	for _, p := range append(append([]Point{}, s.Convergences...), s.Divergences...) {
		cs = append(cs, audit.Claim{Text: p.Text, Evidence: p.Evidence})
	}
	for _, a := range s.Alternatives {
		cs = append(cs, audit.Claim{Text: a, Evidence: union})
	}
	return cs
}

// Answer tek soruya verilen kanıta bağlı cevaptır (kişi dosyası sohbeti, persona'ya sorma).
type Answer struct {
	Answer        string   `json:"answer"`
	Evidence      []string `json:"evidence"`
	Abstain       bool     `json:"abstain"`
	AbstainReason string   `json:"abstain_reason"`
	Limits        string   `json:"limits"`
}

// ParseAnswer cevabı doğrular: ya kanıtlı cevap ya gerekçeli çekimserlik.
func ParseAnswer(raw string) (Answer, error) {
	var a Answer
	if err := decodeStrict(raw, &a); err != nil {
		return a, err
	}
	if a.Abstain {
		if strings.TrimSpace(a.AbstainReason) == "" {
			return a, fmt.Errorf("%w: çekimserlik gerekçesiz", ErrInvalid)
		}
		return a, nil
	}
	if strings.TrimSpace(a.Answer) == "" || len(a.Evidence) == 0 {
		return a, fmt.Errorf("%w: cevap ve en az bir kanıt gerekir (veri yoksa abstain kullan)", ErrInvalid)
	}
	return a, nil
}

// Claims cevabı denetim iddiasına çevirir.
func (a Answer) Claims() []audit.Claim {
	if a.Abstain {
		return nil
	}
	return []audit.Claim{{Text: a.Answer, Evidence: a.Evidence}}
}
