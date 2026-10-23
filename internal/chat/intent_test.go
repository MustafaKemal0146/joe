package chat

import (
	"strings"
	"testing"
)

func TestRouteIntentFindsPersonWithTurkishSuffixesAndNeverStartsAnalysis(t *testing.T) {
	env := &fakeEnv{}
	cases := []struct {
		text, to string
	}{
		{"Ayşe'yi analiz et", "/konsey?kisi=2"},
		{"ayseyi incele", "/konsey?kisi=2"},
		{"ben ve Ayşe uyumu", "/konsey?kisi=1&ikinci=2&tur=pair"},
		{"Ayşe ile ilişki analizi yap", "/konsey?kisi=1&ikinci=2&tur=pair"},
		{"dosya ekle", "/joe?sihirbaz=1"},
	}
	for _, c := range cases {
		r, ok := RouteIntent(c.text, env)
		if !ok || r.Nav == nil || r.Nav.To != c.to {
			t.Errorf("%q → %+v ok=%v, beklenen %s", c.text, r, ok, c.to)
		}
	}
	if len(env.calls) != 0 {
		t.Fatalf("yönlendirme yazma/analiz çağrısı yapmamalı: %v", env.calls)
	}
}

func TestRouteIntentLeavesUnknownAndAsksWhenAmbiguous(t *testing.T) {
	env := &fakeEnv{}
	for _, s := range []string{"Zeynep'i analiz et", "hava nasıl", "analiz nedir", "x"} {
		if _, ok := RouteIntent(s, env); ok {
			t.Errorf("%q yönlendirilmemeli (model ya da genel yol)", s)
		}
	}
	amb := &ambiguousEnv{&fakeEnv{}}
	r, ok := RouteIntent("Ayşe'yi analiz et", amb)
	if !ok || r.Nav != nil || !strings.Contains(r.Text, "hangisini") {
		t.Fatalf("belirsizlikte sorulmalı, bağlantı verilmemeli: %+v", r)
	}
}

type ambiguousEnv struct{ *fakeEnv }

func (*ambiguousEnv) ListPeople() []map[string]any {
	return []map[string]any{{"id": 1, "name": "Ben", "is_self": true}, {"id": 2, "name": "Ayşe Yılmaz"}, {"id": 3, "name": "Ayşe Kaya"}}
}
