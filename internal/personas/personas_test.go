package personas

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MustafaKemal0146/joe/internal/audit"
)

func TestAllThirteenBuiltinPersonasLoadInOrder(t *testing.T) {
	ps, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 13 {
		t.Fatalf("13 persona beklenir: %d", len(ps))
	}
	for i, id := range order {
		if ps[i].ID != id {
			t.Errorf("sıra %d: %s, beklenen %s", i, ps[i].ID, id)
		}
	}
	names := map[string]bool{}
	for _, p := range ps {
		if p.Source != "builtin" || names[p.Name] {
			t.Errorf("%s: kaynak/yineleme", p.ID)
		}
		names[p.Name] = true
		if len(p.Focus) < 3 || len(p.Limits) < 2 || len([]rune(p.Framework)) < 100 {
			t.Errorf("%s: içerik yetersiz (odak %d, sınır %d)", p.ID, len(p.Focus), len(p.Limits))
		}
	}
}

func TestEveryPersonaProhibitsImitationAndTheyAreNeverClinical(t *testing.T) {
	ps, _ := Load("")
	for _, p := range ps {
		joined := strings.Join(p.Limits, " ")
		if !strings.Contains(joined, "taklit etmez") {
			t.Errorf("%s: taklit yasağı sınırlarda yok", p.ID)
		}
		prompt := SystemPrompt(p)
		for _, must := range []string{"Klinik tanı", "karşı hipotez", "abstain", "VERİDİR", p.Name} {
			if !strings.Contains(prompt, must) {
				t.Errorf("%s: istemde %q yok", p.ID, must)
			}
		}
	}
}

func TestPersonaFocusTextsPassTheEpistemicAudit(t *testing.T) {
	// Persona metinlerinin kendisi teşhis/tehlike dili içermemeli.
	ps, _ := Load("")
	for _, p := range ps {
		text := p.Framework + " " + strings.Join(p.Focus, " ") + " " + strings.Join(p.Limits, " ")
		if r := audit.Check(nil, text, nil); r.Has(audit.RuleDiagnosis) || r.Has(audit.RuleDanger) {
			t.Errorf("%s: persona metni yasak dil içeriyor: %+v", p.ID, r.Violations)
		}
	}
}

func TestUserOverrideReplacesAndAddsAndInvalidFails(t *testing.T) {
	dir := t.TempDir()
	override := "---\nid: jung\nad: Jung (özel)\nokul: Özel\n---\n\n## Çerçeve\nKullanıcının kendi yazdığı, yeterince uzun bir çerçeve açıklaması burada yer alır.\n\n## Veride nelere bakar\n- Birinci odak\n- İkinci odak\n\n## Sınırlar\n- Tanı koymaz\n"
	os.WriteFile(filepath.Join(dir, "jung.md"), []byte(override), 0o600)
	extra := strings.ReplaceAll(strings.ReplaceAll(override, "id: jung", "id: ozel-1"), "Jung (özel)", "Yeni Mercek")
	os.WriteFile(filepath.Join(dir, "ozel.md"), []byte(extra), 0o600)
	ps, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 14 || ps[1].Name != "Jung (özel)" || ps[1].Source != "user" || ps[13].ID != "ozel-1" {
		t.Fatalf("üzerine yazma/ekleme: %d, %+v", len(ps), ps[1])
	}
	os.WriteFile(filepath.Join(dir, "bozuk.md"), []byte("başlık yok"), 0o600)
	if _, err := Load(dir); err == nil || !strings.Contains(err.Error(), "bozuk.md") {
		t.Fatalf("geçersiz dosya adıyla hata vermeli: %v", err)
	}
}

func TestParseRejectsBadIDsAndMissingSections(t *testing.T) {
	good := "---\nid: x1\nad: X\nokul: Y\n---\n\n## Çerçeve\nYeterince uzun bir çerçeve açıklaması, kırk karakteri rahatça geçiyor.\n\n## Veride nelere bakar\n- bir\n- iki\n\n## Sınırlar\n- sınır\n"
	if _, err := parse("g.md", []byte(good), "user"); err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"../etc": strings.Replace(good, "id: x1", "id: ../etc", 1),
		"BÜYÜK":  strings.Replace(good, "id: x1", "id: BÜYÜK", 1),
		"ad yok": strings.Replace(good, "ad: X", "ad:", 1),
		"odak":   strings.Replace(good, "- iki\n", "", 1),
		"sınır":  strings.Replace(good, "- sınır\n", "", 1),
		"kısa":   strings.Replace(good, "Yeterince uzun bir çerçeve açıklaması, kırk karakteri rahatça geçiyor.", "kısa", 1),
	}
	for name, c := range cases {
		if _, err := parse("c.md", []byte(c), "user"); err == nil {
			t.Errorf("%s reddedilmeliydi", name)
		}
	}
}

const validOutput = "```json\n" + `{
  "abstain": false, "abstain_reason": "",
  "observations": [{"text": "Gece 23:00'te mesajların çoğu yazılmış.", "evidence": ["K1", "K2"]}],
  "hypotheses": [{"claim": "Gece yazışması bir yakınlık arayışı olabilir.", "evidence": ["K1"],
     "counter_hypothesis": "Gündüz çalışma saatleri yüzünden yalnızca geceye kalmış olabilir.",
     "confidence": "low", "falsified_by": "Hafta sonu gündüzleri de aynı yoğunlukta yazılıyorsa."}],
  "open_questions": ["İş saatleri neler?"], "data_needed": ["Hafta içi/sonu ayrımı"]
}` + "\n```"

func TestParseOutputValidAndFenced(t *testing.T) {
	o, err := ParseOutput("İşte yanıt:\n" + validOutput + "\nBitti.")
	if err != nil {
		t.Fatal(err)
	}
	if len(o.Observations) != 1 || o.Hypotheses[0].Confidence != "low" {
		t.Fatalf("%+v", o)
	}
	if r := o.Audit(audit.ValidIDs(3)); !r.OK() {
		t.Fatalf("temiz çıktı denetimden geçmeli: %+v", r.Violations)
	}
	if r := o.Audit(audit.ValidIDs(1)); !r.Has(audit.RuleUnknownEvidence) {
		t.Fatal("pakette olmayan K2 yakalanmalı")
	}
}

func TestParseOutputRejectsInvalid(t *testing.T) {
	cases := map[string]string{
		"json yok":            "üzgünüm yapamam",
		"bozuk json":          `{"abstain": `,
		"bilinmeyen alan":     `{"abstain":false,"observations":[{"text":"x","evidence":["K1"]}],"diagnosis":"x"}`,
		"gözlem yok":          `{"abstain":false,"observations":[]}`,
		"kanıtsız gözlem":     `{"abstain":false,"observations":[{"text":"x","evidence":[]}]}`,
		"karşı hipotez yok":   `{"abstain":false,"observations":[{"text":"x","evidence":["K1"]}],"hypotheses":[{"claim":"c","evidence":["K1"],"counter_hypothesis":"","confidence":"low","falsified_by":"f"}]}`,
		"çürütme koşulu yok":  `{"abstain":false,"observations":[{"text":"x","evidence":["K1"]}],"hypotheses":[{"claim":"c","evidence":["K1"],"counter_hypothesis":"k","confidence":"low","falsified_by":""}]}`,
		"geçersiz güven":      `{"abstain":false,"observations":[{"text":"x","evidence":["K1"]}],"hypotheses":[{"claim":"c","evidence":["K1"],"counter_hypothesis":"k","confidence":"kesin","falsified_by":"f"}]}`,
		"gerekçesiz çekimser": `{"abstain":true,"abstain_reason":" "}`,
	}
	for name, raw := range cases {
		if _, err := ParseOutput(raw); err == nil || !strings.Contains(err.Error(), "şemaya uymuyor") {
			t.Errorf("%s reddedilmeliydi: %v", name, err)
		}
	}
	if o, err := ParseOutput(`{"abstain":true,"abstain_reason":"Veri yetersiz: yalnızca 12 mesaj."}`); err != nil || !o.Abstain {
		t.Fatalf("gerekçeli çekimserlik geçerli: %v", err)
	}
}

func TestAuditCatchesDiagnosisInsideHypothesis(t *testing.T) {
	raw := strings.Replace(validOutput, "Gece yazışması bir yakınlık arayışı olabilir.", "Kişide borderline kişilik bozukluğu var.", 1)
	o, err := ParseOutput(raw)
	if err != nil {
		t.Fatal(err)
	}
	if r := o.Audit(audit.ValidIDs(3)); !r.Has(audit.RuleDiagnosis) {
		t.Fatalf("teşhis dili yakalanmalı: %+v", r.Violations)
	}
}

func TestPreferredKindsReferenceRealPersonasAndKnownKinds(t *testing.T) {
	ps, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]Persona{}
	for _, p := range ps {
		ids[p.ID] = p
	}
	for id, kinds := range preferredKinds {
		if _, ok := ids[id]; !ok {
			t.Errorf("preferredKinds: bilinmeyen persona %q", id)
		}
		for _, k := range kinds {
			if kindHelp[k] == "" {
				t.Errorf("%s: bilinmeyen kesit türü %q", id, k)
			}
		}
	}
	for id, p := range ids {
		if len(preferredKinds[id]) == 0 {
			t.Errorf("%s için öncelikli kesit türü tanımlı değil", id)
		}
		if sec := Section(p); !strings.Contains(sec, "Pakette özellikle şu türdeki kesitlere dikkat et") {
			t.Errorf("%s bölümünde tür yönlendirmesi yok", id)
		}
	}
}

const goodObs = `{"text":"Gece yazışması arttı.","evidence":["K1"],"quote":"uyuyamadım bugün","about":"a"}`
const goodHyp = `{"claim":"Gevşeme olabilir.","evidence":["K1"],"counter_hypothesis":"İş yoğunluğu.","confidence":"low","falsified_by":"Gündüz de aynıysa.","quote":"","about":"ikisi"}`

func outputJSON(obs, hyp string) string {
	return `{"abstain":false,"abstain_reason":"","observations":[` + obs + `],"hypotheses":[` + hyp + `],"open_questions":["x"],"data_needed":["y"]}`
}

func TestOutputAcceptsQuoteAndAboutAndRejectsBadAbout(t *testing.T) {
	raw := outputJSON(strings.Join([]string{goodObs, goodObs, goodObs}, ","), goodHyp)
	o, err := ParseOutput(raw)
	if err != nil || o.Observations[0].Quote != "uyuyamadım bugün" || o.Observations[0].About != "a" || o.Hypotheses[0].About != "ikisi" {
		t.Fatalf("alıntı ve about okunmalı: %v %+v", err, o)
	}
	bad := strings.Replace(raw, `"about":"a"`, `"about":"c"`, 1)
	if _, err := ParseOutput(bad); err == nil || !strings.Contains(err.Error(), "about") {
		t.Fatalf("geçersiz about reddedilmeli: %v", err)
	}
}

func TestLenientParsingPrunesBadItemsButNeverInventsAndKeepsAMinimum(t *testing.T) {
	noEvidence := `{"text":"Kanıtsız gözlem.","evidence":[]}`
	raw := outputJSON(strings.Join([]string{goodObs, noEvidence, goodObs, goodObs, goodObs}, ","), goodHyp)
	if _, err := ParseOutput(raw); err == nil {
		t.Fatal("katı ayrıştırma tek bir kanıtsız gözlemi reddetmeli")
	}
	o, pruned, err := ParseOutputLenient(raw)
	if err != nil || len(o.Observations) != 4 || len(pruned) != 1 || !strings.Contains(pruned[0], "gözlem 2") {
		t.Fatalf("1 madde ayıklanıp 4 kalmalı: %v %d %v", err, len(o.Observations), pruned)
	}
	// çok fazla geçersiz madde: yeterli kalmaz → reddedilir (uydurma yok)
	few := outputJSON(strings.Join([]string{goodObs, noEvidence, noEvidence}, ","), goodHyp)
	if _, _, err := ParseOutputLenient(few); err == nil {
		t.Fatal("ayıklamadan sonra 3'ten az gözlem kalırsa reddedilmeli")
	}
	// geçerli çıktıda ayıklama olmaz
	if _, pr, err := ParseOutputLenient(outputJSON(strings.Join([]string{goodObs, goodObs, goodObs}, ","), goodHyp)); err != nil || len(pr) != 0 {
		t.Fatalf("temiz çıktı: %v %v", err, pr)
	}
	// karşı hipotezsiz hipotez de ayıklanır, ama en az bir hipotez kalmalı
	noCounter := `{"claim":"X","evidence":["K1"],"counter_hypothesis":"","confidence":"low","falsified_by":"z"}`
	o, pr, err := ParseOutputLenient(outputJSON(strings.Join([]string{goodObs, goodObs, goodObs}, ","), goodHyp+","+noCounter))
	if err != nil || len(o.Hypotheses) != 1 || len(pr) != 1 {
		t.Fatalf("karşı hipotezsiz madde ayıklanmalı: %v %d %v", err, len(o.Hypotheses), pr)
	}
	// çekimserlik değişmeden geçer
	ab := `{"abstain":true,"abstain_reason":"Veri az.","observations":[],"hypotheses":[],"open_questions":[],"data_needed":[]}`
	if o, pr, err := ParseOutputLenient(ab); err != nil || !o.Abstain || len(pr) != 0 {
		t.Fatalf("çekimserlik: %v", err)
	}
}
