package zipfs

import (
	"archive/zip"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func makeZip(t *testing.T, files map[string]string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "a.zip")
	f, _ := os.Create(p)
	zw := zip.NewWriter(f)
	for n, c := range files {
		w, _ := zw.Create(n)
		w.Write([]byte(c))
	}
	zw.Close()
	f.Close()
	return p
}

func TestZipReadsWithoutExtracting(t *testing.T) {
	p := makeZip(t, map[string]string{"a/b.json": "{}", "c.txt": "merhaba"})
	src, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	b, err := fs.ReadFile(src.FS, "a/b.json")
	if err != nil || string(b) != "{}" {
		t.Fatalf("okunamadı: %v %q", err, b)
	}
	if src.SHA256 == "" {
		t.Fatal("parmak izi boş")
	}
	entries, _ := fs.ReadDir(src.FS, "a")
	if len(entries) != 1 {
		t.Fatalf("dizin listesi: %d", len(entries))
	}
}

func TestZipTraversalNameCannotEscape(t *testing.T) {
	p := makeZip(t, map[string]string{"../evil.txt": "x"})
	src, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	if _, err := fs.ReadFile(src.FS, "../evil.txt"); err == nil {
		t.Fatal("yol geçişi okunamamalı")
	}
}

func TestOversizedEntrySkipped(t *testing.T) {
	old := MaxFileBytes
	MaxFileBytes = 10
	defer func() { MaxFileBytes = old }()
	p := makeZip(t, map[string]string{"big.txt": strings.Repeat("a", 100), "ok.txt": "a"})
	src, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	if _, err := fs.ReadFile(src.FS, "big.txt"); err == nil {
		t.Fatal("büyük girdi atlanmalı")
	}
	if _, err := fs.ReadFile(src.FS, "ok.txt"); err != nil {
		t.Fatal("küçük girdi okunmalı")
	}
	if len(src.Warnings) != 1 {
		t.Fatalf("uyarı beklenirdi: %v", src.Warnings)
	}
}

func TestSymlinkRejected(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "t.txt")
	os.WriteFile(target, []byte("x"), 0o600)
	link := filepath.Join(dir, "l")
	if err := os.Symlink(target, link); err != nil {
		t.Skip("symlink yok")
	}
	if _, err := Open(link); err == nil {
		t.Fatal("sembolik bağlantı reddedilmeli")
	}
}

func TestSingleFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "chat.txt")
	os.WriteFile(p, []byte("selam"), 0o600)
	src, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := fs.ReadFile(src.FS, "chat.txt")
	if string(b) != "selam" {
		t.Fatal("tek dosya okunamadı")
	}
}

func TestOpenMultiMergesPartsAndWarnsOnGapAndConflict(t *testing.T) {
	mk := func(name string, files map[string]string) string {
		p := filepath.Join(t.TempDir(), name)
		f, _ := os.Create(p)
		zw := zip.NewWriter(f)
		for n, c := range files {
			w, _ := zw.Create(n)
			w.Write([]byte(c))
		}
		zw.Close()
		f.Close()
		return p
	}
	a := mk("instagram-user-1.zip", map[string]string{"messages/inbox/a/message_1.json": "A", "dup.txt": "ilk"})
	b := mk("instagram-user-3.zip", map[string]string{"messages/inbox/b/message_1.json": "B", "dup.txt": "ikinci"})
	src, err := OpenMulti([]string{b, a}) // sıra önemsiz
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	for _, p := range []string{"messages/inbox/a/message_1.json", "messages/inbox/b/message_1.json"} {
		if _, err := fs.ReadFile(src.FS, p); err != nil {
			t.Fatalf("%s okunamadı: %v", p, err)
		}
	}
	if got, _ := fs.ReadFile(src.FS, "dup.txt"); string(got) != "ilk" {
		t.Fatalf("çakışmada ilk parça kazanmalı: %q", got)
	}
	entries, err := fs.ReadDir(src.FS, "messages/inbox")
	if err != nil || len(entries) != 2 {
		t.Fatalf("dizin birleşmeli: %v %v", entries, err)
	}
	var seen []string
	fs.WalkDir(src.FS, ".", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			seen = append(seen, p)
		}
		return nil
	})
	if len(seen) != 3 {
		t.Fatalf("WalkDir tüm dosyaları görmeli: %v", seen)
	}
	joined := strings.Join(src.Warnings, "|")
	if !strings.Contains(joined, "parça 2 eksik") || !strings.Contains(joined, "çakışan yol") {
		t.Fatalf("uyarılar: %v", src.Warnings)
	}
	again, _ := OpenMulti([]string{a, b})
	defer again.Close()
	if again.SHA256 != src.SHA256 {
		t.Fatal("parmak izi parça sırasından bağımsız olmalı")
	}
}

func TestOpenMultiSingleAndEmpty(t *testing.T) {
	if _, err := OpenMulti(nil); err == nil {
		t.Fatal("boş liste hata vermeli")
	}
	p := makeZip(t, map[string]string{"x.txt": "1"})
	s, err := OpenMulti([]string{p})
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
}
