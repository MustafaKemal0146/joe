package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/MustafaKemal0146/joe/internal/chat"
	"github.com/MustafaKemal0146/joe/internal/llm"
	"github.com/MustafaKemal0146/joe/internal/store"
)

type scriptChat struct {
	n     atomic.Int32
	steps []llm.Message
}

func (s *scriptChat) Chat(ctx context.Context, req llm.Request) (llm.Message, llm.Usage, error) {
	i := int(s.n.Add(1)) - 1
	if i >= len(s.steps) {
		i = len(s.steps) - 1
	}
	return s.steps[i], llm.Usage{PromptTokens: 900, CompletionTokens: 80}, nil
}

func toolStep(name, args string) llm.Message {
	return llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "t1", Name: name, Arguments: args}}}
}

func say(t string) llm.Message { return llm.Message{Role: llm.RoleAssistant, Content: t} }

func newChat(t *testing.T, h interface {
	ServeHTTP(http.ResponseWriter, *http.Request)
}) string {
	t.Helper()
	rec := do(h, "POST", "/api/chats", map[string]any{"title": "deneme"}, nil)
	if rec.Code != 201 {
		t.Fatalf("sohbet açma: %d %s", rec.Code, rec.Body)
	}
	var c store.ChatSession
	json.Unmarshal(rec.Body.Bytes(), &c)
	return c.ID
}

func send(t *testing.T, h interface {
	ServeHTTP(http.ResponseWriter, *http.Request)
}, id, text string) map[string]any {
	t.Helper()
	rec := do(h, "POST", "/api/chats/"+id+"/messages", map[string]any{"text": text}, nil)
	if rec.Code != 200 {
		t.Fatalf("mesaj: %d %s", rec.Code, rec.Body)
	}
	var out map[string]any
	json.Unmarshal(rec.Body.Bytes(), &out)
	return out
}

func TestChatRoutedCommandWorksWithoutAnyProvider(t *testing.T) {
	_, h, _, _, _ := councilFixture(t) // sağlayıcı anahtarı yok
	id := newChat(t, h)
	out := send(t, h, id, "kişiler")
	msgs := out["messages"].([]any)
	if len(msgs) != 2 || !strings.Contains(msgs[1].(map[string]any)["content"].(string), "kişi dosyası") {
		t.Fatalf("yanıt: %v", out)
	}
	// model gerektiren istek açıklayıcı hata verir ama 200 döner ve geçmişe işlenir
	out = send(t, h, id, "Ayşe ile ilişkisini ayrıntılı biçimde yorumlayıp anlatır mısın lütfen")
	if out["error"] == nil || !strings.Contains(out["error"].(string), "Ayarlar") {
		t.Fatalf("anahtar yok hatası: %v", out)
	}
}

func TestChatRiskMessageGetsResponsibleReplyOverHTTP(t *testing.T) {
	srv, h, _, _, _ := councilFixture(t)
	called := &scriptChat{steps: []llm.Message{say("x")}}
	srv.ChatModelFactory = func(string, string) (chat.Model, error) { return called, nil }
	id := newChat(t, h)
	out := send(t, h, id, "kendime zarar vermek istiyorum")
	if called.n.Load() != 0 {
		t.Fatal("risk içeren mesaj modele gitmemeli")
	}
	if !strings.Contains(fmt.Sprint(out["messages"]), "112") {
		t.Fatalf("112 yönlendirmesi: %v", out["messages"])
	}
}

func TestChatWriteToolRequiresConfirmationOverHTTP(t *testing.T) {
	srv, h, _, _, _ := councilFixture(t)
	model := &scriptChat{steps: []llm.Message{toolStep("create_person", `{"name":"Deniz Yıldız","identities":["deniz.y"]}`), say("Onayına sundum.")}}
	srv.ChatModelFactory = func(string, string) (chat.Model, error) { return model, nil }
	id := newChat(t, h)
	out := send(t, h, id, "Deniz Yıldız için kişi dosyası aç, kullanıcı adı deniz.y")

	people := func() int {
		var ps []store.Person
		json.Unmarshal(do(h, "GET", "/api/people", nil, nil).Body.Bytes(), &ps)
		return len(ps)
	}
	before := people()
	acts := out["actions"].([]any)
	if len(acts) != 1 || acts[0].(map[string]any)["status"] != "pending" {
		t.Fatalf("bekleyen eylem: %v", out["actions"])
	}
	if people() != before {
		t.Fatal("onaydan önce kişi açılmamalı")
	}
	aid := acts[0].(map[string]any)["id"].(string)

	if rec := do(h, "POST", "/api/chats/"+id+"/actions/yok/confirm", nil, nil); rec.Code != 404 {
		t.Fatalf("olmayan eylem: %d", rec.Code)
	}
	if rec := do(h, "POST", "/api/chats/"+id+"/actions/"+aid+"/confirm", nil, nil); rec.Code != 200 {
		t.Fatalf("onay: %d %s", rec.Code, rec.Body)
	}
	if people() != before+1 {
		t.Fatal("onaydan sonra kişi açılmış olmalı")
	}
	if rec := do(h, "POST", "/api/chats/"+id+"/actions/"+aid+"/confirm", nil, nil); rec.Code != 409 || people() != before+1 {
		t.Fatalf("ikinci onay 409 vermeli ve tekrar kişi açmamalı: %d", rec.Code)
	}
	// ret yolu
	model.steps = []llm.Message{toolStep("delete_source", `{"source_id":1}`), say("Onayına sundum.")}
	model.n.Store(0)
	out = send(t, h, id, "kaynak 1'i sil")
	acts = out["actions"].([]any)
	var del map[string]any
	for _, a := range acts {
		if a.(map[string]any)["tool"] == "delete_source" {
			del = a.(map[string]any)
		}
	}
	if del == nil || del["danger"] != true {
		t.Fatalf("silme tehlikeli işaretli bekleyen eylem olmalı: %v", acts)
	}
	if rec := do(h, "POST", "/api/chats/"+id+"/actions/"+del["id"].(string)+"/reject", nil, nil); rec.Code != 200 {
		t.Fatalf("ret: %d", rec.Code)
	}
	var srcs []store.Source
	json.Unmarshal(do(h, "GET", "/api/sources", nil, nil).Body.Bytes(), &srcs)
	if len(srcs) != 1 {
		t.Fatal("reddedilen silme kaynağı silmemeli")
	}
}

func TestChatToolResultsNeverContainRawMessageText(t *testing.T) {
	srv, h, _, me, _ := councilFixture(t)
	model := &scriptChat{steps: []llm.Message{toolStep("get_profile_summary", fmt.Sprintf(`{"person_id":%d}`, me.ID)), toolStep("search_count", `{"query":"akşam"}`), say("Özet hazır.")}}
	srv.ChatModelFactory = func(string, string) (chat.Model, error) { return model, nil }
	id := newChat(t, h)
	send(t, h, id, "Beni özetle ve akşam kelimesini say")
	var d struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	json.Unmarshal(do(h, "GET", "/api/chats/"+id, nil, nil).Body.Bytes(), &d)
	tools := 0
	for _, m := range d.Messages {
		if m.Role == "tool" {
			tools++
			if strings.Contains(m.Content, "akşam mesajı") {
				t.Fatalf("araç sonucu ham mesaj metni içeriyor: %s", m.Content)
			}
		}
	}
	if tools != 2 {
		t.Fatalf("iki araç sonucu beklenir: %d", tools)
	}
	if !strings.Contains(fmt.Sprint(d.Messages), `"count": 30`) && !strings.Contains(fmt.Sprint(d.Messages), "count") {
		t.Fatal("arama sayısı dönmeli")
	}
}

func TestChatSessionsRenameDeleteAndSettings(t *testing.T) {
	_, h, _, _, _ := councilFixture(t)
	id := newChat(t, h)
	send(t, h, id, "yardım")
	if rec := do(h, "PATCH", "/api/chats/"+id, map[string]any{"title": "Yeni ad"}, nil); rec.Code != 200 || !strings.Contains(rec.Body.String(), "Yeni ad") {
		t.Fatalf("yeniden adlandırma: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "PATCH", "/api/chats/"+id, map[string]any{"title": " "}, nil); rec.Code != 400 {
		t.Fatalf("boş başlık: %d", rec.Code)
	}
	rec := do(h, "GET", "/api/chats", nil, nil)
	if !strings.Contains(rec.Body.String(), id) {
		t.Fatalf("liste: %s", rec.Body)
	}
	if rec := do(h, "POST", "/api/chats", map[string]any{"person_id": 9999}, nil); rec.Code != 404 {
		t.Fatalf("olmayan kişiye bağlı sohbet: %d", rec.Code)
	}
	if rec := do(h, "POST", "/api/chats/yok/messages", map[string]any{"text": "merhaba nasılsın"}, nil); rec.Code != 404 {
		t.Fatalf("olmayan sohbet: %d", rec.Code)
	}
	if rec := do(h, "POST", "/api/chats/"+id+"/messages", map[string]any{"text": " "}, nil); rec.Code != 400 {
		t.Fatalf("boş mesaj: %d", rec.Code)
	}
	if rec := do(h, "DELETE", "/api/chats/"+id, nil, nil); rec.Code != 204 {
		t.Fatalf("silme: %d", rec.Code)
	}
	if rec := do(h, "GET", "/api/chats/"+id, nil, nil); rec.Code != 404 {
		t.Fatalf("silinen sohbet: %d", rec.Code)
	}
	// ayarlar
	if rec := do(h, "PUT", "/api/settings", map[string]any{"chat_budget_usd": -2}, nil); rec.Code != 400 {
		t.Fatalf("negatif sohbet bütçesi: %d", rec.Code)
	}
	rec = do(h, "PUT", "/api/settings", map[string]any{"chat_budget_usd": 0.5, "chat_model": "deepseek-flash"}, nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"chat_budget_usd":0.5`) {
		t.Fatalf("ayar: %d %s", rec.Code, rec.Body)
	}
}

func TestPersonNotesEndpointsRequireSourceAndValidate(t *testing.T) {
	_, h, _, me, _ := councilFixture(t)
	url := fmt.Sprintf("/api/people/%d/notes", me.ID)
	if rec := do(h, "POST", url, map[string]any{"text": " "}, nil); rec.Code != 400 {
		t.Fatalf("boş not: %d", rec.Code)
	}
	rec := do(h, "POST", url, map[string]any{"text": "Gece yazmayı seviyor"}, nil)
	var n store.Note
	json.Unmarshal(rec.Body.Bytes(), &n)
	if rec.Code != 201 || n.Source != "user" {
		t.Fatalf("not: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "POST", url, map[string]any{"text": "x", "source": "sahte"}, nil); rec.Code != 400 {
		t.Fatalf("kaynak alanı istemciden kabul edilmemeli (bilinmeyen alan): %d", rec.Code)
	}
	if !strings.Contains(do(h, "GET", url, nil, nil).Body.String(), "Gece yazmayı seviyor") {
		t.Fatal("liste")
	}
	if rec := do(h, "DELETE", fmt.Sprintf("/api/notes/%d", n.ID), nil, nil); rec.Code != 204 {
		t.Fatalf("silme: %d", rec.Code)
	}
	if rec := do(h, "DELETE", fmt.Sprintf("/api/notes/%d", n.ID), nil, nil); rec.Code != 404 {
		t.Fatalf("tekrar silme: %d", rec.Code)
	}
	if rec := do(h, "POST", "/api/people/9999/notes", map[string]any{"text": "x"}, nil); rec.Code != 404 {
		t.Fatalf("olmayan kişi: %d", rec.Code)
	}
}
