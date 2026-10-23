package store

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/MustafaKemal0146/joe/internal/model"
)

// Identity bir kişiye bağlı ad ya da kullanıcı adıdır.
type Identity struct {
	ID    int64  `json:"id"`
	Value string `json:"value"`
	Norm  string `json:"norm"`
}

// Person bir kişi dosyasıdır.
type Person struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	Note       string     `json:"note"`
	IsSelf     bool       `json:"is_self"`
	CreatedAt  time.Time  `json:"created_at"`
	Identities []Identity `json:"identities"`
}

// CreatePerson yeni bir kişi dosyası açar.
func (s *Store) CreatePerson(name, note string, self bool, identities ...string) (Person, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Person{}, errors.New("kişi adı boş olamaz")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return Person{}, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`INSERT INTO people(name,note,is_self,created_at) VALUES(?,?,?,?)`, name, note, b2i(self), time.Now().UnixMilli())
	if err != nil {
		return Person{}, err
	}
	id, _ := res.LastInsertId()
	for _, v := range append([]string{name}, identities...) {
		if err := addIdentity(tx, id, v); err != nil {
			return Person{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Person{}, err
	}
	return s.Person(id)
}

type execer interface {
	Exec(string, ...any) (sql.Result, error)
}

func addIdentity(tx execer, personID int64, value string) error {
	value = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(value), "@"))
	n := model.Norm(value)
	if n == "" {
		return nil
	}
	_, err := tx.Exec(`INSERT OR IGNORE INTO identities(person_id,value,norm) VALUES(?,?,?)`, personID, value, n)
	return err
}

// AddIdentity kişiye yeni bir ad/kullanıcı adı ekler.
func (s *Store) AddIdentity(personID int64, value string) error {
	if model.Norm(value) == "" {
		return errors.New("kimlik boş olamaz")
	}
	if _, err := s.Person(personID); err != nil {
		return err
	}
	return addIdentity(s.db, personID, value)
}

// RemoveIdentity bir kimliği kaldırır. Kişinin son kimliği kaldırılamaz.
func (s *Store) RemoveIdentity(personID, identityID int64) error {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM identities WHERE person_id=?`, personID).Scan(&n); err != nil {
		return err
	}
	if n <= 1 {
		return errors.New("kişinin en az bir kimliği olmalı")
	}
	res, err := s.db.Exec(`DELETE FROM identities WHERE id=? AND person_id=?`, identityID, personID)
	if err != nil {
		return err
	}
	if c, _ := res.RowsAffected(); c == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// Person bir kişiyi kimlikleriyle birlikte döndürür.
func (s *Store) Person(id int64) (Person, error) {
	ps, err := s.people(`WHERE p.id=?`, id)
	if err != nil {
		return Person{}, err
	}
	if len(ps) == 0 {
		return Person{}, sql.ErrNoRows
	}
	return ps[0], nil
}

// People tüm kişileri döndürür (kendin önce).
func (s *Store) People() ([]Person, error) { return s.people(``) }

func (s *Store) people(where string, args ...any) ([]Person, error) {
	rows, err := s.db.Query(`SELECT p.id,p.name,p.note,p.is_self,p.created_at,COALESCE(i.id,0),COALESCE(i.value,''),COALESCE(i.norm,'')
		FROM people p LEFT JOIN identities i ON i.person_id=p.id `+where+` ORDER BY p.is_self DESC, p.name COLLATE NOCASE, i.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Person{}
	idx := map[int64]int{}
	for rows.Next() {
		var p Person
		var self int
		var at int64
		var ident Identity
		if err := rows.Scan(&p.ID, &p.Name, &p.Note, &self, &at, &ident.ID, &ident.Value, &ident.Norm); err != nil {
			return nil, err
		}
		i, ok := idx[p.ID]
		if !ok {
			p.IsSelf, p.CreatedAt = self == 1, fromMS(at)
			p.Identities = []Identity{}
			out = append(out, p)
			i = len(out) - 1
			idx[p.ID] = i
		}
		if ident.ID != 0 {
			out[i].Identities = append(out[i].Identities, ident)
		}
	}
	return out, rows.Err()
}

// UpdatePerson adı, notu ve "ben" işaretini günceller. Tek bir kişi "ben" olabilir.
func (s *Store) UpdatePerson(id int64, name, note *string, self *bool) error {
	p, err := s.Person(id)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if name != nil {
		if strings.TrimSpace(*name) == "" {
			return errors.New("kişi adı boş olamaz")
		}
		p.Name = strings.TrimSpace(*name)
	}
	if note != nil {
		p.Note = *note
	}
	if self != nil {
		p.IsSelf = *self
		if *self {
			if _, err := tx.Exec(`UPDATE people SET is_self=0 WHERE id<>?`, id); err != nil {
				return err
			}
		}
	}
	if _, err := tx.Exec(`UPDATE people SET name=?, note=?, is_self=? WHERE id=?`, p.Name, p.Note, b2i(p.IsSelf), id); err != nil {
		return err
	}
	return tx.Commit()
}

// DeletePerson kişi dosyasını siler (kaynak veri etkilenmez).
func (s *Store) DeletePerson(id int64) error {
	res, err := s.db.Exec(`DELETE FROM people WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// EnsureOwnerPerson, kaynağın sahibi için kişi dosyası yoksa açar. İlk kişi "ben" sayılır.
// Var olan bir kişinin kimliğiyle eşleşirse o kişiye döner.
func (s *Store) EnsureOwnerPerson(src Source) (Person, error) {
	var idents []string
	for _, v := range []string{src.OwnerName, src.OwnerHandle} {
		if model.Norm(v) != "" {
			idents = append(idents, v)
		}
	}
	if len(idents) == 0 {
		return Person{}, errors.New("sahip bilinmiyor")
	}
	people, err := s.People()
	if err != nil {
		return Person{}, err
	}
	for _, p := range people {
		for _, id := range p.Identities {
			for _, v := range idents {
				if id.Norm == model.Norm(v) {
					for _, v2 := range idents {
						_ = addIdentity(s.db, p.ID, v2)
					}
					return s.Person(p.ID)
				}
			}
		}
	}
	name := src.OwnerName
	if name == "" {
		name = src.OwnerHandle
	}
	return s.CreatePerson(name, "", len(people) == 0, idents...)
}
