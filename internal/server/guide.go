package server

import (
	"net/http"
	"strings"

	"github.com/MustafaKemal0146/joe/internal/analytics"
	"github.com/MustafaKemal0146/joe/internal/store"
)

// guideOf bir kişinin tüm bire-bir sohbetlerinin rehberini döndürür: ölçümler, yakınlık/çatışma
// ifade sayıları, kullanıcının koyduğu etiketler ve (reddedilmemiş) etiket önerileri. LLM kullanmaz.
func (s *Server) guideOf(w http.ResponseWriter, r *http.Request) {
	p, c, subj, ok := s.subject(w, r, "id")
	if !ok {
		return
	}
	_ = p
	labels, err := s.Store.ContactLabels()
	if err != nil {
		serverErr(w, err)
		return
	}
	rows := analytics.BuildGuide(c, subj, s.Loc)
	counts := map[string]int{}
	unlabeled, suggested := 0, 0
	for i := range rows {
		if l, ok := labels[rows[i].Key]; ok {
			rows[i].Label, rows[i].Dismissed = l.Label, l.Dismissed
		}
		if rows[i].Label != "" || rows[i].Dismissed {
			rows[i].Suggestion = nil // etiketlenmiş ya da reddedilmiş sohbette öneri gösterilmez
		}
		if rows[i].Label == "" {
			unlabeled++
		} else {
			counts[rows[i].Label]++
		}
		if rows[i].Suggestion != nil {
			suggested++
		}
	}
	writeJSON(w, 200, map[string]any{"rows": rows, "label_counts": counts, "unlabeled": unlabeled, "suggested": suggested, "labels": analytics.RelationLabels})
}

// setContactLabel bir sohbet karşısının etiketini koyar, kaldırır ya da önerisini reddeder.
func (s *Server) setContactLabel(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Key     string `json:"key"`
		Label   string `json:"label"`
		Dismiss bool   `json:"dismiss"`
	}
	if !decode(w, r, &body) {
		return
	}
	body.Key = strings.TrimSpace(body.Key)
	if body.Key == "" || len(body.Key) > 200 {
		fail(w, 400, "geçersiz anahtar")
		return
	}
	if !analytics.ValidLabel(body.Label) {
		fail(w, 400, "bilinmeyen etiket")
		return
	}
	if err := s.Store.SetContactLabel(body.Key, body.Label, body.Dismiss); err != nil {
		serverErr(w, err)
		return
	}
	s.dropProfiles()
	w.WriteHeader(204)
}

// metaOf kişinin kendi hesap etkinliğinden (mesaj içeriğine bakmadan) bağları, takip dengesini ve
// etkinlik saatlerini döndürür.
func (s *Server) metaOf(w http.ResponseWriter, r *http.Request) {
	_, c, subj, ok := s.subject(w, r, "id")
	if !ok {
		return
	}
	guide := analytics.BuildGuide(c, subj, s.Loc)
	v := analytics.BuildMetaView(c, subj.OwnSources, guide, s.Loc, intQuery(r, "max", 40, 5, 200))
	if v.Ties == nil {
		v.Ties = []analytics.MetaTie{}
	}
	if v.Monthly == nil {
		v.Monthly = []analytics.MonthCount{}
	}
	writeJSON(w, 200, v)
}

func joinExtra(a, b string) string {
	if a == "" {
		return b
	}
	return a + "\n\n" + b
}

// labelsByKey kullanıcının etiketlerini anahtar → etiket olarak döndürür (reddedilmiş öneriler hariç).
func (s *Server) labelsByKey() map[string]string {
	ls, err := s.Store.ContactLabels()
	if err != nil {
		return nil
	}
	m := make(map[string]string, len(ls))
	for k, l := range ls {
		if l.Label != "" {
			m[k] = l.Label
		}
	}
	return m
}

// labelOfPerson bir kişi dosyasının kimliklerinden birine konmuş etiketi döndürür.
func (s *Server) labelOfPerson(p store.Person) string {
	labels := s.labelsByKey()
	for _, id := range p.Identities {
		if l := labels[id.Norm]; l != "" {
			return l
		}
	}
	return ""
}

// guideExtra özne için rehber özetini kanıt paketinin ek metrik metni olarak kurar.
func (s *Server) guideExtra(c *store.Corpus, subj analytics.Subject, owner store.Person) string {
	rows := analytics.BuildGuide(c, subj, s.Loc)
	return analytics.GuideSummaryText(rows, s.labelsByKey(), owner.Name, 8)
}

// pairContext iki kişilik analizde karşı tarafın etiketini ve bu sohbetin ifade ölçümlerini verir.
func (s *Server) pairContext(c *store.Corpus, subj analytics.Subject, other store.Person) string {
	labels := s.labelsByKey()
	norms := map[string]bool{}
	for _, id := range other.Identities {
		norms[id.Norm] = true
	}
	for _, r := range analytics.BuildGuide(c, subj, s.Loc) {
		if norms[r.Key] {
			return analytics.PairContextText(r, labels[r.Key])
		}
	}
	return ""
}
