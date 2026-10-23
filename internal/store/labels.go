package store

import "time"

// ContactLabel bir sohbet karşısına kullanıcının koyduğu ilişki etiketidir (anne, sevgili, arkadaş…).
// Etiketi her zaman kullanıcı koyar ya da bir öneriyi onaylar; Dismissed, önerinin reddedildiğini söyler.
type ContactLabel struct {
	Norm      string `json:"key"`
	Label     string `json:"label"`
	Dismissed bool   `json:"suggestion_dismissed"`
}

// SetContactLabel etiketi yazar. label doluysa etiket konur (ret bayrağı sıfırlanır); label boşken
// dismiss=true ise yalnızca öneri reddedilir; ikisi de boşsa kayıt silinir.
func (s *Store) SetContactLabel(norm, label string, dismiss bool) error {
	if label == "" && !dismiss {
		_, err := s.db.Exec(`DELETE FROM contact_labels WHERE norm=?`, norm)
		return err
	}
	d := 0
	if dismiss && label == "" {
		d = 1
	}
	_, err := s.db.Exec(`INSERT INTO contact_labels(norm,label,dismissed,updated_at) VALUES(?,?,?,?)
		ON CONFLICT(norm) DO UPDATE SET label=excluded.label, dismissed=excluded.dismissed, updated_at=excluded.updated_at`,
		norm, label, d, time.Now().UnixMilli())
	return err
}

// ContactLabels tüm etiketleri anahtara göre döndürür.
func (s *Store) ContactLabels() (map[string]ContactLabel, error) {
	rows, err := s.db.Query(`SELECT norm,label,dismissed FROM contact_labels`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]ContactLabel{}
	for rows.Next() {
		var c ContactLabel
		var d int
		if err := rows.Scan(&c.Norm, &c.Label, &d); err != nil {
			return nil, err
		}
		c.Dismissed = d == 1
		out[c.Norm] = c
	}
	return out, rows.Err()
}
