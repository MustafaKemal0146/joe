// Package budget, LLM çağrılarının maliyetini önceden tahmin eder, bütçe tavanını zorlar ve
// çalıştırma onaylarını yönetir. Hiçbir sayı uydurulmaz: modelin fiyatı kataloğda yoksa
// tahmin üretilmez (Known=false).
package budget

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/MustafaKemal0146/joe/internal/llm"
)

// CachedInputFraction önbellekten gelen girdi token'larının tam fiyata oranı varsayımıdır.
// DeepSeek, Anthropic ve OpenAI önbellek isabetini yaklaşık bunun altında fiyatlar; kesin oran
// sağlayıcıya göre değişir, bu yüzden tahmin tek sayı değil aralıktır.
const CachedInputFraction = 0.1

// ErrOverBudget tahmin ya da harcama tavanı aştığında döner.
var ErrOverBudget = errors.New("bütçe tavanı aşılacak")

// EstimateTokens bir metnin yaklaşık token sayısını verir. Türkçe metin ve JSON, tokenlayıcıda pahalıdır:
// DeepSeek ile ölçüldü, Türkçe persona çıktısı ≈2,4 karakter/token. Güvenli taraf olsun diye 2,5 alınır;
// önceki karakter/3 tahmini maliyeti ve çıktı sınırını %20 düşük gösteriyordu.
func EstimateTokens(text string) int {
	n := utf8.RuneCountInString(text)
	return (n*2 + 4) / 5
}

// Plan bir konsey/sohbet çalışmasının tahmin girdisidir.
type Plan struct {
	Model string
	// SharedPrefixTokens her çağrıda birebir aynı olan ön ektir (kanıt paketi + sistem istemi).
	SharedPrefixTokens int
	// PerCallTokens çağrıya özgü ek girdidir (persona talimatı gibi).
	PerCallTokens int
	// Calls çağrı sayısı.
	Calls int
	// OutputTokensPerCall bir çağrıdan beklenen azami çıktı token'ı.
	OutputTokensPerCall int
}

// Estimate maliyet tahmini sonucudur.
type Estimate struct {
	Known        bool    `json:"known"`
	Model        string  `json:"model"`
	Calls        int     `json:"calls"`
	PromptTokens int     `json:"prompt_tokens"`
	OutputTokens int     `json:"output_tokens"`
	LowUSD       float64 `json:"low_usd"`  // ön ek önbelleği tam isabet ederse
	HighUSD      float64 `json:"high_usd"` // hiç önbellek yoksa
	Note         string  `json:"note,omitempty"`
}

// Compute planın maliyetini aralık olarak hesaplar.
func Compute(p Plan) Estimate {
	e := Estimate{Model: p.Model, Calls: p.Calls}
	if p.Calls <= 0 {
		return e
	}
	e.PromptTokens = p.Calls * (p.SharedPrefixTokens + p.PerCallTokens)
	e.OutputTokens = p.Calls * p.OutputTokensPerCall
	info, ok := llm.ModelMeta(p.Model)
	if !ok || (info.CostPer1MIn == 0 && info.CostPer1MOut == 0) {
		e.Note = "Bu modelin fiyatı bilinmiyor; tutar tahmin edilmedi."
		return e
	}
	e.Known = true
	in, out := info.CostPer1MIn/1e6, info.CostPer1MOut/1e6
	outCost := float64(e.OutputTokens) * out
	e.HighUSD = float64(e.PromptTokens)*in + outCost
	// İlk çağrı ön eki yazar (tam fiyat); sonrakiler ön eki önbellekten okur.
	cachedPrefix := float64(p.SharedPrefixTokens*(p.Calls-1)) * in * CachedInputFraction
	fullPrefix := float64(p.SharedPrefixTokens) * in
	perCall := float64(p.PerCallTokens*p.Calls) * in
	e.LowUSD = fullPrefix + cachedPrefix + perCall + outCost
	e.Note = fmt.Sprintf("Aralığın alt ucu, ön ekin önbellekten %%%.0f fiyatla okunduğunu varsayar; sağlayıcıya göre değişir.", CachedInputFraction*100)
	return e
}

// Guard bütçe tavanını zorlar.
type Guard struct {
	// CapUSD oturum başına üst sınırdır; 0 ya da negatifse hiçbir çağrıya izin verilmez.
	CapUSD float64
}

// Allow harcanan ve bir sonraki adımın en kötü durum tahmini tavanı aşmıyorsa nil döner.
func (g Guard) Allow(spentUSD, nextHighUSD float64) error {
	if g.CapUSD <= 0 {
		return fmt.Errorf("%w: tavan tanımlı değil", ErrOverBudget)
	}
	if spentUSD+nextHighUSD > g.CapUSD+1e-9 {
		return fmt.Errorf("%w: harcanan $%.4f + sıradaki en çok $%.4f > tavan $%.2f", ErrOverBudget, spentUSD, nextHighUSD, g.CapUSD)
	}
	return nil
}

// Remaining kalan bütçeyi döndürür (negatif olmaz).
func (g Guard) Remaining(spentUSD float64) float64 { return math.Max(0, g.CapUSD-spentUSD) }

// Approval kullanıcının onayladığı bir çalıştırmayı temsil eder.
type Approval struct {
	ID       string
	Estimate Estimate
	CapUSD   float64
	// Binding onayın hangi içeriğe verildiğini mühürler (ör. paket özeti + persona listesi + model).
	// Çalıştırma sırasında içerik değişmişse onay geçersizdir: kullanıcı önizlemedekini onayladı.
	Binding   string
	ExpiresAt time.Time
}

// Approvals onayları belleğte tutar. Onay tek kullanımlıktır ve süresi dolar; pahalı işler
// (konsey) onay kimliği olmadan başlatılamaz.
type Approvals struct {
	mu  sync.Mutex
	m   map[string]Approval
	ttl time.Duration
	now func() time.Time
}

// NewApprovals 15 dakika geçerli onaylar üretir.
func NewApprovals() *Approvals {
	return &Approvals{m: map[string]Approval{}, ttl: 15 * time.Minute, now: time.Now}
}

// Grant yeni bir onay üretir.
func (a *Approvals) Grant(est Estimate, capUSD float64, binding string) Approval {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	ap := Approval{ID: hex.EncodeToString(b), Estimate: est, CapUSD: capUSD, Binding: binding}
	a.mu.Lock()
	defer a.mu.Unlock()
	ap.ExpiresAt = a.now().Add(a.ttl)
	for id, old := range a.m { // süresi dolanları temizle
		if a.now().After(old.ExpiresAt) {
			delete(a.m, id)
		}
	}
	a.m[ap.ID] = ap
	return ap
}

// ErrNoApproval onay yok, kullanılmış ya da süresi dolmuş demektir.
var ErrNoApproval = errors.New("geçerli onay yok")

// Consume onayı tek seferlik harcar.
func (a *Approvals) Consume(id string) (Approval, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	ap, ok := a.m[id]
	if !ok {
		return Approval{}, ErrNoApproval
	}
	delete(a.m, id)
	if a.now().After(ap.ExpiresAt) {
		return Approval{}, ErrNoApproval
	}
	return ap, nil
}
