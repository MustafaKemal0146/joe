package analytics

import (
	"strings"
	"testing"
	"time"

	"github.com/MustafaKemal0146/joe/internal/model"
	"github.com/MustafaKemal0146/joe/internal/store"
)

func callRow(sender, kind string, ts time.Time, secs any) store.MessageRow {
	m := store.MessageRow{}
	m.Sender, m.Kind, m.TS = sender, kind, ts
	if secs != nil {
		m.Meta = map[string]any{"seconds": secs}
	}
	return m
}

func TestCallsText(t *testing.T) {
	d := time.Date(2026, 3, 1, 23, 30, 0, 0, time.UTC)
	rows := []store.MessageRow{
		callRow("A", model.KindCall, d, float64(600)),
		callRow("B", model.KindCall, d.AddDate(0, 1, 0).Add(3*time.Hour), int64(30)),
		callRow("A", model.KindMissedCall, d.AddDate(0, 1, 1), nil),
		callRow("A", model.KindMissedCall, d.AddDate(0, 2, 0), nil),
	}
	out := CallsText(rows, time.UTC)
	for _, want := range []string{"A başlattı: 3 arama (1 cevaplandı, 2 cevapsız", "B başlattı: 1 arama", "Toplam: 4 kayıt", "en uzun 10 dk 0 sn", "1 dakikadan kısa: 1", "Aylık arama sayısı"} {
		if !strings.Contains(out, want) {
			t.Errorf("%q yok:\n%s", want, out)
		}
	}
	if CallsText(rows[:2], time.UTC) != "" {
		t.Error("az kayıtta metrik üretilmemeli")
	}
}
