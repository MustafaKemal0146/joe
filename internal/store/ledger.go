package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

// Usage bir LLM çağrısının (ya da önbellekten dönen çağrının) kaydıdır.
type Usage struct {
	Session          string  `json:"session"`
	Kind             string  `json:"kind"` // persona, moderator, synthesis, chat…
	Provider         string  `json:"provider"`
	Model            string  `json:"model"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	CachedTokens     int     `json:"cached_tokens"`
	CostUSD          float64 `json:"cost_usd"`
	FromCache        bool    `json:"from_cache"`
}

// RecordUsage harcamayı deftere yazar. Yerel yanıt önbelleğinden dönen çağrıların maliyeti 0'dır.
func (s *Store) RecordUsage(u Usage) error {
	if strings.TrimSpace(u.Session) == "" {
		return errors.New("oturum kimliği boş")
	}
	if u.FromCache {
		u.CostUSD = 0
	}
	_, err := s.db.Exec(`INSERT INTO ledger(ts,session,kind,provider,model,prompt_tokens,completion_tokens,cached_tokens,cost_usd,from_cache)
		VALUES(?,?,?,?,?,?,?,?,?,?)`, time.Now().UnixMilli(), u.Session, u.Kind, u.Provider, u.Model,
		u.PromptTokens, u.CompletionTokens, u.CachedTokens, u.CostUSD, b2i(u.FromCache))
	return err
}

// SessionSpend bir oturumun toplam harcamasıdır (USD).
func (s *Store) SessionSpend(session string) (float64, error) {
	var v sql.NullFloat64
	err := s.db.QueryRow(`SELECT SUM(cost_usd) FROM ledger WHERE session=?`, session).Scan(&v)
	return v.Float64, err
}

// SessionSpendRow oturum bazlı harcama özetidir.
type SessionSpendRow struct {
	Session string    `json:"session"`
	USD     float64   `json:"usd"`
	Calls   int       `json:"calls"`
	Cached  int       `json:"cached_calls"`
	Last    time.Time `json:"last"`
}

// SpendSummary toplam harcama özetidir.
type SpendSummary struct {
	TotalUSD         float64           `json:"total_usd"`
	Calls            int               `json:"calls"`
	CachedCalls      int               `json:"cached_calls"`
	PromptTokens     int               `json:"prompt_tokens"`
	CompletionTokens int               `json:"completion_tokens"`
	Sessions         []SessionSpendRow `json:"sessions"`
}

// Spend toplam ve oturum bazlı harcamayı döndürür (en son 20 oturum).
func (s *Store) Spend() (SpendSummary, error) {
	var sum SpendSummary
	var usd sql.NullFloat64
	var calls, cached, pt, ct sql.NullInt64
	if err := s.db.QueryRow(`SELECT SUM(cost_usd),COUNT(*),SUM(from_cache),SUM(prompt_tokens),SUM(completion_tokens) FROM ledger`).
		Scan(&usd, &calls, &cached, &pt, &ct); err != nil {
		return sum, err
	}
	sum.TotalUSD, sum.Calls, sum.CachedCalls = usd.Float64, int(calls.Int64), int(cached.Int64)
	sum.PromptTokens, sum.CompletionTokens = int(pt.Int64), int(ct.Int64)
	rows, err := s.db.Query(`SELECT session,SUM(cost_usd),COUNT(*),SUM(from_cache),MAX(ts) FROM ledger GROUP BY session ORDER BY MAX(ts) DESC, MAX(id) DESC LIMIT 20`)
	if err != nil {
		return sum, err
	}
	defer rows.Close()
	sum.Sessions = []SessionSpendRow{}
	for rows.Next() {
		var r SessionSpendRow
		var ts int64
		if err := rows.Scan(&r.Session, &r.USD, &r.Calls, &r.Cached, &ts); err != nil {
			return sum, err
		}
		r.Last = fromMS(ts)
		sum.Sessions = append(sum.Sessions, r)
	}
	return sum, rows.Err()
}

// CacheKey yanıt önbelleği anahtarını üretir: (paket özeti, persona, model, istem sürümü…).
// Parçalar birbirine karışmasın diye uzunlukla ayrılır.
func CacheKey(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte{byte(len(p) >> 24), byte(len(p) >> 16), byte(len(p) >> 8), byte(len(p))})
		h.Write([]byte(p))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// CachedResponse önbellekten dönen yanıttır.
type CachedResponse struct {
	Response         string
	PromptTokens     int
	CompletionTokens int
}

// CacheGet önbellekte yanıt arar.
func (s *Store) CacheGet(key string) (CachedResponse, bool, error) {
	var r CachedResponse
	err := s.db.QueryRow(`SELECT response,prompt_tokens,completion_tokens FROM response_cache WHERE key=?`, key).
		Scan(&r.Response, &r.PromptTokens, &r.CompletionTokens)
	if errors.Is(err, sql.ErrNoRows) {
		return r, false, nil
	}
	return r, err == nil, err
}

// CachePut yanıtı önbelleğe yazar (aynı anahtar varsa değiştirir).
func (s *Store) CachePut(key string, r CachedResponse) error {
	if strings.TrimSpace(r.Response) == "" {
		return errors.New("boş yanıt önbelleğe yazılmaz")
	}
	_, err := s.db.Exec(`INSERT INTO response_cache(key,response,prompt_tokens,completion_tokens,created_at) VALUES(?,?,?,?,?)
		ON CONFLICT(key) DO UPDATE SET response=excluded.response,prompt_tokens=excluded.prompt_tokens,completion_tokens=excluded.completion_tokens,created_at=excluded.created_at`,
		key, r.Response, r.PromptTokens, r.CompletionTokens, time.Now().UnixMilli())
	return err
}

// UsageRows bir oturumun defter satırlarını (çağrı sırasıyla) döndürür.
func (s *Store) UsageRows(session string) ([]Usage, error) {
	rows, err := s.db.Query(`SELECT session,kind,provider,model,prompt_tokens,completion_tokens,cached_tokens,cost_usd,from_cache FROM ledger WHERE session=? ORDER BY id`, session)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Usage
	for rows.Next() {
		var u Usage
		var fc int
		if err := rows.Scan(&u.Session, &u.Kind, &u.Provider, &u.Model, &u.PromptTokens, &u.CompletionTokens, &u.CachedTokens, &u.CostUSD, &fc); err != nil {
			return nil, err
		}
		u.FromCache = fc == 1
		out = append(out, u)
	}
	return out, rows.Err()
}
