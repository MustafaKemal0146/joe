package store

import "time"

// HiddenNode ilişki haritasından kullanıcının kaldırdığı bir düğümdür. Veri silinmez; yalnızca
// haritada gösterilmez ve istenirse geri getirilir.
type HiddenNode struct {
	Key      string    `json:"id"`
	Label    string    `json:"label"`
	HiddenAt time.Time `json:"hidden_at"`
}

// HideNode düğümü haritadan gizler (varsa etiketi günceller).
func (s *Store) HideNode(key, label string) error {
	_, err := s.db.Exec(`INSERT INTO map_hidden(node_key,label,hidden_at) VALUES(?,?,?)
		ON CONFLICT(node_key) DO UPDATE SET label=excluded.label`, key, label, time.Now().UnixMilli())
	return err
}

// UnhideNode düğümü haritaya geri getirir.
func (s *Store) UnhideNode(key string) error {
	_, err := s.db.Exec(`DELETE FROM map_hidden WHERE node_key=?`, key)
	return err
}

// HiddenNodes gizlenen düğümleri (en yeni önce) döndürür.
func (s *Store) HiddenNodes() ([]HiddenNode, error) {
	rows, err := s.db.Query(`SELECT node_key,label,hidden_at FROM map_hidden ORDER BY hidden_at DESC, node_key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []HiddenNode{}
	for rows.Next() {
		var h HiddenNode
		var at int64
		if err := rows.Scan(&h.Key, &h.Label, &at); err != nil {
			return nil, err
		}
		h.HiddenAt = fromMS(at)
		out = append(out, h)
	}
	return out, rows.Err()
}
