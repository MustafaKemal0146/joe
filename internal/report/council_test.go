package report

import (
	"strings"
	"testing"
	"time"

	"github.com/MustafaKemal0146/joe/internal/store"
)

func TestCouncilMarkdownSectionsAndVerification(t *testing.T) {
	syn := `{"summary":"x","sections":[{"title":"Ritim","text":"Yazışma akşamlara kayıyor.","evidence":["K1"]}],"convergences":[{"text":"Ortak okuma","evidence":["K1"]}]}`
	ver := `{"results":[{"id":"s1","support":"supported","note":""},{"id":"c1","support":"unsupported","note":""}]}`
	in := CouncilInput{
		Session: store.CouncilSession{ID: "t1"},
		Outputs: []store.CouncilOutput{
			{Stage: "synthesis", Status: store.OutDone, Output: []byte(syn)},
			{Stage: "verify", Status: store.OutDone, Output: []byte(ver)},
		},
		GeneratedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	out := CouncilMarkdown(in)
	for _, want := range []string{"Ritim", "Yazışma akşamlara kayıyor.", "kanıtla destekleniyor", "kanıtla desteklenmiyor", "Kanıt doğrulaması:"} {
		if !strings.Contains(out, want) {
			t.Errorf("rapor %q içermiyor", want)
		}
	}
}
