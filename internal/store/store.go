// Package store Joe'nun yerel SQLite veri deposudur: kaynaklar, sohbetler, mesajlar, olaylar,
// kişiler ve kimlikler. Tam metin arama için FTS5 kullanır.
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/MustafaKemal0146/joe/internal/model"
)

// Store veri deposudur.
type Store struct{ db *sql.DB }

var migrations = []string{
	`CREATE TABLE sources(
		id INTEGER PRIMARY KEY, platform TEXT NOT NULL, label TEXT NOT NULL,
		owner_name TEXT NOT NULL DEFAULT '', owner_handle TEXT NOT NULL DEFAULT '',
		owner_confirmed INTEGER NOT NULL DEFAULT 0, sha256 TEXT NOT NULL UNIQUE,
		imported_at INTEGER NOT NULL, stats TEXT NOT NULL DEFAULT '{}',
		warnings TEXT NOT NULL DEFAULT '[]', candidates TEXT NOT NULL DEFAULT '[]');
	CREATE TABLE conversations(
		id INTEGER PRIMARY KEY, source_id INTEGER NOT NULL REFERENCES sources(id) ON DELETE CASCADE,
		title TEXT NOT NULL, is_group INTEGER NOT NULL, participants TEXT NOT NULL,
		msg_count INTEGER NOT NULL, first_ts INTEGER NOT NULL, last_ts INTEGER NOT NULL);
	CREATE INDEX conv_source ON conversations(source_id);
	CREATE TABLE messages(
		id INTEGER PRIMARY KEY, conv_id INTEGER NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
		source_id INTEGER NOT NULL, sender TEXT NOT NULL, ts INTEGER NOT NULL, kind TEXT NOT NULL,
		text TEXT NOT NULL DEFAULT '', reactions TEXT NOT NULL DEFAULT '', meta TEXT NOT NULL DEFAULT '');
	CREATE INDEX msg_conv ON messages(conv_id, ts);
	CREATE INDEX msg_source ON messages(source_id);
	CREATE VIRTUAL TABLE messages_fts USING fts5(text, content='messages', content_rowid='id', tokenize='unicode61 remove_diacritics 2');
	CREATE TRIGGER msg_ai AFTER INSERT ON messages BEGIN INSERT INTO messages_fts(rowid,text) VALUES (new.id,new.text); END;
	CREATE TRIGGER msg_ad AFTER DELETE ON messages BEGIN INSERT INTO messages_fts(messages_fts,rowid,text) VALUES('delete',old.id,old.text); END;
	CREATE TABLE events(
		id INTEGER PRIMARY KEY, source_id INTEGER NOT NULL REFERENCES sources(id) ON DELETE CASCADE,
		kind TEXT NOT NULL, ts INTEGER NOT NULL, actor TEXT NOT NULL, counterpart TEXT NOT NULL DEFAULT '',
		text TEXT NOT NULL DEFAULT '', meta TEXT NOT NULL DEFAULT '');
	CREATE INDEX ev_source ON events(source_id, kind);
	CREATE TABLE people(
		id INTEGER PRIMARY KEY, name TEXT NOT NULL, note TEXT NOT NULL DEFAULT '',
		is_self INTEGER NOT NULL DEFAULT 0, created_at INTEGER NOT NULL);
	CREATE TABLE identities(
		id INTEGER PRIMARY KEY, person_id INTEGER NOT NULL REFERENCES people(id) ON DELETE CASCADE,
		value TEXT NOT NULL, norm TEXT NOT NULL, UNIQUE(person_id, norm));
	CREATE INDEX ident_norm ON identities(norm);`,
	`CREATE TABLE ledger(
		id INTEGER PRIMARY KEY, ts INTEGER NOT NULL, session TEXT NOT NULL, kind TEXT NOT NULL DEFAULT '',
		provider TEXT NOT NULL DEFAULT '', model TEXT NOT NULL DEFAULT '',
		prompt_tokens INTEGER NOT NULL DEFAULT 0, completion_tokens INTEGER NOT NULL DEFAULT 0,
		cached_tokens INTEGER NOT NULL DEFAULT 0, cost_usd REAL NOT NULL DEFAULT 0, from_cache INTEGER NOT NULL DEFAULT 0);
	CREATE INDEX ledger_session ON ledger(session);
	CREATE TABLE response_cache(
		key TEXT PRIMARY KEY, response TEXT NOT NULL, prompt_tokens INTEGER NOT NULL DEFAULT 0,
		completion_tokens INTEGER NOT NULL DEFAULT 0, created_at INTEGER NOT NULL);`,
	`CREATE TABLE council_sessions(
		id TEXT PRIMARY KEY, kind TEXT NOT NULL, subject_ids TEXT NOT NULL, persona_ids TEXT NOT NULL,
		provider TEXT NOT NULL, model TEXT NOT NULL, status TEXT NOT NULL, pack_hash TEXT NOT NULL,
		pack_text TEXT NOT NULL, pack_json TEXT NOT NULL, question TEXT NOT NULL DEFAULT '',
		estimate_json TEXT NOT NULL DEFAULT '', error TEXT NOT NULL DEFAULT '',
		created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL);
	CREATE TABLE council_outputs(
		id INTEGER PRIMARY KEY, session_id TEXT NOT NULL REFERENCES council_sessions(id) ON DELETE CASCADE,
		stage TEXT NOT NULL, persona_id TEXT NOT NULL DEFAULT '', status TEXT NOT NULL,
		raw TEXT NOT NULL DEFAULT '', output_json TEXT NOT NULL DEFAULT '', audit_json TEXT NOT NULL DEFAULT '',
		prompt_tokens INTEGER NOT NULL DEFAULT 0, completion_tokens INTEGER NOT NULL DEFAULT 0,
		cost_usd REAL NOT NULL DEFAULT 0, from_cache INTEGER NOT NULL DEFAULT 0, attempts INTEGER NOT NULL DEFAULT 0,
		error TEXT NOT NULL DEFAULT '', updated_at INTEGER NOT NULL, UNIQUE(session_id, stage, persona_id));
	CREATE INDEX council_out_session ON council_outputs(session_id);`,
	`CREATE TABLE embeddings(
		msg_id INTEGER PRIMARY KEY REFERENCES messages(id) ON DELETE CASCADE,
		model TEXT NOT NULL, dim INTEGER NOT NULL, vec BLOB NOT NULL);
	CREATE INDEX emb_model ON embeddings(model);`,
	`CREATE TABLE chat_sessions(
		id TEXT PRIMARY KEY, title TEXT NOT NULL, person_id INTEGER REFERENCES people(id) ON DELETE SET NULL,
		created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL);
	CREATE TABLE chat_messages(
		id INTEGER PRIMARY KEY, session_id TEXT NOT NULL REFERENCES chat_sessions(id) ON DELETE CASCADE,
		role TEXT NOT NULL, content TEXT NOT NULL DEFAULT '', meta TEXT NOT NULL DEFAULT '',
		cost_usd REAL NOT NULL DEFAULT 0, created_at INTEGER NOT NULL);
	CREATE INDEX chat_msg_session ON chat_messages(session_id, id);
	CREATE TABLE chat_actions(
		id TEXT PRIMARY KEY, session_id TEXT NOT NULL REFERENCES chat_sessions(id) ON DELETE CASCADE,
		tool TEXT NOT NULL, args TEXT NOT NULL, summary TEXT NOT NULL, danger INTEGER NOT NULL DEFAULT 0,
		status TEXT NOT NULL, result TEXT NOT NULL DEFAULT '', created_at INTEGER NOT NULL);
	CREATE INDEX chat_act_session ON chat_actions(session_id);
	CREATE TABLE person_notes(
		id INTEGER PRIMARY KEY, person_id INTEGER NOT NULL REFERENCES people(id) ON DELETE CASCADE,
		text TEXT NOT NULL, source TEXT NOT NULL, created_at INTEGER NOT NULL);
	CREATE INDEX note_person ON person_notes(person_id);`,
	`CREATE TABLE osint_runs(
		id TEXT PRIMARY KEY, username TEXT NOT NULL, status TEXT NOT NULL, total INTEGER NOT NULL DEFAULT 0,
		done INTEGER NOT NULL DEFAULT 0, found INTEGER NOT NULL DEFAULT 0, failed INTEGER NOT NULL DEFAULT 0,
		error TEXT NOT NULL DEFAULT '', started_at INTEGER NOT NULL, finished_at INTEGER NOT NULL DEFAULT 0);
	CREATE TABLE osint_findings(
		id INTEGER PRIMARY KEY, run_id TEXT NOT NULL REFERENCES osint_runs(id) ON DELETE CASCADE,
		site TEXT NOT NULL, category TEXT NOT NULL DEFAULT '', url TEXT NOT NULL, outcome TEXT NOT NULL,
		detail TEXT NOT NULL DEFAULT '', review TEXT NOT NULL DEFAULT 'unreviewed',
		person_id INTEGER REFERENCES people(id) ON DELETE SET NULL, found_at INTEGER NOT NULL);
	CREATE INDEX osint_find_run ON osint_findings(run_id);
	CREATE INDEX osint_find_person ON osint_findings(person_id);`,
	`ALTER TABLE osint_runs ADD COLUMN kind TEXT NOT NULL DEFAULT 'sites'`,
	`CREATE TABLE map_hidden(node_key TEXT PRIMARY KEY, label TEXT NOT NULL, hidden_at INTEGER NOT NULL)`,
	`CREATE TABLE contact_labels(norm TEXT PRIMARY KEY, label TEXT NOT NULL DEFAULT '', dismissed INTEGER NOT NULL DEFAULT 0, updated_at INTEGER NOT NULL)`,
}

// Open veritabanını açar ve şemayı günceller.
func Open(path string) (*Store, error) {
	dsn := path
	if path != ":memory:" {
		dsn = "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)"
	} else {
		dsn = "file::memory:?_pragma=foreign_keys(1)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // tek yazar; bellek içi veritabanı bağlantıya bağlıdır
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close bağlantıyı kapatır.
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	var v int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		return err
	}
	for i := v; i < len(migrations); i++ {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("migrasyon %d: %w", i+1, err)
		}
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version=%d", i+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func ms(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

func fromMS(v int64) time.Time {
	if v == 0 {
		return time.Time{}
	}
	return time.UnixMilli(v).UTC()
}

func jsonOrEmpty(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case map[string]any:
		if len(t) == 0 {
			return ""
		}
	case []model.Reaction:
		if len(t) == 0 {
			return ""
		}
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// Source bir içe aktarılmış kaynağın özetidir.
type Source struct {
	ID             int64          `json:"id"`
	Platform       string         `json:"platform"`
	Label          string         `json:"label"`
	OwnerName      string         `json:"owner_name"`
	OwnerHandle    string         `json:"owner_handle"`
	OwnerConfirmed bool           `json:"owner_confirmed"`
	SHA256         string         `json:"sha256"`
	ImportedAt     time.Time      `json:"imported_at"`
	Stats          map[string]int `json:"stats"`
	Warnings       []string       `json:"warnings"`
	Candidates     []string       `json:"candidates"`
}

// ErrDuplicate aynı içeriğin yeniden içe aktarıldığını bildirir.
type ErrDuplicate struct{ SourceID int64 }

func (e ErrDuplicate) Error() string { return "bu kaynak zaten içe aktarılmış" }

// SaveImport ayrıştırılmış kaynağı tek işlemde kaydeder. Aynı SHA-256 ikinci kez kaydedilmez.
func (s *Store) SaveImport(imp *model.Import, sha string) (int64, error) {
	var existing int64
	if err := s.db.QueryRow("SELECT id FROM sources WHERE sha256=?", sha).Scan(&existing); err == nil {
		return existing, ErrDuplicate{existing}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	stats, _ := json.Marshal(imp.Stats)
	warns, _ := json.Marshal(imp.Warnings)
	cands, _ := json.Marshal(imp.Candidates)
	confirmed := 0
	if imp.OwnerName != "" && !imp.OwnerGuessed {
		confirmed = 1
	}
	res, err := tx.Exec(`INSERT INTO sources(platform,label,owner_name,owner_handle,owner_confirmed,sha256,imported_at,stats,warnings,candidates)
		VALUES(?,?,?,?,?,?,?,?,?,?)`, imp.Platform, imp.Label, imp.OwnerName, imp.OwnerHandle, confirmed, sha, time.Now().UnixMilli(), string(stats), string(warns), string(cands))
	if err != nil {
		return 0, err
	}
	sid, _ := res.LastInsertId()

	insMsg, err := tx.Prepare(`INSERT INTO messages(conv_id,source_id,sender,ts,kind,text,reactions,meta) VALUES(?,?,?,?,?,?,?,?)`)
	if err != nil {
		return 0, err
	}
	defer insMsg.Close()
	for _, c := range imp.Conversations {
		parts, _ := json.Marshal(c.Participants)
		var first, last int64
		if n := len(c.Messages); n > 0 {
			first, last = ms(c.Messages[0].TS), ms(c.Messages[n-1].TS)
		}
		r, err := tx.Exec(`INSERT INTO conversations(source_id,title,is_group,participants,msg_count,first_ts,last_ts) VALUES(?,?,?,?,?,?,?)`,
			sid, c.Title, b2i(c.IsGroup), string(parts), len(c.Messages), first, last)
		if err != nil {
			return 0, err
		}
		cid, _ := r.LastInsertId()
		for _, m := range c.Messages {
			if _, err := insMsg.Exec(cid, sid, m.Sender, ms(m.TS), m.Kind, m.Text, jsonOrEmpty(m.Reactions), jsonOrEmpty(m.Meta)); err != nil {
				return 0, err
			}
		}
	}
	insEv, err := tx.Prepare(`INSERT INTO events(source_id,kind,ts,actor,counterpart,text,meta) VALUES(?,?,?,?,?,?,?)`)
	if err != nil {
		return 0, err
	}
	defer insEv.Close()
	for _, e := range imp.Events {
		if _, err := insEv.Exec(sid, e.Kind, ms(e.TS), e.Actor, e.Counterpart, e.Text, jsonOrEmpty(e.Meta)); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return sid, nil
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func scanSource(row interface{ Scan(...any) error }) (Source, error) {
	var s Source
	var conf int
	var at int64
	var stats, warns, cands string
	if err := row.Scan(&s.ID, &s.Platform, &s.Label, &s.OwnerName, &s.OwnerHandle, &conf, &s.SHA256, &at, &stats, &warns, &cands); err != nil {
		return s, err
	}
	s.OwnerConfirmed = conf == 1
	s.ImportedAt = fromMS(at)
	json.Unmarshal([]byte(stats), &s.Stats)
	json.Unmarshal([]byte(warns), &s.Warnings)
	json.Unmarshal([]byte(cands), &s.Candidates)
	return s, nil
}

const sourceCols = `id,platform,label,owner_name,owner_handle,owner_confirmed,sha256,imported_at,stats,warnings,candidates`

// Sources tüm kaynakları döndürür.
func (s *Store) Sources() ([]Source, error) {
	rows, err := s.db.Query(`SELECT ` + sourceCols + ` FROM sources ORDER BY imported_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Source{}
	for rows.Next() {
		src, err := scanSource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, src)
	}
	return out, rows.Err()
}

// Source tek bir kaynağı döndürür.
func (s *Store) Source(id int64) (Source, error) {
	return scanSource(s.db.QueryRow(`SELECT `+sourceCols+` FROM sources WHERE id=?`, id))
}

// SetOwner kaynağın sahibini belirler ve onaylı sayar.
func (s *Store) SetOwner(id int64, name, handle string) error {
	name, handle = strings.TrimSpace(name), strings.TrimPrefix(strings.TrimSpace(handle), "@")
	if name == "" && handle == "" {
		return errors.New("sahip adı boş olamaz")
	}
	res, err := s.db.Exec(`UPDATE sources SET owner_name=?, owner_handle=?, owner_confirmed=1 WHERE id=?`, name, handle, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeleteSource kaynağı ve bağlı tüm verisini siler.
func (s *Store) DeleteSource(id int64) error {
	res, err := s.db.Exec(`DELETE FROM sources WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// Conversation bir sohbetin özetidir.
type Conversation struct {
	ID           int64     `json:"id"`
	SourceID     int64     `json:"source_id"`
	Title        string    `json:"title"`
	IsGroup      bool      `json:"is_group"`
	Participants []string  `json:"participants"`
	Count        int       `json:"count"`
	First        time.Time `json:"first"`
	Last         time.Time `json:"last"`
}

// Conversations bir kaynağın sohbetlerini (mesaj sayısına göre azalan) döndürür.
func (s *Store) Conversations(sourceID int64) ([]Conversation, error) {
	rows, err := s.db.Query(`SELECT id,source_id,title,is_group,participants,msg_count,first_ts,last_ts FROM conversations WHERE source_id=? ORDER BY msg_count DESC`, sourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Conversation{}
	for rows.Next() {
		var c Conversation
		var g int
		var parts string
		var f, l int64
		if err := rows.Scan(&c.ID, &c.SourceID, &c.Title, &g, &parts, &c.Count, &f, &l); err != nil {
			return nil, err
		}
		c.IsGroup = g == 1
		c.First, c.Last = fromMS(f), fromMS(l)
		json.Unmarshal([]byte(parts), &c.Participants)
		out = append(out, c)
	}
	return out, rows.Err()
}

// MessageRow depodan okunan bir mesajdır.
type MessageRow struct {
	ID       int64
	ConvID   int64
	SourceID int64
	model.Message
}

func scanMessage(rows *sql.Rows) (MessageRow, error) {
	var m MessageRow
	var ts int64
	var reactions, meta string
	if err := rows.Scan(&m.ID, &m.ConvID, &m.SourceID, &m.Sender, &ts, &m.Kind, &m.Text, &reactions, &meta); err != nil {
		return m, err
	}
	m.TS = fromMS(ts)
	if reactions != "" {
		json.Unmarshal([]byte(reactions), &m.Reactions)
	}
	if meta != "" {
		json.Unmarshal([]byte(meta), &m.Meta)
	}
	return m, nil
}

const msgCols = `id,conv_id,source_id,sender,ts,kind,text,reactions,meta`

// Messages bir sohbetin mesajlarını zamana göre döndürür.
func (s *Store) Messages(convID int64, limit, offset int) ([]MessageRow, error) {
	rows, err := s.db.Query(`SELECT `+msgCols+` FROM messages WHERE conv_id=? ORDER BY ts, id LIMIT ? OFFSET ?`, convID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MessageRow{}
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// MessageIndex bir mesajın sohbet içindeki sırasını (0'dan) döndürür; mesaj sohbette değilse ok=false.
func (s *Store) MessageIndex(convID, msgID int64) (int, bool) {
	var ts int64
	if err := s.db.QueryRow(`SELECT ts FROM messages WHERE id=? AND conv_id=?`, msgID, convID).Scan(&ts); err != nil {
		return 0, false
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM messages WHERE conv_id=? AND (ts<? OR (ts=? AND id<?))`, convID, ts, ts, msgID).Scan(&n); err != nil {
		return 0, false
	}
	return n, true
}

// EventRow depodan okunan bir olaydır.
type EventRow struct {
	ID       int64
	SourceID int64
	model.Event
}

// Corpus analiz için belleğe yüklenen kaynak verisidir.
type Corpus struct {
	Sources  map[int64]Source
	Convs    []Conversation
	Messages []MessageRow // conv_id, ts sıralı
	Events   []EventRow
}

// LoadCorpus verilen kaynakların (boşsa tümünün) verisini yükler.
func (s *Store) LoadCorpus(sourceIDs []int64) (*Corpus, error) {
	all, err := s.Sources()
	if err != nil {
		return nil, err
	}
	want := map[int64]bool{}
	for _, id := range sourceIDs {
		want[id] = true
	}
	c := &Corpus{Sources: map[int64]Source{}}
	for _, src := range all {
		if len(want) == 0 || want[src.ID] {
			c.Sources[src.ID] = src
			convs, err := s.Conversations(src.ID)
			if err != nil {
				return nil, err
			}
			c.Convs = append(c.Convs, convs...)
		}
	}
	if len(c.Sources) == 0 {
		return c, nil
	}
	rows, err := s.db.Query(`SELECT ` + msgCols + ` FROM messages ORDER BY conv_id, ts, id`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		if _, ok := c.Sources[m.SourceID]; ok {
			c.Messages = append(c.Messages, m)
		}
	}
	rows.Close()
	erows, err := s.db.Query(`SELECT id,source_id,kind,ts,actor,counterpart,text,meta FROM events ORDER BY ts, id`)
	if err != nil {
		return nil, err
	}
	defer erows.Close()
	for erows.Next() {
		var e EventRow
		var ts int64
		var meta string
		if err := erows.Scan(&e.ID, &e.SourceID, &e.Kind, &ts, &e.Actor, &e.Counterpart, &e.Text, &meta); err != nil {
			return nil, err
		}
		if _, ok := c.Sources[e.SourceID]; !ok {
			continue
		}
		e.TS = fromMS(ts)
		if meta != "" {
			json.Unmarshal([]byte(meta), &e.Meta)
		}
		c.Events = append(c.Events, e)
	}
	return c, erows.Err()
}

// SearchHit bir arama sonucudur.
type SearchHit struct {
	MessageRow
	Title   string `json:"title"`
	Snippet string `json:"snippet"`
}

// SearchAny verilen sözcüklerden HERHANGİ BİRİNİ (önek eşleşmesiyle) içeren mesajları, en iyi
// eşleşme önce olmak üzere döndürür. Doğal dil sorularından çıkarılan anahtar sözcükler içindir:
// Türkçe ekler ("gecelerde", "geceleri") önek eşleşmesiyle yakalanır.
func (s *Store) SearchAny(terms []string, limit int) ([]SearchHit, error) {
	var parts []string
	for _, t := range terms {
		if t = strings.TrimSpace(t); t != "" {
			parts = append(parts, `"`+strings.ReplaceAll(t, `"`, `""`)+`"*`)
		}
	}
	if len(parts) == 0 {
		return []SearchHit{}, nil
	}
	rows, err := s.db.Query(`SELECT m.id,m.conv_id,m.source_id,m.sender,m.ts,m.kind,m.text,m.reactions,m.meta,c.title,
		snippet(messages_fts,0,'«','»','…',12)
		FROM messages_fts JOIN messages m ON m.id=messages_fts.rowid JOIN conversations c ON c.id=m.conv_id
		WHERE messages_fts MATCH ? ORDER BY rank LIMIT ?`, strings.Join(parts, " OR "), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SearchHit{}
	for rows.Next() {
		var h SearchHit
		var ts int64
		var reactions, meta string
		if err := rows.Scan(&h.ID, &h.ConvID, &h.SourceID, &h.Sender, &ts, &h.Kind, &h.Text, &reactions, &meta, &h.Title, &h.Snippet); err != nil {
			return nil, err
		}
		h.TS = fromMS(ts)
		out = append(out, h)
	}
	return out, rows.Err()
}

// Search mesajlarda tam metin arar. Sorgu kullanıcı girdisidir; FTS sözdizimi olarak değil,
// tırnaklanmış sözcük öbekleri olarak yorumlanır.
func (s *Store) Search(q string, limit int) ([]SearchHit, error) {
	terms := strings.Fields(q)
	if len(terms) == 0 {
		return []SearchHit{}, nil
	}
	quoted := make([]string, len(terms))
	for i, t := range terms {
		quoted[i] = `"` + strings.ReplaceAll(t, `"`, `""`) + `"`
	}
	rows, err := s.db.Query(`SELECT m.id,m.conv_id,m.source_id,m.sender,m.ts,m.kind,m.text,m.reactions,m.meta,c.title,
		snippet(messages_fts,0,'«','»','…',12)
		FROM messages_fts JOIN messages m ON m.id=messages_fts.rowid JOIN conversations c ON c.id=m.conv_id
		WHERE messages_fts MATCH ? ORDER BY rank LIMIT ?`, strings.Join(quoted, " "), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SearchHit{}
	for rows.Next() {
		var h SearchHit
		var ts int64
		var reactions, meta string
		if err := rows.Scan(&h.ID, &h.ConvID, &h.SourceID, &h.Sender, &ts, &h.Kind, &h.Text, &reactions, &meta, &h.Title, &h.Snippet); err != nil {
			return nil, err
		}
		h.TS = fromMS(ts)
		out = append(out, h)
	}
	return out, rows.Err()
}
