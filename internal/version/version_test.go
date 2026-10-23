package version

import (
	"strings"
	"testing"
)

func TestStringHasVersionAndPlatform(t *testing.T) {
	old := Version
	defer func() { Version = old }()
	Version = "v1.2.3"
	s := String()
	if !strings.HasPrefix(s, "joe v1.2.3") || !strings.Contains(s, "/") {
		t.Fatalf("sürüm metni: %q", s)
	}
}

func TestDefaultIsDev(t *testing.T) {
	if Version != "dev" {
		t.Skip("ldflags ile ayarlanmış")
	}
	if v, _, _ := Info(); v != "dev" {
		t.Fatalf("varsayılan dev olmalı: %q", v)
	}
}
