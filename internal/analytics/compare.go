package analytics

import (
	"fmt"
	"math"
	"sort"

	"github.com/MustafaKemal0146/joe/internal/model"
	"github.com/MustafaKemal0146/joe/internal/store"
)

// Dimension karşılaştırmanın tek bir boyutudur. Score 0–1 arasıdır ve yargı değil benzerlik
// ya da denge ölçüsüdür; Detail sayıları açıklar.
type Dimension struct {
	Key    string  `json:"key"`
	Label  string  `json:"label"`
	Score  float64 `json:"score"`
	Detail string  `json:"detail"`
}

// Direct iki kişinin birbirleriyle olan doğrudan sohbetinin özetidir.
type Direct struct {
	Partner
	Platforms []string `json:"platforms"`
}

// Comparison iki kişinin deterministik karşılaştırmasıdır.
type Comparison struct {
	A          string      `json:"a"`
	B          string      `json:"b"`
	Dimensions []Dimension `json:"dimensions"`
	// Overall mevcut boyutların ortalamasıdır; kişilik ya da ilişki hakkında hüküm değildir.
	Overall      float64  `json:"overall"`
	Direct       *Direct  `json:"direct,omitempty"`
	SharedWords  []string `json:"shared_words"`
	SharedPeople []string `json:"shared_people"`
	Missing      []string `json:"missing"`
	Caveat       string   `json:"caveat"`
}

const minForRhythm = 50

func hist(h [24]int) []float64 {
	out := make([]float64, 24)
	for i, v := range h {
		out[i] = float64(v)
	}
	return out
}

func weekHist(h [7]int) []float64 {
	out := make([]float64, 7)
	for i, v := range h {
		out[i] = float64(v)
	}
	return out
}

func topSet(cs []Count, n int) map[string]bool {
	m := map[string]bool{}
	for i, c := range cs {
		if i >= n {
			break
		}
		m[c.Key] = true
	}
	return m
}

func circle(c *store.Corpus, s Subject, exclude ...Subject) map[string]bool {
	out := map[string]bool{}
	skip := func(name string) bool {
		if name == "" || name == model.Owner {
			return true
		}
		for _, x := range exclude {
			if x.Is(name) {
				return true
			}
		}
		return s.Is(name)
	}
	for _, conv := range c.Convs {
		if name, ok := partnerOf(conv, s); ok && !skip(name) {
			out[model.Norm(name)] = true
		}
	}
	for _, e := range c.Events {
		if !s.OwnSources[e.SourceID] {
			continue
		}
		switch e.Kind {
		case model.EvLikePost, model.EvComment, model.EvStoryLike, model.EvSearchAccount, model.EvFollowing, model.EvCloseFriend, model.EvSaved:
			if !skip(e.Counterpart) {
				out[model.Norm(e.Counterpart)] = true
			}
		case model.EvFollower:
			if !skip(e.Actor) {
				out[model.Norm(e.Actor)] = true
			}
		}
	}
	return out
}

func lat(p Profile) float64 { return p.Latency.Median }

// Compare iki kişiyi aynı veri üzerinde kıyaslar: ritim, tempo, ifade, sözcük ve sosyal çevre
// benzerliği ile (varsa) aralarındaki doğrudan sohbetin dengesi.
func Compare(c *store.Corpus, a, b Subject, opt Options) Comparison {
	pa, pb := BuildProfile(c, a, opt), BuildProfile(c, b, opt)
	out := Comparison{A: a.Name, B: b.Name, SharedWords: []string{}, SharedPeople: []string{}, Missing: []string{},
		Caveat: "Bu göstergeler yalnızca eldeki veriden hesaplanan benzerlik ve denge ölçüleridir. İlişkinin kalitesi, uyumu ya da gidişatı hakkında hüküm değildir."}
	add := func(key, label string, score float64, detail string) {
		out.Dimensions = append(out.Dimensions, Dimension{key, label, math.Max(0, math.Min(1, score)), detail})
	}

	if pa.Messages >= minForRhythm && pb.Messages >= minForRhythm {
		add("rhythm", "Günlük ritim", cosine(hist(pa.Hours), hist(pb.Hours)), "Mesajlaşmanın saat dağılımlarının benzerliği")
		add("weekday", "Haftalık ritim", cosine(weekHist(pa.Weekday), weekHist(pb.Weekday)), "Hafta günlerine dağılımın benzerliği")
		add("night", "Gece aktivitesi", 1-math.Abs(pa.NightRatio-pb.NightRatio)*4,
			fmt.Sprintf("00:00–06:00 payı: %%%.0f ve %%%.0f", pa.NightRatio*100, pb.NightRatio*100))
		add("length", "Mesaj uzunluğu", ratio(pa.Language.AvgWords, pb.Language.AvgWords),
			fmt.Sprintf("Ortalama sözcük: %.1f ve %.1f", pa.Language.AvgWords, pb.Language.AvgWords))
	} else {
		out.Missing = append(out.Missing, "Ritim ve mesaj uzunluğu için her iki kişiden de en az 50 mesaj gerekir.")
	}
	if pa.Latency.N >= 20 && pb.Latency.N >= 20 {
		add("tempo", "Yanıt temposu", ratio(lat(pa), lat(pb)),
			fmt.Sprintf("Medyan yanıt süresi: %s ve %s", dur(lat(pa)), dur(lat(pb))))
	} else {
		out.Missing = append(out.Missing, "Yanıt temposu için her iki kişiden de en az 20 yanıt örneği gerekir.")
	}
	ea, eb := topSet(pa.Language.TopEmoji, 8), topSet(pb.Language.TopEmoji, 8)
	if len(ea) > 0 && len(eb) > 0 {
		sim := (jaccard(ea, eb) + ratio(pa.Language.EmojiPerMsg, pb.Language.EmojiPerMsg)) / 2
		add("emoji", "İfade biçimi (emoji)", sim, fmt.Sprintf("Mesaj başına emoji: %.2f ve %.2f", pa.Language.EmojiPerMsg, pb.Language.EmojiPerMsg))
	}
	wa, wb := topSet(pa.Language.TopWords, 30), topSet(pb.Language.TopWords, 30)
	if len(wa) >= 10 && len(wb) >= 10 {
		add("vocab", "Ortak söz dağarcığı", math.Min(1, jaccard(wa, wb)*3), "En sık 30 sözcüğün örtüşmesi")
		for _, w := range pa.Language.TopWords {
			if wb[w.Key] {
				out.SharedWords = append(out.SharedWords, w.Key)
			}
		}
	}
	ca, cb := circle(c, a, b), circle(c, b, a)
	if len(ca) >= 3 && len(cb) >= 3 {
		add("circle", "Ortak çevre", math.Min(1, jaccard(ca, cb)*4), fmt.Sprintf("%d ve %d kişilik çevrenin örtüşmesi", len(ca), len(cb)))
		for n := range ca {
			if cb[n] {
				out.SharedPeople = append(out.SharedPeople, n)
			}
		}
	}

	if d := direct(c, a, b); d != nil {
		out.Direct = d
		sub, par := d.Messages.Subject, d.Messages.Partner
		add("direct_volume", "Mesaj dengesi", 1-math.Abs(d.SubjectShare-0.5)*2,
			fmt.Sprintf("%s %d, %s %d mesaj", a.Name, sub, b.Name, par))
		if d.Sessions >= 5 {
			sh := float64(d.Starts.Subject) / float64(d.Starts.Subject+d.Starts.Partner)
			add("direct_initiative", "Konuşma başlatma dengesi", 1-math.Abs(sh-0.5)*2,
				fmt.Sprintf("Başlatan: %s %d, %s %d (toplam %d konuşma)", a.Name, d.Starts.Subject, b.Name, d.Starts.Partner, d.Sessions))
		}
		if d.Latency.Subject.N >= 10 && d.Latency.Partner.N >= 10 {
			add("direct_reply", "Karşılıklı yanıt hızı", ratio(d.Latency.Subject.Median, d.Latency.Partner.Median),
				fmt.Sprintf("Medyan yanıt: %s ve %s", dur(d.Latency.Subject.Median), dur(d.Latency.Partner.Median)))
		}
	} else {
		out.Missing = append(out.Missing, "İki kişi arasında doğrudan bir bire-bir sohbet bulunamadı; yalnızca davranış benzerliği hesaplandı.")
	}

	if len(out.Dimensions) > 0 {
		var sum float64
		for _, d := range out.Dimensions {
			sum += d.Score
		}
		out.Overall = sum / float64(len(out.Dimensions))
	}
	return out
}

func dur(sec float64) string {
	switch {
	case sec < 90:
		return fmt.Sprintf("%.0f sn", sec)
	case sec < 5400:
		return fmt.Sprintf("%.0f dk", sec/60)
	case sec < 86400*2:
		return fmt.Sprintf("%.1f sa", sec/3600)
	}
	return fmt.Sprintf("%.1f gün", sec/86400)
}

// direct a ile b arasındaki bire-bir sohbeti bulur (en çok mesajlı olanı).
func direct(c *store.Corpus, a, b Subject) *Direct {
	by := convMsgs(c)
	var best *Direct
	var platforms []string
	seen := map[string]bool{}
	for _, conv := range c.Convs {
		if conv.IsGroup || len(conv.Participants) != 2 {
			continue
		}
		x, y := conv.Participants[0], conv.Participants[1]
		if !((a.Is(x) && b.Is(y)) || (a.Is(y) && b.Is(x))) {
			continue
		}
		r := analyseDyad(by[conv.ID], a, b.Name)
		if r.Total == 0 {
			continue
		}
		r.Platform = c.Sources[conv.SourceID].Platform
		r.ConvID, r.SourceID = conv.ID, conv.SourceID
		if !seen[r.Platform] {
			seen[r.Platform] = true
			platforms = append(platforms, r.Platform)
		}
		if best == nil || r.Total > best.Total {
			best = &Direct{Partner: r.Partner}
		}
	}
	if best != nil {
		best.Platforms = platforms
		sort.Strings(best.Platforms)
	}
	return best
}
