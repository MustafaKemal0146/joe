package scan

import (
	"os"
	"path/filepath"
	"testing"
)

// JOE_LISTS_DIR gerçek sherlock.json / maigret.json dosyalarının bulunduğu klasördür. Ayarlı değilse
// atlanır: gerçek listeler depoya eklenmez (ağ ve lisans nedeniyle).
func TestParseRealListsWhenAvailable(t *testing.T) {
	dir := os.Getenv("JOE_LISTS_DIR")
	if dir == "" {
		t.Skip("JOE_LISTS_DIR ayarlı değil")
	}
	for f, min := range map[string]int{"sherlock.json": 300, "maigret.json": 2000} {
		data, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil {
			t.Skip(err)
		}
		format := FormatSherlock
		if f == "maigret.json" {
			format = FormatMaigret
		}
		l, err := Parse(format, data)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		t.Logf("%s: %d kullanılabilir, %d atlandı", f, len(l.Sites), l.Skipped)
		if len(l.Sites) < min {
			t.Errorf("%s: en az %d site beklenir: %d", f, min, len(l.Sites))
		}
		for _, s := range l.Sites {
			if !s.Valid || !validSite(s.URICheck) {
				t.Fatalf("geçersiz site sızdı: %+v", s)
			}
		}
	}
}
