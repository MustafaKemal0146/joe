package report

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/MustafaKemal0146/joe/internal/council"
	"github.com/MustafaKemal0146/joe/internal/evidence"
	"github.com/MustafaKemal0146/joe/internal/personas"
	"github.com/MustafaKemal0146/joe/internal/store"
)

// CouncilInput bir konsey oturumunun rapor girdileridir.
type CouncilInput struct {
	Session     store.CouncilSession
	Outputs     []store.CouncilOutput
	Subjects    []string // oturumdaki kişi adları (sıralı)
	Personas    []personas.Persona
	GeneratedAt time.Time
	Version     string
	Loc         *time.Location
}

func ids(e []string) string {
	if len(e) == 0 {
		return ""
	}
	return " [" + strings.Join(e, ", ") + "]"
}

func para(b *strings.Builder, s string) {
	for _, p := range strings.Split(strings.TrimSpace(s), "\n\n") {
		if p = strings.TrimSpace(p); p != "" {
			fmt.Fprintf(b, "%s\n\n", p)
		}
	}
}

func bullets(b *strings.Builder, hash, title string, items []string) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(b, "%s %s\n\n", hash, title)
	for _, it := range items {
		fmt.Fprintf(b, "- %s\n", strings.TrimSpace(it))
	}
	b.WriteString("\n")
}

// CouncilMarkdown konsey sentezini, persona görüşlerini ve kullanılan kanıt kesitlerinin listesini
// tek Markdown belgesinde toplar. Ham mesaj metni içermez; kesitler başlık ve tarih aralığıyla anılır.
// Yorumlar yapay zekâ çıktısıdır; belge bunu başta belirtir.
func CouncilMarkdown(in CouncilInput) string {
	loc := in.Loc
	if loc == nil {
		loc = time.UTC
	}
	var b strings.Builder
	title := strings.Join(in.Subjects, " × ")
	if title == "" {
		title = "Konsey oturumu"
	}
	fmt.Fprintf(&b, "# Konsey raporu: %s\n\n", title)
	fmt.Fprintf(&b, "- Oturum: `%s` · tür: %s · model: %s · durum: %s\n", in.Session.ID, in.Session.Kind, in.Session.Model, in.Session.Status)
	fmt.Fprintf(&b, "- Oluşturulma: %s\n\n", in.Session.CreatedAt.In(loc).Format("02.01.2006 15:04"))
	b.WriteString("> Bu belgedeki yorumlar bir yapay zekâ konseyinin kanıta bağlı **hipotezleridir**; klinik tanı, tehlike değerlendirmesi ya da kesin hüküm değildir. Köşeli parantezdeki kimlikler (K1…) aşağıdaki kanıt kesitlerine işaret eder.\n\n")

	councilBody(&b, in, loc, 2)
	b.WriteString("---\n")
	fmt.Fprintf(&b, "Bu rapor `joe %s` ile oluşturuldu.\n", in.Version)
	return b.String()
}

// councilBody sentezi, persona görüşlerini ve kanıt listesini yazar. lvl ana bölümlerin Markdown
// başlık düzeyidir (alt başlıklar lvl+1).
func councilBody(b *strings.Builder, in CouncilInput, loc *time.Location, lvl int) {
	h := func(n int) string { return strings.Repeat("#", lvl+n) }
	name := map[string]string{}
	for _, p := range in.Personas {
		name[p.ID] = p.Name
	}
	pname := func(id string) string {
		if n := name[id]; n != "" {
			return n
		}
		return id
	}
	var syn *council.Synthesis
	for _, o := range in.Outputs {
		if o.Stage != "synthesis" || o.Status != store.OutDone || len(o.Output) == 0 {
			continue
		}
		var s council.Synthesis
		if json.Unmarshal(o.Output, &s) == nil {
			syn = &s
		}
	}
	checks := verificationChecks(in.Outputs)
	if syn != nil {
		fmt.Fprintf(b, "%s Genel değerlendirme\n\n", h(0))
		if len(syn.Sections) > 0 {
			for i, sec := range syn.Sections {
				fmt.Fprintf(b, "%s %s\n\n", h(1), strings.TrimSpace(sec.Title))
				para(b, sec.Text)
				if len(sec.Evidence) > 0 {
					fmt.Fprintf(b, "Kanıt:%s\n\n", ids(sec.Evidence))
				}
				if note := checkNote(checks, fmt.Sprintf("s%d", i+1)); note != "" {
					fmt.Fprintf(b, "_%s_\n\n", note)
				}
			}
		} else {
			para(b, syn.Summary)
		}
		if strings.TrimSpace(syn.Evolution) != "" {
			fmt.Fprintf(b, "%s Zaman içinde gelişim\n\n", h(1))
			para(b, syn.Evolution)
			if len(syn.EvolutionEvidence) > 0 {
				fmt.Fprintf(b, "Kanıt:%s\n\n", ids(syn.EvolutionEvidence))
			}
		}
		pts := func(title, prefix string, ps []council.Point) {
			if len(ps) == 0 {
				return
			}
			fmt.Fprintf(b, "%s %s\n\n", h(1), title)
			for i, p := range ps {
				fmt.Fprintf(b, "- %s%s", strings.TrimSpace(p.Text), ids(p.Evidence))
				if note := checkNote(checks, fmt.Sprintf("%s%d", prefix, i+1)); note != "" {
					fmt.Fprintf(b, " _(%s)_", note)
				}
				b.WriteString("\n")
			}
			b.WriteString("\n")
		}
		pts("Uzlaşılan noktalar", "c", syn.Convergences)
		pts("Ayrışan noktalar", "d", syn.Divergences)
		bullets(b, h(1), "Alternatif açıklamalar", syn.Alternatives)
		bullets(b, h(1), "Açık sorular", syn.OpenQuestion)
		bullets(b, h(1), "Gereken ek veri", syn.DataNeeded)
		if line := checkSummary(checks); line != "" {
			fmt.Fprintf(b, "**Kanıt doğrulaması:** %s\n\n", line)
		}
		if syn.Confidence != "" {
			fmt.Fprintf(b, "**Güven düzeyi:** %s\n\n", syn.Confidence)
		}
		if strings.TrimSpace(syn.Limits) != "" {
			fmt.Fprintf(b, "**Sınırlar:** %s\n\n", strings.TrimSpace(syn.Limits))
		}
	} else {
		fmt.Fprintf(b, "%s Genel değerlendirme\n\nBu oturumda üretilmiş bir genel değerlendirme yok.\n\n", h(0))
	}

	// Moderatör: uzlaşı ve ayrışmalar (her mercek kendi cümleleriyle)
	for _, o := range in.Outputs {
		if o.Stage != "moderator" || o.Status != store.OutDone || len(o.Output) == 0 {
			continue
		}
		var m council.Moderator
		if json.Unmarshal(o.Output, &m) != nil || len(m.Agreements)+len(m.Disagreements) == 0 {
			continue
		}
		fmt.Fprintf(b, "%s Moderatör: uzlaşı ve ayrışmalar\n\n", h(0))
		for _, a := range m.Agreements {
			var who []string
			for _, id := range a.Personas {
				who = append(who, pname(id))
			}
			fmt.Fprintf(b, "- **Uzlaşı:** %s%s (%s)\n", strings.TrimSpace(a.Point), ids(a.Evidence), strings.Join(who, ", "))
		}
		b.WriteString("\n")
		for _, d := range m.Disagreements {
			fmt.Fprintf(b, "%s %s\n\n", h(1), strings.TrimSpace(d.Topic))
			if strings.TrimSpace(d.Meaning) != "" {
				fmt.Fprintf(b, "**Bu ne demek?** %s\n\n", strings.TrimSpace(d.Meaning))
			}
			for _, p := range d.Positions {
				fmt.Fprintf(b, "- **%s:** %s%s\n", pname(p.Persona), strings.TrimSpace(p.View), ids(p.Evidence))
			}
			b.WriteString("\n")
		}
		bullets(b, h(1), "Eksik kalanlar", m.Gaps)
	}

	fmt.Fprintf(b, "%s Persona görüşleri\n\n", h(0))
	n := 0
	for _, o := range in.Outputs {
		if o.Stage != "persona" || o.Status != store.OutDone || len(o.Output) == 0 {
			continue
		}
		var po personas.Output
		if json.Unmarshal(o.Output, &po) != nil {
			continue
		}
		n++
		fmt.Fprintf(b, "%s %s\n\n", h(1), pname(o.PersonaID))
		if po.Abstain {
			fmt.Fprintf(b, "_Çekimser kaldı: %s_\n\n", strings.TrimSpace(po.AbstainReason))
			continue
		}
		if len(po.Observations) > 0 {
			b.WriteString("**Gözlemler**\n\n")
			for _, ob := range po.Observations {
				fmt.Fprintf(b, "- %s%s\n", strings.TrimSpace(ob.Text), ids(ob.Evidence))
			}
			b.WriteString("\n")
		}
		if len(po.Hypotheses) > 0 {
			b.WriteString("**Hipotezler**\n\n")
			for _, h := range po.Hypotheses {
				fmt.Fprintf(b, "- %s%s (güven: %s)\n  - Karşı hipotez: %s\n  - Çürütme koşulu: %s\n", strings.TrimSpace(h.Claim), ids(h.Evidence), h.Confidence, strings.TrimSpace(h.Counter), strings.TrimSpace(h.Falsified))
			}
			b.WriteString("\n")
		}
	}
	if n == 0 {
		b.WriteString("Kullanılabilir persona çıktısı yok.\n\n")
	}

	var pack evidence.Pack
	if len(in.Session.PackJSON) > 0 && json.Unmarshal(in.Session.PackJSON, &pack) == nil && len(pack.Items) > 0 {
		fmt.Fprintf(b, "%s Kanıt kesitleri\n\n", h(0))
		b.WriteString("Konseye giden kesitlerin listesi (ham mesajlar bu belgeye eklenmez; uygulamada K kimliğine basarak okuyabilirsin).\n\n")
		for _, it := range pack.Items {
			span := day(it.From, loc)
			if day(it.To, loc) != span {
				span += " – " + day(it.To, loc)
			}
			fmt.Fprintf(b, "- **%s** — %s (%s; %d mesaj)", it.ID, esc(it.Title), span, len(it.Messages))
			if it.Why != "" {
				fmt.Fprintf(b, " · %s", esc(it.Why))
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
}

// verificationChecks doğrulama aşamasının sonuçlarını iddia kimliğine göre döndürür.
func verificationChecks(outs []store.CouncilOutput) map[string]council.ClaimCheck {
	out := map[string]council.ClaimCheck{}
	for _, o := range outs {
		if o.Stage != "verify" || o.Status != store.OutDone || len(o.Output) == 0 {
			continue
		}
		var v council.Verification
		if json.Unmarshal(o.Output, &v) != nil {
			continue
		}
		for _, r := range v.Results {
			out[r.ID] = r
		}
	}
	return out
}

func checkNote(checks map[string]council.ClaimCheck, id string) string {
	switch checks[id].Support {
	case council.SupportYes:
		return "kanıtla destekleniyor"
	case council.SupportPartial:
		return "kısmen destekleniyor"
	case council.SupportNo:
		return "kanıtla desteklenmiyor"
	}
	return ""
}

func checkSummary(checks map[string]council.ClaimCheck) string {
	if len(checks) == 0 {
		return ""
	}
	var y, p, n int
	for _, c := range checks {
		switch c.Support {
		case council.SupportYes:
			y++
		case council.SupportPartial:
			p++
		case council.SupportNo:
			n++
		}
	}
	return fmt.Sprintf("%d iddia destekleniyor, %d kısmen, %d desteklenmiyor.", y, p, n)
}
