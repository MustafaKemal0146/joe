package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// ChatSession bir sohbet oturumudur.
type ChatSession struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	PersonID  int64     `json:"person_id,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	SpentUSD  float64   `json:"spent_usd"`
}

// ChatMessage sohbetteki bir mesajdır. Rol: user | assistant | tool | notice.
type ChatMessage struct {
	ID        int64           `json:"id"`
	SessionID string          `json:"session_id"`
	Role      string          `json:"role"`
	Content   string          `json:"content"`
	Meta      json.RawMessage `json:"meta,omitempty"`
	CostUSD   float64         `json:"cost_usd"`
	CreatedAt time.Time       `json:"created_at"`
}

// ChatAction modelin önerdiği, kullanıcı onayı bekleyen (ya da sonuçlanmış) bir eylemdir.
type ChatAction struct {
	ID        string          `json:"id"`
	SessionID string          `json:"session_id"`
	Tool      string          `json:"tool"`
	Args      json.RawMessage `json:"args"`
	Summary   string          `json:"summary"`
	Danger    bool            `json:"danger"`
	Status    string          `json:"status"` // pending | done | rejected | failed
	Result    string          `json:"result,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

// Eylem durumları.
const (
	ActionPending  = "pending"
	ActionDone     = "done"
	ActionRejected = "rejected"
	ActionFailed   = "failed"
)

// ErrChatNotFound oturum yok.
var ErrChatNotFound = errors.New("sohbet bulunamadı")

// CreateChat yeni bir oturum açar.
func (s *Store) CreateChat(id, title string, personID int64) (ChatSession, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "Yeni sohbet"
	}
	if r := []rune(title); len(r) > 120 {
		title = string(r[:120])
	}
	now := time.Now().UnixMilli()
	var pid any
	if personID > 0 {
		pid = personID
	}
	if _, err := s.db.Exec(`INSERT INTO chat_sessions(id,title,person_id,created_at,updated_at) VALUES(?,?,?,?,?)`, id, title, pid, now, now); err != nil {
		return ChatSession{}, err
	}
	return s.Chat(id)
}

func scanChat(row interface{ Scan(...any) error }) (ChatSession, error) {
	var c ChatSession
	var pid sql.NullInt64
	var cr, up int64
	if err := row.Scan(&c.ID, &c.Title, &pid, &cr, &up); err != nil {
		return c, err
	}
	c.PersonID = pid.Int64
	c.CreatedAt, c.UpdatedAt = fromMS(cr), fromMS(up)
	return c, nil
}

// Chat bir oturumu döndürür (harcamasıyla).
func (s *Store) Chat(id string) (ChatSession, error) {
	c, err := scanChat(s.db.QueryRow(`SELECT id,title,person_id,created_at,updated_at FROM chat_sessions WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrChatNotFound
	}
	if err != nil {
		return c, err
	}
	c.SpentUSD, _ = s.SessionSpend("chat:" + id)
	return c, nil
}

// Chats oturumları (en son güncellenen önce) listeler.
func (s *Store) Chats(limit int) ([]ChatSession, error) {
	rows, err := s.db.Query(`SELECT id,title,person_id,created_at,updated_at FROM chat_sessions ORDER BY updated_at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	var out []ChatSession
	for rows.Next() {
		c, err := scanChat(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, c)
	}
	rows.Close()
	for i := range out {
		out[i].SpentUSD, _ = s.SessionSpend("chat:" + out[i].ID)
	}
	if out == nil {
		out = []ChatSession{}
	}
	return out, nil
}

// RenameChat başlığı değiştirir.
func (s *Store) RenameChat(id, title string) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return errors.New("başlık boş olamaz")
	}
	if r := []rune(title); len(r) > 120 {
		title = string(r[:120])
	}
	res, err := s.db.Exec(`UPDATE chat_sessions SET title=?, updated_at=? WHERE id=?`, title, time.Now().UnixMilli(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrChatNotFound
	}
	return nil
}

// DeleteChat oturumu, mesajlarını ve eylemlerini siler; harcama defteri satırları (para kaydı) kalır.
func (s *Store) DeleteChat(id string) error {
	res, err := s.db.Exec(`DELETE FROM chat_sessions WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrChatNotFound
	}
	return nil
}

// AppendChat bir mesaj ekler ve oturumun güncellenme zamanını yeniler.
func (s *Store) AppendChat(m ChatMessage) (ChatMessage, error) {
	switch m.Role {
	case "user", "assistant", "tool", "notice":
	default:
		return m, errors.New("geçersiz rol")
	}
	now := time.Now().UnixMilli()
	res, err := s.db.Exec(`INSERT INTO chat_messages(session_id,role,content,meta,cost_usd,created_at) VALUES(?,?,?,?,?,?)`,
		m.SessionID, m.Role, m.Content, string(m.Meta), m.CostUSD, now)
	if err != nil {
		return m, err
	}
	m.ID, _ = res.LastInsertId()
	m.CreatedAt = fromMS(now)
	_, _ = s.db.Exec(`UPDATE chat_sessions SET updated_at=? WHERE id=?`, now, m.SessionID)
	return m, nil
}

// ChatMessages bir oturumun mesajlarını eskiden yeniye döndürür.
func (s *Store) ChatMessages(sessionID string) ([]ChatMessage, error) {
	rows, err := s.db.Query(`SELECT id,session_id,role,content,meta,cost_usd,created_at FROM chat_messages WHERE session_id=? ORDER BY id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChatMessage{}
	for rows.Next() {
		var m ChatMessage
		var meta string
		var ts int64
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Role, &m.Content, &meta, &m.CostUSD, &ts); err != nil {
			return nil, err
		}
		if meta != "" {
			m.Meta = json.RawMessage(meta)
		}
		m.CreatedAt = fromMS(ts)
		out = append(out, m)
	}
	return out, rows.Err()
}

// CreateAction bekleyen bir eylem kaydeder.
func (s *Store) CreateAction(a ChatAction) error {
	_, err := s.db.Exec(`INSERT INTO chat_actions(id,session_id,tool,args,summary,danger,status,created_at) VALUES(?,?,?,?,?,?,?,?)`,
		a.ID, a.SessionID, a.Tool, string(a.Args), a.Summary, b2i(a.Danger), ActionPending, time.Now().UnixMilli())
	return err
}

// Action bir eylemi döndürür; oturum eşleşmezse bulunamadı sayılır.
func (s *Store) Action(sessionID, id string) (ChatAction, error) {
	var a ChatAction
	var args string
	var danger int
	var ts int64
	err := s.db.QueryRow(`SELECT id,session_id,tool,args,summary,danger,status,result,created_at FROM chat_actions WHERE id=? AND session_id=?`, id, sessionID).
		Scan(&a.ID, &a.SessionID, &a.Tool, &args, &a.Summary, &danger, &a.Status, &a.Result, &ts)
	if errors.Is(err, sql.ErrNoRows) {
		return a, errors.New("eylem bulunamadı")
	}
	a.Args, a.Danger, a.CreatedAt = json.RawMessage(args), danger == 1, fromMS(ts)
	return a, err
}

// ClaimAction bekleyen eylemi atomik olarak "işleniyor"a alır; zaten işlenmişse false döner (çift onay engeli).
func (s *Store) ClaimAction(sessionID, id string) (bool, error) {
	res, err := s.db.Exec(`UPDATE chat_actions SET status='claimed' WHERE id=? AND session_id=? AND status=?`, id, sessionID, ActionPending)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// FinishAction eylemin sonucunu yazar.
func (s *Store) FinishAction(id, status, result string) error {
	_, err := s.db.Exec(`UPDATE chat_actions SET status=?, result=? WHERE id=?`, status, result, id)
	return err
}

// Actions bir oturumun eylemlerini döndürür.
func (s *Store) Actions(sessionID string) ([]ChatAction, error) {
	rows, err := s.db.Query(`SELECT id,session_id,tool,args,summary,danger,status,result,created_at FROM chat_actions WHERE session_id=? ORDER BY created_at, id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChatAction{}
	for rows.Next() {
		var a ChatAction
		var args string
		var danger int
		var ts int64
		if err := rows.Scan(&a.ID, &a.SessionID, &a.Tool, &args, &a.Summary, &danger, &a.Status, &a.Result, &ts); err != nil {
			return nil, err
		}
		a.Args, a.Danger, a.CreatedAt = json.RawMessage(args), danger == 1, fromMS(ts)
		out = append(out, a)
	}
	return out, rows.Err()
}

// ---- Kişi dosyası belleği ----

// Note kullanıcının onayladığı, kaynağı belli bir kişi notudur. Ham mesaj tutulmaz.
type Note struct {
	ID        int64     `json:"id"`
	PersonID  int64     `json:"person_id"`
	Text      string    `json:"text"`
	Source    string    `json:"source"` // "user", "chat:<id>", "K3 (konsey <id>)"…
	CreatedAt time.Time `json:"created_at"`
}

const maxNoteChars = 1000

// AddNote kaynağı zorunlu bir not ekler: kaynaksız bellek kaydı tutulmaz.
func (s *Store) AddNote(personID int64, text, source string) (Note, error) {
	text, source = strings.TrimSpace(text), strings.TrimSpace(source)
	if text == "" {
		return Note{}, errors.New("not boş olamaz")
	}
	if len([]rune(text)) > maxNoteChars {
		return Note{}, errors.New("not 1000 karakteri aşamaz")
	}
	if source == "" {
		return Note{}, errors.New("notun kaynağı belirtilmeli")
	}
	if _, err := s.Person(personID); err != nil {
		return Note{}, err
	}
	now := time.Now().UnixMilli()
	res, err := s.db.Exec(`INSERT INTO person_notes(person_id,text,source,created_at) VALUES(?,?,?,?)`, personID, text, source, now)
	if err != nil {
		return Note{}, err
	}
	id, _ := res.LastInsertId()
	return Note{ID: id, PersonID: personID, Text: text, Source: source, CreatedAt: fromMS(now)}, nil
}

// Notes bir kişinin notlarını eskiden yeniye döndürür.
func (s *Store) Notes(personID int64) ([]Note, error) {
	rows, err := s.db.Query(`SELECT id,person_id,text,source,created_at FROM person_notes WHERE person_id=? ORDER BY id`, personID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Note{}
	for rows.Next() {
		var n Note
		var ts int64
		if err := rows.Scan(&n.ID, &n.PersonID, &n.Text, &n.Source, &ts); err != nil {
			return nil, err
		}
		n.CreatedAt = fromMS(ts)
		out = append(out, n)
	}
	return out, rows.Err()
}

// DeleteNote bir notu siler.
func (s *Store) DeleteNote(id int64) error {
	res, err := s.db.Exec(`DELETE FROM person_notes WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}
