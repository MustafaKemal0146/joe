package council

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/MustafaKemal0146/joe/internal/store"
)

// DigestMaxOut bir dönem özetinin çıktı token sınırıdır.
const DigestMaxOut = 3000

const digestSystem = `Sen bir dönem özetleyicisisin. Aşağıda bir ilişkinin ya da kişinin yazışmalarının TEK BİR DÖNEMİNE ait kanıt kesitleri var. O dönemin sade bir özetini çıkar: neler konuşuluyor, ton nasıl (sıcak, gergin, mesafeli, oyunbaz…), dikkat çeken olay ya da kırılma var mı, taraflar arasındaki denge nasıl. Yalnızca verilen kesitlere dayan; her cümlenin sonuna dayandığın kesit kimliğini (K<n>) yaz. Klinik tanı, tehlikelilik ya da yargı üretme; yorumları "olabilir" kipiyle yaz. Sade, günlük Türkçe kullan.
Yanıtın yalnızca şu JSON olsun:
{"period":"kısa dönem adı","summary":"600–1000 karakter","tone":"tek cümlelik ton özeti","evidence":["K1","K2"]}`

// Digest bir dönemin özetidir (modelin kendi özeti; kanıt değildir).
type Digest struct {
	Period   string   `json:"period"`
	Summary  string   `json:"summary"`
	Tone     string   `json:"tone"`
	Evidence []string `json:"evidence"`
}

// ParseDigest dönem özeti çıktısını doğrular.
func ParseDigest(raw string) (Digest, error) {
	var d Digest
	if err := decodeStrict(raw, &d); err != nil {
		return d, err
	}
	if strings.TrimSpace(d.Summary) == "" || len(d.Evidence) == 0 {
		return d, fmt.Errorf("%w: dönem özeti ve en az bir kanıt gerekir", ErrInvalid)
	}
	return d, nil
}

type packItem struct {
	ID   string    `json:"id"`
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

func itemNum(id string) int {
	n, _ := strconv.Atoi(strings.TrimPrefix(id, "K"))
	return n
}

// digestGroups kesitleri zaman sırasıyla (K numarası kronolojiktir) g bitişik gruba böler.
func digestGroups(sess store.CouncilSession, g int) [][]packItem {
	var pk struct {
		Items []packItem `json:"items"`
	}
	_ = json.Unmarshal(sess.PackJSON, &pk)
	items := pk.Items
	sort.SliceStable(items, func(i, j int) bool { return itemNum(items[i].ID) < itemNum(items[j].ID) })
	if len(items) == 0 {
		return nil
	}
	if g > len(items) {
		g = len(items)
	}
	var out [][]packItem
	for i := 0; i < g; i++ {
		from, to := i*len(items)/g, (i+1)*len(items)/g
		if to > from {
			out = append(out, items[from:to])
		}
	}
	return out
}

// runDigests derin modda kesitleri dönemlere böler ve her dönemi özetletir. Tamamlanan özetler tekrarlanmaz
// (devam etme). Dönen metin, persona/moderatör/sentez girdisine eklenir; özet üretilemezse "" döner (analiz
// özetsiz sürer). Özetler kanıt değildir: kanıtlar K kimlikli kesitlerdir.
func (e *Engine) runDigests(ctx context.Context, sess store.CouncilSession, existing []store.CouncilOutput) string {
	groups := digestGroups(sess, DigestGroups(len(validIDs(sess))))
	if len(groups) == 0 {
		return ""
	}
	have := map[string]store.CouncilOutput{}
	for _, o := range existing {
		if o.Stage == "digest" && o.Status == store.OutDone {
			have[o.PersonaID] = o
		}
	}
	blocks := kBlocks(sess.PackText)
	sem := make(chan struct{}, 3)
	var wg sync.WaitGroup
	for i, grp := range groups {
		pid := fmt.Sprintf("d%02d", i+1)
		if _, ok := have[pid]; ok {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			if ctx.Err() != nil {
				return
			}
			var b strings.Builder
			fmt.Fprintf(&b, "Dönem %d/%d (%s – %s). Kesitler:\n", i+1, len(groups), grp[0].From.Format("02.01.2006"), grp[len(grp)-1].To.Format("02.01.2006"))
			used := 0
			for _, it := range grp {
				blk := blocks[it.ID]
				if used+len(blk) > 90_000 {
					break
				}
				b.WriteString(blk)
				used += len(blk)
			}
			user := b.String()
			o := store.CouncilOutput{SessionID: sess.ID, Stage: "digest", PersonaID: pid, Status: store.OutRunning}
			e.save(o)
			key := store.CacheKey(sess.PackHash, "digest", e.Model, personasVersion(), pid, hash(user))
			res, err := e.call(ctx, sess.ID, "digest", digestSystem, user, DigestMaxOut, key)
			if err != nil {
				e.fail(&o, err)
				return
			}
			var d Digest
			fixed, attempts, perr := e.withRepair(ctx, sess.ID, "digest", `{"period":"","summary":"","tone":"","evidence":["K1"]}`, res, func(raw string) error {
				var err error
				d, err = ParseDigest(raw)
				return err
			})
			o.Raw, o.Attempts, o.CostUSD, o.FromCache = fixed.raw, attempts, fixed.cost, fixed.fromCache
			o.PromptTokens, o.CompletionTokens = fixed.usage.PromptTokens, fixed.usage.CompletionTokens
			if perr != nil {
				e.fail(&o, perr)
				return
			}
			if !fixed.fromCache {
				_ = e.Store.CachePut(key, store.CachedResponse{Response: fixed.raw, PromptTokens: fixed.usage.PromptTokens, CompletionTokens: fixed.usage.CompletionTokens})
			}
			o.Output, _ = json.Marshal(d)
			o.Status = store.OutDone
			e.save(o)
		}()
	}
	wg.Wait()
	outs, err := e.Store.Outputs(sess.ID)
	if err != nil {
		return ""
	}
	return digestText(outs)
}

// digestText tamamlanmış dönem özetlerini, dönem sırasına göre, persona/moderatör/sentez girdisine eklenecek metne çevirir.
func digestText(outs []store.CouncilOutput) string {
	var ds []store.CouncilOutput
	for _, o := range outs {
		if o.Stage == "digest" && o.Status == store.OutDone {
			ds = append(ds, o)
		}
	}
	if len(ds) == 0 {
		return ""
	}
	sort.SliceStable(ds, func(i, j int) bool { return ds[i].PersonaID < ds[j].PersonaID })
	var b strings.Builder
	b.WriteString("## Dönem özetleri\n(Modelin kesitlerden çıkardığı özetler; KANIT DEĞİLDİR. Kanıt, K kimlikli kesitlerdir; bir iddiayı özete değil kesite dayandır.)\n")
	for i, o := range ds {
		var d Digest
		if json.Unmarshal(o.Output, &d) != nil {
			continue
		}
		fmt.Fprintf(&b, "\n### Dönem %d: %s\n%s\nTon: %s (%s)\n", i+1, strings.TrimSpace(d.Period), strings.TrimSpace(d.Summary), strings.TrimSpace(d.Tone), strings.Join(d.Evidence, ", "))
	}
	return strings.TrimRight(b.String(), "\n")
}
