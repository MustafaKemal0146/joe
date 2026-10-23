package council

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/MustafaKemal0146/joe/internal/audit"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MustafaKemal0146/joe/internal/budget"
	"github.com/MustafaKemal0146/joe/internal/llm"
	"github.com/MustafaKemal0146/joe/internal/personas"
	"github.com/MustafaKemal0146/joe/internal/store"
)

const validPersona = `{"abstain":false,"abstain_reason":"","observations":[{"text":"Gece mesajları artmış.","evidence":["K1","K2"]}],
"hypotheses":[{"claim":"Yakınlık arayışı olabilir.","evidence":["K1"],"counter_hypothesis":"Çalışma saatleri yüzünden gece kalmış olabilir.","confidence":"low","falsified_by":"Hafta sonu gündüz de yoğunsa."}],
"open_questions":["İş saatleri?"],"data_needed":["Hafta içi/sonu ayrımı"]}`

const validModerator = `{"agreements":[{"point":"Gece yoğunluğu ortak gözlem.","personas":["freud","jung"],"evidence":["K1"]}],
"disagreements":[{"topic":"Gecenin anlamı","positions":[{"persona":"freud","view":"Denetimin gevşemesi olabilir.","evidence":["K1"]},{"persona":"jung","view":"Gölge teması olabilir.","evidence":["K2"]}]}],"gaps":["Gündüz verisi az."]}`

const validSynthesis = `{"summary":"Gece yoğunluğu öne çıkıyor; anlamı belirsiz.","convergences":[{"text":"Gece yoğunluğu.","evidence":["K1"]}],
"divergences":[{"text":"Anlam farklı okunuyor.","evidence":["K2"]}],"alternative_explanations":["Çalışma saatleri."],"open_questions":["İş saatleri?"],"data_needed":["Gündüz verisi"],"confidence":"low","limits":"Yalnızca iki kaynak."}`

type call struct{ system, user string }

type fake struct {
	mu      sync.Mutex
	calls   []call
	persona func(name string) (string, error)
	verify  func(user string) string // nil: tüm iddialar "supported"
	n       atomic.Int32
}

func (f *fake) Complete(ctx context.Context, system, user string, max int) (string, llm.Usage, error) {
	f.mu.Lock()
	f.calls = append(f.calls, call{system, user})
	f.mu.Unlock()
	f.n.Add(1)
	if err := ctx.Err(); err != nil {
		return "", llm.Usage{}, err
	}
	u := llm.Usage{PromptTokens: 1000, CompletionTokens: 300}
	switch {
	case system == moderatorSystem:
		return validModerator, u, nil
	case system == synthesisSystem:
		return validSynthesis, u, nil
	case system == repairSystem:
		return validPersona, u, nil
	case system == digestSystem:
		return `{"period":"Bir dönem","summary":"Akşam yazışmaları sürüyor.","tone":"sakin","evidence":["K1"]}`, u, nil
	case system == verifySystem:
		if f.verify != nil {
			return f.verify(user), u, nil
		}
		return verifyAllSupported(user), u, nil
	}
	name := user[strings.LastIndex(user, "# Mercek: ")+len("# Mercek: "):]
	name = name[:strings.Index(name, " (")]
	if f.persona != nil {
		out, err := f.persona(name)
		return out, u, err
	}
	return validPersona, u, nil
}

func (f *fake) count(pred func(call) bool) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if pred(c) {
			n++
		}
	}
	return n
}

func setup(t *testing.T, ids ...string) (*Engine, *store.Store, string, *fake) {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ps, _ := personas.Load("")
	f := &fake{}
	eng := &Engine{Store: st, Caller: f, Provider: "deepseek", Model: "deepseek-flash", CapUSD: 1, Concurrency: 2, Personas: ps}
	if len(ids) == 0 {
		ids = []string{"freud", "jung", "klein"}
	}
	id := newSession(t, st, "paket-1", ids)
	return eng, st, id, f
}

func newSession(t *testing.T, st *store.Store, packHash string, ids []string) string {
	t.Helper()
	id := "s-" + packHash + "-" + strings.Join(ids, "")
	err := st.CreateCouncil(store.CouncilSession{ID: id, Kind: "single", SubjectIDs: []int64{1}, PersonaIDs: ids, Provider: "deepseek", Model: "deepseek-flash",
		PackHash: packHash, PackText: "# Kanıt paketi\n\n## Metrikler\nKişi: Ben\n\n## K1 — a\n[x] Ben: selam\n\n## K2 — b\n[y] Ayşe: merhaba\n",
		PackJSON: json.RawMessage(`{"items":[{"id":"K1"},{"id":"K2"},{"id":"K3"}]}`)})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func outputsByKey(t *testing.T, st *store.Store, id string) map[string]store.CouncilOutput {
	t.Helper()
	outs, _ := st.Outputs(id)
	m := map[string]store.CouncilOutput{}
	for _, o := range outs {
		m[o.Stage+"|"+o.PersonaID] = o
	}
	return m
}

func TestHappyPathRunsPersonasModeratorSynthesis(t *testing.T) {
	eng, st, id, f := setup(t)
	if err := eng.Run(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	sess, _ := st.Council(id)
	if sess.Status != store.CouncilDone {
		t.Fatalf("durum %s (%s)", sess.Status, sess.Error)
	}
	if got := int(f.n.Load()); got != 6 { // 3 persona + moderatör + sentez + doğrulama = N+3
		t.Fatalf("çağrı sayısı %d, beklenen 6", got)
	}
	m := outputsByKey(t, st, id)
	for _, k := range []string{"persona|freud", "persona|jung", "persona|klein", "moderator|", "synthesis|"} {
		if m[k].Status != store.OutDone || len(m[k].Output) == 0 || len(m[k].Audit) == 0 {
			t.Errorf("%s tamamlanmamış: %+v", k, m[k])
		}
	}
	sum, _ := st.Spend()
	if sum.Calls != 6 || sum.TotalUSD <= 0 {
		t.Fatalf("defter: %+v", sum)
	}
	var syn Synthesis
	json.Unmarshal(m["synthesis|"].Output, &syn)
	if syn.Confidence != "low" || len(syn.Alternatives) == 0 {
		t.Fatalf("sentez: %+v", syn)
	}
}

func TestPersonaCallsShareIdenticalPrefix(t *testing.T) {
	eng, _, id, f := setup(t)
	eng.Run(context.Background(), id)
	var systems []string
	var users []string
	f.mu.Lock()
	for _, c := range f.calls {
		if strings.Contains(c.user, "# Mercek: ") {
			systems = append(systems, c.system)
			users = append(users, c.user)
		}
	}
	f.mu.Unlock()
	if len(users) != 3 {
		t.Fatalf("persona çağrısı %d", len(users))
	}
	cut := strings.Index(users[0], "# Mercek: ")
	for i := range users {
		if systems[i] != systems[0] {
			t.Fatal("sistem istemi bütün persona'larda birebir aynı olmalı (ön ek önbelleği)")
		}
		if users[i][:cut] != users[0][:cut] {
			t.Fatal("kanıt paketi bütün çağrılarda birebir aynı ön ek olmalı")
		}
	}
	if users[0] == users[1] {
		t.Fatal("persona bölümü farklı olmalı")
	}
}

func TestFailedPersonaThenResumeOnlyRetriesMissing(t *testing.T) {
	eng, st, id, f := setup(t)
	f.persona = func(name string) (string, error) {
		if name == "Carl Gustav Jung" {
			return "", errors.New("ağ hatası")
		}
		return validPersona, nil
	}
	eng.Run(context.Background(), id)
	sess, _ := st.Council(id)
	if sess.Status != store.CouncilPartial {
		t.Fatalf("kısmi bekleniyordu: %s", sess.Status)
	}
	m := outputsByKey(t, st, id)
	if m["persona|jung"].Status != store.OutFailed || !strings.Contains(m["persona|jung"].Error, "ağ hatası") {
		t.Fatalf("jung hatası kaydedilmeli: %+v", m["persona|jung"])
	}
	if m["synthesis|"].Status != store.OutDone {
		t.Fatal("kalan persona'larla sentez yapılmalı")
	}
	// devam: yalnızca jung yeniden denensin, sentez zaten tamam
	f.persona = nil
	before := int(f.n.Load())
	eng.Run(context.Background(), id)
	if added := int(f.n.Load()) - before; added != 1 {
		t.Fatalf("devamda yalnızca 1 çağrı beklenir (jung), %d yapıldı", added)
	}
	sess, _ = st.Council(id)
	if sess.Status != store.CouncilDone {
		t.Fatalf("devamdan sonra tamam olmalı: %s", sess.Status)
	}
}

func TestInvalidJSONGetsOneRepairAndNeverFabricates(t *testing.T) {
	eng, st, id, f := setup(t, "freud", "jung")
	f.persona = func(name string) (string, error) {
		if name == "Sigmund Freud" {
			return "üzgünüm, JSON veremiyorum", nil // onarım çağrısı geçerli döner
		}
		return validPersona, nil
	}
	eng.Run(context.Background(), id)
	m := outputsByKey(t, st, id)
	if m["persona|freud"].Status != store.OutDone || m["persona|freud"].Attempts != 2 {
		t.Fatalf("onarımla tamamlanmalı: %+v", m["persona|freud"])
	}
	if f.count(func(c call) bool { return c.system == repairSystem }) != 1 {
		t.Fatal("tam bir onarım çağrısı beklenir")
	}

	// onarım da başarısızsa: çıktı uydurulmaz, ham metin saklanır, oturum kısmi kalır
	eng2, st2, id2, f2 := setup(t, "freud", "jung")
	f2.persona = func(name string) (string, error) {
		if name == "Sigmund Freud" {
			return "hâlâ JSON değil", nil
		}
		return validPersona, nil
	}
	eng2.Caller = &repairBroken{f2}
	eng2.Run(context.Background(), id2)
	m2 := outputsByKey(t, st2, id2)
	if m2["persona|freud"].Status != store.OutFailed || len(m2["persona|freud"].Output) != 0 || m2["persona|freud"].Raw == "" {
		t.Fatalf("başarısız çıktı uydurulmamalı, ham metin korunmalı: %+v", m2["persona|freud"])
	}
}

// repairBroken onarım çağrılarına da geçersiz yanıt verir.
type repairBroken struct{ f *fake }

func (r *repairBroken) Complete(ctx context.Context, system, user string, max int) (string, llm.Usage, error) {
	if system == repairSystem {
		return "yine bozuk", llm.Usage{PromptTokens: 10, CompletionTokens: 5}, nil
	}
	return r.f.Complete(ctx, system, user, max)
}

func TestBudgetCapStopsAndKeepsResults(t *testing.T) {
	eng, st, id, f := setup(t)
	eng.Concurrency = 1
	// Tavan: bir çağrının en kötü durum tahmini + 0.001. Gerçek çağrı ≈ 0.00066 harcar; üçüncü
	// çağrının (harcanan 0.00132 + en kötü tahmin) tavanı aşması beklenir.
	sess0, _ := st.Council(id)
	p0, _ := eng.persona("freud")
	high := budget.Compute(budget.Plan{Model: eng.Model, SharedPrefixTokens: budget.EstimateTokens(personas.CommonSystem() + sess0.PackText + personas.Section(p0)), Calls: 1, OutputTokensPerCall: PersonaMaxOut}).HighUSD
	eng.CapUSD = high + 0.001
	eng.Run(context.Background(), id)
	sess, _ := st.Council(id)
	if sess.Status != store.CouncilBudgetStop {
		t.Fatalf("bütçe durdurması bekleniyordu: %s (%s)", sess.Status, sess.Error)
	}
	if n := int(f.n.Load()); n >= 5 {
		t.Fatalf("tavan sonrası çağrı yapılmamalı: %d çağrı", n)
	}
	spent, _ := st.SessionSpend(id)
	if spent > eng.CapUSD {
		t.Fatalf("harcama tavanı aştı: %.5f > %.5f", spent, eng.CapUSD)
	}
	m := outputsByKey(t, st, id)
	done, skipped := 0, 0
	for k, o := range m {
		if strings.HasPrefix(k, "persona|") {
			if o.Status == store.OutDone {
				done++
			}
			if o.Status == store.OutSkipped {
				skipped++
			}
		}
	}
	if done == 0 || skipped == 0 {
		t.Fatalf("eldekiler korunmalı, kalanlar atlanmalı: done=%d skipped=%d", done, skipped)
	}
}

func TestEveryCallBlockedWhenCapIsZero(t *testing.T) {
	eng, st, id, f := setup(t)
	eng.CapUSD = 0
	eng.Run(context.Background(), id)
	if f.n.Load() != 0 {
		t.Fatal("tavan 0 iken hiçbir çağrı yapılmamalı")
	}
	if sess, _ := st.Council(id); sess.Status != store.CouncilBudgetStop {
		t.Fatalf("durum %s", sess.Status)
	}
}

func TestSecondSessionWithSamePackIsServedFromCache(t *testing.T) {
	eng, st, id, f := setup(t)
	eng.Run(context.Background(), id)
	first := int(f.n.Load())
	id2 := "s-ikinci"
	st.CreateCouncil(store.CouncilSession{ID: id2, Kind: "single", SubjectIDs: []int64{1}, PersonaIDs: []string{"freud", "jung", "klein"}, Provider: "deepseek", Model: "deepseek-flash",
		PackHash: "paket-1", PackText: "# Kanıt paketi\n\n## Metrikler\nKişi: Ben\n\n## K1 — a\n[x] Ben: selam\n\n## K2 — b\n[y] Ayşe: merhaba\n",
		PackJSON: json.RawMessage(`{"items":[{"id":"K1"},{"id":"K2"},{"id":"K3"}]}`)})
	eng.Run(context.Background(), id2)
	if int(f.n.Load()) != first {
		t.Fatalf("aynı paket için LLM çağrısı yapılmamalı: %d → %d", first, f.n.Load())
	}
	if spent, _ := st.SessionSpend(id2); spent != 0 {
		t.Fatalf("önbellekten dönen oturum 0 USD olmalı: %v", spent)
	}
	if sess, _ := st.Council(id2); sess.Status != store.CouncilDone {
		t.Fatalf("durum %s", sess.Status)
	}
}

func TestCancelKeepsFinishedOutputs(t *testing.T) {
	eng, st, id, f := setup(t)
	eng.Concurrency = 1
	ctx, cancel := context.WithCancel(context.Background())
	f.persona = func(name string) (string, error) {
		if name == "Carl Gustav Jung" {
			cancel() // ikinci persona sırasında iptal
		}
		return validPersona, nil
	}
	eng.Run(ctx, id)
	sess, _ := st.Council(id)
	if sess.Status != store.CouncilCanceled {
		t.Fatalf("durum %s", sess.Status)
	}
	if m := outputsByKey(t, st, id); m["persona|freud"].Status != store.OutDone {
		t.Fatal("iptalden önce biten çıktı korunmalı")
	}
}

func TestAuditFlagsDiagnosisButKeepsOutput(t *testing.T) {
	eng, st, id, f := setup(t, "freud", "jung")
	bad := strings.Replace(validPersona, "Yakınlık arayışı olabilir.", "Kişide borderline kişilik bozukluğu var.", 1)
	f.persona = func(name string) (string, error) {
		if name == "Sigmund Freud" {
			return bad, nil
		}
		return validPersona, nil
	}
	eng.Run(context.Background(), id)
	o := outputsByKey(t, st, id)["persona|freud"]
	if o.Status != store.OutDone || !strings.Contains(string(o.Audit), "teshis_dili") {
		t.Fatalf("ihlal işaretlenmeli, çıktı saklanmalı: %s", o.Audit)
	}
}

func TestSinglePersonaSkipsModerator(t *testing.T) {
	eng, st, id, f := setup(t, "freud")
	eng.Run(context.Background(), id)
	if int(f.n.Load()) != 3 { // persona + sentez + doğrulama
		t.Fatalf("tek persona: 3 çağrı beklenir, %d", f.n.Load())
	}
	if m := outputsByKey(t, st, id); m["moderator|"].ID != 0 {
		t.Fatal("tek persona ile moderatör çalışmamalı")
	}
}

func TestParsersRejectInvalid(t *testing.T) {
	if _, err := ParseModerator(`{"agreements":[],"disagreements":[],"gaps":[]}`); err == nil {
		t.Error("boş moderatör reddedilmeli")
	}
	if _, err := ParseModerator(`{"disagreements":[{"topic":"x","positions":[{"persona":"a","view":"v","evidence":["K1"]}]}]}`); err == nil {
		t.Error("tek görüşlü ayrışma reddedilmeli")
	}
	if _, err := ParseSynthesis(`{"summary":"x","confidence":"kesin","alternative_explanations":["a"]}`); err == nil {
		t.Error("geçersiz güven reddedilmeli")
	}
	if _, err := ParseSynthesis(`{"summary":"x","confidence":"low","alternative_explanations":[]}`); err == nil {
		t.Error("alternatifsiz sentez reddedilmeli")
	}
	if _, err := ParseSynthesis(`{"summary":"x","confidence":"low","alternative_explanations":["a"],"convergences":[{"text":"t","evidence":[]}]}`); err == nil {
		t.Error("kanıtsız madde reddedilmeli")
	}
	if _, err := ParseAnswer(`{"answer":"x","evidence":[]}`); err == nil {
		t.Error("kanıtsız cevap reddedilmeli")
	}
	if a, err := ParseAnswer(`{"abstain":true,"abstain_reason":"veri yok"}`); err != nil || !a.Abstain {
		t.Errorf("gerekçeli çekimserlik geçerli: %v", err)
	}
}

func TestCacheSurvivesDifferentPersonaCompletionOrder(t *testing.T) {
	// Gerçek hata: moderatör/sentez önbellek anahtarı persona bitiş sırasına bağlıydı; eşzamanlı çalışmada
	// ikinci oturum gereksiz yere yeniden ödeniyordu. Burada ilk oturumda en yavaş persona ilk sıradaki olur.
	for round := 0; round < 5; round++ {
		eng, st, id, f := setup(t)
		eng.Concurrency = 3
		f.persona = func(name string) (string, error) {
			if name == "Sigmund Freud" {
				time.Sleep(60 * time.Millisecond) // en yavaş: en son biter
			}
			return validPersona, nil
		}
		if err := eng.Run(context.Background(), id); err != nil {
			t.Fatal(err)
		}
		first := int(f.n.Load())
		id2 := fmt.Sprintf("s-ikinci-%d", round)
		st.CreateCouncil(store.CouncilSession{ID: id2, Kind: "single", SubjectIDs: []int64{1}, PersonaIDs: []string{"freud", "jung", "klein"}, Provider: "deepseek", Model: "deepseek-flash",
			PackHash: "paket-1", PackText: "# Kanıt paketi\n\n## Metrikler\nKişi: Ben\n\n## K1 — a\n[x] Ben: selam\n\n## K2 — b\n[y] Ayşe: merhaba\n",
			PackJSON: json.RawMessage(`{"items":[{"id":"K1"},{"id":"K2"},{"id":"K3"}]}`)})
		f.persona = nil
		if err := eng.Run(context.Background(), id2); err != nil {
			t.Fatal(err)
		}
		if int(f.n.Load()) != first {
			t.Fatalf("tur %d: ikinci oturum önbellekten dönmeliydi: %d → %d çağrı", round, first, f.n.Load())
		}
	}
}

// truncating ilk persona yanıtını token sınırında keser (yarım JSON), daha büyük sınırla geçerli yanıt verir.
type truncating struct {
	f      *fake
	always bool
	mu     sync.Mutex
	maxes  []int
}

func (c *truncating) Complete(ctx context.Context, system, user string, max int) (string, llm.Usage, error) {
	if system == moderatorSystem || system == synthesisSystem || system == repairSystem || system == verifySystem || system == digestSystem {
		return c.f.Complete(ctx, system, user, max)
	}
	c.mu.Lock()
	c.maxes = append(c.maxes, max)
	n := len(c.maxes)
	c.mu.Unlock()
	if c.always || n == 1 {
		return validPersona[:len(validPersona)/2], llm.Usage{PromptTokens: 1000, CompletionTokens: max}, nil // sınıra dayandı
	}
	return validPersona, llm.Usage{PromptTokens: 1000, CompletionTokens: 400}, nil
}

func TestTruncatedOutputIsRetriedWithBiggerLimitNotRepaired(t *testing.T) {
	eng, st, id, f := setup(t, "freud")
	c := &truncating{f: f}
	eng.Caller = c
	if err := eng.Run(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	o := outputsByKey(t, st, id)["persona|freud"]
	if o.Status != store.OutDone || o.Attempts != 2 {
		t.Fatalf("yeniden denemeyle tamamlanmalı: %+v", o)
	}
	if len(c.maxes) < 2 || c.maxes[1] != c.maxes[0]*2 { // sonraki çağrılar (sentez, doğrulama) yok sayılır
		t.Fatalf("ikinci deneme iki kat sınırla yapılmalı: %v", c.maxes)
	}
	if f.count(func(cl call) bool { return cl.system == repairSystem }) != 0 {
		t.Fatal("kesilmiş JSON için onarım çağrısı boşa para harcar; yapılmamalı")
	}
	// kesik yanıt önbelleğe girmemeli: aynı anahtarla yeniden istenince geçerli yanıt dönmeli
	key := store.CacheKey("paket-1", "freud", "deepseek-flash", "v2", "single")
	if cached, ok, _ := st.CacheGet(key); ok && !strings.Contains(cached.Response, "observations") {
		t.Fatalf("kesik yanıt önbelleğe yazılmış: %q", cached.Response)
	}
}

func TestTwiceTruncatedFailsWithoutRepairOrFabrication(t *testing.T) {
	eng, st, id, f := setup(t, "freud", "jung")
	c := &truncating{f: f, always: true}
	eng.Caller = c
	eng.Run(context.Background(), id)
	m := outputsByKey(t, st, id)
	for _, k := range []string{"persona|freud", "persona|jung"} {
		o := m[k]
		if o.Status != store.OutFailed || len(o.Output) != 0 || !strings.Contains(o.Error, "kesildi") {
			t.Fatalf("%s: kesilen çıktı uydurulmadan başarısız olmalı: %+v", k, o)
		}
	}
	if f.count(func(cl call) bool { return cl.system == repairSystem }) != 0 {
		t.Fatal("onarım çağrısı yapılmamalı")
	}
	if len(c.maxes) != 4 { // iki persona × (ilk + iki kat)
		t.Fatalf("her persona için tam iki deneme: %v", c.maxes)
	}
}

func TestSynthesisEvolutionIsOptionalAndAudited(t *testing.T) {
	old, err := ParseSynthesis(validSynthesis) // evolution alanı olmayan eski biçim
	if err != nil || old.Evolution != "" {
		t.Fatalf("eski biçim hâlâ geçerli olmalı: %v", err)
	}
	if len(old.Claims()) != len(old.Convergences)+len(old.Divergences)+len(old.Alternatives)+1 {
		t.Fatal("evolution yokken ek iddia üretilmemeli")
	}
	withEvo := strings.Replace(validSynthesis, `"summary":`, `"evolution":"Ocak'ta yakın, Mart'ta mesafeli bir seyir olabilir.","evolution_evidence":["K1","K9"],"summary":`, 1)
	s, err := ParseSynthesis(withEvo)
	if err != nil || s.Evolution == "" || len(s.EvolutionEvidence) != 2 {
		t.Fatalf("%v %+v", err, s)
	}
	foundEvo := false
	for _, c := range s.Claims() {
		if strings.Contains(c.Text, "Mart'ta mesafeli") {
			foundEvo = true
			if len(c.Evidence) != 2 {
				t.Fatalf("evolution kendi kanıtlarıyla denetlenmeli: %v", c.Evidence)
			}
		}
	}
	if !foundEvo {
		t.Fatal("evolution denetim iddialarında olmalı")
	}
	rep := auditCheck(s)
	if !strings.Contains(rep, "gecersiz_kanit") {
		t.Fatalf("evolution'daki olmayan kanıt (K9) yakalanmalı: %s", rep)
	}
}

func auditCheck(s Synthesis) string {
	rep := audit.Check(s.Claims(), "", audit.ValidIDs(3))
	b, _ := json.Marshal(rep)
	return string(b)
}

func TestSynthesisThatDropsModeratorDisagreementsIsFlagged(t *testing.T) {
	eng, st, id, f := setup(t, "freud", "jung")
	noDiv := strings.Replace(validSynthesis, `"divergences":[{"text":"Anlam farklı okunuyor.","evidence":["K2"]}]`, `"divergences":[]`, 1)
	g := &swapSynthesis{f: f, out: noDiv}
	eng.Caller = g
	eng.Run(context.Background(), id)
	o := outputsByKey(t, st, id)["synthesis|"]
	if o.Status != store.OutDone || !strings.Contains(string(o.Audit), "ayrisma_kayboldu") {
		t.Fatalf("kaybolan ayrışma işaretlenmeli, çıktı saklanmalı: %s", o.Audit)
	}
	// sentez ayrışmayı yansıtıyorsa uyarı yok
	eng2, st2, id2, _ := setup(t, "freud", "jung")
	eng2.Run(context.Background(), id2)
	if o := outputsByKey(t, st2, id2)["synthesis|"]; strings.Contains(string(o.Audit), "ayrisma_kayboldu") {
		t.Fatalf("yansıtılan ayrışma için uyarı olmamalı: %s", o.Audit)
	}
}

type swapSynthesis struct {
	f   *fake
	out string
}

func (s *swapSynthesis) Complete(ctx context.Context, system, user string, max int) (string, llm.Usage, error) {
	if system == synthesisSystem {
		return s.out, llm.Usage{PromptTokens: 100, CompletionTokens: 50}, nil
	}
	return s.f.Complete(ctx, system, user, max)
}

func TestShortOverviewIsFlaggedAndLongOneIsNot(t *testing.T) {
	// validSynthesis'in özeti çok kısa: istenen 4000 karakterin altında → uyarı, çıktı korunur
	eng, st, id, _ := setup(t, "freud", "jung")
	eng.Run(context.Background(), id)
	o := outputsByKey(t, st, id)["synthesis|"]
	if o.Status != store.OutDone || !strings.Contains(string(o.Audit), "kisa_degerlendirme") || !strings.Contains(string(o.Audit), "en az 4000") {
		t.Fatalf("kısa özet işaretlenmeli, çıktı saklanmalı: %s", o.Audit)
	}
	long := strings.Repeat("Bu cümle sade bir dille yazılmış uzun genel değerlendirmenin bir parçasıdır. ", 70) // > 4000 karakter
	eng2, st2, id2, f2 := setup(t, "freud", "jung")
	g := &swapSynthesis{f: f2, out: strings.Replace(validSynthesis, `"summary":"Gece yoğunluğu öne çıkıyor; anlamı belirsiz."`, `"summary":"`+long+`"`, 1)}
	eng2.Caller = g
	eng2.Run(context.Background(), id2)
	if o := outputsByKey(t, st2, id2)["synthesis|"]; o.Status != store.OutDone || strings.Contains(string(o.Audit), "kisa_degerlendirme") {
		t.Fatalf("yeterince uzun özet için uyarı olmamalı: %s", o.Audit)
	}
}

func TestModeratorMeaningFieldIsOptionalAndParsed(t *testing.T) {
	with := strings.Replace(validModerator, `"topic":"Gecenin anlamı",`, `"topic":"Gece yazışmaları ne anlama geliyor?","meaning":"Gece yazmak yakınlık da olabilir, uykusuzluk da.",`, 1)
	m, err := ParseModerator(with)
	if err != nil || m.Disagreements[0].Meaning == "" {
		t.Fatalf("anlam alanı okunmalı: %v %+v", err, m.Disagreements)
	}
	if m2, err := ParseModerator(validModerator); err != nil || m2.Disagreements[0].Meaning != "" {
		t.Fatalf("anlam alanı isteğe bağlı olmalı: %v", err)
	}
}

func TestPromptsAskForPlainLanguageAndLongOverview(t *testing.T) {
	for name, text := range map[string]string{"moderatör": moderatorSystem, "sentez": synthesisSystem, "persona sözleşmesi": personas.Contract} {
		if !strings.Contains(strings.ToLower(text), "sade") {
			t.Errorf("%s sade dil istemeli", name)
		}
	}
	if !strings.Contains(synthesisSystem, "EN AZ 4000, EN FAZLA 8000") || !strings.Contains(moderatorSystem, "4–6 cümle") {
		t.Fatal("uzunluk hedefleri istemde olmalı")
	}
	if SynthesisMaxOut < 8000 || ModeratorMaxOut < 8000 {
		t.Fatalf("8000 karakterlik çıktı için token sınırı yetmez: %d %d", SynthesisMaxOut, ModeratorMaxOut)
	}
}

func personaWith(obs ...string) string {
	return `{"abstain":false,"abstain_reason":"","observations":[` + strings.Join(obs, ",") + `],
"hypotheses":[{"claim":"Yakınlık arayışı olabilir.","evidence":["K1"],"counter_hypothesis":"Başka açıklama.","confidence":"low","falsified_by":"Aksi görülürse."}],
"open_questions":["x"],"data_needed":["y"]}`
}

func TestInventedQuoteIsFlaggedMisplacedQuoteWarnedAndRealQuoteIsClean(t *testing.T) {
	eng, st, id, f := setup(t, "freud", "jung")
	good := `{"text":"Selamlaşma var.","evidence":["K1"],"quote":"Ben: selam"}`
	elsewhere := `{"text":"Cevap geldi.","evidence":["K1"],"quote":"Ayşe: merhaba"}`
	invented := `{"text":"Sevgi ifadesi.","evidence":["K2"],"quote":"seni çok seviyorum canım"}`
	metric := `{"text":"Gece payı yüksek.","evidence":["K1"]}`
	f.persona = func(name string) (string, error) {
		if name == "Sigmund Freud" {
			return personaWith(good, elsewhere, invented, metric), nil
		}
		return personaWith(good, good, good), nil
	}
	eng.Run(context.Background(), id)
	m := outputsByKey(t, st, id)
	fa := string(m["persona|freud"].Audit)
	if m["persona|freud"].Status != store.OutDone || !strings.Contains(fa, "alinti_bulunamadi") || !strings.Contains(fa, "seni çok seviyorum") || !strings.Contains(fa, "alinti_baska_kesitte") {
		t.Fatalf("uydurma ve yanlış yerdeki alıntı işaretlenmeli, çıktı saklanmalı: %s", fa)
	}
	if ja := string(m["persona|jung"].Audit); strings.Contains(ja, "alinti_") {
		t.Fatalf("gerçek alıntı temiz olmalı: %s", ja)
	}
}

func TestInvalidItemsArePrunedInsteadOfDiscardingTheWholePersona(t *testing.T) {
	eng, st, id, f := setup(t, "freud", "jung")
	ok := `{"text":"Selamlaşma var.","evidence":["K1"],"quote":"Ben: selam"}`
	bad := `{"text":"Kanıtsız madde.","evidence":[]}`
	f.persona = func(name string) (string, error) { return personaWith(ok, bad, ok, ok), nil }
	eng.Run(context.Background(), id)
	o := outputsByKey(t, st, id)["persona|freud"]
	if o.Status != store.OutDone || o.Attempts != 1 || !strings.Contains(string(o.Audit), "ayiklandi") || !strings.Contains(string(o.Audit), "gözlem 2") {
		t.Fatalf("geçersiz madde ayıklanmalı, çıktı korunmalı, onarım çağrısı gerekmemeli: %d %s", o.Attempts, o.Audit)
	}
	var po personas.Output
	json.Unmarshal(o.Output, &po)
	if len(po.Observations) != 3 {
		t.Fatalf("3 geçerli gözlem kalmalı: %d", len(po.Observations))
	}
	// ayıklamadan sonra yeterli madde kalmıyorsa ayıklama devreye girmez: eskisi gibi onarım çağrısına gidilir
	eng2, st2, id2, f2 := setup(t, "klein", "adler")
	f2.persona = func(name string) (string, error) { return personaWith(ok, bad, bad), nil }
	eng2.Run(context.Background(), id2)
	o2 := outputsByKey(t, st2, id2)["persona|klein"]
	if o2.Attempts != 2 || strings.Contains(string(o2.Audit), "ayiklandi") {
		t.Fatalf("yeterli madde yoksa ayıklanmamalı, onarıma gidilmeli: %d %s", o2.Attempts, o2.Audit)
	}
}

func TestQuoteNormalizationAndEllipsisParts(t *testing.T) {
	pack := "## K1 — x\n[01.02.2026 20:00] Ayşe: Yarın görüşelim mi, çok özledim seni!!\n"
	q := newQuoteChecker(pack)
	for quote, want := range map[string]string{
		"yarın görüşelim mi":             "ok",
		"YARIN GÖRÜŞELİM mi çok özledim": "ok",
		"Yarın görüşelim … özledim seni": "ok",
		"yarın görüşemeyiz":              "missing",
		"selam":                          "none", // tek sözcük doğrulanmaz
		"":                               "none",
	} {
		if got := q.check(quote, []string{"K1"}); got != want {
			t.Errorf("%q: %q, beklenen %q", quote, got, want)
		}
	}
	if got := q.check("yarın görüşelim mi", []string{"K9"}); got != "elsewhere" {
		t.Errorf("atıf yapılan kesitte değilse 'elsewhere': %q", got)
	}
}

// verifyAllSupported verilen iddiaların hepsini "supported" olarak işaretleyen bir denetçi yanıtı üretir.
func verifyAllSupported(user string) string {
	var ids []string
	for _, m := range regexp.MustCompile(`(?m)^\[([a-z]\d+)\] `).FindAllStringSubmatch(user, -1) {
		ids = append(ids, `{"id":"`+m[1]+`","support":"supported","note":"Kesitlerde görülüyor."}`)
	}
	return `{"results":[` + strings.Join(ids, ",") + `]}`
}

func TestSectionsSynthesisBuildsSummaryAndIsAuditedPerSection(t *testing.T) {
	raw := `{"summary":"","sections":[{"title":"Genel resim","text":"Gece yoğunluğu öne çıkıyor.","evidence":["K1"]},{"title":"Temkinler","text":"Veri iki kaynakla sınırlı.","evidence":["K2"]}],
"convergences":[{"text":"Gece yoğunluğu.","evidence":["K1"]}],"divergences":[{"text":"Anlam farklı okunuyor.","evidence":["K2"]}],
"alternative_explanations":["Çalışma saatleri."],"open_questions":["x"],"data_needed":["y"],"confidence":"low","limits":"z"}`
	s, err := ParseSynthesis(raw)
	if err != nil || len(s.Sections) != 2 || !strings.Contains(s.Summary, "Gece yoğunluğu öne çıkıyor.") || !strings.Contains(s.Summary, "Veri iki kaynakla sınırlı.") || strings.Contains(s.Summary, "Genel resim") {
		t.Fatalf("özet bölüm metinlerinden (başlıksız) kurulmalı: %v %+v", err, s)
	}
	cl := s.Claims()
	if len(cl) < 2 || cl[0].Evidence[0] != "K1" || cl[1].Evidence[0] != "K2" {
		t.Fatalf("her bölüm kendi kanıtıyla denetlenmeli: %+v", cl[:2])
	}
	bad := strings.Replace(raw, `"title":"Temkinler"`, `"title":""`, 1)
	if _, err := ParseSynthesis(bad); err == nil {
		t.Fatal("başlıksız bölüm reddedilmeli")
	}
	if _, err := ParseSynthesis(validSynthesis); err != nil { // eski biçim (yalnızca summary) hâlâ geçerli
		t.Fatalf("eski biçim: %v", err)
	}
}

func TestStrongModelHandlesModeratorSynthesisAndVerifyWhilePersonasUseMainModel(t *testing.T) {
	eng, st, id, mainFake := setup(t, "freud", "jung")
	strongFake := &fake{}
	eng.Strong, eng.StrongModel = strongFake, "deepseek-v4-pro"
	eng.Run(context.Background(), id)
	isStage := func(f *fake, system string) int { return f.count(func(c call) bool { return c.system == system }) }
	if isStage(mainFake, moderatorSystem)+isStage(mainFake, synthesisSystem)+isStage(mainFake, verifySystem) != 0 {
		t.Fatal("moderatör/sentez/doğrulama ana modele gitmemeli")
	}
	if isStage(strongFake, moderatorSystem) != 1 || isStage(strongFake, synthesisSystem) != 1 || isStage(strongFake, verifySystem) < 1 {
		t.Fatalf("güçlü modele 1 moderatör, 1 sentez, ≥1 doğrulama gitmeli: %d %d %d", isStage(strongFake, moderatorSystem), isStage(strongFake, synthesisSystem), isStage(strongFake, verifySystem))
	}
	if mainFake.count(func(c call) bool { return strings.Contains(c.user, "# Mercek:") }) != 2 || strongFake.count(func(c call) bool { return strings.Contains(c.user, "# Mercek:") }) != 0 {
		t.Fatal("persona çağrıları yalnızca ana modele gitmeli")
	}
	// moderatör ve sentez, atıf yapılan kesitlerin ham metnini görür
	mod := strongFake.calls[0]
	for _, c := range strongFake.calls {
		if c.system == moderatorSystem {
			mod = c
		}
	}
	if !strings.Contains(mod.user, "Atıf yapılan kanıt kesitleri") || !strings.Contains(mod.user, "Ben: selam") {
		t.Fatalf("moderatör atıf yapılan kesitlerin metnini almalı:\n%s", mod.user)
	}
	// harcama güçlü model adıyla deftere yazılır
	var strongRows int
	rows, _ := st.UsageRows(id)
	for _, r := range rows {
		if r.Model == "deepseek-v4-pro" {
			strongRows++
		}
	}
	if strongRows < 3 {
		t.Fatalf("güçlü model çağrıları kendi model adıyla kaydedilmeli: %d", strongRows)
	}
}

func TestVerifyStoresPerClaimSupportAndCanFlagUnsupported(t *testing.T) {
	eng, st, id, f := setup(t, "freud", "jung")
	f.verify = func(user string) string {
		out := verifyAllSupported(user)
		return strings.Replace(out, `"id":"d1","support":"supported"`, `"id":"d1","support":"unsupported"`, 1)
	}
	eng.Run(context.Background(), id)
	o := outputsByKey(t, st, id)["verify|"]
	if o.Status != store.OutDone {
		t.Fatalf("doğrulama kaydedilmeli: %+v", o)
	}
	var v Verification
	json.Unmarshal(o.Output, &v)
	by := map[string]string{}
	for _, c := range v.Results {
		by[c.ID] = c.Support
	}
	if by["d1"] != SupportNo || by["c1"] != SupportYes || by["s1"] != SupportYes {
		t.Fatalf("iddia başına destek: %v", by)
	}
	// doğrulama başarısız olsa bile oturum ve sentez bozulmaz
	eng2, st2, id2, f2 := setup(t, "klein", "adler")
	f2.verify = func(string) string { return "geçersiz çıktı" }
	eng2.Run(context.Background(), id2)
	sess, _ := st2.Council(id2)
	if sess.Status != store.CouncilDone || outputsByKey(t, st2, id2)["synthesis|"].Status != store.OutDone {
		t.Fatalf("doğrulama hatası oturumu bozmamalı: %s", sess.Status)
	}
}

func TestParseVerificationRequiresExactlyTheRequestedClaims(t *testing.T) {
	refs := []claimRef{{ID: "s1"}, {ID: "c1"}}
	ok := `{"results":[{"id":"s1","support":"partial","note":"x"},{"id":"c1","support":"supported","note":""}]}`
	if v, err := ParseVerification(ok, refs); err != nil || len(v.Results) != 2 {
		t.Fatalf("geçerli: %v", err)
	}
	for name, raw := range map[string]string{
		"eksik":    `{"results":[{"id":"s1","support":"supported","note":""}]}`,
		"geçersiz": `{"results":[{"id":"s1","support":"belki","note":""},{"id":"c1","support":"supported","note":""}]}`,
	} {
		if _, err := ParseVerification(raw, refs); err == nil {
			t.Errorf("%s reddedilmeli", name)
		}
	}
	// bilinmeyen ve yinelenen kimlikler yok sayılır
	extra := `{"results":[{"id":"s1","support":"supported","note":""},{"id":"s1","support":"unsupported","note":""},{"id":"zzz","support":"supported","note":""},{"id":"c1","support":"supported","note":""}]}`
	if v, err := ParseVerification(extra, refs); err != nil || len(v.Results) != 2 || v.Results[0].Support != "supported" {
		t.Fatalf("fazlalıklar ayıklanmalı: %v %+v", err, v)
	}
}

func TestDeepModeDigestsFeedEveryStageAndResumeSkipsFinishedOnes(t *testing.T) {
	eng, st, id, f := setup(t, "freud", "jung")
	eng.Depth = "derin"
	eng.Run(context.Background(), id)
	digests := f.count(func(c call) bool { return c.system == digestSystem })
	if digests < 1 {
		t.Fatalf("dönem özeti çağrısı yapılmalı: %d", digests)
	}
	// persona, moderatör ve sentez girdileri dönem özetlerini içerir; özetler kanıt olarak etiketlenmez
	for _, sys := range []string{moderatorSystem, synthesisSystem} {
		ok := false
		for _, c := range f.calls {
			if c.system == sys && strings.Contains(c.user, "## Dönem özetleri") && strings.Contains(c.user, "KANIT DEĞİLDİR") {
				ok = true
			}
		}
		if !ok {
			t.Errorf("%s girdisinde dönem özetleri olmalı", sys[:20])
		}
	}
	personaWithDigest := f.count(func(c call) bool {
		return strings.Contains(c.user, "# Mercek:") && strings.Contains(c.user, "## Dönem özetleri")
	})
	if personaWithDigest != 2 {
		t.Fatalf("iki persona da özetleri görmeli: %d", personaWithDigest)
	}
	// aynı oturum yeniden çalıştırılınca bitmiş özetler tekrar çağrılmaz
	before := f.count(func(c call) bool { return c.system == digestSystem })
	eng.Run(context.Background(), id)
	if after := f.count(func(c call) bool { return c.system == digestSystem }); after != before {
		t.Fatalf("bitmiş özetler yeniden üretilmemeli: %d → %d", before, after)
	}
	outs, _ := st.Outputs(id)
	if txt := digestText(outs); !strings.Contains(txt, "Dönem 1") || !strings.Contains(txt, "Ton: sakin") {
		t.Fatalf("özet metni: %s", txt)
	}
	// normal modda dönem özeti yok
	eng2, _, id2, f2 := setup(t, "klein", "adler")
	eng2.Run(context.Background(), id2)
	if f2.count(func(c call) bool { return c.system == digestSystem }) != 0 {
		t.Fatal("normal modda dönem özeti çağrısı olmamalı")
	}
}
