package analytics

import (
	"sort"
	"strconv"

	"github.com/MustafaKemal0146/joe/internal/model"
	"github.com/MustafaKemal0146/joe/internal/store"
)

// EgoNeighbor merkezdeki kişinin bir bağlantısıdır.
type EgoNeighbor struct {
	Node
	Breakdown map[string]int `json:"breakdown"`
	// Relations etkileşim türlerinden çıkarılan düz etiketlerdir ("karşılıklı takip", "yakın arkadaş"…).
	// Hüküm değil, ölçülen etkinliğin adıdır.
	Relations []string `json:"relations"`
	// Circle "yakin" (yakın arkadaş, karşılıklı takip ya da yoğun mesajlaşma) ya da "genis".
	Circle string `json:"circle"`
}

// Ego bir kişinin çevresidir (ego ağı): kişi + doğrudan bağlantıları ve bağlantıların kendi arasındaki kenarlar.
type Ego struct {
	Center    Node          `json:"center"`
	Neighbors []EgoNeighbor `json:"neighbors"`
	Edges     []Edge        `json:"edges"` // merkez–bağlantı ve bağlantılar arası kenarlar
	Total     int           `json:"total_neighbors"`
	// OwnData kişinin kendi hesabından gelen etkinlik (takip, beğeni…) verisi olup olmadığıdır.
	OwnData bool `json:"own_data"`
}

const closeDMs = 50 // karşı tarafın bu kadar bire-bir mesajı tek başına "yakın çevre" sayılır (sezgisel eşik; hüküm değil)

func relationsOf(b map[string]int, centerIsOwner bool) []string {
	var r []string
	has := func(k string) bool { return b[k] > 0 }
	switch {
	case has(model.EvFollowing) && has(model.EvFollower):
		r = append(r, "karşılıklı takip")
	case has(model.EvFollowing):
		if centerIsOwner {
			r = append(r, "takip ediyor")
		} else {
			r = append(r, "takip bağlantısı")
		}
	case has(model.EvFollower):
		if centerIsOwner {
			r = append(r, "takipçisi")
		} else {
			r = append(r, "takip bağlantısı")
		}
	}
	if has(model.EvCloseFriend) {
		r = append(r, "yakın arkadaş")
	}
	if has("dm") {
		r = append(r, "mesajlaşıyor")
	} else if has("group") || has("group_reply") {
		r = append(r, "ortak grup")
	}
	if has(model.EvLikePost) || has(model.EvLikeComment) || has(model.EvStoryLike) {
		r = append(r, "beğeni")
	}
	if has(model.EvComment) {
		r = append(r, "yorum")
	}
	if has(model.EvStoryInteraction) {
		r = append(r, "hikâye etkileşimi")
	}
	if has(model.EvStoryView) {
		r = append(r, "hikâyelerini izliyor")
	}
	if has("reaction") {
		r = append(r, "tepki")
	}
	if has(model.EvSaved) {
		r = append(r, "kaydetme")
	}
	if has(model.EvSearchAccount) {
		r = append(r, "arama")
	}
	return r
}

func circleOf(b map[string]int) string {
	if b[model.EvCloseFriend] > 0 || (b[model.EvFollowing] > 0 && b[model.EvFollower] > 0) || b["dm"] >= closeDMs {
		return "yakin"
	}
	return "genis"
}

// EgoNetwork kişinin çevresini kurar: tüm kaynaklardan kurulan ağda kişinin düğümüne bağlı en güçlü
// maxNeighbors bağlantı ve aralarındaki kenarlar. Kişinin hiç verisi yoksa Neighbors boş döner.
func EgoNetwork(c *store.Corpus, people []store.Person, personID int64, hidden map[string]bool, maxNeighbors int) Ego {
	g := BuildGraphHidden(c, people, 0, hidden)
	center := "p" + strconv.FormatInt(personID, 10)
	byID := map[string]Node{}
	for _, n := range g.Nodes {
		byID[n.ID] = n
	}
	out := Ego{Neighbors: []EgoNeighbor{}, Edges: []Edge{}}
	cn, ok := byID[center]
	if !ok {
		for _, p := range people {
			if p.ID == personID {
				out.Center = Node{ID: center, Label: p.Name, PersonID: p.ID}
			}
		}
		return out
	}
	out.Center = cn
	var inc []Edge
	for _, e := range g.Edges {
		if e.A == center || e.B == center {
			inc = append(inc, e)
		}
	}
	sort.SliceStable(inc, func(i, j int) bool { return inc[i].Strength > inc[j].Strength })
	out.Total = len(inc)
	if maxNeighbors > 0 && len(inc) > maxNeighbors {
		inc = inc[:maxNeighbors]
	}
	keep := map[string]bool{center: true}
	for _, e := range inc {
		other := e.A
		if other == center {
			other = e.B
		}
		keep[other] = true
		n := byID[other]
		out.Neighbors = append(out.Neighbors, EgoNeighbor{Node: n, Breakdown: e.Breakdown, Relations: relationsOf(e.Breakdown, cn.IsOwner), Circle: circleOf(e.Breakdown)})
		if cn.IsOwner && e.Breakdown[model.EvFollowing]+e.Breakdown[model.EvFollower]+e.Breakdown[model.EvCloseFriend] > 0 {
			out.OwnData = true
		}
	}
	for _, e := range g.Edges {
		if keep[e.A] && keep[e.B] {
			out.Edges = append(out.Edges, e)
		}
	}
	return out
}
