package whatsapp

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MustafaKemal0146/joe/internal/ingest/zipfs"
	"github.com/MustafaKemal0146/joe/internal/model"
)

func parseText(t *testing.T, name, text string) *model.Import {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	os.WriteFile(p, []byte(text), 0o600)
	src, err := zipfs.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	imp, err := Parse(src, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	return imp
}

func TestAndroidTurkishFormat(t *testing.T) {
	imp := parseText(t, "WhatsApp Sohbeti - Ayşe.txt", `26.09.2026 20:37 - Mesajlar ve aramalar uçtan uca şifrelidir.
26.09.2026 20:38 - Ayşe: selam
nasılsın bugün
26.09.2026 20:40 - Ben: iyiyim <Medya dahil edilmedi>
27.09.2026 09:00 - Ayşe: Bu mesaj silindi
27.09.2026 09:05 - Ben: merhaba <Bu mesaj düzenlendi>
`)
	c := imp.Conversations[0]
	if len(c.Messages) != 5 {
		t.Fatalf("mesaj sayısı %d", len(c.Messages))
	}
	if c.Messages[0].Kind != model.KindSystem {
		t.Errorf("ilk satır sistem mesajı olmalı: %+v", c.Messages[0])
	}
	if c.Messages[1].Text != "selam\nnasılsın bugün" {
		t.Errorf("çok satırlı birleşmedi: %q", c.Messages[1].Text)
	}
	if c.Messages[2].Kind != model.KindMedia || c.Messages[3].Kind != model.KindDeleted {
		t.Errorf("türler: %v %v", c.Messages[2].Kind, c.Messages[3].Kind)
	}
	if c.Messages[4].Meta["edited"] != true || c.Messages[4].Text != "merhaba" {
		t.Errorf("düzenlenmiş: %+v", c.Messages[4])
	}
	if c.Messages[1].TS.Day() != 26 || c.Messages[1].TS.Month() != 9 {
		t.Errorf("gün/ay: %v", c.Messages[1].TS)
	}
	if imp.OwnerName != "Ben" || !imp.OwnerGuessed {
		t.Errorf("sahip dosya adından çıkarılmalı: %q", imp.OwnerName)
	}
	if imp.Conversations[0].Title != "Ayşe" {
		t.Errorf("başlık: %q", imp.Conversations[0].Title)
	}
}

func TestIOSBracketFormatWithAMPM(t *testing.T) {
	imp := parseText(t, "_chat.txt", "[9/26/26, 8:37:09 PM] Ali: akşam\n[9/26/26, 12:05:00 AM] Veli: gece\n[9/27/26, 12:30:00 PM] Ali: öğle\n")
	c := imp.Conversations[0]
	if len(c.Messages) != 3 {
		t.Fatalf("mesaj sayısı %d", len(c.Messages))
	}
	// 9/27 → ay/gün (27>12 kesin)
	if c.Messages[0].TS.Hour() != 0 || c.Messages[1].TS.Hour() != 20 || c.Messages[2].TS.Hour() != 12 {
		t.Errorf("12 saat dönüşümü: %v %v %v", c.Messages[0].TS, c.Messages[1].TS, c.Messages[2].TS)
	}
	if c.Messages[1].TS.Month() != 9 || c.Messages[1].TS.Day() != 26 {
		t.Errorf("ay/gün: %v", c.Messages[1].TS)
	}
	if imp.OwnerName != "" || len(imp.Candidates) != 2 {
		t.Errorf("sahip belirsiz olmalı, aday: %v", imp.Candidates)
	}
}

func TestSystemLineWithColonInsideQuotesIsNotSender(t *testing.T) {
	imp := parseText(t, "c.txt", "01.02.2026 10:00 - Ali: merhaba\n01.02.2026 10:01 - Ali konuyu \"Plan: yarın\" olarak değiştirdi\n01.02.2026 10:02 - Veli: tamam\n")
	c := imp.Conversations[0]
	if c.Messages[1].Kind != model.KindSystem {
		t.Fatalf("sistem mesajı olmalı: %+v", c.Messages[1])
	}
	if len(c.Participants) != 2 {
		t.Fatalf("katılımcılar: %v", c.Participants)
	}
}

func TestAmbiguousDateWarns(t *testing.T) {
	imp := parseText(t, "c.txt", "01/02/2026 10:00 - Ali: a\n01/02/2026 10:01 - Veli: b\n")
	found := false
	for _, w := range imp.Warnings {
		if len(w) > 0 {
			found = true
		}
	}
	if !found {
		t.Fatal("belirsiz tarih sırası uyarı vermeli")
	}
}

func TestNotAChatFails(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.txt")
	os.WriteFile(p, []byte("bu bir sohbet değil\nsatır\n"), 0o600)
	src, _ := zipfs.Open(p)
	if _, err := Parse(src, time.UTC); err == nil {
		t.Fatal("sohbet olmayan dosya hata vermeli")
	}
}
