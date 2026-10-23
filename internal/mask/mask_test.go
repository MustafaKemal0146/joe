package mask

import (
	"strings"
	"testing"
)

func TestEmailAndConsistentPlaceholders(t *testing.T) {
	m := New()
	got := m.Apply("yaz: Ali@Example.com ve ali@example.com, sonra can@x.org")
	if strings.Contains(got, "example.com") || strings.Contains(got, "x.org") {
		t.Fatalf("e-posta sızdı: %s", got)
	}
	if strings.Count(got, "[EPOSTA-1]") != 2 || !strings.Contains(got, "[EPOSTA-2]") {
		t.Fatalf("aynı değer aynı yer tutucuyu almalı (büyük/küçük harf bağımsız): %s", got)
	}
}

func TestPhoneVariantsMaskedButDatesAndTimesKept(t *testing.T) {
	m := New()
	for _, s := range []string{"+90 532 123 45 67", "0532 123 45 67", "05321234567", "(0212) 555 12 34", "+1 415-555-2671"} {
		out := m.Apply("ara " + s + " olur mu")
		if !strings.Contains(out, "[TEL-") || strings.ContainsAny(out, "5") && strings.Contains(out, "123 45") {
			t.Errorf("telefon maskelenmedi %q → %q", s, out)
		}
	}
	keep := "26.09.2026 20:37:09 tarihinde 1700000000000 ve 12345 sayısı"
	if got := New().Apply(keep); got != keep {
		t.Fatalf("tarih/saat/zaman damgası bozulmamalı: %q", got)
	}
}

func TestIPPublicAndPrivateAndInvalid(t *testing.T) {
	m := New()
	got := m.Apply("giriş 203.0.113.7 ve 999.1.1.1 ve 2001:db8::1")
	if strings.Contains(got, "203.0.113.7") || strings.Contains(got, "2001:db8::1") {
		t.Fatalf("IP sızdı: %s", got)
	}
	if !strings.Contains(got, "999.1.1.1") {
		t.Fatalf("geçersiz IP olduğu gibi kalmalı: %s", got)
	}
}

func TestTCKNAndIBANNeedValidChecksum(t *testing.T) {
	m := New()
	valid := "10000000146" // geçerli sağlama toplamlı TCKN
	if !tcknValid(valid) {
		t.Fatal("test TCKN'si geçerli olmalı")
	}
	if got := m.Apply("kimlik " + valid); strings.Contains(got, valid) {
		t.Fatalf("geçerli TCKN maskelenmeli: %s", got)
	}
	if got := New().Apply("sayı 12345678901"); !strings.Contains(got, "12345678901") {
		t.Fatalf("sağlaması geçersiz 11 haneli sayı kalmalı: %s", got)
	}
	iban := "TR33 0006 1005 1978 6457 8413 26"
	if !ibanValid(iban) {
		t.Fatal("test IBAN'ı geçerli olmalı")
	}
	if got := New().Apply("iban " + iban); strings.Contains(got, "0006") {
		t.Fatalf("IBAN sızdı: %s", got)
	}
}

func TestHandleInURLAndGeo(t *testing.T) {
	m := New()
	got := m.Apply("bak https://www.instagram.com/ayse_k/ ve 41.0082, 28.9784")
	if strings.Contains(got, "ayse_k") || strings.Contains(got, "41.0082") {
		t.Fatalf("hesap/konum sızdı: %s", got)
	}
	if !strings.Contains(got, "instagram.com/") {
		t.Fatalf("alan adı korunmalı: %s", got)
	}
}

func TestUnmaskRoundTripAndKindSwitch(t *testing.T) {
	m := New()
	in := "ali@example.com ve +90 532 123 45 67 ve 203.0.113.7"
	masked := m.Apply(in)
	un := m.Unmask(masked)
	for _, want := range []string{"ali@example.com", "203.0.113.7"} {
		if !strings.Contains(un, want) {
			t.Fatalf("geri çözülemedi %q: %s", want, un)
		}
	}
	only := New(Email)
	if got := only.Apply("a@b.co 203.0.113.7"); !strings.Contains(got, "203.0.113.7") || strings.Contains(got, "a@b.co") {
		t.Fatalf("yalnız e-posta açıkken: %s", got)
	}
	if r := m.Report(); r[Email] != 1 || r[IP] != 1 {
		t.Fatalf("rapor: %v", r)
	}
}

func TestIdempotentOnPlaceholders(t *testing.T) {
	m := New()
	once := m.Apply("a@b.co")
	if twice := m.Apply(once); twice != once {
		t.Fatalf("yer tutucu yeniden maskelenmemeli: %q → %q", once, twice)
	}
}
