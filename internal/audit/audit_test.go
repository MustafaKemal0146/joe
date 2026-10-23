package audit

import "testing"

func TestEvidenceRules(t *testing.T) {
	valid := ValidIDs(5)
	r := Check([]Claim{
		{Text: "Gece yazışmaları artıyor.", Evidence: []string{"K1", "K2"}, Observation: true},
		{Text: "Olmayan kanıta dayanıyor.", Evidence: []string{"K9"}},
		{Text: "Hiçbir kanıtı yok.", Observation: true},
		{Text: "Metinde geçiyor (K3) ve (K42)."},
	}, "", valid)
	if !r.Has(RuleUnknownEvidence) || !r.Has(RuleNoEvidence) {
		t.Fatalf("beklenen ihlaller yok: %+v", r.Violations)
	}
	var unknown, none int
	for _, v := range r.Violations {
		switch v.Rule {
		case RuleUnknownEvidence:
			unknown++
		case RuleNoEvidence:
			none++
		}
	}
	if unknown != 2 || none != 1 {
		t.Fatalf("yanlış sayım: geçersiz=%d kanıtsız=%d (%+v)", unknown, none, r.Violations)
	}
	if got := r.Cited; len(got) != 3 || got[0] != "K1" || got[1] != "K2" || got[2] != "K3" {
		t.Fatalf("anılan geçerli kimlikler: %v", got)
	}
}

func TestInlineEvidenceCountsAsEvidence(t *testing.T) {
	r := Check([]Claim{{Text: "Gece 03:00 mesajları arttı (K4).", Observation: true}}, "", ValidIDs(5))
	if !r.OK() {
		t.Fatalf("metin içi kanıt yeterli olmalı: %+v", r.Violations)
	}
}

func TestDiagnosisAndDangerLanguageFlagged(t *testing.T) {
	bad := []struct {
		text string
		rule Rule
	}{
		{"Bu kişi borderline kişilik bozukluğu gösteriyor (K1).", RuleDiagnosis},
		{"Kişide bipolar bozukluk var (K1).", RuleDiagnosis},
		{"He is a narcissistic person with a personality disorder (K1).", RuleDiagnosis},
		{"Açıkça bir psikopat (K1).", RuleDiagnosis},
		{"Bu kişi tehlikeli ve şiddete meyilli (K1).", RuleDanger},
		{"Muhtemelen suç işleyecek biri (K1).", RuleDanger},
		{"İşe uygun değil (K1).", RuleDanger},
		{"This person is dangerous and abusive (K1).", RuleDanger},
		{"Kesinlikle böyle biri, kişiliği değişmez (K1).", RuleCertainty},
	}
	for _, c := range bad {
		r := Check([]Claim{{Text: c.text, Evidence: []string{"K1"}, Observation: true}}, "", ValidIDs(2))
		if !r.Has(c.rule) {
			t.Errorf("%q için %s bekleniyordu: %+v", c.text, c.rule, r.Violations)
		}
	}
}

func TestLegitimateTheoreticalTermsAndNegationsPass(t *testing.T) {
	ok := []string{
		"Winnicott çerçevesinde 'yeterince iyi' bakım ilişkisi okunabilir (K1).",
		"Klein'a göre bölme savunması örüntüsü görülüyor olabilir; alternatif olarak yorgunluk (K2).",
		"Bu bir klinik tanı değildir ve teşhis koymuyorum (K1).",
		"Tanı koymadan yalnızca mesajlaşma örüntüsünü anlatıyorum (K1).",
		"Dangerous is not a claim I make here; this is not a diagnosis (K1).",
		"Yanıt gecikmesi 3 kat arttı; bu depresyon göstergesi değildir (K1).",
	}
	for _, s := range ok {
		if r := Check([]Claim{{Text: s, Evidence: []string{"K1", "K2"}}}, "", ValidIDs(3)); !r.OK() {
			t.Errorf("meşru metin işaretlendi %q: %+v", s, r.Violations)
		}
	}
}

func TestFreeTextChecked(t *testing.T) {
	r := Check(nil, "Sentez: kişi psikopat ve K77 gösteriyor.", ValidIDs(3))
	if !r.Has(RuleDiagnosis) || !r.Has(RuleUnknownEvidence) {
		t.Fatalf("serbest metin denetlenmeli: %+v", r.Violations)
	}
	for _, v := range r.Violations {
		if v.Claim != -1 {
			t.Fatalf("serbest metin ihlali -1 olmalı: %+v", v)
		}
	}
}

func TestEmptyClaimIgnored(t *testing.T) {
	if r := Check([]Claim{{Text: "   "}}, "", ValidIDs(1)); !r.OK() {
		t.Fatalf("boş iddia ihlal sayılmamalı: %+v", r.Violations)
	}
}

func TestTurkishInitialLettersMatch(t *testing.T) {
	// Go'nun \b sınırı Türkçe harfle başlayan sözcüklerde çalışmıyordu (rx paketiyle düzeltildi).
	for _, c := range []struct {
		text string
		rule Rule
	}{
		{"Kişi şizofreni hastası (K1).", RuleDiagnosis},
		{"Şizoid kişilik bozukluğu görülüyor (K1).", RuleDiagnosis},
		{"Şiddete meyilli biri (K1).", RuleDanger},
		{"Bu kişi şiddet uygulayacak (K1).", RuleDanger},
	} {
		if r := Check([]Claim{{Text: c.text, Evidence: []string{"K1"}}}, "", ValidIDs(2)); !r.Has(c.rule) {
			t.Errorf("%q için %s bekleniyordu: %+v", c.text, c.rule, r.Violations)
		}
	}
	if r := Check([]Claim{{Text: "Bu şiddetli bir tartışma değildi, sadece gergin (K1).", Evidence: []string{"K1"}}}, "", ValidIDs(2)); r.Has(RuleDanger) {
		t.Errorf("'şiddetli' sözcüğü tehlikelilik hükmü sayılmamalı: %+v", r.Violations)
	}
}
