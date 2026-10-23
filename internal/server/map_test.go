package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type graphResp struct {
	Nodes []struct {
		ID      string `json:"id"`
		Label   string `json:"label"`
		IsOwner bool   `json:"is_owner"`
	} `json:"nodes"`
	Hidden []struct {
		ID    string `json:"id"`
		Label string `json:"label"`
	} `json:"hidden"`
}

func (g graphResp) has(label string) bool {
	for _, n := range g.Nodes {
		if n.Label == label {
			return true
		}
	}
	return false
}

func mapFixture(t *testing.T) (*Server, http.Handler, []byte) {
	t.Helper()
	srv, h := newServer(t)
	var sb strings.Builder
	for i := 0; i < 30; i++ {
		fmt.Fprintf(&sb, "26.09.2026 20:%02d - Ben: selam %d\n26.09.2026 21:%02d - Ayşe: merhaba %d\n", i, i, i, i)
	}
	p := filepath.Join(t.TempDir(), "WhatsApp Sohbeti - Ayşe.txt")
	os.WriteFile(p, []byte(sb.String()), 0o600)
	rec := do(h, "POST", "/api/import", map[string]string{"path": p}, nil)
	var imp importResult
	json.Unmarshal(rec.Body.Bytes(), &imp)
	do(h, "POST", fmt.Sprintf("/api/sources/%d/owner", imp.Source.ID), map[string]string{"name": "Ben"}, nil)
	return srv, h, nil
}

func getGraph(t *testing.T, h http.Handler) graphResp {
	t.Helper()
	var g graphResp
	json.Unmarshal(do(h, "GET", "/api/graph?max=50", nil, nil).Body.Bytes(), &g)
	return g
}

func TestMapHideRestoreAndDeletedPersonDoesNotReappear(t *testing.T) {
	_, h, _ := mapFixture(t)
	g := getGraph(t, h)
	if !g.has("Ben") || !g.has("Ayşe") {
		t.Fatalf("başlangıç haritası: %+v", g.Nodes)
	}
	var owner, ayse string
	for _, n := range g.Nodes {
		if n.IsOwner {
			owner = n.ID
		}
		if n.Label == "Ayşe" {
			ayse = n.ID
		}
	}
	// sahip gizlenemez
	if rec := do(h, "POST", "/api/graph/hide", map[string]string{"id": owner, "label": "Ben"}, nil); rec.Code != 400 {
		t.Fatalf("sahip gizlenemez: %d", rec.Code)
	}
	if rec := do(h, "POST", "/api/graph/hide", map[string]string{"id": "x"}, nil); rec.Code != 400 {
		t.Fatalf("geçersiz düğüm: %d", rec.Code)
	}
	// gizle → haritadan çıkar, "gizlenenler"de görünür; geri getir → döner
	if rec := do(h, "POST", "/api/graph/hide", map[string]string{"id": ayse, "label": "Ayşe"}, nil); rec.Code != 204 {
		t.Fatalf("gizle: %d %s", rec.Code, rec.Body)
	}
	g = getGraph(t, h)
	if g.has("Ayşe") || len(g.Hidden) != 1 || g.Hidden[0].Label != "Ayşe" {
		t.Fatalf("gizlenen düğüm haritada olmamalı: %+v", g)
	}
	do(h, "POST", "/api/graph/unhide", map[string]string{"id": ayse}, nil)
	if g = getGraph(t, h); !g.has("Ayşe") || len(g.Hidden) != 0 {
		t.Fatalf("geri getirme: %+v", g)
	}

	// kişi dosyası (kaynak içe aktarılırken karşı taraf için kendiliğinden açıldı) silinince düğüm yalın hesap olarak yeniden belirmez
	var all []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	json.Unmarshal(do(h, "GET", "/api/people", nil, nil).Body.Bytes(), &all)
	var p struct{ ID int64 }
	for _, x := range all {
		if x.Name == "Ayşe" {
			p.ID = x.ID
		}
	}
	if p.ID == 0 {
		t.Fatal("karşı taraf dosyası kendiliğinden açılmış olmalı")
	}
	if rec := do(h, "DELETE", fmt.Sprintf("/api/people/%d", p.ID), nil, nil); rec.Code != 204 {
		t.Fatalf("kişi silme: %d", rec.Code)
	}
	g = getGraph(t, h)
	if g.has("Ayşe") || len(g.Hidden) == 0 {
		t.Fatalf("silinen kişi haritada yeniden belirmemeli: %+v", g)
	}
	// ... ve gizlenenlerden geri getirilebilir (veri silinmemiştir)
	do(h, "POST", "/api/graph/unhide", map[string]string{"id": g.Hidden[0].ID}, nil)
	if !getGraph(t, h).has("Ayşe") {
		t.Fatal("veri silinmediği için geri getirilebilmeli")
	}
}

func TestPersonNetworkEndpoint(t *testing.T) {
	_, h, _ := mapFixture(t)
	var people []struct {
		ID     int64 `json:"id"`
		IsSelf bool  `json:"is_self"`
	}
	json.Unmarshal(do(h, "GET", "/api/people", nil, nil).Body.Bytes(), &people)
	if len(people) == 0 {
		t.Fatal("sahip kişisi yok")
	}
	rec := do(h, "GET", fmt.Sprintf("/api/people/%d/network?max=10", people[0].ID), nil, nil)
	var e struct {
		Center    struct{ Label string } `json:"center"`
		Neighbors []struct {
			Label     string   `json:"label"`
			Relations []string `json:"relations"`
			Circle    string   `json:"circle"`
		} `json:"neighbors"`
		Total int `json:"total_neighbors"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil || rec.Code != 200 {
		t.Fatalf("çevre: %d %v", rec.Code, err)
	}
	if e.Center.Label != "Ben" || len(e.Neighbors) != 1 || e.Neighbors[0].Label != "Ayşe" || e.Neighbors[0].Relations[0] != "mesajlaşıyor" || e.Neighbors[0].Circle != "genis" {
		t.Fatalf("çevre içeriği: %+v", e)
	}
	if rec := do(h, "GET", "/api/people/9999/network", nil, nil); rec.Code != 404 {
		t.Fatalf("olmayan kişi: %d", rec.Code)
	}
}

func TestGuideListsAllDMsAndLabelsRoundTrip(t *testing.T) {
	_, h, _ := mapFixture(t)
	var people []struct {
		ID int64 `json:"id"`
	}
	json.Unmarshal(do(h, "GET", "/api/people", nil, nil).Body.Bytes(), &people)
	url := fmt.Sprintf("/api/people/%d/guide", people[0].ID)
	type guide struct {
		Rows []struct {
			Name      string `json:"name"`
			Key       string `json:"key"`
			Label     string `json:"label"`
			Dismissed bool   `json:"suggestion_dismissed"`
			Total     int    `json:"total"`
		} `json:"rows"`
		LabelCounts map[string]int `json:"label_counts"`
		Unlabeled   int            `json:"unlabeled"`
	}
	read := func() guide {
		var g guide
		json.Unmarshal(do(h, "GET", url, nil, nil).Body.Bytes(), &g)
		return g
	}
	g := read()
	if len(g.Rows) != 1 || g.Rows[0].Name != "Ayşe" || g.Rows[0].Total != 60 || g.Unlabeled != 1 {
		t.Fatalf("rehber: %+v", g)
	}
	key := g.Rows[0].Key
	if rec := do(h, "PUT", "/api/contact-labels", map[string]any{"key": key, "label": "yakin_arkadas"}, nil); rec.Code != 204 {
		t.Fatalf("etiket koy: %d %s", rec.Code, rec.Body)
	}
	if g = read(); g.Rows[0].Label != "yakin_arkadas" || g.LabelCounts["yakin_arkadas"] != 1 || g.Unlabeled != 0 {
		t.Fatalf("etiket yansımalı: %+v", g)
	}
	if rec := do(h, "PUT", "/api/contact-labels", map[string]any{"key": key, "label": "toksik"}, nil); rec.Code != 400 {
		t.Fatalf("bilinmeyen etiket reddedilmeli: %d", rec.Code)
	}
	// öneriyi reddetme: etiketsiz ama reddedilmiş
	do(h, "PUT", "/api/contact-labels", map[string]any{"key": key, "label": "", "dismiss": true}, nil)
	if g = read(); g.Rows[0].Label != "" || !g.Rows[0].Dismissed {
		t.Fatalf("ret kaydı: %+v", g.Rows[0])
	}
	// etiketi kaldırma
	do(h, "PUT", "/api/contact-labels", map[string]any{"key": key, "label": ""}, nil)
	if g = read(); g.Rows[0].Dismissed || g.Rows[0].Label != "" {
		t.Fatalf("kaldırma: %+v", g.Rows[0])
	}
	if rec := do(h, "GET", "/api/people/9999/guide", nil, nil); rec.Code != 404 {
		t.Fatalf("olmayan kişi: %d", rec.Code)
	}
}

func TestHighlightsEndpointAndAroundWindow(t *testing.T) {
	_, h, _ := mapFixture(t)
	var people []struct {
		ID int64 `json:"id"`
	}
	json.Unmarshal(do(h, "GET", "/api/people", nil, nil).Body.Bytes(), &people)
	var guide struct {
		Rows []struct {
			ConvID int64 `json:"conv_id"`
		} `json:"rows"`
	}
	json.Unmarshal(do(h, "GET", fmt.Sprintf("/api/people/%d/guide", people[0].ID), nil, nil).Body.Bytes(), &guide)
	conv := guide.Rows[0].ConvID

	rec := do(h, "GET", fmt.Sprintf("/api/conversations/%d/highlights?person=%d", conv, people[0].ID), nil, nil)
	var hl struct {
		Partner    string `json:"partner"`
		Highlights []struct {
			Kind      string `json:"kind"`
			MessageID int64  `json:"message_id"`
		} `json:"highlights"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &hl); err != nil || rec.Code != 200 || hl.Partner != "Ayşe" || len(hl.Highlights) == 0 {
		t.Fatalf("öne çıkanlar: %d %s", rec.Code, rec.Body)
	}
	// seçili anı ortalayan pencere: X-Offset ve mesaj kimliği
	target := hl.Highlights[len(hl.Highlights)-1].MessageID
	rec = do(h, "GET", fmt.Sprintf("/api/conversations/%d/messages?limit=10&around=%d", conv, target), nil, nil)
	var win []struct {
		ID int64 `json:"id"`
	}
	json.Unmarshal(rec.Body.Bytes(), &win)
	found := false
	for _, m := range win {
		found = found || m.ID == target
	}
	if rec.Header().Get("X-Offset") == "" || !found || len(win) < 6 {
		t.Fatalf("pencere hedefi içermeli: offset=%q len=%d found=%v", rec.Header().Get("X-Offset"), len(win), found)
	}
	// olmayan kişi / sohbet
	if rec := do(h, "GET", fmt.Sprintf("/api/conversations/%d/highlights?person=9999", conv), nil, nil); rec.Code != 404 {
		t.Fatalf("olmayan kişi: %d", rec.Code)
	}
	if rec := do(h, "GET", fmt.Sprintf("/api/conversations/99999/highlights?person=%d", people[0].ID), nil, nil); rec.Code != 404 {
		t.Fatalf("olmayan sohbet: %d", rec.Code)
	}
}

func TestMetaEndpointReturnsShapeEvenWithoutEvents(t *testing.T) {
	_, h, _ := mapFixture(t)
	var people []struct {
		ID int64 `json:"id"`
	}
	json.Unmarshal(do(h, "GET", "/api/people", nil, nil).Body.Bytes(), &people)
	rec := do(h, "GET", fmt.Sprintf("/api/people/%d/meta", people[0].ID), nil, nil)
	var v struct {
		Ties   []any `json:"ties"`
		Hours  []int `json:"hours"`
		Events int   `json:"events"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil || rec.Code != 200 || v.Ties == nil || len(v.Hours) != 24 || v.Events != 0 {
		t.Fatalf("meta: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "GET", "/api/people/9999/meta", nil, nil); rec.Code != 404 {
		t.Fatalf("olmayan kişi: %d", rec.Code)
	}
}
