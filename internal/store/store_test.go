package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/MustafaKemal0146/joe/internal/model"
)

func sample() *model.Import {
	t0 := time.Date(2026, 3, 1, 22, 0, 0, 0, time.UTC)
	return &model.Import{
		Platform: "instagram", Label: "x.zip", OwnerName: "Ben Sahip", OwnerHandle: "benim", Stats: map[string]int{"messages": 3},
		Conversations: []model.Conversation{{
			Title: "Ayşe Yılmaz", Platform: "instagram", Participants: []string{"Ben Sahip", "Ayşe Yılmaz"},
			Messages: []model.Message{
				{Sender: "Ben Sahip", TS: t0, Kind: model.KindText, Text: "yarın görüşelim mi"},
				{Sender: "Ayşe Yılmaz", TS: t0.Add(time.Minute), Kind: model.KindText, Text: "olur tabii", Reactions: []model.Reaction{{Actor: "Ben Sahip", Emoji: "❤"}}},
				{Sender: "Ayşe Yılmaz", TS: t0.Add(2 * time.Minute), Kind: model.KindMedia, Meta: map[string]any{"media": "photo"}},
			}}},
		Events: []model.Event{{Kind: model.EvLikePost, TS: t0, Actor: model.Owner, Counterpart: "ayse_y"}},
	}
}

func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestSaveImportRoundTripAndDedupe(t *testing.T) {
	s := open(t)
	id, err := s.SaveImport(sample(), "sha1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveImport(sample(), "sha1"); err == nil {
		t.Fatal("aynı içerik ikinci kez kaydedilmemeli")
	} else {
		var d ErrDuplicate
		if !errors.As(err, &d) || d.SourceID != id {
			t.Fatalf("yinelenen hata: %v", err)
		}
	}
	c, err := s.LoadCorpus(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Messages) != 3 || len(c.Events) != 1 || len(c.Convs) != 1 {
		t.Fatalf("yüklenen: %d mesaj %d olay %d sohbet", len(c.Messages), len(c.Events), len(c.Convs))
	}
	if c.Messages[1].Reactions[0].Emoji != "❤" || c.Messages[2].Meta["media"] != "photo" {
		t.Fatalf("tepki/meta kaybolmuş: %+v", c.Messages)
	}
	if !c.Sources[id].OwnerConfirmed {
		t.Fatal("profilden gelen sahip onaylı olmalı")
	}
}

func TestSearchFindsTurkishWithoutSyntaxErrors(t *testing.T) {
	s := open(t)
	s.SaveImport(sample(), "sha1")
	hits, err := s.Search("görüşelim", 10)
	if err != nil || len(hits) != 1 {
		t.Fatalf("arama: %v %d", err, len(hits))
	}
	for _, q := range []string{`"`, `a OR`, `*`, `NEAR(`, ``} {
		if _, err := s.Search(q, 5); err != nil {
			t.Errorf("kullanıcı girdisi FTS hatası vermemeli %q: %v", q, err)
		}
	}
}

func TestDeleteSourceCascades(t *testing.T) {
	s := open(t)
	id, _ := s.SaveImport(sample(), "sha1")
	if err := s.DeleteSource(id); err != nil {
		t.Fatal(err)
	}
	c, _ := s.LoadCorpus(nil)
	if len(c.Messages)+len(c.Events)+len(c.Convs) != 0 {
		t.Fatal("bağlı veri silinmeli")
	}
	hits, _ := s.Search("görüşelim", 5)
	if len(hits) != 0 {
		t.Fatal("arama dizini de temizlenmeli")
	}
}

func TestPeopleOwnerAutoAndIdentities(t *testing.T) {
	s := open(t)
	id, _ := s.SaveImport(sample(), "sha1")
	src, _ := s.Source(id)
	p, err := s.EnsureOwnerPerson(src)
	if err != nil || !p.IsSelf || len(p.Identities) != 2 {
		t.Fatalf("sahip kişisi: %+v %v", p, err)
	}
	again, _ := s.EnsureOwnerPerson(src)
	if again.ID != p.ID {
		t.Fatal("aynı sahip için ikinci kişi açılmamalı")
	}
	a, err := s.CreatePerson("Ayşe Yılmaz", "", false, "@ayse_y")
	if err != nil || a.IsSelf {
		t.Fatalf("kişi: %+v %v", a, err)
	}
	if err := s.RemoveIdentity(a.ID, a.Identities[0].ID); err != nil {
		t.Fatal(err)
	}
	a, _ = s.Person(a.ID)
	if err := s.RemoveIdentity(a.ID, a.Identities[0].ID); err == nil {
		t.Fatal("son kimlik kaldırılamamalı")
	}
	yes := true
	s.UpdatePerson(a.ID, nil, nil, &yes)
	p, _ = s.Person(p.ID)
	if p.IsSelf {
		t.Fatal("yalnız bir kişi 'ben' olabilir")
	}
}

func TestSetOwnerValidation(t *testing.T) {
	s := open(t)
	id, _ := s.SaveImport(sample(), "sha1")
	if s.SetOwner(id, " ", "") == nil {
		t.Fatal("boş sahip reddedilmeli")
	}
	if s.SetOwner(999, "x", "") == nil {
		t.Fatal("olmayan kaynak hata vermeli")
	}
}

func TestExportJSONIsValidAndComplete(t *testing.T) {
	s := open(t)
	id, _ := s.SaveImport(sample(), "sha1")
	src, _ := s.Source(id)
	s.EnsureOwnerPerson(src)
	var buf bytes.Buffer
	if err := s.ExportJSON(&buf, "v-test"); err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Format        int              `json:"format"`
		Version       string           `json:"version"`
		Sources       []map[string]any `json:"sources"`
		People        []map[string]any `json:"people"`
		Conversations []struct {
			Messages []map[string]any `json:"messages"`
		} `json:"conversations"`
		Events []map[string]any `json:"events"`
	}
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("geçerli JSON olmalı: %v\n%s", err, buf.String())
	}
	if doc.Format != ExportFormat || doc.Version != "v-test" || len(doc.Sources) != 1 || len(doc.People) != 1 || len(doc.Events) != 1 {
		t.Fatalf("belge: %+v", doc)
	}
	if len(doc.Conversations) != 1 || len(doc.Conversations[0].Messages) != 3 {
		t.Fatalf("mesajlar eksik: %+v", doc.Conversations)
	}
}

func TestExportEmptyStoreIsValidJSON(t *testing.T) {
	s := open(t)
	var buf bytes.Buffer
	if err := s.ExportJSON(&buf, "v"); err != nil {
		t.Fatal(err)
	}
	var v any
	if err := json.Unmarshal(buf.Bytes(), &v); err != nil {
		t.Fatalf("boş depo da geçerli JSON üretmeli: %v\n%s", err, buf.String())
	}
}

func TestWipeRemovesEverythingIncludingSearchIndex(t *testing.T) {
	s := open(t)
	id, _ := s.SaveImport(sample(), "sha1")
	src, _ := s.Source(id)
	s.EnsureOwnerPerson(src)
	if err := s.Wipe(); err != nil {
		t.Fatal(err)
	}
	c, _ := s.LoadCorpus(nil)
	people, _ := s.People()
	hits, _ := s.Search("görüşelim", 5)
	if len(c.Sources)+len(c.Messages)+len(c.Events)+len(people)+len(hits) != 0 {
		t.Fatalf("hiçbir şey kalmamalı: %d kaynak %d mesaj %d olay %d kişi %d arama", len(c.Sources), len(c.Messages), len(c.Events), len(people), len(hits))
	}
	// Silinen veri yeniden aktarılabilmeli (SHA kaydı da silindi).
	if _, err := s.SaveImport(sample(), "sha1"); err != nil {
		t.Fatalf("silmeden sonra yeniden içe aktarma: %v", err)
	}
}

func TestLedgerSessionSpendAndSummary(t *testing.T) {
	s := open(t)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(s.RecordUsage(Usage{Session: "a", Kind: "persona", Model: "m", PromptTokens: 1000, CompletionTokens: 200, CostUSD: 0.01}))
	must(s.RecordUsage(Usage{Session: "a", Kind: "persona", Model: "m", PromptTokens: 1000, CompletionTokens: 200, CostUSD: 0.5, FromCache: true}))
	must(s.RecordUsage(Usage{Session: "b", Kind: "synthesis", Model: "m", PromptTokens: 10, CostUSD: 0.02}))
	if v, _ := s.SessionSpend("a"); v != 0.01 {
		t.Fatalf("önbellekten dönen çağrı 0 USD olmalı: %v", v)
	}
	if v, _ := s.SessionSpend("yok"); v != 0 {
		t.Fatalf("olmayan oturum 0: %v", v)
	}
	sum, err := s.Spend()
	if err != nil || sum.Calls != 3 || sum.CachedCalls != 1 || math.Abs(sum.TotalUSD-0.03) > 1e-9 || len(sum.Sessions) != 2 || sum.Sessions[0].Session != "b" {
		t.Fatalf("özet: %+v %v", sum, err)
	}
	if s.RecordUsage(Usage{Session: " "}) == nil {
		t.Fatal("oturumsuz kayıt reddedilmeli")
	}
}

func TestResponseCacheKeyIsUnambiguousAndStored(t *testing.T) {
	if CacheKey("ab", "c") == CacheKey("a", "bc") {
		t.Fatal("parça sınırları anahtara karışmamalı")
	}
	if CacheKey("x", "y") != CacheKey("x", "y") {
		t.Fatal("anahtar deterministik olmalı")
	}
	s := open(t)
	k := CacheKey("paket", "jung", "model", "v1")
	if _, ok, _ := s.CacheGet(k); ok {
		t.Fatal("henüz yok")
	}
	if err := s.CachePut(k, CachedResponse{Response: "{\"ok\":1}", PromptTokens: 5, CompletionTokens: 7}); err != nil {
		t.Fatal(err)
	}
	r, ok, err := s.CacheGet(k)
	if err != nil || !ok || r.Response != "{\"ok\":1}" || r.CompletionTokens != 7 {
		t.Fatalf("%+v %v %v", r, ok, err)
	}
	s.CachePut(k, CachedResponse{Response: "yeni"})
	if r, _, _ := s.CacheGet(k); r.Response != "yeni" {
		t.Fatal("aynı anahtar güncellenmeli")
	}
	if s.CachePut("k2", CachedResponse{Response: "  "}) == nil {
		t.Fatal("boş yanıt önbelleğe yazılmamalı")
	}
}

func TestWipeAlsoClearsLedgerAndCache(t *testing.T) {
	s := open(t)
	s.RecordUsage(Usage{Session: "a", CostUSD: 1})
	s.CachePut("k", CachedResponse{Response: "x"})
	if err := s.Wipe(); err != nil {
		t.Fatal(err)
	}
	if sum, _ := s.Spend(); sum.Calls != 0 {
		t.Fatal("defter silinmeli")
	}
	if _, ok, _ := s.CacheGet("k"); ok {
		t.Fatal("önbellek silinmeli")
	}
}

func TestChatSessionsMessagesAndCascade(t *testing.T) {
	s := open(t)
	p, _ := s.CreatePerson("Ayşe", "", false)
	c, err := s.CreateChat("c1", "  ", p.ID)
	if err != nil || c.Title != "Yeni sohbet" || c.PersonID != p.ID {
		t.Fatalf("%+v %v", c, err)
	}
	if _, err := s.CreateChat("c1", "x", 0); err == nil {
		t.Fatal("aynı kimlik ikinci kez açılamaz")
	}
	m1, _ := s.AppendChat(ChatMessage{SessionID: "c1", Role: "user", Content: "merhaba"})
	m2, _ := s.AppendChat(ChatMessage{SessionID: "c1", Role: "assistant", Content: "selam", CostUSD: 0.001})
	if m2.ID <= m1.ID {
		t.Fatal("kimlikler artmalı")
	}
	if _, err := s.AppendChat(ChatMessage{SessionID: "c1", Role: "hacker", Content: "x"}); err == nil {
		t.Fatal("geçersiz rol reddedilmeli")
	}
	if _, err := s.AppendChat(ChatMessage{SessionID: "yok", Role: "user", Content: "x"}); err == nil {
		t.Fatal("olmayan oturuma mesaj eklenemez")
	}
	msgs, _ := s.ChatMessages("c1")
	if len(msgs) != 2 || msgs[0].Content != "merhaba" || msgs[1].Role != "assistant" {
		t.Fatalf("%+v", msgs)
	}
	s.RecordUsage(Usage{Session: "chat:c1", CostUSD: 0.02})
	got, _ := s.Chat("c1")
	if got.SpentUSD != 0.02 {
		t.Fatalf("harcama: %v", got.SpentUSD)
	}
	if err := s.RenameChat("c1", "Ayşe hakkında"); err != nil {
		t.Fatal(err)
	}
	if s.RenameChat("c1", " ") == nil || s.RenameChat("yok", "x") == nil {
		t.Fatal("boş/olmayan yeniden adlandırma reddedilmeli")
	}
	s.CreateAction(ChatAction{ID: "a1", SessionID: "c1", Tool: "create_person", Args: json.RawMessage(`{}`), Summary: "x"})
	// kişi silinince oturum kalır, bağ kopar
	s.DeletePerson(p.ID)
	if got, _ := s.Chat("c1"); got.PersonID != 0 {
		t.Fatal("kişi silinince oturum bağı kopmalı, oturum silinmemeli")
	}
	if err := s.DeleteChat("c1"); err != nil {
		t.Fatal(err)
	}
	if left, _ := s.ChatMessages("c1"); len(left) != 0 {
		t.Fatal("mesajlar silinmeli")
	}
	if acts, _ := s.Actions("c1"); len(acts) != 0 {
		t.Fatal("eylemler silinmeli")
	}
	if v, _ := s.SessionSpend("chat:c1"); v != 0.02 {
		t.Fatal("para kaydı (defter) silinmemeli")
	}
	if s.DeleteChat("c1") == nil {
		t.Fatal("olmayan oturum silinemez")
	}
}

func TestActionClaimIsAtomicAndSessionScoped(t *testing.T) {
	s := open(t)
	s.CreateChat("c1", "a", 0)
	s.CreateChat("c2", "b", 0)
	s.CreateAction(ChatAction{ID: "a1", SessionID: "c1", Tool: "t", Args: json.RawMessage(`{}`), Summary: "s"})
	if _, err := s.Action("c2", "a1"); err == nil {
		t.Fatal("başka oturumdan eylem görülemez")
	}
	if ok, _ := s.ClaimAction("c2", "a1"); ok {
		t.Fatal("başka oturum eylemi sahiplenemez")
	}
	if ok, _ := s.ClaimAction("c1", "a1"); !ok {
		t.Fatal("ilk sahiplenme başarılı olmalı")
	}
	if ok, _ := s.ClaimAction("c1", "a1"); ok {
		t.Fatal("ikinci sahiplenme (çift onay) reddedilmeli")
	}
	s.FinishAction("a1", ActionDone, "tamam")
	a, _ := s.Action("c1", "a1")
	if a.Status != ActionDone || a.Result != "tamam" {
		t.Fatalf("%+v", a)
	}
}

func TestNotesRequireSourceAndPerson(t *testing.T) {
	s := open(t)
	p, _ := s.CreatePerson("Ayşe", "", false)
	if _, err := s.AddNote(p.ID, "gece yazıyor", ""); err == nil {
		t.Fatal("kaynaksız not reddedilmeli")
	}
	if _, err := s.AddNote(p.ID, " ", "user"); err == nil {
		t.Fatal("boş not reddedilmeli")
	}
	if _, err := s.AddNote(p.ID, strings.Repeat("a", 1001), "user"); err == nil {
		t.Fatal("çok uzun not reddedilmeli")
	}
	if _, err := s.AddNote(999, "x", "user"); err == nil {
		t.Fatal("olmayan kişiye not eklenemez")
	}
	n, err := s.AddNote(p.ID, "Gece 02'de yazıyor", "chat:c1")
	if err != nil || n.Source != "chat:c1" {
		t.Fatalf("%+v %v", n, err)
	}
	if ns, _ := s.Notes(p.ID); len(ns) != 1 {
		t.Fatal("not listelenmeli")
	}
	if err := s.DeleteNote(n.ID); err != nil {
		t.Fatal(err)
	}
	if s.DeleteNote(n.ID) == nil {
		t.Fatal("silinen not tekrar silinemez")
	}
	s.AddNote(p.ID, "yeniden", "user")
	s.DeletePerson(p.ID)
	if ns, _ := s.Notes(p.ID); len(ns) != 0 {
		t.Fatal("kişi silinince notlar da silinmeli")
	}
}

func TestExportAndWipeIncludeChatsAndNotes(t *testing.T) {
	s := open(t)
	p, _ := s.CreatePerson("Ayşe", "", false)
	s.CreateChat("c1", "t", p.ID)
	s.AppendChat(ChatMessage{SessionID: "c1", Role: "user", Content: "gizli sohbet metni"})
	s.AddNote(p.ID, "gizli not", "user")
	var buf bytes.Buffer
	if err := s.ExportJSON(&buf, "v"); err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Chats []struct {
			Messages []map[string]any `json:"messages"`
		} `json:"chats"`
		Notes []map[string]any `json:"notes"`
	}
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil || len(doc.Chats) != 1 || len(doc.Chats[0].Messages) != 1 || len(doc.Notes) != 1 {
		t.Fatalf("dışa aktarım sohbet ve notları içermeli: %v\n%s", err, buf.String())
	}
	s.Wipe()
	if cs, _ := s.Chats(10); len(cs) != 0 {
		t.Fatal("silme sohbetleri de temizlemeli")
	}
}

func TestOsintReviewAndAttachRules(t *testing.T) {
	s := open(t)
	p, _ := s.CreatePerson("Ayşe", "", false)
	s.CreateOsintRun("r1", "ayse", 3)
	s.AddOsintFindings("r1", []OsintFinding{
		{Site: "GitHub", Category: "coding", URL: "https://github.com/ayse", Outcome: "found"},
		{Site: "Engelli", URL: "https://x.example/ayse", Outcome: "failed", Detail: "HTTP 403"},
	})
	fs, _ := s.OsintFindings("r1")
	if len(fs) != 2 || fs[0].Outcome != "found" || fs[0].Review != ReviewUnreviewed || fs[0].Username != "ayse" {
		t.Fatalf("bulgular (bulunanlar önce): %+v", fs)
	}
	found, failed := fs[0], fs[1]
	// doğrulanmamış bulgu kişiye bağlanamaz
	if s.AttachFinding(found.ID, p.ID) == nil {
		t.Fatal("doğrulanmamış bulgu bağlanamaz")
	}
	if s.ReviewFinding(failed.ID, ReviewConfirmed) == nil {
		t.Fatal("denetlenemeyen site 'doğrulanamaz'")
	}
	if s.ReviewFinding(found.ID, "belki") == nil {
		t.Fatal("geçersiz karar reddedilmeli")
	}
	if err := s.ReviewFinding(found.ID, ReviewConfirmed); err != nil {
		t.Fatal(err)
	}
	if s.AttachFinding(found.ID, 999) == nil {
		t.Fatal("olmayan kişiye bağlanamaz")
	}
	if err := s.AttachFinding(found.ID, p.ID); err != nil {
		t.Fatal(err)
	}
	if pf, _ := s.PersonFindings(p.ID); len(pf) != 1 || pf[0].URL != "https://github.com/ayse" {
		t.Fatalf("kişinin bulguları: %+v", pf)
	}
	// karar geri çekilince bağ da kopar: onaysız bulgu kişide kalmaz
	s.ReviewFinding(found.ID, ReviewUnsure)
	if pf, _ := s.PersonFindings(p.ID); len(pf) != 0 {
		t.Fatal("onay geri çekilince bulgu kişi dosyasından çıkmalı")
	}
	// kişi silinince bulgu kalır, bağ kopar
	s.ReviewFinding(found.ID, ReviewConfirmed)
	s.AttachFinding(found.ID, p.ID)
	s.DeletePerson(p.ID)
	if fs, _ := s.OsintFindings("r1"); fs[0].PersonID != 0 {
		t.Fatal("kişi silinince bağ kopmalı")
	}
	s.UpdateOsintProgress("r1", 3, 1, 1)
	s.FinishOsintRun("r1", OsintDone, "")
	if r, _ := s.OsintRun("r1"); r.Status != OsintDone || r.Done != 3 || r.FinishedAt.IsZero() {
		t.Fatalf("%+v", r)
	}
	s.CreateOsintRun("r2", "can", 1)
	s.InterruptOsint()
	if r, _ := s.OsintRun("r2"); r.Status != OsintCanceled {
		t.Fatal("yarım kalan tarama iptal sayılmalı")
	}
	if err := s.DeleteOsintRun("r1"); err != nil {
		t.Fatal(err)
	}
	if fs, _ := s.OsintFindings("r1"); len(fs) != 0 {
		t.Fatal("tarama silinince bulgular da silinmeli")
	}
	if s.DeleteOsintRun("r1") == nil {
		t.Fatal("olmayan tarama")
	}
}
