package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/MustafaKemal0146/joe/internal/council"
	"github.com/MustafaKemal0146/joe/internal/llm"
	"github.com/MustafaKemal0146/joe/internal/store"
)

const (
	fakePersona   = `{"abstain":false,"abstain_reason":"","observations":[{"text":"Mesajlaşma akşam saatlerinde yoğun.","evidence":["K1"]}],"hypotheses":[{"claim":"Düzenli bir rutin olabilir.","evidence":["K1"],"counter_hypothesis":"Yalnızca boş zaman akşama denk geliyor olabilir.","confidence":"low","falsified_by":"Sabah da aynı yoğunlukta yazılıyorsa."}],"open_questions":[],"data_needed":["Hafta sonu verisi"]}`
	fakeModerator = `{"agreements":[{"point":"Akşam yoğunluğu.","personas":["freud","jung"],"evidence":["K1"]}],"disagreements":[],"gaps":["Gündüz verisi yok."]}`
	fakeSynthesis = `{"summary":"Akşam rutini öne çıkıyor.","convergences":[{"text":"Akşam yoğunluğu.","evidence":["K1"]}],"divergences":[],"alternative_explanations":["Boş zamanın akşama denk gelmesi."],"open_questions":[],"data_needed":["Gündüz verisi"],"confidence":"low","limits":"Kısa veri."}`
	fakeAnswer    = `{"answer":"Mesajlar akşama yoğunlaşıyor (K1).","evidence":["K1"],"abstain":false,"abstain_reason":"","limits":"Kısa veri."}`
)

type fakeLLM struct{ n atomic.Int32 }

func (f *fakeLLM) Complete(ctx context.Context, system, user string, max int) (string, llm.Usage, error) {
	f.n.Add(1)
	u := llm.Usage{PromptTokens: 800, CompletionTokens: 200}
	switch {
	case strings.Contains(system, "moderatörsün"):
		return fakeModerator, u, nil
	case strings.Contains(system, "nötr bir sentezcisin"):
		return fakeSynthesis, u, nil
	case strings.Contains(system, "dönem özetleyicisisin"):
		return `{"period":"Bir dönem","summary":"Akşam yazışmaları sürüyor.","tone":"sakin","evidence":["K1"]}`, u, nil
	case strings.Contains(system, "kanıt denetçisisin"):
		var ids []string
		for _, m := range regexp.MustCompile(`(?m)^\[([a-z]\d+)\] `).FindAllStringSubmatch(user, -1) {
			ids = append(ids, `{"id":"`+m[1]+`","support":"supported","note":"ok"}`)
		}
		return `{"results":[` + strings.Join(ids, ",") + `]}`, u, nil
	case strings.Contains(user, "# Soru"):
		return fakeAnswer, u, nil
	}
	return fakePersona, u, nil
}

// councilFixture iki kişili bir WhatsApp sohbeti içe aktarır; "Ben" onaylı sahip ve "ben", Ayşe ayrı kişi.
func councilFixture(t *testing.T) (*Server, http.Handler, *fakeLLM, store.Person, store.Person) {
	t.Helper()
	srv, h := newServer(t)
	return councilFixture2(t, srv, h)
}

func councilFixture2(t *testing.T, srv *Server, h http.Handler) (*Server, http.Handler, *fakeLLM, store.Person, store.Person) {
	t.Helper()
	f := &fakeLLM{}
	srv.CallerFactory = func(provider, model string) (council.Caller, error) { return f, nil }
	var sb strings.Builder
	for i := 0; i < 60; i++ {
		who := "Ben"
		if i%2 == 1 {
			who = "Ayşe"
		}
		fmt.Fprintf(&sb, "26.09.2026 20:%02d - %s: akşam mesajı %d\n", i%60, who, i)
	}
	p := filepath.Join(t.TempDir(), "WhatsApp Sohbeti - Ayşe.txt")
	os.WriteFile(p, []byte(sb.String()), 0o600)
	rec := do(h, "POST", "/api/import", map[string]string{"path": p}, nil)
	var imp importResult
	json.Unmarshal(rec.Body.Bytes(), &imp)
	do(h, "POST", fmt.Sprintf("/api/sources/%d/owner", imp.Source.ID), map[string]string{"name": "Ben"}, nil)
	rec = do(h, "POST", "/api/people", map[string]any{"name": "Ayşe"}, nil)
	var ayse store.Person
	json.Unmarshal(rec.Body.Bytes(), &ayse)
	rec = do(h, "GET", "/api/people", nil, nil)
	var people []store.Person
	json.Unmarshal(rec.Body.Bytes(), &people)
	var me store.Person
	for _, p := range people {
		if p.IsSelf {
			me = p
		}
	}
	return srv, h, f, me, ayse
}

func plan(t *testing.T, h http.Handler, body map[string]any) map[string]any {
	t.Helper()
	rec := do(h, "POST", "/api/council/plan", body, nil)
	if rec.Code != 200 {
		t.Fatalf("plan: %d %s", rec.Code, rec.Body)
	}
	var out map[string]any
	json.Unmarshal(rec.Body.Bytes(), &out)
	return out
}

func TestCouncilRequiresApprovalAndRunsToCompletion(t *testing.T) {
	srv, h, f, me, _ := councilFixture(t)
	body := map[string]any{"subject_id": me.ID, "persona_ids": []string{"freud", "jung"}}

	// onaysız başlatma reddedilir ve hiçbir çağrı yapılmaz
	if rec := do(h, "POST", "/api/council", body, nil); rec.Code != 400 {
		t.Fatalf("onaysız başlatma: %d %s", rec.Code, rec.Body)
	}
	if f.n.Load() != 0 {
		t.Fatal("onaysız LLM çağrısı yapılmamalı")
	}

	pl := plan(t, h, body)
	if pl["allowed"] != true || pl["approval_id"] == "" {
		t.Fatalf("plan onay vermeli: %v", pl)
	}
	est := pl["estimate"].(map[string]any)
	if est["calls"].(float64) != 7 { // 2 persona + moderatör + sentez + 3 doğrulama (üst sınır tahmini)
		t.Fatalf("çağrı sayısı: %v", est["calls"])
	}
	if prev, _ := pl["preview"].(string); !strings.Contains(prev, "## K1 —") {
		t.Fatalf("önizleme metni olmalı: %q", prev)
	}

	body["approval_id"] = pl["approval_id"]
	rec := do(h, "POST", "/api/council", body, nil)
	if rec.Code != 202 {
		t.Fatalf("başlatma: %d %s", rec.Code, rec.Body)
	}
	var started struct{ ID string }
	json.Unmarshal(rec.Body.Bytes(), &started)
	srv.WaitCouncil()

	rec = do(h, "GET", "/api/council/"+started.ID, nil, nil)
	var got struct {
		Session struct {
			Status string `json:"status"`
		} `json:"session"`
		Outputs []struct {
			Stage  string `json:"stage"`
			Status string `json:"status"`
		} `json:"outputs"`
		Spent float64 `json:"spent_usd"`
	}
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Session.Status != "done" || len(got.Outputs) != 5 || got.Spent <= 0 { // 2 persona + moderatör + sentez + doğrulama
		t.Fatalf("oturum: %+v", got)
	}
	if f.n.Load() != 5 {
		t.Fatalf("LLM çağrısı %d, beklenen 4", f.n.Load())
	}

	// aynı onay ikinci kez kullanılamaz
	if rec := do(h, "POST", "/api/council", body, nil); rec.Code != 400 {
		t.Fatalf("onay tek kullanımlık olmalı: %d", rec.Code)
	}
	// liste
	rec = do(h, "GET", "/api/council", nil, nil)
	if !strings.Contains(rec.Body.String(), started.ID) {
		t.Fatalf("liste: %s", rec.Body)
	}
}

func TestCouncilApprovalIsBoundToContent(t *testing.T) {
	_, h, f, me, _ := councilFixture(t)
	body := map[string]any{"subject_id": me.ID, "persona_ids": []string{"freud", "jung"}}
	pl := plan(t, h, body)
	// farklı persona listesiyle başlatma: önizlemede onaylanan içerik değişti → 409
	body["persona_ids"] = []string{"freud", "jung", "klein"}
	body["approval_id"] = pl["approval_id"]
	if rec := do(h, "POST", "/api/council", body, nil); rec.Code != 409 {
		t.Fatalf("içerik değişince 409 beklenir: %d %s", rec.Code, rec.Body)
	}
	if f.n.Load() != 0 {
		t.Fatal("reddedilen başlatma çağrı yapmamalı")
	}
	// uydurma onay
	body["approval_id"] = "uydurma"
	if rec := do(h, "POST", "/api/council", body, nil); rec.Code != 400 {
		t.Fatalf("geçersiz onay: %d", rec.Code)
	}
}

func TestCouncilConsentRequiredForOthersAndPairs(t *testing.T) {
	_, h, _, me, ayse := councilFixture(t)
	rec := do(h, "POST", "/api/council/plan", map[string]any{"subject_id": ayse.ID, "persona_ids": []string{"freud"}}, nil)
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), "onaylaman") {
		t.Fatalf("başkası için rıza gerekir: %d %s", rec.Code, rec.Body)
	}
	pl := plan(t, h, map[string]any{"subject_id": ayse.ID, "persona_ids": []string{"freud"}, "consent": true})
	if pl["allowed"] != true {
		t.Fatalf("rızayla plan: %v", pl)
	}
	// kendin için rıza gerekmez
	plan(t, h, map[string]any{"subject_id": me.ID, "persona_ids": []string{"freud"}})
	// çift analizde her zaman gerekir
	rec = do(h, "POST", "/api/council/plan", map[string]any{"kind": "pair", "subject_id": me.ID, "partner_id": ayse.ID, "persona_ids": []string{"freud"}}, nil)
	if rec.Code != 400 {
		t.Fatalf("çift analizde rıza gerekir: %d", rec.Code)
	}
	if rec := do(h, "POST", "/api/council/plan", map[string]any{"kind": "pair", "subject_id": me.ID, "partner_id": me.ID, "consent": true}, nil); rec.Code != 400 {
		t.Fatalf("aynı kişiyle çift analiz: %d", rec.Code)
	}
}

func TestCouncilPairPackIncludesComparisonAndRuns(t *testing.T) {
	srv, h, f, me, ayse := councilFixture(t)
	body := map[string]any{"kind": "pair", "subject_id": me.ID, "partner_id": ayse.ID, "persona_ids": []string{"freud", "jung"}, "consent": true}
	pl := plan(t, h, body)
	prev := pl["preview"].(string)
	if !strings.Contains(prev, "Karşılaştırma (Ben × Ayşe)") || !strings.Contains(prev, "## K1 —") {
		t.Fatalf("çift paket karşılaştırma ve kesit içermeli:\n%.600s", prev)
	}
	body["approval_id"] = pl["approval_id"]
	rec := do(h, "POST", "/api/council", body, nil)
	if rec.Code != 202 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	srv.WaitCouncil()
	if f.n.Load() != 5 { // 2 persona + moderatör + sentez + 1 doğrulama
		t.Fatalf("çağrı %d", f.n.Load())
	}
}

func TestCouncilDeniedWhenOverBudgetOrUnknownModel(t *testing.T) {
	_, h, f, me, _ := councilFixture(t)
	do(h, "PUT", "/api/settings", map[string]any{"budget_usd": 0.0001}, nil)
	pl := plan(t, h, map[string]any{"subject_id": me.ID})
	if pl["allowed"] != false || pl["approval_id"] != nil || !strings.Contains(pl["reason"].(string), "bütçe") {
		t.Fatalf("tavan altında onay verilmemeli: %v", pl)
	}
	do(h, "PUT", "/api/settings", map[string]any{"budget_usd": 5.0, "active_model": "bilinmeyen-model"}, nil)
	pl = plan(t, h, map[string]any{"subject_id": me.ID})
	if pl["allowed"] != false {
		t.Fatalf("fiyatı bilinmeyen model onaylanmamalı: %v", pl)
	}
	if f.n.Load() != 0 {
		t.Fatal("hiç çağrı yapılmamalı")
	}
}

func TestAskFlowAnswersWithEvidence(t *testing.T) {
	srv, h, f, me, _ := councilFixture(t)
	body := map[string]any{"kind": "ask", "subject_id": me.ID, "question": "Hangi saatlerde yazışıyor?"}
	if rec := do(h, "POST", "/api/council/plan", map[string]any{"kind": "ask", "subject_id": me.ID, "question": "?"}, nil); rec.Code != 400 {
		t.Fatalf("çok kısa soru: %d", rec.Code)
	}
	pl := plan(t, h, body)
	if pl["estimate"].(map[string]any)["calls"].(float64) != 1 {
		t.Fatalf("soru tek çağrı: %v", pl["estimate"])
	}
	body["approval_id"] = pl["approval_id"]
	rec := do(h, "POST", "/api/council", body, nil)
	var started struct{ ID string }
	json.Unmarshal(rec.Body.Bytes(), &started)
	srv.WaitCouncil()
	rec = do(h, "GET", "/api/council/"+started.ID, nil, nil)
	if !strings.Contains(rec.Body.String(), `"stage":"answer"`) || !strings.Contains(rec.Body.String(), `"status":"done"`) || f.n.Load() != 1 {
		t.Fatalf("cevap: %s", rec.Body)
	}
	// persona'ya sorma: birden çok persona reddedilir
	rec = do(h, "POST", "/api/council/plan", map[string]any{"kind": "ask", "subject_id": me.ID, "question": "Nasıl okunur?", "persona_ids": []string{"freud", "jung"}}, nil)
	if rec.Code != 400 {
		t.Fatalf("soru tek persona'ya sorulur: %d", rec.Code)
	}
	plan(t, h, map[string]any{"kind": "ask", "subject_id": me.ID, "question": "Nasıl okunur?", "persona_ids": []string{"jung"}})
}

func TestCouncilDeleteCancelAndNotFound(t *testing.T) {
	srv, h, _, me, _ := councilFixture(t)
	body := map[string]any{"subject_id": me.ID, "persona_ids": []string{"freud"}}
	pl := plan(t, h, body)
	body["approval_id"] = pl["approval_id"]
	rec := do(h, "POST", "/api/council", body, nil)
	var started struct{ ID string }
	json.Unmarshal(rec.Body.Bytes(), &started)
	srv.WaitCouncil()
	if rec := do(h, "POST", "/api/council/"+started.ID+"/cancel", nil, nil); rec.Code != 409 {
		t.Fatalf("çalışmayan oturum iptal edilemez: %d", rec.Code)
	}
	if rec := do(h, "POST", "/api/council/"+started.ID+"/resume", nil, nil); rec.Code != 409 {
		t.Fatalf("tamamlanan oturum devam ettirilemez: %d", rec.Code)
	}
	if rec := do(h, "DELETE", "/api/council/"+started.ID, nil, nil); rec.Code != 204 {
		t.Fatalf("silme: %d", rec.Code)
	}
	if rec := do(h, "GET", "/api/council/"+started.ID, nil, nil); rec.Code != 404 {
		t.Fatalf("silinen oturum: %d", rec.Code)
	}
	if rec := do(h, "DELETE", "/api/council/yok", nil, nil); rec.Code != 404 {
		t.Fatalf("olmayan oturum: %d", rec.Code)
	}
}

func TestCouncilReportMarkdownHasSynthesisPersonasAndEvidenceList(t *testing.T) {
	srv, h, _, me, _ := councilFixture(t)
	body := map[string]any{"subject_id": me.ID, "persona_ids": []string{"freud", "jung"}}
	pl := plan(t, h, body)
	body["approval_id"] = pl["approval_id"]
	rec := do(h, "POST", "/api/council", body, nil)
	var started struct{ ID string }
	json.Unmarshal(rec.Body.Bytes(), &started)
	srv.WaitCouncil()

	rec = do(h, "GET", "/api/council/"+started.ID+"/report.md", nil, nil)
	md := rec.Body.String()
	if rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/markdown") || !strings.Contains(rec.Header().Get("Content-Disposition"), "joe-konsey-") {
		t.Fatalf("başlıklar: %d %v", rec.Code, rec.Header())
	}
	for _, want := range []string{"# Konsey raporu:", "## Genel değerlendirme", "## Moderatör: uzlaşı ve ayrışmalar", "## Persona görüşleri", "## Kanıt kesitleri", "**K1**", "klinik tanı"} {
		if !strings.Contains(md, want) {
			t.Fatalf("rapor %q içermeli:\n%s", want, md)
		}
	}
	if rec := do(h, "GET", "/api/council/yok/report.md", nil, nil); rec.Code != 404 {
		t.Fatalf("olmayan oturum: %d", rec.Code)
	}
}

func TestAskFollowUpCarriesPreviousExchange(t *testing.T) {
	srv, h, _, me, ayse := councilFixture(t)
	ask := func(extra map[string]any) (string, map[string]any, int) {
		body := map[string]any{"kind": "ask", "subject_id": me.ID, "question": "Hangi saatlerde yazışıyor?"}
		for k, v := range extra {
			body[k] = v
		}
		rec := do(h, "POST", "/api/council/plan", body, nil)
		var pl map[string]any
		json.Unmarshal(rec.Body.Bytes(), &pl)
		if rec.Code != 200 {
			return "", pl, rec.Code
		}
		body["approval_id"] = pl["approval_id"]
		rec = do(h, "POST", "/api/council", body, nil)
		var st struct{ ID string }
		json.Unmarshal(rec.Body.Bytes(), &st)
		srv.WaitCouncil()
		return st.ID, pl, rec.Code
	}
	first, _, code := ask(nil)
	if code != 202 {
		t.Fatalf("ilk soru: %d", code)
	}
	id2, _, code := ask(map[string]any{"question": "Peki ya gece?", "follow_up_of": first})
	if code != 202 {
		t.Fatalf("takip sorusu: %d", code)
	}
	sess, _ := srv.Store.Council(id2)
	if !strings.Contains(sess.Question, "Önceki soru: Hangi saatlerde") || !strings.HasSuffix(sess.Question, "Yeni soru: Peki ya gece?") {
		t.Fatalf("takip bağlamı: %q", sess.Question)
	}
	// zincir: bağlam iç içe büyümez
	id3, _, _ := ask(map[string]any{"question": "Hafta sonu?", "follow_up_of": id2})
	s3, _ := srv.Store.Council(id3)
	if strings.Count(s3.Question, "Önceki soru:") != 1 || !strings.Contains(s3.Question, "Önceki soru: Peki ya gece?") {
		t.Fatalf("zincir: %q", s3.Question)
	}
	// başka kişiye ait ya da olmayan oturum reddedilir
	if _, _, code := ask(map[string]any{"follow_up_of": "yok"}); code != 400 {
		t.Fatalf("olmayan oturum: %d", code)
	}
	if _, _, code := ask(map[string]any{"subject_id": ayse.ID, "consent": true, "follow_up_of": first}); code != 400 {
		t.Fatalf("başka kişinin oturumu: %d", code)
	}
}

func TestAskPackContainsQuestionRelevantSnippets(t *testing.T) {
	_, h, _, me, _ := councilFixture(t)
	pl := plan(t, h, map[string]any{"kind": "ask", "subject_id": me.ID, "question": "akşam mesajları ne zaman yoğun?"})
	prev, _ := pl["preview"].(string)
	if !strings.Contains(prev, "soruyla ilgili") {
		t.Fatalf("soruyla ilgili kesit olmalı:\n%.600s", prev)
	}
	// ilgisiz soru: ilgili kesit eklenmez, paket yine kurulur
	pl = plan(t, h, map[string]any{"kind": "ask", "subject_id": me.ID, "question": "zürafa hakkında ne düşünüyor?"})
	if prev, _ := pl["preview"].(string); strings.Contains(prev, "soruyla ilgili") || pl["allowed"] != true {
		t.Fatalf("ilgisiz soruda ilgili kesit olmamalı: %v", pl["allowed"])
	}
}

func TestModeratorAskCarriesPersonaAnswersAndObjectionRerunsOnlyOnePersona(t *testing.T) {
	srv, h, f, me, _ := councilFixture(t)
	run := func(body map[string]any) string {
		body["kind"], body["subject_id"] = "ask", me.ID
		pl := plan(t, h, body)
		body["approval_id"] = pl["approval_id"]
		rec := do(h, "POST", "/api/council", body, nil)
		if rec.Code != 202 {
			t.Fatalf("başlat: %d %s", rec.Code, rec.Body)
		}
		var st struct{ ID string }
		json.Unmarshal(rec.Body.Bytes(), &st)
		srv.WaitCouncil()
		return st.ID
	}
	a := run(map[string]any{"question": "Bu ilişki nasıl okunur?", "persona_ids": []string{"jung"}})
	b := run(map[string]any{"question": "Bu ilişki nasıl okunur?", "persona_ids": []string{"klein"}})
	before := f.n.Load()
	m := run(map[string]any{"question": ModeratorQuestion, "context_of": []string{a, b}})
	sess, _ := srv.Store.Council(m)
	if !strings.Contains(sess.Question, "@jung:") || !strings.Contains(sess.Question, "@klein:") || !strings.HasSuffix(sess.Question, ModeratorQuestion) {
		t.Fatalf("moderatör bağlamı: %q", sess.Question)
	}
	if f.n.Load()-before != 1 {
		t.Fatalf("moderatör tek çağrı olmalı: %d", f.n.Load()-before)
	}
	// itiraz: yalnızca ilgili persona tek çağrıyla yeniden çalışır
	before = f.n.Load()
	run(map[string]any{"question": "Buna katılmıyorum, gece yazışmaları ne olacak?", "persona_ids": []string{"jung"}, "follow_up_of": a})
	if f.n.Load()-before != 1 {
		t.Fatalf("itiraz tek persona çağrısı olmalı: %d", f.n.Load()-before)
	}
	// başka kişinin / olmayan oturum bağlamı reddedilir; 5'ten fazlası reddedilir
	rec := do(h, "POST", "/api/council/plan", map[string]any{"kind": "ask", "subject_id": me.ID, "question": ModeratorQuestion, "context_of": []string{"yok"}}, nil)
	if rec.Code != 400 {
		t.Fatalf("olmayan bağlam: %d", rec.Code)
	}
	rec = do(h, "POST", "/api/council/plan", map[string]any{"kind": "ask", "subject_id": me.ID, "question": ModeratorQuestion, "context_of": []string{a, a, a, a, a, a}}, nil)
	if rec.Code != 400 {
		t.Fatalf("çok bağlam: %d", rec.Code)
	}
}

func TestPersonReportIncludesCouncilSectionsUnlessOptedOut(t *testing.T) {
	srv, h, _, me, _ := councilFixture(t)
	// konsey yokken rapor metrik raporudur
	rec := do(h, "GET", fmt.Sprintf("/api/people/%d/report.md", me.ID), nil, nil)
	if strings.Contains(rec.Body.String(), "Konsey yorumları") || !strings.Contains(rec.Body.String(), "Konsey (yapay zekâ) yorumu içermez") {
		t.Fatalf("konseysiz rapor:\n%.300s", rec.Body)
	}
	body := map[string]any{"subject_id": me.ID, "persona_ids": []string{"freud", "jung"}}
	pl := plan(t, h, body)
	body["approval_id"] = pl["approval_id"]
	do(h, "POST", "/api/council", body, nil)
	srv.WaitCouncil()
	// bir soru oturumu rapora girmez
	ask := map[string]any{"kind": "ask", "subject_id": me.ID, "question": "Hangi saatlerde yazışıyor?"}
	apl := plan(t, h, ask)
	ask["approval_id"] = apl["approval_id"]
	do(h, "POST", "/api/council", ask, nil)
	srv.WaitCouncil()

	url := fmt.Sprintf("/api/people/%d/report.md", me.ID)
	md := do(h, "GET", url, nil, nil).Body.String()
	for _, want := range []string{"## Konsey yorumları (yapay zekâ)", "klinik tanı", "### Oturum ", "#### Genel değerlendirme", "#### Persona görüşleri", "##### Sigmund Freud", "#### Kanıt kesitleri", "**K1**", "yapay zekâ çıktısıdır"} {
		if !strings.Contains(md, want) {
			t.Fatalf("rapor %q içermeli:\n%s", want, md)
		}
	}
	if strings.Count(md, "### Oturum ") != 1 {
		t.Fatalf("yalnızca konsey oturumu girmeli (soru oturumu değil): %d", strings.Count(md, "### Oturum "))
	}
	// metrik bölümleri korunur ve konsey bölümünden ÖNCE gelir
	if strings.Index(md, "## Zaman çizelgesi") > strings.Index(md, "## Konsey yorumları") || !strings.Contains(md, "## Bire-bir sohbetler") {
		t.Fatal("metrik bölümleri konsey bölümünden önce ve eksiksiz olmalı")
	}
	// konseysiz indirme
	plain := do(h, "GET", url+"?konsey=0", nil, nil).Body.String()
	if strings.Contains(plain, "Konsey yorumları") || strings.Contains(plain, "Persona görüşleri") {
		t.Fatal("?konsey=0 konsey içermemeli")
	}
	// HTML (PDF) sürümü aynı içeriği başlık düzeyleriyle taşır
	html := do(h, "GET", fmt.Sprintf("/api/people/%d/report.html", me.ID), nil, nil).Body.String()
	for _, want := range []string{"<h2>Konsey yorumları", "<h3>Oturum ", "<h4>Genel değerlendirme</h4>", "<h5>Sigmund Freud</h5>", "<li class=\"sub\">"} {
		if !strings.Contains(html, want) {
			t.Fatalf("HTML %q içermeli", want)
		}
	}
	if strings.Contains(do(h, "GET", fmt.Sprintf("/api/people/%d/report.html?konsey=0", me.ID), nil, nil).Body.String(), "Konsey yorumları") {
		t.Fatal("HTML ?konsey=0 konsey içermemeli")
	}
}

func TestPersonReportCouncilHeadingsNestUnderSession(t *testing.T) {
	srv, h, _, me, _ := councilFixture(t)
	body := map[string]any{"subject_id": me.ID, "persona_ids": []string{"freud", "jung"}}
	pl := plan(t, h, body)
	body["approval_id"] = pl["approval_id"]
	do(h, "POST", "/api/council", body, nil)
	srv.WaitCouncil()
	md := do(h, "GET", fmt.Sprintf("/api/people/%d/report.md", me.ID), nil, nil).Body.String()
	from := strings.Index(md, "### Oturum ")
	if from < 0 {
		t.Fatal("oturum bölümü yok")
	}
	for _, line := range strings.Split(md[from:], "\n") {
		if strings.HasPrefix(line, "### ") && !strings.HasPrefix(line, "### Oturum ") {
			t.Errorf("oturum içindeki başlık #### veya daha derin olmalı: %q", line)
		}
		if strings.HasPrefix(line, "## ") {
			t.Errorf("oturum içinde ana bölüm başlığı olmamalı: %q", line)
		}
	}
	// bağımsız konsey raporunda alt başlıklar ### kalır
	id := ""
	list, _ := srv.Store.Councils(5)
	for _, c := range list {
		if c.Kind == "single" {
			id = c.ID
		}
	}
	solo := do(h, "GET", "/api/council/"+id+"/report.md", nil, nil).Body.String()
	if !strings.Contains(solo, "\n## Genel değerlendirme") || !strings.Contains(solo, "\n### Alternatif açıklamalar") {
		t.Fatalf("bağımsız rapor başlık düzeyi bozuldu:\n%s", solo)
	}
}

func TestCouncilPacksCarryRelationshipGuideAndLabels(t *testing.T) {
	_, h, _, me, ayse := councilFixture(t)
	// etiket yokken kendi analizinde rehber özeti var, ama "etiketsiz"
	pl := plan(t, h, map[string]any{"subject_id": me.ID, "persona_ids": []string{"freud"}})
	prev, _ := pl["preview"].(string)
	if !strings.Contains(prev, "İlişki rehberi") || !strings.Contains(prev, "etiketsiz: 1 sohbet") {
		t.Fatalf("rehber özeti olmalı:\n%.900s", prev)
	}
	do(h, "PUT", "/api/contact-labels", map[string]any{"key": "ayşe", "label": "yakin_arkadas"}, nil)
	pl = plan(t, h, map[string]any{"subject_id": me.ID, "persona_ids": []string{"freud"}})
	if prev, _ = pl["preview"].(string); !strings.Contains(prev, "yakın arkadaş: 1 sohbet") || !strings.Contains(prev, "hüküm değildir") {
		t.Fatalf("etiket rehbere yansımalı:\n%.900s", prev)
	}
	// iki kişilik analizde karşı tarafın etiketi ve ifade ölçümleri
	pl = plan(t, h, map[string]any{"kind": "pair", "subject_id": me.ID, "partner_id": ayse.ID, "persona_ids": []string{"freud"}, "consent": true})
	if prev, _ = pl["preview"].(string); !strings.Contains(prev, "İlişki bağlamı (Ayşe ile)") || !strings.Contains(prev, "\"yakın arkadaş\"") {
		t.Fatalf("ilişki bağlamı olmalı:\n%.900s", prev)
	}
	// soru-cevapta rehber eklenmez (maliyet ve odak)
	pl = plan(t, h, map[string]any{"kind": "ask", "subject_id": me.ID, "question": "Hangi saatlerde yazışıyor?"})
	if prev, _ = pl["preview"].(string); strings.Contains(prev, "İlişki rehberi") {
		t.Fatal("soru-cevap paketine rehber eklenmemeli")
	}
}

func TestPairCanBeScopedToOneConversationWhenNamesAreShared(t *testing.T) {
	srv, h, _, me, _ := councilFixture(t)
	// aynı karşı tarafla ikinci bir sohbet (farklı içerik → ayrı kaynak): "Ayşe" adı iki sohbette ortak
	var sb strings.Builder
	for i := 0; i < 20; i++ {
		fmt.Fprintf(&sb, "10.10.2026 10:%02d - Ben: ikinci sohbet %d\n10.10.2026 11:%02d - Ayşe: cevap %d\n", i, i, i, i)
	}
	p2 := filepath.Join(t.TempDir(), "WhatsApp Sohbeti - Ayşe 2.txt")
	os.WriteFile(p2, []byte(sb.String()), 0o600)
	do(h, "POST", "/api/import", map[string]string{"path": p2}, nil)

	var ayse struct{ ID int64 }
	var people []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	json.Unmarshal(do(h, "GET", "/api/people", nil, nil).Body.Bytes(), &people)
	for _, p := range people {
		if p.Name == "Ayşe" {
			ayse.ID = p.ID
		}
	}
	c, err := srv.loadCorpus()
	if err != nil || len(c.Convs) < 2 {
		t.Fatalf("iki sohbet bekleniyordu: %v %d", err, len(c.Convs))
	}
	smallest := c.Convs[0]
	for _, cv := range c.Convs {
		if cv.Count < smallest.Count {
			smallest = cv
		}
	}
	msgs := func(preview string) string {
		i := strings.Index(preview, "Sohbet (Ayşe")
		if i < 0 {
			return "yok"
		}
		return preview[i : i+60]
	}
	merged := plan(t, h, map[string]any{"kind": "pair", "subject_id": me.ID, "partner_id": ayse.ID, "persona_ids": []string{"freud"}, "consent": true})
	scoped := plan(t, h, map[string]any{"kind": "pair", "subject_id": me.ID, "partner_id": ayse.ID, "persona_ids": []string{"freud"}, "consent": true, "partner_conv_id": smallest.ID})
	mp, _ := merged["preview"].(string)
	sp, _ := scoped["preview"].(string)
	if mp == sp {
		t.Fatal("kapsamlı paket birleşik paketten farklı olmalı")
	}
	if !strings.Contains(sp, fmt.Sprintf("mesaj %d/%d", smallest.Count/2, smallest.Count/2)) {
		t.Fatalf("kapsamlı pakette yalnızca seçilen sohbetin sayıları olmalı (%d mesaj): %s", smallest.Count, msgs(sp))
	}
	// başka kişinin ya da olmayan sohbet kimliği reddedilir
	rec := do(h, "POST", "/api/council/plan", map[string]any{"kind": "pair", "subject_id": me.ID, "partner_id": ayse.ID, "persona_ids": []string{"freud"}, "consent": true, "partner_conv_id": 999999}, nil)
	if rec.Code != 400 {
		t.Fatalf("olmayan sohbet: %d", rec.Code)
	}
}

func TestGoalDepthAndStrongModelFlowIntoPlanSessionAndLedger(t *testing.T) {
	srv, h, f, me, _ := councilFixture(t)
	body := map[string]any{"subject_id": me.ID, "persona_ids": []string{"freud", "jung"}, "goal": "Bu ilişkide kim daha çok emek veriyor? Beni 0532 111 22 33 numarasından ara.", "depth": "derin"}
	pl := plan(t, h, body)
	prev, _ := pl["preview"].(string)
	if !strings.Contains(prev, "## Kullanıcının amacı") || !strings.Contains(prev, "kim daha çok emek veriyor") || strings.Contains(prev, "111 22 33") {
		t.Fatalf("amaç pakette ve maskeli olmalı:\n%.700s", prev)
	}
	if pl["depth"] != "derin" || pl["strong_model"] != "deepseek-v4-pro" {
		t.Fatalf("derinlik ve güçlü model planda görünmeli: %v %v", pl["depth"], pl["strong_model"])
	}
	cov := pl["coverage"].(map[string]any)
	if cov["items"].(float64) < 3 || cov["messages"].(float64) <= 0 || cov["total_messages"].(float64) != 30 {
		t.Fatalf("kapsam özeti: %v", cov)
	}
	normal := plan(t, h, map[string]any{"subject_id": me.ID, "persona_ids": []string{"freud", "jung"}})
	if pl["estimate"].(map[string]any)["calls"].(float64) <= normal["estimate"].(map[string]any)["calls"].(float64) {
		t.Fatal("derin mod daha çok çağrı (dönem özetleri) tahmin etmeli")
	}
	body["approval_id"] = pl["approval_id"]
	rec := do(h, "POST", "/api/council", body, nil)
	if rec.Code != 202 {
		t.Fatalf("başlat: %d", rec.Code)
	}
	var st struct{ ID string }
	json.Unmarshal(rec.Body.Bytes(), &st)
	srv.WaitCouncil()
	outs, _ := srv.Store.Outputs(st.ID)
	digests := 0
	for _, o := range outs {
		if o.Stage == "digest" && o.Status == "done" {
			digests++
		}
	}
	if digests < 3 {
		t.Fatalf("derin modda en az 3 dönem özeti üretilmeli: %d", digests)
	}
	rows, _ := srv.Store.UsageRows(st.ID)
	strong := 0
	for _, r := range rows {
		if r.Model == "deepseek-v4-pro" {
			strong++
		}
	}
	if strong < 3 { // moderatör + sentez + doğrulama
		t.Fatalf("güçlü model çağrıları kendi adıyla kaydedilmeli: %d", strong)
	}
	_ = f
	// geçersiz istekler
	for name, b := range map[string]map[string]any{
		"uzun amaç":      {"subject_id": me.ID, "persona_ids": []string{"freud"}, "goal": strings.Repeat("a", 601)},
		"geçersiz derin": {"subject_id": me.ID, "persona_ids": []string{"freud"}, "depth": "çok"},
	} {
		if rec := do(h, "POST", "/api/council/plan", b, nil); rec.Code != 400 {
			t.Errorf("%s reddedilmeli: %d", name, rec.Code)
		}
	}
}
