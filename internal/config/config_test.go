package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKeysAreEncryptedAtRest(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.enc")
	h, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	id, key := "deepseek", "sk-cok-gizli"
	if err := h.Apply(Update{ProviderID: &id, APIKey: &key}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(p)
	if strings.Contains(string(raw), key) {
		t.Fatal("anahtar diskte düz metin")
	}
	h2, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	r, _ := h2.Resolve("deepseek")
	if r.APIKey != key {
		t.Fatalf("anahtar geri okunamadı: %q", r.APIKey)
	}
	for _, s := range h2.Statuses() {
		if s.ID == "deepseek" && !s.HasKey {
			t.Fatal("durum anahtarı görmeli")
		}
	}
}

func TestEnvOverridesFile(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "env-key")
	h, _ := Load(filepath.Join(t.TempDir(), "c.enc"))
	r, _ := h.Resolve("deepseek")
	if r.APIKey != "env-key" || !h.Ready() {
		t.Fatalf("ortam değişkeni geçmeli: %q ready=%v", r.APIKey, h.Ready())
	}
}

func TestRejectsUnknownProviderAndNegativeBudget(t *testing.T) {
	h, _ := Load(filepath.Join(t.TempDir(), "c.enc"))
	bad := "yok"
	if h.Apply(Update{ActiveProvider: &bad}) == nil {
		t.Fatal("bilinmeyen sağlayıcı reddedilmeli")
	}
	neg := -1.0
	if h.Apply(Update{BudgetUSD: &neg}) == nil {
		t.Fatal("negatif bütçe reddedilmeli")
	}
}

func TestEmbedSettingsValidatedAndEncrypted(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.enc")
	h, _ := Load(p)
	bad := "ftp://x"
	if h.Apply(Update{EmbedURL: &bad}) == nil {
		t.Fatal("http dışı adres reddedilmeli")
	}
	url, model, key := "http://127.0.0.1:11434/v1", "bge-m3", "gizli-embed-anahtari"
	if err := h.Apply(Update{EmbedURL: &url, EmbedModel: &model, EmbedKey: &key}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(p)
	if strings.Contains(string(raw), key) || strings.Contains(string(raw), "bge-m3") {
		t.Fatal("ayarlar diskte düz metin")
	}
	h2, _ := Load(p)
	u, m, k, ok := h2.Embed()
	if !ok || u != url || m != model || k != key {
		t.Fatalf("%q %q %q %v", u, m, k, ok)
	}
	t.Setenv("JOE_EMBED_KEY", "env-anahtar")
	if _, _, k, _ := h2.Embed(); k != "env-anahtar" {
		t.Fatal("ortam değişkeni geçmeli")
	}
	empty := ""
	h2.Apply(Update{EmbedURL: &empty})
	if _, _, _, ok := h2.Embed(); ok {
		t.Fatal("adres silinince yapılandırılmamış sayılmalı")
	}
}

func TestChatSettingsDefaultsAndValidation(t *testing.T) {
	h, _ := Load(filepath.Join(t.TempDir(), "c.enc"))
	if h.Get().ChatBudgetUSD != 0.10 {
		t.Fatalf("varsayılan sohbet bütçesi: %v", h.Get().ChatBudgetUSD)
	}
	neg := -1.0
	if h.Apply(Update{ChatBudgetUSD: &neg}) == nil {
		t.Fatal("negatif sohbet bütçesi reddedilmeli")
	}
	b, m := 0.5, "deepseek-flash"
	if err := h.Apply(Update{ChatBudgetUSD: &b, ChatModel: &m}); err != nil || h.Get().ChatModel != m || h.Get().ChatBudgetUSD != 0.5 {
		t.Fatalf("%v %+v", err, h.Get())
	}
}

func TestEffectiveStrongModelAutoManualAndOff(t *testing.T) {
	c := Config{ActiveProvider: "deepseek", ActiveModel: "deepseek-flash"}
	if got := c.EffectiveStrongModel(); got != "deepseek-v4-pro" {
		t.Fatalf("DeepSeek flash için otomatik güçlü model: %q", got)
	}
	c.StrongModel = "-"
	if c.EffectiveStrongModel() != "" {
		t.Fatal("'-' güçlü modeli kapatmalı")
	}
	c.StrongModel = "özel-model"
	if c.EffectiveStrongModel() != "özel-model" {
		t.Fatal("elle seçilen model kullanılmalı")
	}
	c.StrongModel = "deepseek-flash"
	if c.EffectiveStrongModel() != "" {
		t.Fatal("etkin modelle aynıysa ayrı model yok")
	}
	if (Config{ActiveProvider: "openai", ActiveModel: "gpt-x"}).EffectiveStrongModel() != "" {
		t.Fatal("başka sağlayıcıda otomatik güçlü model yok")
	}
}
