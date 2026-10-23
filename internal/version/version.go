// Package version, ikilinin sürüm bilgisini taşır. Sürüm yayında `-ldflags -X` ile gömülür;
// geliştirme derlemelerinde Go'nun gömdüğü derleme bilgisinden (commit) tamamlanır.
package version

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

// Yayın derlemesinde ldflags ile ayarlanır: -X .../internal/version.Version=v0.2.0
var (
	Version = "dev"
	Commit  = ""
	Date    = ""
)

// Info sürüm bilgisini döndürür.
func Info() (version, commit, date string) {
	version, commit, date = Version, Commit, Date
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				if commit == "" {
					commit = s.Value
				}
			case "vcs.time":
				if date == "" {
					date = s.Value
				}
			case "vcs.modified":
				if s.Value == "true" && commit != "" {
					commit += "-kirli"
				}
			}
		}
	}
	if len(commit) > 12 && commit[12:] != "" && commit[len(commit)-6:] != "-kirli" {
		commit = commit[:12]
	}
	return
}

// String insan okur tek satırlık sürüm metnidir.
func String() string {
	v, c, d := Info()
	s := "joe " + v
	if c != "" {
		s += " (" + c + ")"
	}
	if d != "" {
		s += " " + d
	}
	return fmt.Sprintf("%s %s/%s", s, runtime.GOOS, runtime.GOARCH)
}
