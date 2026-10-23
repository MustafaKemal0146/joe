package server

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPeopleOverviewSummarisesEachPerson(t *testing.T) {
	_, h := newServer(t)
	if rec := do(h, "GET", "/api/people/overview", nil, nil); rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("boş: %d %s", rec.Code, rec.Body)
	}
	var sb strings.Builder
	for i := 0; i < 30; i++ {
		fmt.Fprintf(&sb, "26.09.2026 23:%02d - Ben: selam %d\n", i, i)
		fmt.Fprintf(&sb, "26.09.2026 23:%02d - Ayşe: merhaba %d\n", i, i)
	}
	p := filepath.Join(t.TempDir(), "WhatsApp Sohbeti - Ayşe.txt")
	os.WriteFile(p, []byte(sb.String()), 0o600)
	rec := do(h, "POST", "/api/import", map[string]string{"path": p}, nil)
	var imp importResult
	json.Unmarshal(rec.Body.Bytes(), &imp)
	do(h, "POST", fmt.Sprintf("/api/sources/%d/owner", imp.Source.ID), map[string]string{"name": "Ben"}, nil)

	rec = do(h, "GET", "/api/people/overview", nil, nil)
	var ov []PersonOverview
	if err := json.Unmarshal(rec.Body.Bytes(), &ov); err != nil || len(ov) == 0 {
		t.Fatalf("yanıt: %v %s", err, rec.Body)
	}
	o := ov[0]
	if o.Messages != 30 || o.TopPartner != "Ayşe" || o.Conversations != 1 || len(o.Platforms) != 1 || o.Platforms[0] != "whatsapp" {
		t.Fatalf("özet: %+v", o)
	}
	if o.TopShare <= 0 || o.TopShare > 1 {
		t.Fatalf("pay aralık dışı: %v", o.TopShare)
	}
	if o.PeakHour != 23 || o.NightRatio != 0 {
		t.Fatalf("saat: %+v", o)
	}
}

func TestImportResultCarriesSummaryAndFlagsDuplicates(t *testing.T) {
	_, h := newServer(t)
	var sb strings.Builder
	for i := 0; i < 12; i++ {
		fmt.Fprintf(&sb, "26.09.2026 20:%02d - Ben: selam %d\n27.09.2026 21:%02d - Ayşe: merhaba %d\n", i, i, i, i)
	}
	p := filepath.Join(t.TempDir(), "WhatsApp Sohbeti - Ayşe.txt")
	os.WriteFile(p, []byte(sb.String()), 0o600)
	imp := func() importResult {
		rec := do(h, "POST", "/api/import", map[string]string{"path": p}, nil)
		var r importResult
		json.Unmarshal(rec.Body.Bytes(), &r)
		return r
	}
	r := imp()
	if r.Duplicate || r.Summary.Messages != 24 || r.Summary.Conversations != 1 || r.Summary.First.IsZero() || !r.Summary.Last.After(r.Summary.First) || len(r.Summary.Titles) != 1 {
		t.Fatalf("özet: %+v", r.Summary)
	}
	if r2 := imp(); !r2.Duplicate || r2.Summary.Messages != 24 {
		t.Fatalf("yinelenen dosya fark edilmeli: %+v", r2)
	}
	bad := filepath.Join(t.TempDir(), "x.txt")
	os.WriteFile(bad, []byte("bu bir sohbet dışa aktarımı değil"), 0o600)
	rec := do(h, "POST", "/api/import", map[string]string{"path": bad}, nil)
	if rec.Code < 400 || !strings.Contains(rec.Body.String(), "error") {
		t.Fatalf("tanınmayan dosya açıklayıcı hata vermeli: %d %s", rec.Code, rec.Body)
	}
}
