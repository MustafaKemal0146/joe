package server

import (
	"net/http"

	"github.com/MustafaKemal0146/joe/internal/model"
	"github.com/MustafaKemal0146/joe/internal/store"
)

// maxAutoPartners bir kaynakta karşı taraf kişi dosyalarının KENDİLİĞİNDEN açılacağı en çok bire-bir sohbet
// sayısıdır. WhatsApp dışa aktarımı tek bir sohbettir (karşı taraf belli); yüzlerce sohbeti olan bir Instagram
// hesabında kişi dosyasını sen seçersin.
const maxAutoPartners = 5

// ensurePartners hesap sahibi doğrulanmış bir kaynakta, az sayıda bire-bir sohbet varsa karşı tarafların kişi
// dosyalarını açar (zaten bir kişi o adı taşıyorsa dokunmaz). İki kişilik analiz için karşı tarafın tanımlı
// olması gerekir; aksi hâlde yalnızca sahip analiz edilebilirdi. Açılanları döndürür.
func (s *Server) ensurePartners(src store.Source) []store.Person {
	if !src.OwnerConfirmed {
		return nil
	}
	convs, err := s.Store.Conversations(src.ID)
	if err != nil {
		return nil
	}
	var dms []store.Conversation
	for _, c := range convs {
		if !c.IsGroup && len(c.Participants) == 2 {
			dms = append(dms, c)
		}
	}
	if len(dms) == 0 || len(dms) > maxAutoPartners {
		return nil
	}
	people, err := s.Store.People()
	if err != nil {
		return nil
	}
	known := map[string]bool{}
	for _, p := range people {
		for _, id := range p.Identities {
			known[id.Norm] = true
		}
	}
	owner := map[string]bool{model.Norm(src.OwnerName): true, model.Norm(src.OwnerHandle): true}
	var created []store.Person
	for _, c := range dms {
		for _, name := range c.Participants {
			n := model.Norm(name)
			if n == "" || owner[n] || known[n] {
				continue
			}
			p, err := s.Store.CreatePerson(name, "", false, name)
			if err != nil {
				continue
			}
			known[n] = true
			created = append(created, p)
		}
	}
	if len(created) > 0 {
		s.dropProfiles()
	}
	return created
}

// ensureAllPartners açılışta, önceden yüklenmiş kaynaklar için eksik karşı taraf dosyalarını tamamlar.
func (s *Server) ensureAllPartners() {
	srcs, err := s.Store.Sources()
	if err != nil {
		return
	}
	for _, src := range srcs {
		if src.OwnerConfirmed {
			_, _ = s.Store.EnsureOwnerPerson(src) // sahip dosyası yoksa açılır
			s.ensurePartners(src)
		}
	}
}

// sourcePartners bir kaynağın eksik karşı taraf kişi dosyalarını tamamlar (elle tetiklenen, tekrarlanabilir işlem).
func (s *Server) sourcePartners(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	src, err := s.Store.Source(id)
	if err != nil {
		fail(w, 404, "kaynak bulunamadı")
		return
	}
	if !src.OwnerConfirmed {
		fail(w, 409, "Önce hesap sahibini doğrula.")
		return
	}
	created := s.ensurePartners(src)
	if created == nil {
		created = []store.Person{}
	}
	writeJSON(w, 200, map[string]any{"created": created})
}
