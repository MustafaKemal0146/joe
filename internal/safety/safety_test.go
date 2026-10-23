package safety

import (
	"strings"
	"testing"
)

func TestSelfHarmLevels(t *testing.T) {
	cases := []struct {
		text string
		want Level
	}{
		{"Kendimi öldüreceğim, karar verdim.", Acute},
		{"Bu gece intihar edeceğim", Acute},
		{"I'm going to kill myself tonight", Acute},
		{"Artık ölmek istiyorum", Concern},
		{"Yaşamak istemiyorum bazen", Concern},
		{"kendime zarar veriyorum son zamanlarda", Concern},
		{"İntiharı düşünüyorum", Concern},
		{"I just want to die", Concern},
		{"she is suicidal", Concern},
	}
	for _, c := range cases {
		got := Detect(c.text)
		if got.Level != c.want || !got.Has(SelfHarm) {
			t.Errorf("%q → %v %v, beklenen %v", c.text, got.Level, got.Categories, c.want)
		}
	}
}

func TestViolenceToOthers(t *testing.T) {
	for _, s := range []string{"Seni öldüreceğim!", "I'm going to kill him", "sana zarar vereceğim"} {
		if got := Detect(s); !got.Has(Violence) || got.Level == None {
			t.Errorf("%q şiddet olarak işaretlenmeliydi: %+v", s, got)
		}
	}
}

func TestIdiomsAndNegationsAreNotFlagged(t *testing.T) {
	for _, s := range []string{
		"Ölüyorum gülmekten, çok komiksin",
		"Bu sınavda öldüm resmen",
		"Kahve içmezsem ölürüm",
		"Seni çok seviyorum, öldürme beni",
		"intiharı düşünmüyorum, merak etme",
		"Kendimi asla öldürmem, öyle biri değilim",
		"I don't want to die, I'm fine",
		"Bu filmde 'seni öldüreceğim' replikini hiç sevmedim değil",
		"Yarın sınav var, ölmek istemiyorum diye çalışıyorum",
	} {
		// son örnek bilerek sınır: "ölmek istiyorum" değil "istemiyorum" → işaretlenmemeli
		if got := Detect(s); got.Level != None {
			t.Errorf("%q yanlış alarm: %+v", s, got)
		}
	}
}

func TestEmptyAndCaseInsensitive(t *testing.T) {
	if Detect("").Level != None || Detect("merhaba nasılsın").Level != None {
		t.Fatal("zararsız metin işaretlenmemeli")
	}
	if Detect("İNTİHAR EDECEĞİM").Level != Acute {
		t.Fatal("büyük harf ve İ ile de eşleşmeli")
	}
}

func TestResponsesAreSafeAndHonest(t *testing.T) {
	for _, l := range []Level{Concern, Acute} {
		s := UserResponse(l)
		if !strings.Contains(s, "112") || !strings.Contains(s, "tanı koy") {
			t.Errorf("%v: yanıtta 112 ve tanı koymama olmalı: %s", l, s)
		}
		// Doğrulanmamış hat numaraları yazılmamalı.
		for _, bad := range []string{"182", "183", "184", "185"} {
			if strings.Contains(s, bad) {
				t.Errorf("doğrulanmamış numara: %s", bad)
			}
		}
	}
	if UserResponse(None) != "" {
		t.Fatal("risk yoksa yanıt boş")
	}
}

func TestAnalysisNoteHasNoJudgement(t *testing.T) {
	if AnalysisNote(Result{}) != "" {
		t.Fatal("risk yoksa not boş")
	}
	n := AnalysisNote(Result{Level: Concern, Categories: []string{SelfHarm}})
	for _, must := range []string{"değerlendirme ya da tanı değildir", "112", "hükmü üretmez"} {
		if !strings.Contains(n, must) {
			t.Errorf("notta %q yok", must)
		}
	}
}
