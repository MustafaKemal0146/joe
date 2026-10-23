package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MustafaKemal0146/joe/internal/eval"
	"github.com/MustafaKemal0146/joe/internal/personas"
)

func TestConfirmRequiresExactWord(t *testing.T) {
	var out bytes.Buffer
	if !confirm(strings.NewReader("SİL\n"), &out, "?", "SİL") {
		t.Fatal("tam kelime kabul edilmeli")
	}
	for _, in := range []string{"sil\n", "evet\n", "\n", ""} {
		if confirm(strings.NewReader(in), &out, "?", "SİL") {
			t.Errorf("%q kabul edilmemeli", in)
		}
	}
}

func TestWipeWithoutConfirmationKeepsData(t *testing.T) {
	t.Setenv("JOE_HOME", t.TempDir())
	p := filepath.Join(t.TempDir(), "chat.txt")
	os.WriteFile(p, []byte("26.09.2026 20:38 - Ayşe: selam\n26.09.2026 20:39 - Ben: merhaba\n"), 0o600)
	if err := importCmd([]string{p}); err != nil {
		t.Fatal(err)
	}
	if err := wipeCmd(nil, strings.NewReader("hayır\n")); err == nil {
		t.Fatal("onaysız silme hata vermeli")
	}
	st, _, _, _ := open()
	srcs, _ := st.Sources()
	st.Close()
	if len(srcs) != 1 {
		t.Fatalf("onaysız silmede veri korunmalı: %d kaynak", len(srcs))
	}
	if err := wipeCmd([]string{"--yes"}, nil); err != nil {
		t.Fatal(err)
	}
	st, _, _, _ = open()
	srcs, _ = st.Sources()
	st.Close()
	if len(srcs) != 0 {
		t.Fatalf("--yes ile silinmeli: %d kaynak", len(srcs))
	}
}

func TestExportRefusesToOverwrite(t *testing.T) {
	t.Setenv("JOE_HOME", t.TempDir())
	out := filepath.Join(t.TempDir(), "yedek.json")
	if err := exportCmd([]string{out}); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(out)
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("dışa aktarım izni 0600 olmalı: %v", st.Mode().Perm())
	}
	if err := exportCmd([]string{out}); err == nil {
		t.Fatal("var olan dosyanın üzerine yazılmamalı")
	}
}

func TestEvalRefusesWithoutYesAndShowsEstimate(t *testing.T) {
	t.Setenv("JOE_HOME", t.TempDir())
	t.Setenv("DEEPSEEK_API_KEY", "sk-test") // anahtar var ama --yes yok: ağ çağrısı yapılmamalı
	err := evalCmd([]string{"--personas", "jung"})
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("--yes olmadan çalışmamalı: %v", err)
	}
	if err := evalCmd([]string{"--personas", "yok", "--yes"}); err == nil {
		t.Fatal("bilinmeyen persona hata vermeli")
	}
}

func TestEvalEstimateIsPositiveAndScalesWithPersonas(t *testing.T) {
	ps, _ := personas.Load("")
	one, err := evalEstimate("deepseek-flash", eval.Cases(), []string{"jung"}, ps)
	if err != nil || !one.Known || one.HighUSD <= 0 || one.Calls != 8 {
		t.Fatalf("tahmin: %+v %v", one, err)
	}
	three, _ := evalEstimate("deepseek-flash", eval.Cases(), []string{"jung", "freud", "klein"}, ps)
	if three.Calls != 24 || three.HighUSD < 2.5*one.HighUSD {
		t.Fatalf("tahmin persona sayısıyla artmalı: %v → %v", one.HighUSD, three.HighUSD)
	}
	if e, _ := evalEstimate("bilinmeyen", eval.Cases(), []string{"jung"}, ps); e.Known {
		t.Fatal("bilinmeyen modelde tahmin yok")
	}
}
