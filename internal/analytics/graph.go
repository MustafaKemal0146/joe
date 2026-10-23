package analytics

import (
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/MustafaKemal0146/joe/internal/model"
	"github.com/MustafaKemal0146/joe/internal/store"
)

// Düğüm ve kenar ağırlıkları: tek seferlik ilişki olayları ile sık tekrarlanan etkileşimlerin
// aynı ölçekte kıyaslanabilmesi için etkileşim türü başına ağırlık kullanılır.
var edgeWeight = map[string]float64{
	"dm": 1, "group": 0.25, "reaction": 1,
	"group_reply": 0.5, model.EvLikePost: 2, model.EvLikeComment: 2, model.EvComment: 3, model.EvSaved: 2,
	model.EvStoryLike: 2, model.EvStoryView: 0.5, model.EvStoryInteraction: 3, model.EvSearchAccount: 3,
	model.EvFollowing: 5, model.EvFollower: 5, model.EvCloseFriend: 10,
}

// Node ilişki haritasındaki bir kişi ya da hesaptır.
type Node struct {
	ID       string  `json:"id"`
	Label    string  `json:"label"`
	PersonID int64   `json:"person_id,omitempty"` // Joe'da kişi dosyası varsa
	IsOwner  bool    `json:"is_owner"`
	Strength float64 `json:"strength"`
}

// Edge iki düğüm arasındaki etkileşimdir.
type Edge struct {
	A         string         `json:"a"`
	B         string         `json:"b"`
	Strength  float64        `json:"strength"`
	Breakdown map[string]int `json:"breakdown"`
	// Out A→B, In B→A yönündeki etkileşim sayısıdır (yönü olan türlerde).
	Out int `json:"out"`
	In  int `json:"in"`
}

// Graph ilişki haritasıdır.
type Graph struct {
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
	Total int    `json:"total_nodes"` // sınırlamadan önceki düğüm sayısı
}

type resolver struct {
	byNorm map[string]store.Person
}

func newResolver(people []store.Person) resolver {
	r := resolver{byNorm: map[string]store.Person{}}
	for _, p := range people {
		for _, id := range p.Identities {
			r.byNorm[id.Norm] = p
		}
	}
	return r
}

// key bir ad/kullanıcı adını düğüm anahtarına çevirir (kişi dosyası varsa "p<ID>").
func (r resolver) key(name string) (key, label string, pid int64) {
	n := model.Norm(name)
	if n == "" {
		return "", "", 0
	}
	if p, ok := r.byNorm[n]; ok {
		return "p" + strconv.FormatInt(p.ID, 10), p.Name, p.ID
	}
	return "n" + n, name, 0
}

type graphBuilder struct {
	hidden  map[string]bool // haritadan gizlenen düğüm anahtarları
	r       resolver
	nodes   map[string]*Node
	edges   map[[2]string]*Edge
	ownerOf map[int64]string // kaynak → sahip düğüm anahtarı
}

func (g *graphBuilder) node(name string) string {
	k, label, pid := g.r.key(name)
	if k == "" || g.hidden[k] {
		return ""
	}
	if g.nodes[k] == nil {
		g.nodes[k] = &Node{ID: k, Label: label, PersonID: pid}
	}
	return k
}

func (g *graphBuilder) link(a, b, kind string, n int) {
	if a == "" || b == "" || a == b || n <= 0 {
		return
	}
	if a > b {
		a, b = b, a
	}
	key := [2]string{a, b}
	e := g.edges[key]
	if e == nil {
		e = &Edge{A: a, B: b, Breakdown: map[string]int{}}
		g.edges[key] = e
	}
	e.Breakdown[kind] += n
}

func (g *graphBuilder) linkDir(from, to, kind string, n int) {
	if from == "" || to == "" || from == to || n <= 0 {
		return
	}
	g.link(from, to, kind, n)
	a, b := from, to
	flip := false
	if a > b {
		a, b, flip = b, a, true
	}
	e := g.edges[[2]string{a, b}]
	if flip {
		e.In += n
	} else {
		e.Out += n
	}
}

// BuildGraph tüm kaynaklardan bir ilişki haritası kurar. Kaynağın sahibi her zaman bir düğümdür;
// kişi dosyalarındaki kimlikler birden fazla adı/kullanıcı adını tek düğümde birleştirir.
// maxNodes sınırı en güçlü düğümleri tutar.
func BuildGraph(c *store.Corpus, people []store.Person, maxNodes int) Graph {
	return BuildGraphHidden(c, people, maxNodes, nil)
}

// NodeKey bir kişi dosyasının ya da adın harita düğümü anahtarıdır ("p<ID>" ya da "n<norm>").
func NodeKey(people []store.Person, name string) (key, label string, pid int64) {
	return newResolver(people).key(name)
}

// BuildGraphHidden BuildGraph gibi çalışır; hidden içindeki düğümler (ve kenarları) haritaya girmez.
// Hesap sahibi düğümleri gizlenemez: harita sahibi olmadan anlamsızdır.
func BuildGraphHidden(c *store.Corpus, people []store.Person, maxNodes int, hiddenIn map[string]bool) Graph {
	hidden := make(map[string]bool, len(hiddenIn))
	for k, v := range hiddenIn {
		hidden[k] = v
	}
	g := &graphBuilder{hidden: hidden, r: newResolver(people), nodes: map[string]*Node{}, edges: map[[2]string]*Edge{}, ownerOf: map[int64]string{}}
	for id, src := range c.Sources {
		name := src.OwnerName
		if name == "" {
			name = src.OwnerHandle
		}
		k, _, _ := g.r.key(name)
		if k == "" {
			continue
		}
		if hidden[k] {
			delete(hidden, k) // sahip gizlenemez
		}
		k = g.node(name)
		g.nodes[k].IsOwner = true
		g.ownerOf[id] = k
		// Sahibin kullanıcı adı da aynı düğüme çözülsün diye kimlik kümesi kişi dosyasından gelir.
	}
	by := convMsgs(c)
	for _, conv := range c.Convs {
		owner := g.ownerOf[conv.SourceID]
		kind := "dm"
		if conv.IsGroup {
			kind = "group"
		}
		counts := map[string]int{}
		for _, m := range by[conv.ID] {
			if m.Kind == model.KindSystem {
				continue
			}
			counts[m.Sender]++
			for _, rc := range m.Reactions {
				if a, b := g.node(rc.Actor), g.node(m.Sender); a != "" && b != "" && a != b {
					g.linkDir(a, b, "reaction", 1)
				}
			}
		}
		if conv.IsGroup {
			for sender, n := range counts {
				k := g.node(sender)
				if k != owner {
					g.link(owner, k, kind, n)
				}
			}
			g.groupReplies(by[conv.ID])
			continue
		}
		for sender, n := range counts {
			k := g.node(sender)
			if owner == "" || k == "" {
				continue
			}
			if k != owner {
				g.linkDir(k, owner, kind, n)
			}
		}
		// Sahip sohbette adıyla görünmüyorsa (ör. WhatsApp'ta adı yoksa) iki katılımcı arasında kenar kur.
		if len(conv.Participants) == 2 {
			a, b := g.node(conv.Participants[0]), g.node(conv.Participants[1])
			if a != owner && b != owner && a != "" && b != "" {
				g.link(a, b, kind, counts[conv.Participants[0]]+counts[conv.Participants[1]])
			}
		}
	}
	for _, ev := range c.Events {
		owner := g.ownerOf[ev.SourceID]
		if owner == "" {
			continue
		}
		actor, cp := ev.Actor, ev.Counterpart
		var from, to string
		if actor == model.Owner {
			from, to = owner, g.node(cp)
		} else {
			from, to = g.node(actor), owner
		}
		if _, ok := edgeWeight[ev.Kind]; !ok {
			continue
		}
		g.linkDir(from, to, ev.Kind, 1)
	}

	// Güç hesabı.
	for _, e := range g.edges {
		var s float64
		for k, n := range e.Breakdown {
			s += edgeWeight[k] * float64(n)
		}
		e.Strength = math.Log1p(s)
		g.nodes[e.A].Strength += e.Strength
		g.nodes[e.B].Strength += e.Strength
	}
	nodes := make([]Node, 0, len(g.nodes))
	for _, n := range g.nodes {
		nodes = append(nodes, *n)
	}
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].IsOwner != nodes[j].IsOwner {
			return nodes[i].IsOwner
		}
		if nodes[i].Strength != nodes[j].Strength {
			return nodes[i].Strength > nodes[j].Strength
		}
		return nodes[i].ID < nodes[j].ID
	})
	total := len(nodes)
	if maxNodes > 0 && len(nodes) > maxNodes {
		nodes = nodes[:maxNodes]
	}
	keep := map[string]bool{}
	for _, n := range nodes {
		keep[n.ID] = true
	}
	edges := make([]Edge, 0, len(g.edges))
	for _, e := range g.edges {
		if keep[e.A] && keep[e.B] && e.Strength > 0 {
			edges = append(edges, *e)
		}
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].Strength != edges[j].Strength {
			return edges[i].Strength > edges[j].Strength
		}
		return edges[i].A+edges[i].B < edges[j].A+edges[j].B
	})
	return Graph{Nodes: nodes, Edges: edges, Total: total}
}

// LinkSuggestion, aynı kişiye ait olabilecek bir görünen ad ile kullanıcı adı çiftidir.
type LinkSuggestion struct {
	Name   string  `json:"name"`
	Handle string  `json:"handle"`
	Score  float64 `json:"score"`
}

func bigrams(s string) map[string]int {
	r := []rune(s)
	m := map[string]int{}
	for i := 0; i+1 < len(r); i++ {
		m[string(r[i:i+2])]++
	}
	return m
}

func dice(a, b string) float64 {
	if a == b && a != "" {
		return 1
	}
	ba, bb := bigrams(a), bigrams(b)
	inter, total := 0, 0
	for k, n := range ba {
		total += n
		if m := bb[k]; m > 0 {
			if m < n {
				inter += m
			} else {
				inter += n
			}
		}
	}
	for _, n := range bb {
		total += n
	}
	if total == 0 {
		return 0
	}
	return 2 * float64(inter) / float64(total)
}

// Suggest, henüz bir kişi dosyasına bağlanmamış görünen adlar ile kullanıcı adları arasında
// benzerlik önerileri üretir. Öneriler yalnızca öneridir; birleştirme insan onayıyla yapılır.
func Suggest(c *store.Corpus, people []store.Person, minScore float64) []LinkSuggestion {
	r := newResolver(people)
	names, handles := map[string]string{}, map[string]string{}
	for _, conv := range c.Convs {
		for _, p := range conv.Participants {
			if n := model.Norm(p); n != "" {
				if _, ok := r.byNorm[n]; !ok {
					names[n] = p
				}
			}
		}
	}
	for _, e := range c.Events {
		for _, h := range []string{e.Counterpart, e.Actor} {
			if h == "" || h == model.Owner {
				continue
			}
			if n := model.Norm(h); n != "" {
				if _, ok := r.byNorm[n]; !ok {
					handles[n] = h
				}
			}
		}
	}
	var out []LinkSuggestion
	for nn, name := range names {
		for hn, handle := range handles {
			if nn == hn {
				continue
			}
			sc := dice(nn, hn)
			if len(hn) >= 4 && (strings.Contains(nn, hn) || strings.Contains(hn, nn)) && sc < 0.7 {
				sc = 0.7
			}
			if sc >= minScore {
				out = append(out, LinkSuggestion{Name: name, Handle: handle, Score: math.Round(sc*100) / 100})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Name+out[i].Handle < out[j].Name+out[j].Handle
	})
	if len(out) > 40 {
		out = out[:40]
	}
	return out
}

// groupReplies bir grupta art arda (5 dk içinde) konuşan farklı üye çiftleri arasında kenar kurar:
// grubun içindeki alt kümeleşmeleri (kimin kime yanıt verdiği) haritaya taşır. Kalabalık gruplarda
// en çok yazan 8 üye ile sınırlıdır.
func (g *graphBuilder) groupReplies(msgs []store.MessageRow) {
	counts := map[string]int{}
	for _, m := range msgs {
		if m.Kind != model.KindSystem {
			counts[m.Sender]++
		}
	}
	allowed := map[string]bool{}
	for _, c := range top(counts, 8) {
		allowed[c.Key] = true
	}
	var prev *store.MessageRow
	for i := range msgs {
		m := &msgs[i]
		if m.Kind == model.KindSystem {
			continue
		}
		if prev != nil && allowed[m.Sender] && allowed[prev.Sender] && m.TS.Sub(prev.TS) <= replyWindow && model.Norm(m.Sender) != model.Norm(prev.Sender) {
			g.link(g.node(prev.Sender), g.node(m.Sender), "group_reply", 1)
		}
		prev = m
	}
}
