package rx

import (
	"regexp"
	"testing"
)

func TestBoundaryWorksWithTurkishLetters(t *testing.T) {
	cases := []struct {
		pat, text string
		want      bool
	}{
		{`\bölmek\b`, "artık ölmek istiyorum", true},
		{`\bölmek\b`, "bölmek istiyorum", false}, // sözcüğün ortası
		{`\bhiç\b`, "bunu hiç sevmedim", true},
		{`\bhiç\b`, "hiçbir şey", false},
		{`\bşiddet\b`, "şiddet var", true},
		{`\bşiddet\b`, "şiddetli", false},
		{`\bölmek\b`, "ölmek", true}, // metin başı ve sonu
	}
	for _, c := range cases {
		if got := Compile(c.pat).MatchString(c.text); got != c.want {
			t.Errorf("%q içinde %q: %v, beklenen %v", c.text, c.pat, got, c.want)
		}
	}
	// ASCII \b bu davranışı bozar; testin anlamlı olduğunu doğrular.
	if regexpASCIIMatches(`\bölmek\b`, "artık ölmek istiyorum") {
		t.Skip("bu Go sürümünde \\b Unicode farkında")
	}
}

func regexpASCIIMatches(p, s string) bool { return regexp.MustCompile(p).MatchString(s) }
