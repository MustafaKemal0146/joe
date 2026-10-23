// Package ingest, bir kaynağın (ZIP, dizin ya da dosya) hangi platformdan geldiğini anlar ve
// uygun adaptörle kanonik modele çevirir.
package ingest

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"time"

	"github.com/MustafaKemal0146/joe/internal/ingest/instagram"
	"github.com/MustafaKemal0146/joe/internal/ingest/whatsapp"
	"github.com/MustafaKemal0146/joe/internal/ingest/zipfs"
	"github.com/MustafaKemal0146/joe/internal/model"
)

// Parse kaynağı okur. Önce Instagram, tanınmazsa WhatsApp denenir.
func Parse(src *zipfs.Source, loc *time.Location) (*model.Import, error) {
	jsonFiles, txtFiles := 0, 0
	_ = fs.WalkDir(src.FS, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		switch l := strings.ToLower(p); {
		case strings.HasSuffix(l, ".json"):
			jsonFiles++
		case strings.HasSuffix(l, ".txt"):
			txtFiles++
		}
		return nil
	})
	var igErr, waErr error
	if jsonFiles > 0 {
		imp, err := instagram.Parse(src)
		if err == nil {
			return imp, nil
		}
		igErr = err
	}
	if txtFiles > 0 {
		imp, err := whatsapp.Parse(src, loc)
		if err == nil {
			return imp, nil
		}
		waErr = err
	}
	switch {
	case igErr != nil && waErr == nil:
		return nil, igErr
	case waErr != nil && igErr == nil:
		return nil, waErr
	case igErr != nil && waErr != nil:
		return nil, fmt.Errorf("kaynak tanınmadı: %v; %v", igErr, waErr)
	}
	return nil, errors.New("kaynak tanınmadı: ne Instagram JSON dosyası ne de WhatsApp sohbet metni bulundu")
}
