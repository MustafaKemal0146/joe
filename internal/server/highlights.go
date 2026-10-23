package server

import (
	"net/http"
	"strconv"

	"github.com/MustafaKemal0146/joe/internal/analytics"
)

// highlightsOf bir bire-bir sohbetin önemli anlarını döndürür (LLM kullanmaz). Hangi tarafın "sen"
// olduğunu bilmek için ?person=<kişi kimliği> gerekir.
func (s *Server) highlightsOf(w http.ResponseWriter, r *http.Request) {
	convID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	pid, _ := strconv.ParseInt(r.URL.Query().Get("person"), 10, 64)
	person, c, subj, ok := s.subjectByID(w, pid)
	if !ok {
		return
	}
	_ = person
	for _, conv := range c.Convs {
		if conv.ID != convID {
			continue
		}
		partner, ok := analytics.PartnerOf(conv, subj)
		if !ok {
			fail(w, 400, "Bu sohbet bire-bir değil ya da bu kişinin sohbeti değil.")
			return
		}
		hs := analytics.ConversationHighlights(c, convID, subj, partner, s.Loc)
		if hs == nil {
			hs = []analytics.Highlight{}
		}
		writeJSON(w, 200, map[string]any{"partner": partner, "highlights": hs})
		return
	}
	fail(w, 404, "sohbet bulunamadı")
}
