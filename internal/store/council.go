package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// Konsey oturumu ve çıktı durumları.
const (
	CouncilQueued      = "queued"
	CouncilRunning     = "running"
	CouncilDone        = "done"
	CouncilPartial     = "partial"        // bazı persona'lar başarısız, sentez yapıldı
	CouncilFailed      = "failed"         // hiç kullanılabilir çıktı yok
	CouncilBudgetStop  = "budget_stopped" // tavan nedeniyle durdu, eldekiler korunur
	CouncilCanceled    = "canceled"
	CouncilInterrupted = "interrupted" // süreç kapandı; devam ettirilebilir

	OutRunning = "running"
	OutPending = "pending"
	OutDone    = "done"
	OutFailed  = "failed"
	OutSkipped = "skipped"
)

// CouncilSession bir konsey oturumudur.
type CouncilSession struct {
	ID          string          `json:"id"`
	Kind        string          `json:"kind"` // single | pair | ask
	SubjectIDs  []int64         `json:"subject_ids"`
	PersonaIDs  []string        `json:"persona_ids"`
	Provider    string          `json:"provider"`
	Model       string          `json:"model"`
	Status      string          `json:"status"`
	PackHash    string          `json:"pack_hash"`
	PackText    string          `json:"-"` // sağlayıcıya giden maskeli metin
	PackJSON    json.RawMessage `json:"pack,omitempty"`
	Question    string          `json:"question,omitempty"`
	EstimateRaw json.RawMessage `json:"estimate,omitempty"`
	Error       string          `json:"error,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

// CouncilOutput bir aşamanın (persona, moderatör, sentez) çıktısıdır.
type CouncilOutput struct {
	ID               int64           `json:"id"`
	SessionID        string          `json:"session_id"`
	Stage            string          `json:"stage"` // persona | moderator | synthesis | answer
	PersonaID        string          `json:"persona_id"`
	Status           string          `json:"status"`
	Raw              string          `json:"-"`
	Output           json.RawMessage `json:"output,omitempty"`
	Audit            json.RawMessage `json:"audit,omitempty"`
	PromptTokens     int             `json:"prompt_tokens"`
	CompletionTokens int             `json:"completion_tokens"`
	CostUSD          float64         `json:"cost_usd"`
	FromCache        bool            `json:"from_cache"`
	Attempts         int             `json:"attempts"`
	Error            string          `json:"error,omitempty"`
	UpdatedAt        time.Time       `json:"updated_at"`
}

// CreateCouncil yeni bir oturum kaydeder.
func (s *Store) CreateCouncil(c CouncilSession) error {
	subj, _ := json.Marshal(c.SubjectIDs)
	pers, _ := json.Marshal(c.PersonaIDs)
	now := time.Now().UnixMilli()
	_, err := s.db.Exec(`INSERT INTO council_sessions(id,kind,subject_ids,persona_ids,provider,model,status,pack_hash,pack_text,pack_json,question,estimate_json,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, c.ID, c.Kind, string(subj), string(pers), c.Provider, c.Model, CouncilQueued,
		c.PackHash, c.PackText, string(c.PackJSON), c.Question, string(c.EstimateRaw), now, now)
	return err
}

func scanCouncil(row interface{ Scan(...any) error }, withPack bool) (CouncilSession, error) {
	var c CouncilSession
	var subj, pers, packJSON, est string
	var created, updated int64
	if err := row.Scan(&c.ID, &c.Kind, &subj, &pers, &c.Provider, &c.Model, &c.Status, &c.PackHash, &c.PackText, &packJSON, &c.Question, &est, &c.Error, &created, &updated); err != nil {
		return c, err
	}
	json.Unmarshal([]byte(subj), &c.SubjectIDs)
	json.Unmarshal([]byte(pers), &c.PersonaIDs)
	if withPack {
		c.PackJSON = json.RawMessage(packJSON)
	}
	if est != "" {
		c.EstimateRaw = json.RawMessage(est)
	}
	c.CreatedAt, c.UpdatedAt = fromMS(created), fromMS(updated)
	return c, nil
}

const councilCols = `id,kind,subject_ids,persona_ids,provider,model,status,pack_hash,pack_text,pack_json,question,estimate_json,error,created_at,updated_at`

// Council bir oturumu döndürür.
func (s *Store) Council(id string) (CouncilSession, error) {
	return scanCouncil(s.db.QueryRow(`SELECT `+councilCols+` FROM council_sessions WHERE id=?`, id), true)
}

// Councils oturumları (en yeni önce) listeler; paket içeriği döndürülmez.
func (s *Store) Councils(limit int) ([]CouncilSession, error) {
	rows, err := s.db.Query(`SELECT `+councilCols+` FROM council_sessions ORDER BY created_at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CouncilSession{}
	for rows.Next() {
		c, err := scanCouncil(rows, false)
		if err != nil {
			return nil, err
		}
		c.PackText = ""
		out = append(out, c)
	}
	return out, rows.Err()
}

// SetCouncilStatus durumu (ve hata metnini) günceller.
func (s *Store) SetCouncilStatus(id, status, errMsg string) error {
	res, err := s.db.Exec(`UPDATE council_sessions SET status=?, error=?, updated_at=? WHERE id=?`, status, errMsg, time.Now().UnixMilli(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeleteCouncil oturumu ve çıktılarını siler.
func (s *Store) DeleteCouncil(id string) error {
	res, err := s.db.Exec(`DELETE FROM council_sessions WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// InterruptRunning, süreç yeniden başlarken "running/queued" kalmış oturumları devam ettirilebilir
// duruma alır (çıktıları korunur).
func (s *Store) InterruptRunning() error {
	_, err := s.db.Exec(`UPDATE council_sessions SET status=?, updated_at=? WHERE status IN (?,?)`,
		CouncilInterrupted, time.Now().UnixMilli(), CouncilRunning, CouncilQueued)
	return err
}

// SaveOutput bir çıktıyı (aşama, persona) anahtarıyla yazar/günceller.
func (s *Store) SaveOutput(o CouncilOutput) error {
	_, err := s.db.Exec(`INSERT INTO council_outputs(session_id,stage,persona_id,status,raw,output_json,audit_json,prompt_tokens,completion_tokens,cost_usd,from_cache,attempts,error,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(session_id,stage,persona_id) DO UPDATE SET status=excluded.status,raw=excluded.raw,output_json=excluded.output_json,
		audit_json=excluded.audit_json,prompt_tokens=excluded.prompt_tokens,completion_tokens=excluded.completion_tokens,cost_usd=excluded.cost_usd,
		from_cache=excluded.from_cache,attempts=excluded.attempts,error=excluded.error,updated_at=excluded.updated_at`,
		o.SessionID, o.Stage, o.PersonaID, o.Status, o.Raw, string(o.Output), string(o.Audit), o.PromptTokens, o.CompletionTokens,
		o.CostUSD, b2i(o.FromCache), o.Attempts, o.Error, time.Now().UnixMilli())
	return err
}

// Outputs bir oturumun çıktılarını (eklenme sırasıyla) döndürür.
func (s *Store) Outputs(sessionID string) ([]CouncilOutput, error) {
	rows, err := s.db.Query(`SELECT id,session_id,stage,persona_id,status,raw,output_json,audit_json,prompt_tokens,completion_tokens,cost_usd,from_cache,attempts,error,updated_at
		FROM council_outputs WHERE session_id=? ORDER BY id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CouncilOutput{}
	for rows.Next() {
		var o CouncilOutput
		var out1, aud string
		var fc int
		var ts int64
		if err := rows.Scan(&o.ID, &o.SessionID, &o.Stage, &o.PersonaID, &o.Status, &o.Raw, &out1, &aud, &o.PromptTokens, &o.CompletionTokens, &o.CostUSD, &fc, &o.Attempts, &o.Error, &ts); err != nil {
			return nil, err
		}
		if out1 != "" {
			o.Output = json.RawMessage(out1)
		}
		if aud != "" {
			o.Audit = json.RawMessage(aud)
		}
		o.FromCache = fc == 1
		o.UpdatedAt = fromMS(ts)
		out = append(out, o)
	}
	return out, rows.Err()
}

// ErrNoSession oturum yok.
var ErrNoSession = errors.New("konsey oturumu bulunamadı")
