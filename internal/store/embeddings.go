package store

import (
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// EmbedCandidate vektörü henüz çıkarılmamış bir mesajdır.
type EmbedCandidate struct {
	ID   int64
	Text string
}

// embeddable: yalnızca anlamı olan, kısa olmayan metin mesajları.
const embeddableWhere = `kind IN ('text','share') AND length(text) >= 3`

// EmbedStats toplam gömülebilir mesaj ve gömülmüş sayısıdır.
func (s *Store) EmbedStats(model string) (total, done int, err error) {
	if err = s.db.QueryRow(`SELECT COUNT(*) FROM messages WHERE ` + embeddableWhere).Scan(&total); err != nil {
		return
	}
	err = s.db.QueryRow(`SELECT COUNT(*) FROM embeddings WHERE model=?`, model).Scan(&done)
	return
}

// PendingEmbeddings bu model için vektörü olmayan ilk limit mesajı döndürür (kimliğe göre sıralı).
func (s *Store) PendingEmbeddings(model string, limit int) ([]EmbedCandidate, error) {
	rows, err := s.db.Query(`SELECT m.id, m.text FROM messages m
		WHERE m.kind IN ('text','share') AND length(m.text) >= 3 AND NOT EXISTS (SELECT 1 FROM embeddings e WHERE e.msg_id=m.id AND e.model=?)
		ORDER BY m.id LIMIT ?`, model, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EmbedCandidate
	for rows.Next() {
		var c EmbedCandidate
		if err := rows.Scan(&c.ID, &c.Text); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func encodeVec(v []float32) []byte {
	b := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(f))
	}
	return b
}

func decodeVec(b []byte) []float32 {
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return v
}

// SaveEmbeddings vektörleri tek işlemde yazar. Vektörler çağıran tarafından L2-normalize edilmiş olmalıdır.
func (s *Store) SaveEmbeddings(model string, ids []int64, vecs [][]float32) error {
	if len(ids) != len(vecs) {
		return errors.New("kimlik ve vektör sayısı eşleşmiyor")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	st, err := tx.Prepare(`INSERT OR REPLACE INTO embeddings(msg_id,model,dim,vec) VALUES(?,?,?,?)`)
	if err != nil {
		return err
	}
	defer st.Close()
	for i, id := range ids {
		if len(vecs[i]) == 0 {
			return fmt.Errorf("mesaj %d için boş vektör", id)
		}
		if _, err := st.Exec(id, model, len(vecs[i]), encodeVec(vecs[i])); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// VectorIndex bellekte tutulan vektör dizinidir.
type VectorIndex struct {
	Dim  int
	IDs  []int64
	Vecs [][]float32
}

// LoadVectors bir modelin bütün vektörlerini yükler.
func (s *Store) LoadVectors(model string) (*VectorIndex, error) {
	rows, err := s.db.Query(`SELECT msg_id, dim, vec FROM embeddings WHERE model=? ORDER BY msg_id`, model)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	idx := &VectorIndex{}
	for rows.Next() {
		var id int64
		var dim int
		var b []byte
		if err := rows.Scan(&id, &dim, &b); err != nil {
			return nil, err
		}
		v := decodeVec(b)
		if len(v) != dim {
			return nil, fmt.Errorf("mesaj %d: vektör boyutu bozuk", id)
		}
		if idx.Dim == 0 {
			idx.Dim = dim
		} else if idx.Dim != dim {
			return nil, fmt.Errorf("vektör boyutları tutarsız (%d ve %d): model değişmiş olabilir", idx.Dim, dim)
		}
		idx.IDs = append(idx.IDs, id)
		idx.Vecs = append(idx.Vecs, v)
	}
	return idx, rows.Err()
}

// MessagesByIDs verilen kimliklerin mesajlarını (sırayı korumadan) döndürür.
func (s *Store) MessagesByIDs(ids []int64) (map[int64]SearchHit, error) {
	out := map[int64]SearchHit{}
	for _, id := range ids {
		row := s.db.QueryRow(`SELECT m.id,m.conv_id,m.source_id,m.sender,m.ts,m.kind,m.text,m.reactions,m.meta,c.title
			FROM messages m JOIN conversations c ON c.id=m.conv_id WHERE m.id=?`, id)
		var h SearchHit
		var ts int64
		var reactions, meta string
		if err := row.Scan(&h.ID, &h.ConvID, &h.SourceID, &h.Sender, &ts, &h.Kind, &h.Text, &reactions, &meta, &h.Title); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			return nil, err
		}
		h.TS = fromMS(ts)
		out[id] = h
	}
	return out, nil
}
