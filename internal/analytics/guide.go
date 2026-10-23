package analytics

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/MustafaKemal0146/joe/internal/model"
	"github.com/MustafaKemal0146/joe/internal/store"
)

// Etiket türleri: ilişki etiketini her zaman kullanıcı koyar ya da onaylar.
var RelationLabels = []string{"anne", "baba", "kardes", "aile", "sevgili", "eski_sevgili", "yakin_arkadas", "arkadas", "is_okul", "tanidik", "diger"}

// ValidLabel etiketin tanımlı olup olmadığını söyler ("" = etiketi kaldır).
func ValidLabel(l string) bool {
	if l == "" {
		return true
	}
	for _, x := range RelationLabels {
		if x == l {
			return true
		}
	}
	return false
}

// Suggestion bir etiket önerisidir; kullanıcı onaylamadan uygulanmaz. Why, öneriyi doğuran somut
// ipuçlarıdır; Confidence yalnızca ipucu sayısını yansıtır (düşük | orta).
type Suggestion struct {
	Label      string   `json:"label"`
	Why        []string `json:"why"`
	Confidence string   `json:"confidence"`
}

// GuideRow bir bire-bir sohbetin rehber satırıdır: karşılıklılık ölçümleri + ifade sayımları.
type GuideRow struct {
	Partner
	Key       string  `json:"key"` // etiket anahtarı (karşı tarafın normalleştirilmiş adı)
	NightAll  float64 `json:"night_ratio"`
	Texts     int     `json:"texts"`
	Affection Pair    `json:"affection"` // yakınlık ifadesi geçen mesaj sayısı (özne / karşı taraf)
	Conflict  Pair    `json:"conflict"`  // çatışma ifadesi geçen mesaj sayısı
	// Meta, hesap adı (kullanıcı adı) ile görünen ad arasında benzerlikle kurulur: kesin değildir.
	CloseFriendHint bool        `json:"close_friend_hint"`
	MutualFollow    bool        `json:"mutual_follow_hint"`
	Suggestion      *Suggestion `json:"suggestion,omitempty"`
	// Label ve Dismissed sunucuda depodan eklenir.
	Label     string `json:"label"`
	Dismissed bool   `json:"suggestion_dismissed"`
}

// MetaMatcher kullanıcı adı kümelerini görünen adlarla benzerlik üzerinden eşler.
type metaIndex struct {
	handles map[string]map[string]int
}

func bigramSet(s string) (map[string]int, int) {
	m := bigrams(s)
	n := 0
	for _, v := range m {
		n += v
	}
	return m, n
}

func diceSets(a map[string]int, na int, b map[string]int, nb int) float64 {
	if na+nb == 0 {
		return 0
	}
	inter := 0
	for k, n := range a {
		if m := b[k]; m > 0 {
			if m < n {
				inter += m
			} else {
				inter += n
			}
		}
	}
	return 2 * float64(inter) / float64(na+nb)
}

// bestHandle bir görünen ada en çok benzeyen kullanıcı adını (>= eşik) döndürür.
func bestHandle(name string, handles map[string]bool, cache map[string]struct {
	m map[string]int
	n int
}) (string, float64) {
	nn := model.Norm(name)
	if len([]rune(nn)) < 4 {
		return "", 0
	}
	bn, nb := bigramSet(nn)
	best, score := "", 0.0
	for h := range handles {
		c, ok := cache[h]
		if !ok {
			c.m, c.n = bigramSet(model.Norm(h))
			cache[h] = c
		}
		if d := diceSets(bn, nb, c.m, c.n); d > score {
			best, score = h, d
		}
	}
	return best, score
}

const metaThreshold = 0.72

// BuildGuide özne için TÜM bire-bir sohbetlerin rehberini (çok mesajlıdan aza) kurar.
func BuildGuide(c *store.Corpus, s Subject, loc *time.Location) []GuideRow {
	if loc == nil {
		loc = time.UTC
	}
	by := convMsgs(c)
	closeF, following, follower := map[string]bool{}, map[string]bool{}, map[string]bool{}
	closeNames := map[string]bool{} // yakın arkadaş listesindeki görünen adlar (kesin eşleştirme)
	for _, ev := range c.Events {
		switch ev.Kind {
		case model.EvCloseFriend:
			closeF[ev.Counterpart] = true
			if n := model.Norm(ev.Text); n != "" {
				closeNames[n] = true
			}
		case model.EvFollowing:
			following[ev.Counterpart] = true
		case model.EvFollower:
			follower[ev.Actor] = true
		}
	}
	cache := map[string]struct {
		m map[string]int
		n int
	}{}
	var rows []GuideRow
	for _, conv := range c.Convs {
		name, ok := partnerOf(conv, s)
		if !ok {
			continue
		}
		msgs := by[conv.ID]
		r := analyseDyad(msgs, s, name)
		if r.Total == 0 {
			continue
		}
		row := GuideRow{Partner: r.Partner, Key: model.Norm(name)}
		row.Platform = c.Sources[conv.SourceID].Platform
		row.ConvID, row.SourceID = conv.ID, conv.SourceID
		row.Monthly = nil
		night, kin := 0, map[string]int{}
		for _, m := range msgs {
			if m.Kind == model.KindSystem {
				continue
			}
			mine := s.Is(m.Sender)
			if m.TS.In(loc).Hour() < 6 {
				night++
			}
			if m.Kind != model.KindText || m.Text == "" {
				continue
			}
			row.Texts++
			if affectionIn(m.Text) {
				row.Affection.add(mine)
			}
			if conflictIn(m.Text) {
				row.Conflict.add(mine)
			}
			if mine {
				if lab, ok := kinshipAddr[firstWord(m.Text)]; ok {
					kin[lab]++
				}
			}
		}
		row.NightAll = float64(night) / float64(row.Total)
		if closeNames[row.Key] {
			row.CloseFriendHint = true
		} else if h, d := bestHandle(name, closeF, cache); h != "" && d >= metaThreshold {
			row.CloseFriendHint = true
		}
		_, dFollowing := bestHandle(name, following, cache)
		_, dFollower := bestHandle(name, follower, cache)
		row.MutualFollow = dFollowing >= metaThreshold && dFollower >= metaThreshold
		row.Suggestion = suggest(row, kin)
		rows = append(rows, row)
	}
	// Aynı görünen adı paylaşan sohbetler (Instagram silinmiş/devre dışı hesapları "Instagram User" diye
	// adlandırır) tek etikete çarpışmasın diye sohbet kimliğiyle ayrıştırılır; boş ad da öyle.
	count := map[string]int{}
	for _, r := range rows {
		count[r.Key]++
	}
	for i := range rows {
		if rows[i].Key == "" || count[rows[i].Key] > 1 {
			rows[i].Key = rows[i].Key + "#" + itoa(int(rows[i].ConvID))
		}
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Total > rows[j].Total })
	return rows
}

var nameKinship = []struct {
	tok, label string
}{
	{"annem", "anne"}, {"anneciğim", "anne"}, {"annecigim", "anne"}, {"babam", "baba"}, {"babacigim", "baba"},
	{"ablam", "kardes"}, {"abim", "kardes"}, {"kardesim", "kardes"}, {"dayim", "aile"}, {"teyzem", "aile"}, {"amcam", "aile"}, {"halam", "aile"},
}

// suggest deterministik ipuçlarından bir etiket önerir. Öneri bir hüküm değildir; kullanıcı onaylar ya da reddeder.
func suggest(r GuideRow, ownerKin map[string]int) *Suggestion {
	fold := foldASCII(r.Name)
	for _, nk := range nameKinship {
		for _, w := range strings.Fields(fold) {
			if w == foldASCII(nk.tok) {
				return &Suggestion{Label: nk.label, Why: []string{"kayıtlı ad \"" + nk.tok + "\" sözcüğünü içeriyor"}, Confidence: "orta"}
			}
		}
	}
	// Sana ait mesajların ilk sözcüğü aile hitabı olan sohbetler: en az 8 mesaj ve metin mesajlarının %5'i.
	bestKin, bestN := "", 0
	for l, n := range ownerKin {
		if n > bestN {
			bestKin, bestN = l, n
		}
	}
	var why []string
	label := ""
	if bestN >= 8 && float64(bestN)/float64(max(r.Messages.Subject, 1)) >= 0.05 {
		label = bestKin
		why = append(why, "mesajlarına sık sık aile hitabıyla başlıyorsun ("+itoa(bestN)+" mesaj)")
	}
	if label == "" && r.CloseFriendHint {
		label = "yakin_arkadas"
		why = append(why, "yakın arkadaş listendeki bir hesapla ad benzerliği var")
	}
	aff := r.Affection.Subject + r.Affection.Partner
	if label == "" && r.Texts >= 300 && r.Affection.Subject >= 15 && r.Affection.Partner >= 15 &&
		float64(aff)/float64(r.Texts) >= 0.08 && r.Last.Sub(r.First) >= 60*24*time.Hour {
		label = "sevgili"
		why = append(why, "iki taraf da sık sık yakınlık ifadeleri kullanıyor (mesajların %"+itoa(int(100*float64(aff)/float64(r.Texts)))+"'i)")
		if r.NightAll >= 0.15 {
			why = append(why, "gece saatlerinde yoğun yazışıyorsunuz")
		}
	}
	if label == "" {
		return nil
	}
	if label == "yakin_arkadas" && r.MutualFollow {
		why = append(why, "karşılıklı takip ediyorsunuz")
	}
	conf := "düşük"
	if len(why) >= 2 {
		conf = "orta"
	}
	return &Suggestion{Label: label, Why: why, Confidence: conf}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

// LabelTR etiketlerin Türkçe adlarıdır.
var LabelTR = map[string]string{
	"anne": "anne", "baba": "baba", "kardes": "kardeş", "aile": "aile", "sevgili": "sevgili", "eski_sevgili": "eski sevgili",
	"yakin_arkadas": "yakın arkadaş", "arkadas": "arkadaş", "is_okul": "iş/okul", "tanidik": "tanıdık", "diger": "diğer", "": "etiketsiz",
}

func pctOf(a, b int) string {
	if b == 0 {
		return "%0"
	}
	return "%" + itoa(int(100*float64(a)/float64(b)+0.5))
}

// GuideSummaryText rehberi konsey paketinin metrik bölümüne girecek düz metne çevirir: etiket başına
// toplamlar ve en yoğun sohbetler. labels, anahtar → kullanıcının koyduğu etiket. Yalnızca ölçümdür;
// etiketler kullanıcıya aittir, hüküm değildir. Hiç sohbet yoksa "" döner.
func GuideSummaryText(rows []GuideRow, labels map[string]string, owner string, top int) string {
	if len(rows) == 0 {
		return ""
	}
	type agg struct{ chats, total, subj, night, startsS, startsAll, aff, conf, texts int }
	by := map[string]*agg{}
	labeled := 0
	for _, r := range rows {
		l := labels[r.Key]
		if l != "" {
			labeled++
		}
		a := by[l]
		if a == nil {
			a = &agg{}
			by[l] = a
		}
		a.chats++
		a.total += r.Total
		a.subj += r.Messages.Subject
		a.night += int(r.NightAll*float64(r.Total) + 0.5)
		a.startsS += r.Starts.Subject
		a.startsAll += r.Starts.Subject + r.Starts.Partner
		a.aff += r.Affection.Subject + r.Affection.Partner
		a.conf += r.Conflict.Subject + r.Conflict.Partner
		a.texts += r.Texts
	}
	var b strings.Builder
	w := func(f string, a ...any) { b.WriteString(sprintf(f, a...) + "\n") }
	w("İlişki rehberi (%s için; etiketleri kullanıcı koydu, hüküm değildir): %d bire-bir sohbet, %d tanesi etiketli.", owner, len(rows), labeled)
	order := append(append([]string{}, RelationLabels...), "")
	for _, l := range order {
		a := by[l]
		if a == nil {
			continue
		}
		w("- %s: %d sohbet, %d mesaj; %s sana ait, gece payı %s, konuşmayı sen başlatma payı %s; yakınlık ifadesi mesajların %s'i, çatışma ifadesi %s'i (sözcük sayımı).",
			LabelTR[l], a.chats, a.total, pctOf(a.subj, a.total), pctOf(a.night, a.total), pctOf(a.startsS, a.startsAll), pctOf(a.aff, a.texts), pctOf(a.conf, a.texts))
	}
	if top > len(rows) {
		top = len(rows)
	}
	w("En yoğun %d sohbet:", top)
	for _, r := range rows[:top] {
		w("- %s (%s): %d mesaj, %s – %s; sana ait %s, gece %s, başlatma sende %s, medyan yanıt sen %.0f sn / o %.0f sn; yakınlık %d/%d, çatışma %d/%d (sen/o); en uzun sessizlik %.0f gün.",
			r.Name, LabelTR[labels[r.Key]], r.Total, r.First.Format("01.2006"), r.Last.Format("01.2006"), pctOf(r.Messages.Subject, r.Total),
			pctOf(int(r.NightAll*float64(r.Total)+0.5), r.Total), pctOf(r.Starts.Subject, r.Starts.Subject+r.Starts.Partner),
			r.Latency.Subject.Median, r.Latency.Partner.Median, r.Affection.Subject, r.Affection.Partner, r.Conflict.Subject, r.Conflict.Partner, r.LongestSilenceDays)
	}
	return strings.TrimRight(b.String(), "\n")
}

// PairContextText iki kişilik analizde karşı tarafın etiketini ve sohbet ifade ölçümlerini verir.
func PairContextText(r GuideRow, label string) string {
	return sprintf("İlişki bağlamı (%s ile): kullanıcının koyduğu etiket \"%s\"; yakınlık ifadesi geçen mesaj %d/%d, çatışma ifadesi geçen mesaj %d/%d (sen/o; sözcük sayımı, anlam yorumlanmaz); gece payı %s.",
		r.Name, LabelTR[label], r.Affection.Subject, r.Affection.Partner, r.Conflict.Subject, r.Conflict.Partner, pctOf(int(r.NightAll*float64(r.Total)+0.5), r.Total))
}

func sprintf(f string, a ...any) string { return fmt.Sprintf(f, a...) }

// CorpusForConv yalnızca tek bir sohbetin mesajlarını içeren bir veri kümesi döndürür (etkinlik kayıtları
// yoktur). Aynı görünen adı paylaşan birden çok sohbetten (ör. Instagram'ın silinmiş hesapları için
// "Instagram User" adı) yalnızca birini analiz etmek içindir: sohbetler kimlik adıyla ayrıştırılamaz.
func CorpusForConv(c *store.Corpus, convID int64) *store.Corpus {
	out := &store.Corpus{Sources: c.Sources}
	for _, conv := range c.Convs {
		if conv.ID == convID {
			out.Convs = append(out.Convs, conv)
		}
	}
	for _, m := range c.Messages {
		if m.ConvID == convID {
			out.Messages = append(out.Messages, m)
		}
	}
	return out
}
