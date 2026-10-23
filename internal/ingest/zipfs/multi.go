package zipfs

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// OpenMulti birden çok yolu (ör. Instagram'ın -1.zip, -2.zip parçaları) tek kaynak olarak
// birleştirir. Aynı yol birden fazla parçada varsa ilk parça kazanır ve uyarı verilir.
func OpenMulti(paths []string) (*Source, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("en az bir yol gerekli")
	}
	if len(paths) == 1 {
		return Open(paths[0])
	}
	sorted := append([]string(nil), paths...)
	sort.Strings(sorted)
	var srcs []*Source
	cleanup := func() {
		for _, s := range srcs {
			s.Close()
		}
	}
	for _, p := range sorted {
		s, err := Open(p)
		if err != nil {
			cleanup()
			return nil, fmt.Errorf("%s: %w", filepath.Base(p), err)
		}
		srcs = append(srcs, s)
	}
	m := &merged{}
	h := sha256.New()
	var warns []string
	seen := map[string]string{}
	for i, s := range srcs {
		m.parts = append(m.parts, s.FS)
		fmt.Fprintf(h, "%s\n", s.SHA256)
		for _, w := range s.Warnings {
			warns = append(warns, fmt.Sprintf("%s: %s", s.Name, w))
		}
		_ = fs.WalkDir(s.FS, ".", func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			if first, dup := seen[p]; dup {
				warns = append(warns, fmt.Sprintf("çakışan yol %q: %s kullanıldı, %s yok sayıldı", p, first, s.Name))
			} else {
				seen[p] = s.Name
			}
			return nil
		})
		_ = i
	}
	warns = append(warns, missingParts(sorted)...)
	return &Source{
		FS:       m,
		Name:     fmt.Sprintf("%s (+%d parça)", srcs[0].Name, len(srcs)-1),
		SHA256:   hex.EncodeToString(h.Sum(nil)),
		Warnings: warns,
		closer:   closerFunc(cleanup),
	}, nil
}

type closerFunc func()

func (f closerFunc) Close() error { f(); return nil }

var partRx = regexp.MustCompile(`^(.*?)[-_ ](\d{1,3})\.zip$`)

// missingParts "ad-1.zip, ad-3.zip" gibi numaralı parçalarda atlanan numaraları bildirir.
func missingParts(paths []string) []string {
	groups := map[string][]int{}
	for _, p := range paths {
		if m := partRx.FindStringSubmatch(strings.ToLower(filepath.Base(p))); m != nil {
			n, _ := strconv.Atoi(m[2])
			groups[m[1]] = append(groups[m[1]], n)
		}
	}
	var out []string
	for _, nums := range groups {
		sort.Ints(nums)
		for want := 1; want <= nums[len(nums)-1]; want++ {
			found := false
			for _, n := range nums {
				if n == want {
					found = true
				}
			}
			if !found {
				out = append(out, fmt.Sprintf("parça %d eksik görünüyor; dışa aktarım eksik olabilir", want))
			}
		}
	}
	sort.Strings(out)
	return out
}

// merged birkaç fs.FS'i tek ağaç gibi gösterir.
type merged struct{ parts []fs.FS }

func (m *merged) isDir(name string) bool {
	if name == "." {
		return true
	}
	for _, p := range m.parts {
		if st, err := fs.Stat(p, name); err == nil && st.IsDir() {
			return true
		}
	}
	return false
}

func (m *merged) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	if m.isDir(name) {
		return &mergedDir{m: m, name: name}, nil
	}
	for _, p := range m.parts {
		if f, err := p.Open(name); err == nil {
			return f, nil
		}
	}
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
}

func (m *merged) ReadDir(name string) ([]fs.DirEntry, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrInvalid}
	}
	seen := map[string]bool{}
	var out []fs.DirEntry
	found := false
	for _, p := range m.parts {
		es, err := fs.ReadDir(p, name)
		if err != nil {
			continue
		}
		found = true
		for _, e := range es {
			if !seen[e.Name()] {
				seen[e.Name()] = true
				out = append(out, e)
			}
		}
	}
	if !found {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrNotExist}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out, nil
}

type mergedDir struct {
	m      *merged
	name   string
	listed []fs.DirEntry
	pos    int
}

func (d *mergedDir) Stat() (fs.FileInfo, error) { return dirInfo{name: baseName(d.name)}, nil }
func (d *mergedDir) Read([]byte) (int, error) {
	return 0, &fs.PathError{Op: "read", Path: d.name, Err: fs.ErrInvalid}
}
func (d *mergedDir) Close() error { return nil }
func (d *mergedDir) ReadDir(n int) ([]fs.DirEntry, error) {
	if d.listed == nil {
		es, err := d.m.ReadDir(d.name)
		if err != nil {
			return nil, err
		}
		d.listed = es
	}
	if n <= 0 {
		out := d.listed[d.pos:]
		d.pos = len(d.listed)
		return out, nil
	}
	if d.pos >= len(d.listed) {
		return nil, io.EOF
	}
	end := d.pos + n
	if end > len(d.listed) {
		end = len(d.listed)
	}
	out := d.listed[d.pos:end]
	d.pos = end
	return out, nil
}

func baseName(n string) string {
	if n == "." {
		return "."
	}
	return filepath.Base(n)
}

type dirInfo struct{ name string }

func (i dirInfo) Name() string     { return i.name }
func (dirInfo) Size() int64        { return 0 }
func (dirInfo) Mode() fs.FileMode  { return fs.ModeDir | 0o555 }
func (dirInfo) ModTime() time.Time { return time.Time{} }
func (dirInfo) IsDir() bool        { return true }
func (dirInfo) Sys() any           { return nil }
