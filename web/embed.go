// Package web derlenmiş arayüzü (web/dist) Go ikilisine gömer.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// FS gömülü arayüzün kökünü döndürür; arayüz derlenmemişse index.html bulunmaz.
func FS() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		return nil
	}
	return sub
}
