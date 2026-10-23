package secrets

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSealOpenRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.enc")
	ct, err := Seal(p, []byte("sk-gizli"))
	if err != nil {
		t.Fatal(err)
	}
	if string(ct) == "sk-gizli" {
		t.Fatal("düz metin diske gitmemeli")
	}
	pt, err := Open(p, ct)
	if err != nil || string(pt) != "sk-gizli" {
		t.Fatalf("çözülemedi: %v %q", err, pt)
	}
	st, _ := os.Stat(keyPath(p))
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("anahtar izni 0600 olmalı, %v", st.Mode().Perm())
	}
}

func TestOpenRejectsTamperedData(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.enc")
	ct, _ := Seal(p, []byte("x"))
	ct[len(ct)-1] ^= 0xff
	if _, err := Open(p, ct); err == nil {
		t.Fatal("bozulmuş veri reddedilmeli")
	}
}
