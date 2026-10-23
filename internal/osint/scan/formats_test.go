package scan

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func sherlockFixture(base string) []byte {
	return []byte(fmt.Sprintf(`{"$schema":"x",
 "SC":{"errorType":"status_code","url":"%[1]s/sc/{}","urlMain":"%[1]s/","username_claimed":"blue"},
 "Msg":{"errorType":"message","errorMsg":["kullanıcı yok","user gone"],"url":"%[1]s/msg/{}","urlMain":"%[1]s/","username_claimed":"blue"},
 "Redir":{"errorType":"response_url","errorUrl":"%[1]s/home","url":"%[1]s/redir/{}","urlMain":"%[1]s/","username_claimed":"blue"},
 "Probe":{"errorType":"status_code","url":"%[1]s/pretty/{}","urlProbe":"%[1]s/api/{}","urlMain":"%[1]s/","username_claimed":"blue"},
 "Rx":{"errorType":"status_code","regexCheck":"^[0-9]+$","url":"%[1]s/rx/{}","urlMain":"%[1]s/","username_claimed":"1"},
 "Post":{"errorType":"status_code","request_method":"POST","url":"%[1]s/post/{}","urlMain":"%[1]s/","username_claimed":"blue"},
 "Nsfw":{"errorType":"status_code","isNSFW":true,"url":"%[1]s/nsfw/{}","urlMain":"%[1]s/","username_claimed":"blue"},
 "Hdr":{"errorType":"status_code","headers":{"a":"b"},"url":"%[1]s/hdr/{}","urlMain":"%[1]s/","username_claimed":"blue"},
 "Bad":{"errorType":"status_code","url":"javascript:alert({})","urlMain":"x","username_claimed":"blue"},
 "NoMsg":{"errorType":"message","url":"%[1]s/nomsg/{}","urlMain":"%[1]s/","username_claimed":"blue"}
}`, base))
}

func TestParseSherlockMapsRulesAndCountsUnsupportedAsSkipped(t *testing.T) {
	l, err := ParseSherlock(sherlockFixture("http://x.test"))
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]Site{}
	for _, s := range l.Sites {
		names[s.Name] = s
	}
	for _, want := range []string{"SC", "Msg", "Redir", "Probe", "Rx"} {
		if _, ok := names[want]; !ok {
			t.Errorf("%s kullanılabilir olmalı", want)
		}
	}
	// POST, NSFW, özel başlık, tehlikeli şema ve kuralsız 'message' siteleri atlanır ve SAYILIR
	if len(l.Sites) != 5 || l.Skipped != 5 {
		t.Fatalf("kullanılabilir %d, atlanan %d", len(l.Sites), l.Skipped)
	}
	if s := names["Probe"]; !strings.Contains(s.URICheck, "/api/{account}") || !strings.Contains(s.URIPretty, "/pretty/{account}") {
		t.Fatalf("urlProbe yoklama adresi, url gösterim adresi olmalı: %+v", s)
	}
	if len(names["Msg"].MStrings) != 2 || names["Redir"].ErrURL == "" || !names["Redir"].NoRedirect || names["Rx"].Regex == nil {
		t.Fatalf("kurallar: %+v", names)
	}
	if _, err := ParseSherlock([]byte(`{}`)); err == nil {
		t.Fatal("boş liste hata vermeli")
	}
	if _, err := ParseSherlock([]byte(`[1]`)); err == nil {
		t.Fatal("tanınmayan biçim hata vermeli")
	}
}

func maigretFixture(base string) []byte {
	return []byte(fmt.Sprintf(`{"engines":{"Forum":{"site":{"url":"{urlMain}{urlSubpath}/member/{username}","checkType":"message","presenseStrs":["üye profili"],"absenceStrs":["üye bulunamadı"]}}},
"sites":{
 "SC":{"url":"%[1]s/sc/{username}","urlMain":"%[1]s","checkType":"status_code","usernameClaimed":"a","usernameUnclaimed":"b","alexaRank":500,"tags":["coding"]},
 "Eng":{"engine":"Forum","urlMain":"%[1]s","urlSubpath":"/forum","usernameClaimed":"a","usernameUnclaimed":"b","alexaRank":100},
 "Redir":{"url":"%[1]s/redir/{username}","urlMain":"%[1]s","checkType":"response_url","errorUrl":"%[1]s/home?u={username}","usernameClaimed":"a","usernameUnclaimed":"b"},
 "Off":{"url":"%[1]s/off/{username}","urlMain":"%[1]s","checkType":"status_code","disabled":true,"usernameClaimed":"a","usernameUnclaimed":"b"},
 "Gaia":{"url":"%[1]s/g/{username}","urlMain":"%[1]s","checkType":"status_code","type":"gaia_id","usernameClaimed":"a","usernameUnclaimed":"b"},
 "Prot":{"url":"%[1]s/p/{username}","urlMain":"%[1]s","checkType":"status_code","protection":["cf_js_challenge"],"usernameClaimed":"a","usernameUnclaimed":"b"},
 "Post":{"url":"%[1]s/po/{username}","urlMain":"%[1]s","checkType":"status_code","requestMethod":"POST","usernameClaimed":"a","usernameUnclaimed":"b"},
 "NoType":{"url":"%[1]s/n/{username}","urlMain":"%[1]s","usernameClaimed":"a","usernameUnclaimed":"b"},
 "Ign":{"url":"%[1]s/ign/{username}","urlMain":"%[1]s","checkType":"status_code","ignore403":true,"usernameClaimed":"a","usernameUnclaimed":"b","alexaRank":900}
}}`, base))
}

func TestParseMaigretResolvesEnginesAndSkipsUnsupported(t *testing.T) {
	l, err := ParseMaigret(maigretFixture("http://m.test"))
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]Site{}
	for _, s := range l.Sites {
		names[s.Name] = s
	}
	if len(l.Sites) != 4 || l.Skipped != 5 {
		t.Fatalf("kullanılabilir %d (%v), atlanan %d", len(l.Sites), names, l.Skipped)
	}
	e := names["Eng"]
	if e.URICheck != "http://m.test/forum/member/{account}" || len(e.EStrings) != 1 || len(e.MStrings) != 1 {
		t.Fatalf("motor şablonu çözülmeli: %+v", e)
	}
	if names["Redir"].ErrURL != "http://m.test/home?u={account}" || !names["Ign"].Ignore403 || names["SC"].Category != "coding" {
		t.Fatalf("kurallar: %+v", names)
	}
	// popülerliğe göre sıralı sınır: sıra bilgisi olmayan siteler sona gider
	top := l.Limit(2)
	if len(top) != 2 || top[0].Name != "Eng" || top[1].Name != "SC" {
		t.Fatalf("en popüler 2: %v %v", top[0].Name, top[1].Name)
	}
	if got := l.Limit(0); len(got) != 4 {
		t.Fatalf("sınırsız: %d", len(got))
	}
	if _, err := ParseMaigret([]byte(`{"sites":{}}`)); err == nil {
		t.Fatal("boş liste hata vermeli")
	}
}

func TestRunWithSherlockAndMaigretRulesEndToEnd(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case p == "/sc/ayse", p == "/api/ayse":
			w.WriteHeader(204) // herhangi bir 2xx "var" sayılır
		case p == "/msg/ayse":
			w.Write([]byte("hoş geldin ayse"))
		case p == "/redir/ayse":
			w.Write([]byte("profil"))
		case p == "/redir/yok":
			http.Redirect(w, r, "/home", 302)
		case p == "/home":
			w.Write([]byte("ana sayfa"))
		case p == "/forum/member/ayse":
			w.Write([]byte("üye profili: ayse"))
		case p == "/ign/ayse":
			http.Error(w, "yasak", 403)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	sl, _ := ParseSherlock(sherlockFixture(srv.URL))
	res, err := Run(context.Background(), client(), sl.Sites, "ayse", 4, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Outcome{}
	for _, r := range res {
		got[r.Site] = r.Outcome
	}
	for _, name := range []string{"SC", "Msg", "Redir", "Probe"} {
		if got[name] != Found {
			t.Errorf("%s bulunmalı: %v", name, got)
		}
	}
	if _, ok := got["Rx"]; ok {
		t.Error("regexCheck'e uymayan site sonuç üretmemeli")
	}
	// yönlendirme: hesap yok
	res, _ = Run(context.Background(), client(), sl.Sites, "yok", 4, nil)
	for _, r := range res {
		if r.Site == "Redir" {
			t.Errorf("errorUrl'e yönlenen site 'var' sayılmamalı: %+v", r)
		}
	}
	// mesaj kuralı: hata metni varsa hesap yok
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("kullanıcı yok")) }))
	defer srv2.Close()
	sl2, _ := ParseSherlock(sherlockFixture(srv2.URL))
	res, _ = Run(context.Background(), client(), sl2.Sites, "ayse", 4, nil)
	for _, r := range res {
		if r.Site == "Msg" {
			t.Errorf("errorMsg içeren yanıt 'var' sayılmamalı: %+v", r)
		}
	}

	ml, _ := ParseMaigret(maigretFixture(srv.URL))
	res, _ = Run(context.Background(), client(), ml.Sites, "ayse", 4, nil)
	got = map[string]Outcome{}
	for _, r := range res {
		got[r.Site] = r.Outcome
	}
	if got["Eng"] != Found || got["SC"] != Found {
		t.Errorf("motor sitesi ve status_code (204) sitesi bulunmalı: %v", got)
	}
	if got["Ign"] != "" { // ignore403: 403 hata değil, hesap yok
		t.Errorf("ignore403 sitesi 'hata' olmamalı: %v", got)
	}
}
