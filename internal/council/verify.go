package council

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/MustafaKemal0146/joe/internal/store"
)

// Destek düzeyleri.
const (
	SupportYes     = "supported"
	SupportPartial = "partial"
	SupportNo      = "unsupported"
)

// ClaimCheck bir iddianın kanıt desteği denetimidir.
type ClaimCheck struct {
	ID      string `json:"id"`
	Support string `json:"support"`
	Note    string `json:"note"`
}

// Verification sentezdeki iddiaların kanıt desteği sonuçlarıdır.
type Verification struct {
	Results []ClaimCheck `json:"results"`
}

const verifySystem = `Sen sıkı ama adil bir kanıt denetçisisin. Sana sentezden alınmış numaralı iddialar ve her iddianın dayandığı kanıt kesitlerinin ham metni verilecek. Her iddia için kesitlerin onu ne ölçüde desteklediğini değerlendir:
- "supported": iddianın dayandığı olgu kesitlerde doğrudan görülüyor.
- "partial": kısmen destekleniyor; fazla genelleme var ya da bir bölümü kesitlerde yok.
- "unsupported": kesitler bunu göstermiyor ya da tersini gösteriyor.
Kuramsal yorumlar ("olabilir" kipiyle yazılmış okumalar) için, yorumun DAYANDIĞI OLGUNUN kesitlerde olup olmadığına bak; yorumun kendisini doğru/yanlış diye yargılama. Yeni kanıt ekleme, kesitlerde olmayan hiçbir şeyi varsayma. Not alanı: gerekçe, en çok 200 karakter, sade Türkçe.
Yanıtın yalnızca şu JSON olsun, her iddia için bir sonuç ve id'ler verilenle aynı:
{"results":[{"id":"","support":"supported | partial | unsupported","note":""}]}`

const verifyBatch = 8

// claimRef denetlenecek bir iddia ve atıf yaptığı kesitlerdir.
type claimRef struct {
	ID, Text string
	Evidence []string
}

// claimRefs sentezdeki denetlenebilir iddiaları (bölümler, uzlaşı, ayrışma, gelişim) kimlikleriyle çıkarır.
func claimRefs(s Synthesis) []claimRef {
	var out []claimRef
	union := map[string]bool{}
	var all []string
	for _, p := range append(append([]Point{}, s.Convergences...), s.Divergences...) {
		for _, e := range p.Evidence {
			if !union[e] {
				union[e] = true
				all = append(all, e)
			}
		}
	}
	for i, sec := range s.Sections {
		ev := sec.Evidence
		if len(ev) == 0 {
			ev = all
		}
		out = append(out, claimRef{fmt.Sprintf("s%d", i+1), sec.Title + ": " + sec.Text, ev})
	}
	if len(s.Sections) == 0 && strings.TrimSpace(s.Summary) != "" {
		out = append(out, claimRef{"s1", s.Summary, all})
	}
	if strings.TrimSpace(s.Evolution) != "" {
		ev := s.EvolutionEvidence
		if len(ev) == 0 {
			ev = all
		}
		out = append(out, claimRef{"e1", s.Evolution, ev})
	}
	for i, p := range s.Convergences {
		out = append(out, claimRef{fmt.Sprintf("c%d", i+1), p.Text, p.Evidence})
	}
	for i, p := range s.Divergences {
		out = append(out, claimRef{fmt.Sprintf("d%d", i+1), p.Text, p.Evidence})
	}
	return out
}

const maxBlockChars = 2500

// verifyPrompt bir iddia grubu için denetim girdisini kurar.
func verifyPrompt(refs []claimRef, blocks map[string]string) string {
	var b strings.Builder
	b.WriteString("İddialar:\n")
	need := map[string]bool{}
	for _, r := range refs {
		fmt.Fprintf(&b, "\n[%s] %s\nAtıf: %s\n", r.ID, strings.TrimSpace(r.Text), strings.Join(r.Evidence, ", "))
		for _, e := range r.Evidence {
			need[e] = true
		}
	}
	ids := make([]string, 0, len(need))
	for e := range need {
		if blocks[e] != "" {
			ids = append(ids, e)
		}
	}
	sort.Slice(ids, func(i, j int) bool {
		return len(ids[i]) < len(ids[j]) || (len(ids[i]) == len(ids[j]) && ids[i] < ids[j])
	})
	b.WriteString("\nKanıt kesitleri:\n")
	for _, id := range ids {
		blk := blocks[id]
		if len(blk) > maxBlockChars {
			blk = blk[:maxBlockChars] + "\n[…kesildi]\n"
		}
		b.WriteString(blk)
	}
	return b.String()
}

// runVerify sentezdeki iddiaları kanıt kesitleriyle karşılaştırır. Başarısızlık oturumu başarısız yapmaz:
// denetim bir ek güvencedir; çıktı "failed" olarak kaydedilir ve sentez olduğu gibi kalır.
func (e *Engine) runVerify(ctx context.Context, sess store.CouncilSession, syn Synthesis) {
	o := store.CouncilOutput{SessionID: sess.ID, Stage: "verify", Status: store.OutRunning}
	e.save(o)
	refs := claimRefs(syn)
	blocks := kBlocks(sess.PackText)
	var all Verification
	var totalCost float64
	var pt, ct int
	var firstErr error
	for i := 0; i < len(refs); i += verifyBatch {
		end := i + verifyBatch
		if end > len(refs) {
			end = len(refs)
		}
		batch := refs[i:end]
		user := verifyPrompt(batch, blocks)
		key := store.CacheKey(sess.PackHash, "verify", e.strong().model, personasVersion(), hash(user))
		res, err := e.callOn(ctx, e.strong(), sess.ID, "verify", verifySystem, user, 4000, key)
		if err != nil {
			firstErr = err
			break
		}
		var v Verification
		fixed, _, perr := e.withRepair(ctx, sess.ID, "verify", `{"results":[{"id":"","support":"supported | partial | unsupported","note":""}]}`, res, func(raw string) error {
			var err error
			v, err = ParseVerification(raw, batch)
			return err
		})
		totalCost += fixed.cost
		pt += fixed.usage.PromptTokens
		ct += fixed.usage.CompletionTokens
		if perr != nil {
			firstErr = perr
			break
		}
		if !fixed.fromCache {
			_ = e.Store.CachePut(key, store.CachedResponse{Response: fixed.raw, PromptTokens: fixed.usage.PromptTokens, CompletionTokens: fixed.usage.CompletionTokens})
		}
		all.Results = append(all.Results, v.Results...)
	}
	o.CostUSD, o.PromptTokens, o.CompletionTokens = totalCost, pt, ct
	if firstErr != nil && len(all.Results) == 0 {
		e.fail(&o, firstErr)
		return
	}
	o.Output, _ = json.Marshal(all)
	o.Status = store.OutDone
	if firstErr != nil {
		o.Error = "bazı iddialar denetlenemedi: " + firstErr.Error()
	}
	e.save(o)
}

// ParseVerification denetçi çıktısını doğrular: her verilen iddia için tam bir sonuç, geçerli destek düzeyi.
func ParseVerification(raw string, refs []claimRef) (Verification, error) {
	var v Verification
	if err := decodeStrict(raw, &v); err != nil {
		return v, err
	}
	want := map[string]bool{}
	for _, r := range refs {
		want[r.ID] = true
	}
	seen := map[string]bool{}
	var kept []ClaimCheck
	for _, c := range v.Results {
		if !want[c.ID] || seen[c.ID] {
			continue // bilinmeyen ya da yinelenen kimlik yok sayılır
		}
		if c.Support != SupportYes && c.Support != SupportPartial && c.Support != SupportNo {
			return v, fmt.Errorf("%w: iddia %s: destek düzeyi supported/partial/unsupported olmalı, %q geldi", ErrInvalid, c.ID, c.Support)
		}
		if r := []rune(c.Note); len(r) > 400 {
			c.Note = string(r[:400])
		}
		seen[c.ID] = true
		kept = append(kept, c)
	}
	if len(kept) != len(want) {
		return v, fmt.Errorf("%w: %d iddia için sonuç bekleniyordu, %d geldi", ErrInvalid, len(want), len(kept))
	}
	v.Results = kept
	return v, nil
}
