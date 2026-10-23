// Package whatsapp, WhatsApp "Sohbeti dışa aktar" metin dosyalarını kanonik modele çevirir.
//
// Satır başlığının biçimi cihaz, dil ve bölgeye göre değişir ("[26.09.2026 20:37:09] ",
// "9/26/26, 8:37 PM - ", "26/09/2026 20:37 - "...). Ayrıştırıcı tüm bu biçimleri tek bir
// düzenli ifadeyle okur; gün/ay sırası dosyadaki değerlerden çıkarılır, çıkarılamazsa
// uyarı verilip yerel varsayılan kullanılır.
package whatsapp

import (
	"bufio"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/MustafaKemal0146/joe/internal/ingest/zipfs"
	"github.com/MustafaKemal0146/joe/internal/model"
)

var header = regexp.MustCompile(`^\x{200e}?\[?(\d{1,4})([./-])(\d{1,2})[./-](\d{1,4}),?\s+(\d{1,2})[:.](\d{2})(?:[:.](\d{2}))?\s*([AaPp]\.?[Mm]\.?|ÖÖ|ÖS)?\]?\s*(?:[-–]\s*)?(.*)$`)

var invisibles = strings.NewReplacer("\u200e", "", "\u200f", "", "\u202f", " ", "\u00a0", " ", "\ufeff", "")

type rawLine struct {
	a, b, year int
	sep        string
	h, m, s    int
	pm         int // 0 yok, 1 AM, 2 PM
	rest       string
}

type rawMsg struct {
	rawLine
	body []string
}

// Parse bir kaynaktaki WhatsApp sohbet dosyalarını okur (her .txt bir sohbet).
func Parse(src *zipfs.Source, loc *time.Location) (*model.Import, error) {
	if loc == nil {
		loc = time.Local
	}
	imp := &model.Import{Platform: "whatsapp", Label: src.Name, Stats: map[string]int{}, Warnings: append([]string(nil), src.Warnings...)}
	var files []string
	_ = fs.WalkDir(src.FS, ".", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(strings.ToLower(p), ".txt") {
			files = append(files, p)
		}
		return nil
	})
	sort.Strings(files)
	for _, f := range files {
		data, err := zipfs.ReadFileLimited(src.FS, f, 512<<20)
		if err != nil {
			imp.Warnings = append(imp.Warnings, err.Error())
			continue
		}
		conv, warns := parseChat(string(data), loc)
		imp.Warnings = append(imp.Warnings, warns...)
		if len(conv.Messages) == 0 {
			continue
		}
		conv.Title = titleFromName(f, conv.Participants)
		imp.Conversations = append(imp.Conversations, conv)
		imp.Stats["messages"] += len(conv.Messages)
		imp.Stats["conversations"]++
	}
	if len(imp.Conversations) == 0 {
		return nil, fmt.Errorf("WhatsApp sohbeti tanınmadı: tarih-saat başlıklı satır bulunamadı")
	}
	imp.OwnerName, imp.OwnerGuessed, imp.Candidates = detectOwner(files, imp.Conversations)
	return imp, nil
}

var nameRx = []*regexp.Regexp{
	regexp.MustCompile(`(?i)whatsapp chat (?:with|-)\s*(.+?)(?:\.txt)?$`),
	regexp.MustCompile(`(?i)whatsapp sohbeti\s*[-:]?\s*(.+?)(?:\.txt)?$`),
	regexp.MustCompile(`(?i)^(.+?)\s+ile whatsapp sohbeti`),
}

func fileOther(file string) string {
	base := strings.TrimSuffix(path.Base(file), path.Ext(file))
	for _, d := range []string{path.Base(path.Dir(file)), base} {
		for _, rx := range nameRx {
			if m := rx.FindStringSubmatch(d); m != nil {
				return strings.TrimSpace(m[1])
			}
		}
	}
	return ""
}

func titleFromName(file string, parts []string) string {
	if o := fileOther(file); o != "" {
		return o
	}
	if len(parts) > 0 {
		return strings.Join(parts, ", ")
	}
	return "WhatsApp sohbeti"
}

// detectOwner dışa aktarımı yapanı bulmaya çalışır: dosya adındaki karşı tarafı çıkararak.
func detectOwner(files []string, convs []model.Conversation) (string, bool, []string) {
	cands := map[string]bool{}
	for _, c := range convs {
		for _, p := range c.Participants {
			cands[p] = true
		}
	}
	list := make([]string, 0, len(cands))
	for p := range cands {
		list = append(list, p)
	}
	sort.Strings(list)
	if len(convs) == 1 && len(files) == 1 {
		if other := fileOther(files[0]); other != "" && len(convs[0].Participants) == 2 {
			for _, p := range convs[0].Participants {
				if model.Norm(p) != model.Norm(other) {
					return p, true, list
				}
			}
		}
	}
	return "", false, list
}

func atoi(s string) int { n, _ := strconv.Atoi(s); return n }

func parseHeader(line string) (rawLine, bool) {
	line = strings.TrimLeft(invisibles.Replace(line), " ")
	m := header.FindStringSubmatch(line)
	if m == nil {
		return rawLine{}, false
	}
	r := rawLine{a: atoi(m[1]), sep: m[2], b: atoi(m[3]), year: atoi(m[4]), h: atoi(m[5]), m: atoi(m[6]), s: atoi(m[7]), rest: m[9]}
	switch strings.ToUpper(strings.ReplaceAll(m[8], ".", "")) {
	case "AM", "ÖÖ":
		r.pm = 1
	case "PM", "ÖS":
		r.pm = 2
	}
	if r.a > 31 && r.year <= 31 { // yyyy-mm-dd
		r.a, r.year = r.year, r.a
	}
	if r.b < 1 || r.b > 31 || r.a < 1 || r.a > 31 || r.h > 23 || r.m > 59 {
		return rawLine{}, false
	}
	return r, true
}

func parseChat(text string, loc *time.Location) (model.Conversation, []string) {
	conv := model.Conversation{Platform: "whatsapp"}
	var msgs []*rawMsg
	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for sc.Scan() {
		line := sc.Text()
		if rl, ok := parseHeader(line); ok {
			msgs = append(msgs, &rawMsg{rawLine: rl, body: []string{rl.rest}})
			continue
		}
		if len(msgs) > 0 {
			last := msgs[len(msgs)-1]
			last.body = append(last.body, invisibles.Replace(line))
		}
	}
	var warns []string
	dayFirst, certain := detectOrder(msgs)
	if !certain {
		warns = append(warns, "Gün/ay sırası dosyadan kesin çıkarılamadı; "+map[bool]string{true: "gün/ay", false: "ay/gün"}[dayFirst]+" varsayıldı. Tarihleri kontrol et.")
	}
	seen := map[string]bool{}
	for _, m := range msgs {
		ts, ok := buildTime(m.rawLine, dayFirst, loc)
		if !ok {
			continue
		}
		body := strings.TrimSpace(strings.Join(m.body, "\n"))
		sender, content, isMsg := splitSender(body)
		msg := model.Message{TS: ts}
		if !isMsg {
			msg.Kind, msg.Text, msg.Sender = model.KindSystem, body, ""
		} else {
			msg.Sender = sender
			classify(&msg, content)
			if !seen[sender] {
				seen[sender] = true
				conv.Participants = append(conv.Participants, sender)
			}
		}
		conv.Messages = append(conv.Messages, msg)
	}
	sort.SliceStable(conv.Messages, func(i, j int) bool { return conv.Messages[i].TS.Before(conv.Messages[j].TS) })
	conv.IsGroup = len(conv.Participants) > 2
	return conv, warns
}

// detectOrder (gün önce mi, kesin mi) döndürür.
func detectOrder(msgs []*rawMsg) (dayFirst, certain bool) {
	dotted, slashed := 0, 0
	for _, m := range msgs {
		if m.sep == "." {
			dotted++
		} else {
			slashed++
		}
		if m.a > 12 && m.b <= 12 {
			return true, true
		}
		if m.b > 12 && m.a <= 12 {
			return false, true
		}
	}
	if dotted > slashed {
		return true, false
	}
	for _, m := range msgs { // AM/PM görüldüyse büyük olasılıkla ABD biçimi
		if m.pm != 0 {
			return false, false
		}
	}
	return true, false
}

func buildTime(r rawLine, dayFirst bool, loc *time.Location) (time.Time, bool) {
	day, mon := r.a, r.b
	if !dayFirst {
		day, mon = r.b, r.a
	}
	y := r.year
	if y < 100 {
		y += 2000
	}
	h := r.h
	switch r.pm {
	case 1:
		if h == 12 {
			h = 0
		}
	case 2:
		if h < 12 {
			h += 12
		}
	}
	t := time.Date(y, time.Month(mon), day, h, r.m, r.s, 0, loc)
	if t.Month() != time.Month(mon) || t.Day() != day {
		return time.Time{}, false
	}
	return t.UTC(), true
}

// splitSender "Ad: metin" gövdesini ayırır. Gönderen adı kısa ve satır içinde olmalıdır;
// aksi hâlde sistem mesajı sayılır.
func splitSender(body string) (sender, text string, ok bool) {
	i := strings.Index(body, ": ")
	if i <= 0 || i > 64 {
		return "", "", false
	}
	name := body[:i]
	if strings.ContainsAny(name, "\n\"“”") {
		return "", "", false
	}
	return name, strings.TrimSpace(body[i+2:]), true
}

var (
	mediaMarks   = []string{"<media omitted>", "<medya dahil edilmedi>", "image omitted", "video omitted", "audio omitted", "sticker omitted", "gif omitted", "document omitted", "görüntü dahil edilmedi", "video dahil edilmedi", "ses dahil edilmedi", "çıkartma dahil edilmedi", "(dosya ekli)", "(file attached)", "<attached:", "<ekli:"}
	deletedMarks = []string{"this message was deleted", "you deleted this message", "bu mesaj silindi", "bu mesajı sildin"}
	callMarks    = []string{"missed voice call", "missed video call", "cevapsız sesli arama", "cevapsız görüntülü arama"}
	editedMarks  = []string{"<this message was edited>", "<bu mesaj düzenlendi>"}
)

func hasAny(l string, marks []string) bool {
	for _, m := range marks {
		if strings.Contains(l, m) {
			return true
		}
	}
	return false
}

func classify(m *model.Message, content string) {
	l := strings.ToLower(content)
	m.Text = content
	switch {
	case hasAny(l, deletedMarks):
		m.Kind, m.Text = model.KindDeleted, ""
	case hasAny(l, callMarks):
		m.Kind, m.Text = model.KindMissedCall, ""
	case hasAny(l, mediaMarks):
		m.Kind, m.Text = model.KindMedia, ""
		switch {
		case strings.Contains(l, "image") || strings.Contains(l, "görüntü"):
			m.Meta = map[string]any{"media": "photo"}
		case strings.Contains(l, "video"):
			m.Meta = map[string]any{"media": "video"}
		case strings.Contains(l, "audio") || strings.Contains(l, "ses "):
			m.Meta = map[string]any{"media": "audio"}
		case strings.Contains(l, "sticker") || strings.Contains(l, "çıkartma"):
			m.Meta = map[string]any{"media": "sticker"}
		}
	default:
		m.Kind = model.KindText
		for _, e := range editedMarks {
			if strings.HasSuffix(l, e) {
				m.Text = strings.TrimSpace(content[:len(content)-len(e)])
				m.Meta = map[string]any{"edited": true}
				break
			}
		}
	}
}
