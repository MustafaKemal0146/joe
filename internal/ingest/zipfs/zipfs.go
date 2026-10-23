// Package zipfs, bir ZIP arşivini, dizini ya da tek dosyayı açmadan fs.FS olarak sunar.
// Arşiv diske açılmaz; yol geçişi (../) fs.FS sözleşmesi gereği çözülemez. Zip bombasına karşı
// toplam boyut, dosya başına boyut ve sıkıştırma oranı sınırlanır; şifreli girdiler atlanır.
package zipfs

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing/fstest"
)

// Sınırlar. Test edilebilsin diye değişkendir; üretimde gevşetilmez.
var (
	MaxEntries       = 500_000
	MaxFileBytes     = int64(1) << 30  // tek dosya 1 GiB
	MaxTotalBytes    = int64(40) << 30 // toplam 40 GiB (medya dahil arşivler büyüktür)
	MaxRatio         = 2000            // sıkıştırma oranı
	ratioMinSizeSkip = int64(1 << 20)  // 1 MiB altı dosyalarda oran denetlenmez
)

// Source açılmış bir kaynaktır.
type Source struct {
	FS       fs.FS
	Name     string   // dosya/dizin adı
	SHA256   string   // içerik parmak izi (ZIP için dosya özeti)
	Warnings []string // atlanan girdiler
	closer   io.Closer
}

// Close alttaki dosyayı kapatır.
func (s *Source) Close() error {
	if s.closer != nil {
		return s.closer.Close()
	}
	return nil
}

// Open yolun türüne göre kaynağı açar.
func Open(path string) (*Source, error) {
	st, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if st.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("sembolik bağlantılar izlenmez: %s", path)
	}
	switch {
	case st.IsDir():
		return openDir(path)
	case strings.EqualFold(filepath.Ext(path), ".zip"):
		return openZip(path)
	default:
		return openFile(path)
	}
}

func openFile(path string) (*Source, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	h := sha256.New()
	data, err := io.ReadAll(io.TeeReader(f, h))
	if err != nil {
		return nil, err
	}
	name := filepath.Base(path)
	return &Source{
		FS:     fstest.MapFS{name: &fstest.MapFile{Data: data}},
		Name:   name,
		SHA256: hex.EncodeToString(h.Sum(nil)),
	}, nil
}

func openDir(path string) (*Source, error) {
	h := sha256.New()
	var warns []string
	root := os.DirFS(path)
	paths := []string{}
	err := fs.WalkDir(root, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			warns = append(warns, "sembolik bağlantı atlandı: "+p)
			return nil
		}
		if !d.IsDir() {
			paths = append(paths, p)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	for _, p := range paths {
		if info, err := fs.Stat(root, p); err == nil {
			fmt.Fprintf(h, "%s|%d|%d\n", p, info.Size(), info.ModTime().Unix())
		}
	}
	return &Source{FS: &safeDir{root: root}, Name: filepath.Base(path), SHA256: hex.EncodeToString(h.Sum(nil)), Warnings: warns}, nil
}

// safeDir sembolik bağlantılar üzerinden dizin dışına çıkışı engeller.
type safeDir struct{ root fs.FS }

func (s *safeDir) Open(name string) (fs.File, error) {
	if info, err := fs.Stat(s.root, name); err == nil && info.Mode()&fs.ModeSymlink != 0 {
		return nil, fs.ErrPermission
	}
	return s.root.Open(name)
}

func openZip(path string) (*Source, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		f.Close()
		return nil, err
	}
	f.Close()
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("ZIP okunamadı: %w", err)
	}
	if len(zr.File) > MaxEntries {
		zr.Close()
		return nil, fmt.Errorf("arşiv çok fazla girdi içeriyor (%d)", len(zr.File))
	}
	var total int64
	var warns []string
	allowed := make([]*zip.File, 0, len(zr.File))
	for _, zf := range zr.File {
		if zf.Flags&0x1 != 0 {
			warns = append(warns, "şifreli girdi atlandı: "+zf.Name)
			continue
		}
		size := int64(zf.UncompressedSize64)
		if size > MaxFileBytes {
			warns = append(warns, "aşırı büyük girdi atlandı: "+zf.Name)
			continue
		}
		if zf.CompressedSize64 > 0 && size > ratioMinSizeSkip && size/int64(zf.CompressedSize64) > int64(MaxRatio) {
			warns = append(warns, "şüpheli sıkıştırma oranı, girdi atlandı: "+zf.Name)
			continue
		}
		total += size
		if total > MaxTotalBytes {
			zr.Close()
			return nil, fmt.Errorf("arşivin açılmış toplam boyutu sınırı aşıyor")
		}
		allowed = append(allowed, zf)
	}
	return &Source{
		FS:       &filtered{zr: &zr.Reader, allowed: index(allowed)},
		Name:     filepath.Base(path),
		SHA256:   hex.EncodeToString(h.Sum(nil)),
		Warnings: warns,
		closer:   zr,
	}, nil
}

func index(files []*zip.File) map[string]bool {
	m := make(map[string]bool, len(files))
	for _, f := range files {
		m[strings.TrimSuffix(f.Name, "/")] = true
	}
	return m
}

// filtered zip.Reader'ı sarar; atlanan girdilerin okunmasını engeller. Dizin listelemesi de
// atlanan girdileri göstermez.
type filtered struct {
	zr      *zip.Reader
	allowed map[string]bool
}

func (f *filtered) Open(name string) (fs.File, error) {
	if name != "." && !f.allowed[name] && !f.isDir(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return f.zr.Open(name)
}

func (f *filtered) isDir(name string) bool {
	prefix := name + "/"
	for k := range f.allowed {
		if strings.HasPrefix(k, prefix) {
			return true
		}
	}
	return false
}

// ReadFileLimited bir dosyayı en çok max bayta kadar okur; fazlası hata sayılır.
func ReadFileLimited(fsys fs.FS, name string, max int64) ([]byte, error) {
	f, err := fsys.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, fmt.Errorf("%s: dosya %d bayt sınırını aşıyor", name, max)
	}
	return data, nil
}
