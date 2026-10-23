package evidence

import (
	"reflect"
	"testing"
)

func TestQueryTermsStemsTurkishSuffixesAndDropsFillers(t *testing.T) {
	got := QueryTerms("Geceleri neden daha çok yazıyor, ilişkide gece mesajları?")
	want := []string{"gece"} // geceleri→gece, gece→gece (tekil); soru sözcükleri atılır
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%v != %v", got, want)
	}
	if got := QueryTerms("Hangi saatlerde para konuşuyor?"); !reflect.DeepEqual(got, []string{"saat", "para"}) {
		t.Fatalf("%v", got)
	}
	if QueryTerms("ne?") != nil {
		t.Fatal("anlamlı sözcük yoksa boş dönmeli")
	}
	if len(QueryTerms("alfa bravo charlie delta echo foxtrot golf hotel")) != 6 {
		t.Fatal("en çok 6 kök")
	}
}
