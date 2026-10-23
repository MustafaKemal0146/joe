// Package eval, persona çıktı kalitesini tekrarlanabilir biçimde ölçer: bilinen örüntüler
// gömülmüş SENTETİK vakalar kurar (gerçek kişi yok), kanıt paketini üretir, persona'ya okutur
// ve çıktıyı şema, kanıt bağlılığı, tanı dili ve örüntü yakalama ölçütleriyle değerlendirir.
package eval

import (
	"fmt"
	"time"

	"github.com/MustafaKemal0146/joe/internal/analytics"
	"github.com/MustafaKemal0146/joe/internal/evidence"
	"github.com/MustafaKemal0146/joe/internal/model"
	"github.com/MustafaKemal0146/joe/internal/store"
)

// Case bir sentetik değerlendirme vakasıdır.
type Case struct {
	Name string
	// Pattern gömülü örüntünün kısa tarifi (rapora yazılır).
	Pattern string
	// Keywords çıktıda bunlardan en az birinin geçmesi örüntünün yakalandığı anlamına gelir (küçük harf).
	Keywords []string
	// WantAbstain veri bilerek yetersizse çekimserlik (ya da yalnızca düşük güven) beklenir.
	WantAbstain bool
	// WantPhases çıktının dönemleri birbiriyle kıyaslamasını bekler ("dönem" sözcüğü geçer).
	WantPhases bool
	build      func() []model.Message
}

var tz = time.FixedZone("TRT", 3*3600)

func msg(sender string, ts time.Time, text string) model.Message {
	return model.Message{Sender: sender, TS: ts, Kind: model.KindText, Text: text}
}

var filler = []string{"tamam görüşürüz", "bugün nasıl geçti", "akşama bakarız", "haberleşelim", "ne yapıyorsun", "yoruldum biraz", "iyi geceler", "film izleyelim mi"}

// quarrel sert ama sıradan sitem cümleleridir (sentetik).
var quarrel = []string{"yine aramadın", "hep böyle yapıyorsun", "sen de dinlemiyorsun", "bu kadar yeter artık", "haksızlık ediyorsun", "peki öyle olsun", "beni hiç anlamıyorsun", "konuşmak istemiyorum"}

// Cases varsayılan vaka kümesidir.
func Cases() []Case {
	return []Case{
		{
			Name: "gece_kaymasi", Pattern: "Üçüncü aydan sonra yazışma akşamdan gece 01–03 arasına kayıyor ve hacim artıyor.",
			Keywords: []string{"gece", "geç saat", "01:00", "02:00", "gece yarısı"},
			build: func() []model.Message {
				var out []model.Message
				start := time.Date(2026, 1, 1, 0, 0, 0, 0, tz)
				for d := 0; d < 180; d++ {
					day := start.AddDate(0, 0, d)
					hour, n := 20, 4
					if d >= 90 {
						hour, n = 1, 14
					}
					ts := day.Add(time.Duration(hour) * time.Hour)
					for i := 0; i < n; i++ {
						who := "Ayşe"
						if i%2 == 1 {
							who = "Ben"
						}
						out = append(out, msg(who, ts, filler[(d+i)%len(filler)]))
						ts = ts.Add(3 * time.Minute)
					}
				}
				return out
			},
		},
		{
			Name: "tek_tarafli_baslatma", Pattern: "Ben konuşmaların %90'ını başlatıyor; Ayşe saatler sonra yanıt veriyor.",
			Keywords: []string{"başlat", "girişim", "yanıt", "gecik", "asimetri"},
			build: func() []model.Message {
				var out []model.Message
				start := time.Date(2026, 1, 1, 18, 0, 0, 0, tz)
				for d := 0; d < 150; d++ {
					ts := start.AddDate(0, 0, d)
					first, second := "Ben", "Ayşe"
					if d%10 == 9 {
						first, second = second, first
					}
					out = append(out, msg(first, ts, filler[d%len(filler)]))
					out = append(out, msg(first, ts.Add(5*time.Minute), "orada mısın"))
					out = append(out, msg(second, ts.Add(5*time.Hour), "şimdi gördüm, yoğundum"))
					out = append(out, msg(first, ts.Add(5*time.Hour+time.Minute), "sorun değil"))
				}
				return out
			},
		},
		{
			Name: "ani_sessizlik", Pattern: "Düzenli yazışma 45 günlük (≈46 gün aralıklı) tam bir sessizlikle kopuyor, sonra kısa bir dönüş geliyor.",
			Keywords: []string{"sessiz", "kopuş", "ara ver", "uzaklaş", "dönüş"},
			build: func() []model.Message {
				var out []model.Message
				start := time.Date(2026, 1, 1, 21, 0, 0, 0, tz)
				for d := 0; d < 200; d++ {
					if d >= 90 && d < 135 {
						continue
					}
					ts := start.AddDate(0, 0, d)
					for i := 0; i < 6; i++ {
						who := "Ayşe"
						if i%2 == 1 {
							who = "Ben"
						}
						out = append(out, msg(who, ts, filler[(d+i)%len(filler)]))
						ts = ts.Add(4 * time.Minute)
					}
				}
				return out
			},
		},
		{
			Name: "donem_degisimi", Pattern: "İlk dönemde yoğun ve dengeli yazışma, son dönemde seyrek ve Ayşe'nin başlattığı kısa mesajlar.", WantPhases: true,
			Keywords: []string{"azal", "seyrek", "değiş", "yoğunluk", "düş", "dönem"},
			build: func() []model.Message {
				var out []model.Message
				start := time.Date(2026, 1, 1, 0, 0, 0, 0, tz)
				for d := 0; d < 210; d++ {
					ts := start.AddDate(0, 0, d).Add(20 * time.Hour)
					n := 20
					if d >= 140 {
						n = 3
					}
					for i := 0; i < n; i++ {
						who := "Ayşe"
						if (d >= 140 && i != 0) || (d < 140 && i%2 == 1) {
							who = "Ben"
						}
						out = append(out, msg(who, ts, filler[(d+i)%len(filler)]))
						ts = ts.Add(2 * time.Minute)
					}
				}
				return out
			},
		},
		{
			Name: "donem_gece_kaymasi", Pattern: "İlk dönemde akşam saatlerinde dengeli yazışma; son dönemde gece yarısından sonra, çoğu Ben'den gelen mesajlar.", WantPhases: true,
			Keywords: []string{"gece", "geç saat", "kayma", "kayd", "değiş"},
			build: func() []model.Message {
				var out []model.Message
				start := time.Date(2026, 1, 1, 0, 0, 0, 0, tz)
				for d := 0; d < 210; d++ {
					hour, n := 20, 10
					if d >= 140 {
						hour, n = 1, 10
					}
					ts := start.AddDate(0, 0, d).Add(time.Duration(hour) * time.Hour)
					for i := 0; i < n; i++ {
						who := "Ayşe"
						if (d < 140 && i%2 == 1) || (d >= 140 && i%5 != 0) {
							who = "Ben"
						}
						out = append(out, msg(who, ts, filler[(d+i)%len(filler)]))
						ts = ts.Add(2 * time.Minute)
					}
				}
				return out
			},
		},
		{
			Name: "kavga_tirmanisi", Pattern: "Düzenli dengeli yazışma; ortada iki gün boyunca sert, kısa, karşılıklı sitem mesajları yoğunlaşıyor, sonra sakinleşiyor.",
			Keywords: []string{"gerginlik", "çatışma", "sitem", "tartışma", "kavga", "yoğun"},
			build: func() []model.Message {
				var out []model.Message
				start := time.Date(2026, 2, 1, 20, 0, 0, 0, tz)
				for d := 0; d < 120; d++ {
					ts := start.AddDate(0, 0, d)
					n, lines := 4, filler
					if d == 60 || d == 61 {
						n, lines = 40, quarrel
					}
					for i := 0; i < n; i++ {
						who := "Ayşe"
						if i%2 == 1 {
							who = "Ben"
						}
						out = append(out, msg(who, ts, lines[(d+i)%len(lines)]))
						ts = ts.Add(2 * time.Minute)
					}
				}
				return out
			},
		},
		{
			Name: "hayalet_birakma", Pattern: "Ben her gün yazmaya devam ediyor; Ayşe'nin yanıtları giderek seyrekleşiyor ve sonunda tamamen duruyor.",
			Keywords: []string{"yanıt", "tek taraf", "karşılıksız", "cevap", "seyrek", "çekil"},
			build: func() []model.Message {
				var out []model.Message
				start := time.Date(2026, 1, 1, 19, 0, 0, 0, tz)
				for d := 0; d < 150; d++ {
					ts := start.AddDate(0, 0, d)
					for i := 0; i < 5; i++ {
						out = append(out, msg("Ben", ts, filler[(d+i)%len(filler)]))
						ts = ts.Add(3 * time.Minute)
						if d < 100 && (d%(1+d/25) == 0) {
							out = append(out, msg("Ayşe", ts, filler[(d+i+1)%len(filler)]))
							ts = ts.Add(3 * time.Minute)
						}
					}
				}
				return out
			},
		},
		{
			Name: "yetersiz_veri", Pattern: "Yalnızca 12 mesaj var; anlamlı bir örüntü çıkarılamaz.", WantAbstain: true,
			build: func() []model.Message {
				var out []model.Message
				ts := time.Date(2026, 3, 1, 12, 0, 0, 0, tz)
				for i := 0; i < 12; i++ {
					who := "Ayşe"
					if i%2 == 1 {
						who = "Ben"
					}
					out = append(out, msg(who, ts, filler[i%len(filler)]))
					ts = ts.Add(10 * time.Minute)
				}
				return out
			},
		},
	}
}

// Fixture bir vakanın kanıt paketini (ve geçerli kimliklerini) kurar.
func (c Case) Fixture() (evidence.Pack, error) {
	st, err := store.Open(":memory:")
	if err != nil {
		return evidence.Pack{}, err
	}
	defer st.Close()
	imp := &model.Import{Platform: "whatsapp", Label: "sentetik", OwnerName: "Ben", Stats: map[string]int{},
		Conversations: []model.Conversation{{Title: "Ayşe", Platform: "whatsapp", Participants: []string{"Ben", "Ayşe"}, Messages: c.build()}}}
	id, err := st.SaveImport(imp, "eval-"+c.Name)
	if err != nil {
		return evidence.Pack{}, err
	}
	src, _ := st.Source(id)
	me, err := st.EnsureOwnerPerson(src)
	if err != nil {
		return evidence.Pack{}, fmt.Errorf("kişi: %w", err)
	}
	corpus, err := st.LoadCorpus(nil)
	if err != nil {
		return evidence.Pack{}, err
	}
	subj := analytics.NewSubject(me, corpus)
	pr := analytics.BuildProfile(corpus, subj, analytics.Options{Loc: tz})
	return evidence.Build(corpus, subj, pr, evidence.Options{Loc: tz, Partners: 1}), nil
}
