package server

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MustafaKemal0146/joe/internal/model"
)

func whatsappChat(t *testing.T, title string) string {
	t.Helper()
	var sb strings.Builder
	for i := 0; i < 10; i++ {
		fmt.Fprintf(&sb, "26.09.2026 20:%02d - Ben: selam %d\n26.09.2026 21:%02d - %s: merhaba %d\n", i, i, i, title, i)
	}
	p := filepath.Join(t.TempDir(), "WhatsApp Sohbeti - "+title+".txt")
	os.WriteFile(p, []byte(sb.String()), 0o600)
	return p
}

func TestWhatsAppImportOpensPartnerPersonAfterOwnerIsConfirmed(t *testing.T) {
	_, h := newServer(t)
	rec := do(h, "POST", "/api/import", map[string]string{"path": whatsappChat(t, "Ayşe")}, nil)
	var imp importResult
	json.Unmarshal(rec.Body.Bytes(), &imp)
	if !imp.OwnerNeeded || len(imp.Partners) != 0 {
		t.Fatalf("sahip doğrulanmadan kişi açılmamalı: %+v", imp.Partners)
	}
	// sahip doğrulanınca HEM sahip HEM karşı taraf dosyası açılır
	rec = do(h, "POST", fmt.Sprintf("/api/sources/%d/owner", imp.Source.ID), map[string]string{"name": "Ben"}, nil)
	var res importResult
	json.Unmarshal(rec.Body.Bytes(), &res)
	if res.Person == nil || len(res.Partners) != 1 || res.Partners[0].Name != "Ayşe" {
		t.Fatalf("karşı taraf açılmalı: %+v %+v", res.Person, res.Partners)
	}
	var people []struct {
		Name   string `json:"name"`
		IsSelf bool   `json:"is_self"`
	}
	json.Unmarshal(do(h, "GET", "/api/people", nil, nil).Body.Bytes(), &people)
	if len(people) != 2 {
		t.Fatalf("iki kişi bekleniyordu: %+v", people)
	}
	// tekrarlanabilir: ikinci çağrı kopya üretmez
	rec = do(h, "POST", fmt.Sprintf("/api/sources/%d/partners", imp.Source.ID), nil, nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"created":[]`) {
		t.Fatalf("idempotent olmalı: %d %s", rec.Code, rec.Body)
	}
	json.Unmarshal(do(h, "GET", "/api/people", nil, nil).Body.Bytes(), &people)
	if len(people) != 2 {
		t.Fatalf("kopya kişi açıldı: %+v", people)
	}
	// iki kişilik analiz artık planlanabilir
	var all []struct {
		ID     int64 `json:"id"`
		IsSelf bool  `json:"is_self"`
	}
	json.Unmarshal(do(h, "GET", "/api/people", nil, nil).Body.Bytes(), &all)
	var self, other int64
	for _, p := range all {
		if p.IsSelf {
			self = p.ID
		} else {
			other = p.ID
		}
	}
	if rec := do(h, "POST", "/api/council/plan", map[string]any{"kind": "pair", "subject_id": self, "partner_id": other, "persona_ids": []string{"freud"}, "consent": true}, nil); rec.Code != 200 {
		t.Fatalf("iki kişilik plan: %d %s", rec.Code, rec.Body)
	}
}

func TestAutoPartnersSkipsGroupsAndManyChatsAndRunsAtStartup(t *testing.T) {
	srv, h := newServer(t)
	// 6 bire-bir sohbet: kendiliğinden kişi açılmaz (kişiyi kullanıcı seçer)
	var convs []model.Conversation
	for i := 0; i < 6; i++ {
		name := fmt.Sprintf("Kişi%d", i)
		convs = append(convs, model.Conversation{Title: name, Platform: "instagram", Participants: []string{"Ben", name}, Messages: []model.Message{{Sender: name, Kind: model.KindText, Text: "x"}}})
	}
	convs = append(convs, model.Conversation{Title: "Grup", IsGroup: true, Platform: "instagram", Participants: []string{"Ben", "A", "B"}})
	id, err := srv.Store.SaveImport(&model.Import{Platform: "instagram", Label: "ig", OwnerName: "Ben", Stats: map[string]int{}, Conversations: convs}, "many")
	if err != nil {
		t.Fatal(err)
	}
	srv.Store.SetOwner(id, "Ben", "ben")
	src, _ := srv.Store.Source(id)
	if got := srv.ensurePartners(src); len(got) != 0 {
		t.Fatalf("çok sohbetli kaynakta kendiliğinden kişi açılmamalı: %d", len(got))
	}
	// tek sohbetli bir kaynak açılışta tamamlanır
	id2, _ := srv.Store.SaveImport(&model.Import{Platform: "whatsapp", Label: "wa", OwnerName: "Ben", Stats: map[string]int{},
		Conversations: []model.Conversation{{Title: "Zeynep", Platform: "whatsapp", Participants: []string{"Ben", "Zeynep"}, Messages: []model.Message{{Sender: "Zeynep", Kind: model.KindText, Text: "x"}}}}}, "one")
	srv.Store.SetOwner(id2, "Ben", "")
	srv.ensureAllPartners()
	names := map[string]bool{}
	ps, _ := srv.Store.People()
	for _, p := range ps {
		names[p.Name] = true
	}
	if !names["Zeynep"] || names["Kişi0"] || names["A"] {
		t.Fatalf("açılış tamamlaması: %v", names)
	}
	_ = h
}
