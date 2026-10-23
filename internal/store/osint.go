package store

import (
	"database/sql"
	"errors"
	"time"
)

// OSINT çalışma ve bulgu durumları.
const (
	OsintRunning  = "running"
	OsintDone     = "done"
	OsintFailed   = "failed"
	OsintCanceled = "canceled"

	ReviewUnreviewed = "unreviewed"
	ReviewConfirmed  = "confirmed"
	ReviewRejected   = "rejected"
	ReviewUnsure     = "unsure"
)

// OsintRun bir kullanıcı adı taramasıdır.
type OsintRun struct {
	ID         string    `json:"id"`
	Username   string    `json:"username"`
	Kind       string    `json:"kind"` // sites | dir_username | dir_full_name
	Status     string    `json:"status"`
	Total      int       `json:"total"`
	Done       int       `json:"done"`
	Found      int       `json:"found"`
	Failed     int       `json:"failed"`
	Error      string    `json:"error,omitempty"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
}

// OsintFinding bir sitedeki aday bulgudur. Outcome "found" aday profildir; "failed" sitenin
// denetlenemediğini (hata, engel) söyler ve "hesap yok" anlamına gelmez.
type OsintFinding struct {
	ID       int64     `json:"id"`
	RunID    string    `json:"run_id"`
	Username string    `json:"username,omitempty"`
	Site     string    `json:"site"`
	Category string    `json:"category"`
	URL      string    `json:"url"`
	Outcome  string    `json:"outcome"`
	Detail   string    `json:"detail,omitempty"`
	Review   string    `json:"review"`
	PersonID int64     `json:"person_id,omitempty"`
	FoundAt  time.Time `json:"found_at"`
}

// CreateOsintRun yeni bir tarama kaydı açar.
func (s *Store) CreateOsintRun(id, username string, total int) error {
	return s.CreateOsintRunKind(id, username, "sites", total)
}

// CreateOsintRunKind belirli türde (sites, dir_username, dir_full_name) tarama kaydı açar.
func (s *Store) CreateOsintRunKind(id, query, kind string, total int) error {
	_, err := s.db.Exec(`INSERT INTO osint_runs(id,username,kind,status,total,started_at) VALUES(?,?,?,?,?,?)`, id, query, kind, OsintRunning, total, time.Now().UnixMilli())
	return err
}

// UpdateOsintProgress ilerleme sayaçlarını yazar.
func (s *Store) UpdateOsintProgress(id string, done, found, failed int) error {
	_, err := s.db.Exec(`UPDATE osint_runs SET done=?, found=?, failed=? WHERE id=?`, done, found, failed, id)
	return err
}

// FinishOsintRun taramayı bitirir.
func (s *Store) FinishOsintRun(id, status, errMsg string) error {
	_, err := s.db.Exec(`UPDATE osint_runs SET status=?, error=?, finished_at=? WHERE id=?`, status, errMsg, time.Now().UnixMilli(), id)
	return err
}

// AddOsintFindings bulguları tek işlemde yazar.
func (s *Store) AddOsintFindings(runID string, fs []OsintFinding) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	st, err := tx.Prepare(`INSERT INTO osint_findings(run_id,site,category,url,outcome,detail,found_at) VALUES(?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer st.Close()
	now := time.Now().UnixMilli()
	for _, f := range fs {
		if _, err := st.Exec(runID, f.Site, f.Category, f.URL, f.Outcome, f.Detail, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func scanRun(row interface{ Scan(...any) error }) (OsintRun, error) {
	var r OsintRun
	var st, fin int64
	err := row.Scan(&r.ID, &r.Username, &r.Kind, &r.Status, &r.Total, &r.Done, &r.Found, &r.Failed, &r.Error, &st, &fin)
	r.StartedAt, r.FinishedAt = fromMS(st), fromMS(fin)
	return r, err
}

const runCols = `id,username,kind,status,total,done,found,failed,error,started_at,finished_at`

// OsintRun bir taramayı döndürür.
func (s *Store) OsintRun(id string) (OsintRun, error) {
	r, err := scanRun(s.db.QueryRow(`SELECT `+runCols+` FROM osint_runs WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return r, errors.New("tarama bulunamadı")
	}
	return r, err
}

// OsintRuns taramaları (en yeni önce) listeler.
func (s *Store) OsintRuns(limit int) ([]OsintRun, error) {
	rows, err := s.db.Query(`SELECT `+runCols+` FROM osint_runs ORDER BY started_at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []OsintRun{}
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// InterruptOsint süreç yeniden başlarken "running" kalmış taramaları iptal edilmiş sayar.
func (s *Store) InterruptOsint() error {
	_, err := s.db.Exec(`UPDATE osint_runs SET status=?, error='süreç kapandı', finished_at=? WHERE status=?`, OsintCanceled, time.Now().UnixMilli(), OsintRunning)
	return err
}

// DeleteOsintRun taramayı ve bulgularını siler.
func (s *Store) DeleteOsintRun(id string) error {
	res, err := s.db.Exec(`DELETE FROM osint_runs WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("tarama bulunamadı")
	}
	return nil
}

const findCols = `f.id,f.run_id,r.username,f.site,f.category,f.url,f.outcome,f.detail,f.review,COALESCE(f.person_id,0),f.found_at`

func scanFinding(rows *sql.Rows) (OsintFinding, error) {
	var f OsintFinding
	var ts int64
	err := rows.Scan(&f.ID, &f.RunID, &f.Username, &f.Site, &f.Category, &f.URL, &f.Outcome, &f.Detail, &f.Review, &f.PersonID, &ts)
	f.FoundAt = fromMS(ts)
	return f, err
}

func (s *Store) findings(where string, args ...any) ([]OsintFinding, error) {
	rows, err := s.db.Query(`SELECT `+findCols+` FROM osint_findings f JOIN osint_runs r ON r.id=f.run_id `+where+` ORDER BY f.outcome='found' DESC, f.site COLLATE NOCASE, f.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []OsintFinding{}
	for rows.Next() {
		f, err := scanFinding(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// OsintFindings bir taramanın bulgularını döndürür (bulunanlar önce).
func (s *Store) OsintFindings(runID string) ([]OsintFinding, error) {
	return s.findings(`WHERE f.run_id=?`, runID)
}

// ReviewFinding bulgunun insan kararını kaydeder. "found" olmayan satırlar (denetlenemeyen siteler)
// değerlendirilemez. Karar "confirmed" dışına çekilirse kişi bağı da kopar.
func (s *Store) ReviewFinding(id int64, review string) error {
	switch review {
	case ReviewUnreviewed, ReviewConfirmed, ReviewRejected, ReviewUnsure:
	default:
		return errors.New("geçersiz karar")
	}
	res, err := s.db.Exec(`UPDATE osint_findings SET review=?, person_id=CASE WHEN ?='confirmed' THEN person_id ELSE NULL END WHERE id=? AND outcome='found'`, review, review, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("değerlendirilebilir bulgu bulunamadı")
	}
	return nil
}

// AttachFinding yalnızca kullanıcının DOĞRULADIĞI bir bulguyu kişi dosyasına bağlar.
func (s *Store) AttachFinding(id, personID int64) error {
	if _, err := s.Person(personID); err != nil {
		return err
	}
	res, err := s.db.Exec(`UPDATE osint_findings SET person_id=? WHERE id=? AND review='confirmed' AND outcome='found'`, personID, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("yalnızca doğrulanmış (onaylanmış) bulgu kişiye bağlanabilir")
	}
	return nil
}

// PersonFindings bir kişiye bağlı, doğrulanmış bulguları döndürür.
func (s *Store) PersonFindings(personID int64) ([]OsintFinding, error) {
	return s.findings(`WHERE f.person_id=? AND f.review='confirmed'`, personID)
}
