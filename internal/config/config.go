// Package config Joe'nun kullanıcı ayarlarını yönetir: sağlayıcı anahtarları (şifreli),
// etkin sağlayıcı/model ve analiz bütçesi.
package config

import (
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/MustafaKemal0146/joe/internal/secrets"
)

// Provider hazır bir LLM sağlayıcı ön ayarıdır.
type Provider struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	BaseURL      string `json:"base_url"`
	DefaultModel string `json:"default_model"`
	NeedsKey     bool   `json:"needs_key"`
	KeyURL       string `json:"key_url,omitempty"`
	KeyEnv       string `json:"key_env,omitempty"`
	Editable     bool   `json:"editable,omitempty"`
}

// Providers Joe'nun tanıdığı sağlayıcılardır. DeepSeek varsayılandır.
func Providers() []Provider {
	return []Provider{
		{ID: "deepseek", Name: "DeepSeek", BaseURL: "https://api.deepseek.com", DefaultModel: "deepseek-flash", NeedsKey: true, KeyURL: "https://platform.deepseek.com/api_keys", KeyEnv: "DEEPSEEK_API_KEY"},
		{ID: "openrouter", Name: "OpenRouter", BaseURL: "https://openrouter.ai/api/v1", DefaultModel: "openai/gpt-oss-120b", NeedsKey: true, KeyURL: "https://openrouter.ai/keys", KeyEnv: "OPENROUTER_API_KEY"},
		{ID: "openai", Name: "OpenAI", BaseURL: "https://api.openai.com/v1", DefaultModel: "gpt-5-mini", NeedsKey: true, KeyURL: "https://platform.openai.com/api-keys", KeyEnv: "OPENAI_API_KEY"},
		{ID: "anthropic", Name: "Anthropic (Claude)", BaseURL: "https://api.anthropic.com/v1", DefaultModel: "claude-sonnet-4-5", NeedsKey: true, KeyURL: "https://console.anthropic.com/settings/keys", KeyEnv: "ANTHROPIC_API_KEY"},
		{ID: "gemini", Name: "Google Gemini", BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai", DefaultModel: "gemini-2.5-flash", NeedsKey: true, KeyURL: "https://aistudio.google.com/apikey", KeyEnv: "GEMINI_API_KEY"},
		{ID: "groq", Name: "Groq", BaseURL: "https://api.groq.com/openai/v1", DefaultModel: "openai/gpt-oss-120b", NeedsKey: true, KeyURL: "https://console.groq.com/keys", KeyEnv: "GROQ_API_KEY"},
		{ID: "custom", Name: "Özel (OpenAI uyumlu)", Editable: true},
	}
}

// ProviderByID ön ayarı döndürür.
func ProviderByID(id string) (Provider, bool) {
	for _, p := range Providers() {
		if p.ID == id {
			return p, true
		}
	}
	return Provider{}, false
}

// ProviderConfig bir sağlayıcının kullanıcı ayarıdır.
type ProviderConfig struct {
	APIKey  string `json:"api_key,omitempty"`
	BaseURL string `json:"base_url,omitempty"`
}

// Config diske yazılan ayarlardır.
type Config struct {
	ActiveProvider string `json:"active_provider"`
	ActiveModel    string `json:"active_model"`
	// StrongModel moderatör, sentez ve doğrulama çağrıları için kullanılan güçlü modeldir. Boş = otomatik
	// (DeepSeek'te deepseek-flash → deepseek-v4-pro); "-" = kapalı (her şey etkin modelle).
	StrongModel string                    `json:"strong_model"`
	Providers   map[string]ProviderConfig `json:"providers"`
	// Embedding: OpenAI uyumlu /embeddings uç noktası (yerel bge-m3 sunucusu ya da ucuz bir API).
	EmbedURL   string `json:"embed_url"`
	EmbedModel string `json:"embed_model"`
	EmbedKey   string `json:"embed_key"`
	// EmbedAuto true ise yeni bir kaynak içe aktarıldığında gömme dizinlemesi otomatik başlar. Yalnızca
	// gömme adresi yerel (loopback) olduğunda çalışır: mesaj metinleri kullanıcı fark etmeden uzak bir
	// sunucuya gitmesin.
	EmbedAuto bool `json:"embed_auto"`
	// ChatModel sohbet için (daha ucuz) model; boşsa etkin model kullanılır.
	ChatModel string `json:"chat_model"`
	// ChatBudgetUSD tek bir sohbet oturumunun harcama tavanıdır.
	ChatBudgetUSD float64 `json:"chat_budget_usd"`
	// BudgetUSD tek bir konsey oturumu için sert üst sınırdır (0 = sınırsız değil, varsayılan 0.25).
	BudgetUSD float64 `json:"budget_usd"`
}

// Holder ayarları eşzamanlı güvenle tutar ve şifreli dosyaya yazar.
type Holder struct {
	mu   sync.RWMutex
	path string
	cfg  Config
}

// Dir Joe'nun veri klasörünü döndürür: JOE_HOME, yoksa kullanıcı ayar klasörü/joe.
func Dir() (string, error) {
	if d := os.Getenv("JOE_HOME"); d != "" {
		return d, os.MkdirAll(d, 0o700)
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	d := filepath.Join(base, "joe")
	return d, os.MkdirAll(d, 0o700)
}

func defaults() Config {
	return Config{ActiveProvider: "deepseek", ActiveModel: "deepseek-flash", Providers: map[string]ProviderConfig{}, BudgetUSD: 0.25, ChatBudgetUSD: 0.10}
}

// Load ayar dosyasını okur; yoksa varsayılanlarla başlar.
func Load(path string) (*Holder, error) {
	h := &Holder{path: path, cfg: defaults()}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return h, nil
	}
	if err != nil {
		return nil, err
	}
	pt, err := secrets.Open(path, raw)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(pt, &h.cfg); err != nil {
		return nil, err
	}
	if h.cfg.Providers == nil {
		h.cfg.Providers = map[string]ProviderConfig{}
	}
	return h, nil
}

func (h *Holder) saveLocked() error {
	pt, err := json.Marshal(h.cfg)
	if err != nil {
		return err
	}
	ct, err := secrets.Seal(h.path, pt)
	if err != nil {
		return err
	}
	tmp := h.path + ".tmp"
	if err := os.WriteFile(tmp, ct, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, h.path)
}

// Get ayarların bir kopyasını döndürür (anahtarlar dahil; API'ye ASLA olduğu gibi gönderilmez).
func (h *Holder) Get() Config {
	h.mu.RLock()
	defer h.mu.RUnlock()
	c := h.cfg
	c.Providers = make(map[string]ProviderConfig, len(h.cfg.Providers))
	for k, v := range h.cfg.Providers {
		c.Providers[k] = v
	}
	return c
}

// Resolved bir sağlayıcının çağrıya hazır ayarıdır.
type Resolved struct {
	Provider Provider
	BaseURL  string
	APIKey   string
}

// Resolve sağlayıcının adresini ve anahtarını çözer; ortam değişkeni dosyanın üstüne geçer.
func (h *Holder) Resolve(id string) (Resolved, bool) {
	p, ok := ProviderByID(id)
	if !ok {
		return Resolved{}, false
	}
	h.mu.RLock()
	pc := h.cfg.Providers[id]
	h.mu.RUnlock()
	r := Resolved{Provider: p, BaseURL: p.BaseURL, APIKey: pc.APIKey}
	if pc.BaseURL != "" && p.Editable {
		r.BaseURL = pc.BaseURL
	}
	if p.KeyEnv != "" {
		if v := os.Getenv(p.KeyEnv); v != "" {
			r.APIKey = v
		}
	}
	return r, true
}

// Status bir sağlayıcının arayüze gösterilen durumudur; anahtarın kendisi içermez.
type Status struct {
	Provider
	HasKey bool `json:"has_key"`
	Active bool `json:"active"`
}

// Statuses tüm sağlayıcıların durumunu döndürür.
func (h *Holder) Statuses() []Status {
	cfg := h.Get()
	out := make([]Status, 0, len(Providers()))
	for _, p := range Providers() {
		r, _ := h.Resolve(p.ID)
		out = append(out, Status{Provider: p, HasKey: r.APIKey != "", Active: cfg.ActiveProvider == p.ID})
	}
	return out
}

// Ready etkin sağlayıcının çağrıya hazır olup olmadığını söyler.
func (h *Holder) Ready() bool {
	cfg := h.Get()
	r, ok := h.Resolve(cfg.ActiveProvider)
	if !ok || cfg.ActiveModel == "" || r.BaseURL == "" {
		return false
	}
	return !r.Provider.NeedsKey || r.APIKey != ""
}

// Update bir ayar güncellemesidir; nil alanlar değişmez.
type Update struct {
	ActiveProvider *string  `json:"active_provider"`
	ActiveModel    *string  `json:"active_model"`
	StrongModel    *string  `json:"strong_model"`
	BudgetUSD      *float64 `json:"budget_usd"`
	ChatModel      *string  `json:"chat_model"`
	ChatBudgetUSD  *float64 `json:"chat_budget_usd"`
	EmbedURL       *string  `json:"embed_url"`
	EmbedModel     *string  `json:"embed_model"`
	EmbedKey       *string  `json:"embed_key"`
	EmbedAuto      *bool    `json:"embed_auto"`
	// Key, ProviderID için API anahtarını ayarlar ("" gönderilirse silinir).
	ProviderID *string `json:"provider_id"`
	APIKey     *string `json:"api_key"`
	BaseURL    *string `json:"base_url"`
}

// Apply güncellemeyi uygular ve şifreli olarak kaydeder.
func (h *Holder) Apply(u Update) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	next := h.cfg
	next.Providers = make(map[string]ProviderConfig, len(h.cfg.Providers))
	for k, v := range h.cfg.Providers {
		next.Providers[k] = v
	}
	if u.ActiveProvider != nil {
		if _, ok := ProviderByID(*u.ActiveProvider); !ok {
			return errors.New("bilinmeyen sağlayıcı")
		}
		next.ActiveProvider = *u.ActiveProvider
	}
	if u.ActiveModel != nil {
		next.ActiveModel = *u.ActiveModel
	}
	if u.StrongModel != nil {
		next.StrongModel = strings.TrimSpace(*u.StrongModel)
	}
	if u.BudgetUSD != nil {
		if *u.BudgetUSD < 0 {
			return errors.New("bütçe negatif olamaz")
		}
		next.BudgetUSD = *u.BudgetUSD
	}
	if u.ChatModel != nil {
		next.ChatModel = strings.TrimSpace(*u.ChatModel)
	}
	if u.ChatBudgetUSD != nil {
		if *u.ChatBudgetUSD < 0 {
			return errors.New("sohbet bütçesi negatif olamaz")
		}
		next.ChatBudgetUSD = *u.ChatBudgetUSD
	}
	if u.EmbedURL != nil {
		v := strings.TrimSpace(*u.EmbedURL)
		if v != "" {
			pu, err := url.Parse(v)
			if err != nil || (pu.Scheme != "http" && pu.Scheme != "https") || pu.Host == "" {
				return errors.New("gömme adresi http:// ya da https:// ile başlamalı")
			}
		}
		next.EmbedURL = v
	}
	if u.EmbedModel != nil {
		next.EmbedModel = strings.TrimSpace(*u.EmbedModel)
	}
	if u.EmbedKey != nil {
		next.EmbedKey = *u.EmbedKey
	}
	if u.EmbedAuto != nil {
		next.EmbedAuto = *u.EmbedAuto
	}
	if u.ProviderID != nil {
		if _, ok := ProviderByID(*u.ProviderID); !ok {
			return errors.New("bilinmeyen sağlayıcı")
		}
		pc := next.Providers[*u.ProviderID]
		if u.APIKey != nil {
			pc.APIKey = *u.APIKey
		}
		if u.BaseURL != nil {
			pc.BaseURL = *u.BaseURL
		}
		next.Providers[*u.ProviderID] = pc
	}
	if err := func() error {
		old := h.cfg
		h.cfg = next
		err := h.saveLocked()
		if err != nil {
			h.cfg = old
		}
		return err
	}(); err != nil {
		return err
	}
	return nil
}

// Embed gömme ayarını döndürür (anahtar JOE_EMBED_KEY ortam değişkeniyle geçilebilir).
func (h *Holder) Embed() (url, model, key string, ok bool) {
	c := h.Get()
	key = c.EmbedKey
	if v := os.Getenv("JOE_EMBED_KEY"); v != "" {
		key = v
	}
	return c.EmbedURL, c.EmbedModel, key, c.EmbedURL != "" && c.EmbedModel != ""
}

// EffectiveStrongModel güçlü adımlarda (moderatör, sentez, doğrulama) kullanılacak modeli döndürür; "" = etkin model.
func (c Config) EffectiveStrongModel() string {
	switch {
	case c.StrongModel == "-":
		return ""
	case c.StrongModel != "":
		if c.StrongModel == c.ActiveModel {
			return ""
		}
		return c.StrongModel
	case c.ActiveProvider == "deepseek" && c.ActiveModel == "deepseek-flash":
		return "deepseek-v4-pro"
	}
	return ""
}
