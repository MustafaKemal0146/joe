package store

import (
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// ExportFormat dışa aktarım biçiminin sürümüdür.
const ExportFormat = 1

// ExportJSON bütün veriyi (kaynaklar, sohbetler, mesajlar, olaylar, kişiler) akışlı olarak tek bir
// JSON belgesi biçiminde yazar. Bellekte tüm veriyi toplamaz.
func (s *Store) ExportJSON(w io.Writer, version string) error {
	enc := func(v any) error { return json.NewEncoder(w).Encode(v) }
	write := func(str string) error { _, err := io.WriteString(w, str); return err }

	if err := write(fmt.Sprintf(`{"format":%d,"software":"joe","version":%q,"exported_at":%q,`, ExportFormat, version, time.Now().UTC().Format(time.RFC3339))); err != nil {
		return err
	}
	srcs, err := s.Sources()
	if err != nil {
		return err
	}
	if err := write(`"sources":`); err != nil {
		return err
	}
	if err := enc(srcs); err != nil {
		return err
	}
	people, err := s.People()
	if err != nil {
		return err
	}
	if err := write(`,"people":`); err != nil {
		return err
	}
	if err := enc(people); err != nil {
		return err
	}

	// Sohbetler ve mesajlar sohbet sohbet akıtılır.
	if err := write(`,"conversations":[`); err != nil {
		return err
	}
	firstConv := true
	for _, src := range srcs {
		convs, err := s.Conversations(src.ID)
		if err != nil {
			return err
		}
		for _, c := range convs {
			if !firstConv {
				if err := write(","); err != nil {
					return err
				}
			}
			firstConv = false
			if err := write(fmt.Sprintf(`{"source_id":%d,"conversation":`, src.ID)); err != nil {
				return err
			}
			if err := enc(c); err != nil {
				return err
			}
			if err := write(`,"messages":[`); err != nil {
				return err
			}
			offset, first := 0, true
			for {
				rows, err := s.Messages(c.ID, 2000, offset)
				if err != nil {
					return err
				}
				for _, m := range rows {
					if !first {
						if err := write(","); err != nil {
							return err
						}
					}
					first = false
					b, _ := json.Marshal(map[string]any{"sender": m.Sender, "ts": m.TS, "kind": m.Kind, "text": m.Text, "reactions": m.Reactions, "meta": m.Meta})
					if _, err := w.Write(b); err != nil {
						return err
					}
				}
				if len(rows) < 2000 {
					break
				}
				offset += len(rows)
			}
			if err := write("]}"); err != nil {
				return err
			}
		}
	}
	if err := write(`],"events":[`); err != nil {
		return err
	}
	rows, err := s.db.Query(`SELECT source_id,kind,ts,actor,counterpart,text,meta FROM events ORDER BY source_id, ts, id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	first := true
	for rows.Next() {
		var sid, ts int64
		var kind, actor, cp, text, meta string
		if err := rows.Scan(&sid, &kind, &ts, &actor, &cp, &text, &meta); err != nil {
			return err
		}
		var m any
		if meta != "" {
			json.Unmarshal([]byte(meta), &m)
		}
		if !first {
			if err := write(","); err != nil {
				return err
			}
		}
		first = false
		b, _ := json.Marshal(map[string]any{"source_id": sid, "kind": kind, "ts": fromMS(ts), "actor": actor, "counterpart": cp, "text": text, "meta": m})
		if _, err := w.Write(b); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := write("]"); err != nil {
		return err
	}
	// Sohbetler ve kişi notları.
	chats, err := s.Chats(100000)
	if err != nil {
		return err
	}
	type chatOut struct {
		ChatSession
		Messages []ChatMessage `json:"messages"`
		Actions  []ChatAction  `json:"actions"`
	}
	outChats := make([]chatOut, 0, len(chats))
	for _, c := range chats {
		msgs, err := s.ChatMessages(c.ID)
		if err != nil {
			return err
		}
		acts, err := s.Actions(c.ID)
		if err != nil {
			return err
		}
		outChats = append(outChats, chatOut{c, msgs, acts})
	}
	notes := []Note{}
	for _, p := range people {
		ns, err := s.Notes(p.ID)
		if err != nil {
			return err
		}
		notes = append(notes, ns...)
	}
	if err := write(`,"chats":`); err != nil {
		return err
	}
	if err := enc(outChats); err != nil {
		return err
	}
	if err := write(`,"notes":`); err != nil {
		return err
	}
	if err := enc(notes); err != nil {
		return err
	}
	return write("}\n")
}

// Wipe bütün kayıtları siler, arama dizinini boşaltır ve dosyayı küçültür (VACUUM). Bu, güvenli
// silme (secure erase) değildir: disk düzeyinde izler kalabilir; tam güvence için diski şifreli tut.
func (s *Store) Wipe() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	for _, q := range []string{
		`DELETE FROM map_hidden`, `DELETE FROM contact_labels`, `DELETE FROM osint_findings`, `DELETE FROM osint_runs`, `DELETE FROM chat_actions`, `DELETE FROM chat_messages`, `DELETE FROM chat_sessions`, `DELETE FROM person_notes`,
		`DELETE FROM identities`, `DELETE FROM people`, `DELETE FROM events`, `DELETE FROM embeddings`,
		`DELETE FROM messages`, `DELETE FROM conversations`, `DELETE FROM sources`,
		`DELETE FROM ledger`, `DELETE FROM response_cache`, `DELETE FROM council_outputs`, `DELETE FROM council_sessions`,
		`INSERT INTO messages_fts(messages_fts) VALUES('delete-all')`,
	} {
		if _, err := tx.Exec(q); err != nil {
			tx.Rollback()
			return fmt.Errorf("silme: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	// Diskte boşalan sayfaları geri ver ve WAL'ı kes.
	_, _ = s.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
	_, err = s.db.Exec(`VACUUM`)
	return err
}
