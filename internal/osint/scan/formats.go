package scan

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

// ListFormat bir site listesi biçimidir.
type ListFormat string

const (
	FormatWMN      ListFormat = "wmn"
	FormatSherlock ListFormat = "sherlock"
	FormatMaigret  ListFormat = "maigret"
)

// Parse listeyi biçimine göre ayrıştırır.
func Parse(f ListFormat, data []byte) (*List, error) {
	switch f {
	case FormatWMN:
		return ParseList(data)
	case FormatSherlock:
		return ParseSherlock(data)
	case FormatMaigret:
		return ParseMaigret(data)
	}
	return nil, fmt.Errorf("bilinmeyen liste biçimi: %q", f)
}

// Limit siteleri popülerliğe göre sıralayıp ilk n tanesini döndürür (n<=0: hepsi). Sırası bilinmeyen
// siteler sona gider; eşit sıradaki siteler listedeki sırasını korur.
func (l *List) Limit(n int) []Site {
	out := append([]Site(nil), l.Sites...)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].Rank, out[j].Rank
		if (a > 0) != (b > 0) {
			return a > 0
		}
		return a > 0 && a < b
	})
	if n > 0 && n < len(out) {
		out = out[:n]
	}
	return out
}

func strList(v any) []string {
	switch x := v.(type) {
	case string:
		if x != "" {
			return []string{x}
		}
	case []any:
		var out []string
		for _, e := range x {
			if s, ok := e.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func validSite(u string) bool {
	p, err := url.Parse(strings.ReplaceAll(u, "{account}", "x"))
	return err == nil && strings.Contains(u, "{account}") && (p.Scheme == "http" || p.Scheme == "https") && p.Host != ""
}

func compileRx(s string) (*regexp.Regexp, bool) {
	if s == "" {
		return nil, true
	}
	rx, err := regexp.Compile(s)
	return rx, err == nil // Go RE2'dir: bazı PCRE kalıpları derlenmez; o site güvenle atlanır
}

// ParseSherlock sherlock'un data.json dosyasını ayrıştırır. Desteklenmeyen kayıtlar (POST/HEAD
// istekleri, özel başlıklar, NSFW siteler) sessizce yutulmaz; Skipped'a sayılır.
func ParseSherlock(data []byte) (*List, error) {
	var doc map[string]map[string]any
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("Sherlock listesi okunamadı: %w", err)
	}
	doc = map[string]map[string]any{}
	for k, v := range raw {
		if strings.HasPrefix(k, "$") {
			continue
		}
		var m map[string]any
		if json.Unmarshal(v, &m) == nil {
			doc[k] = m
		}
	}
	if len(doc) == 0 {
		return nil, errors.New("Sherlock listesi boş ya da biçimi tanınmadı (site adı → ayar nesneleri bekleniyor)")
	}
	if len(doc) > maxListSites {
		return nil, fmt.Errorf("liste çok büyük (%d > %d)", len(doc), maxListSites)
	}
	names := make([]string, 0, len(doc))
	for n := range doc {
		names = append(names, n)
	}
	sort.Strings(names)
	l := &List{}
	for _, name := range names {
		d := doc[name]
		str := func(k string) string { s, _ := d[k].(string); return s }
		check := str("urlProbe")
		if check == "" {
			check = str("url")
		}
		check = strings.ReplaceAll(check, "{}", "{account}")
		pretty := strings.ReplaceAll(str("url"), "{}", "{account}")
		rx, rxOK := compileRx(str("regexCheck"))
		method := strings.ToUpper(str("request_method"))
		_, hasHeaders := d["headers"]
		_, hasPayload := d["request_payload"]
		nsfw, _ := d["isNSFW"].(bool)
		if !validSite(check) || !rxOK || nsfw || hasHeaders || hasPayload || (method != "" && method != "GET") {
			l.Skipped++
			continue
		}
		s := Site{Name: name, URICheck: check, URIPretty: pretty, ECode: 200, Category: "sherlock", Regex: rx}
		switch str("errorType") {
		case "status_code":
			s.Any2xx = true
			s.MCode = 404
		case "message":
			s.MStrings = strList(d["errorMsg"])
			s.Any2xx = true
			if len(s.MStrings) == 0 {
				l.Skipped++
				continue
			}
		case "response_url":
			s.ErrURL = strings.ReplaceAll(str("errorUrl"), "{}", "{account}")
			s.Any2xx, s.NoRedirect = true, true
		default:
			l.Skipped++
			continue
		}
		s.Valid = true
		l.Sites = append(l.Sites, s)
	}
	if len(l.Sites) == 0 {
		return nil, errors.New("listede kullanılabilir site yok")
	}
	return l, nil
}

// ParseMaigret maigret'in data.json dosyasını ayrıştırır: motor şablonlarını çözer; devre dışı,
// kullanıcı adı olmayan türdeki, korumalı (bot/engel gerektiren), özel istek ya da etkinleştirme
// gerektiren siteleri Skipped'a sayar.
func ParseMaigret(data []byte) (*List, error) {
	var doc struct {
		Sites   map[string]map[string]any `json:"sites"`
		Engines map[string]struct {
			Site map[string]any `json:"site"`
		} `json:"engines"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("Maigret listesi okunamadı: %w", err)
	}
	if len(doc.Sites) == 0 {
		return nil, errors.New("Maigret listesi boş ya da biçimi tanınmadı (\"sites\" nesnesi bekleniyor)")
	}
	if len(doc.Sites) > maxListSites {
		return nil, fmt.Errorf("liste çok büyük (%d > %d)", len(doc.Sites), maxListSites)
	}
	names := make([]string, 0, len(doc.Sites))
	for n := range doc.Sites {
		names = append(names, n)
	}
	sort.Strings(names)
	l := &List{}
	for _, name := range names {
		d := map[string]any{}
		if eng, _ := doc.Sites[name]["engine"].(string); eng != "" {
			for k, v := range doc.Engines[eng].Site {
				d[k] = v
			}
		}
		for k, v := range doc.Sites[name] {
			d[k] = v
		}
		str := func(k string) string { s, _ := d[k].(string); return s }
		disabled, _ := d["disabled"].(bool)
		typ := str("type")
		_, prot := d["protection"]
		_, act := d["activation"]
		_, hdr := d["headers"]
		_, pay := d["requestPayload"]
		method := strings.ToUpper(str("requestMethod"))
		if disabled || (typ != "" && typ != "username") || (prot && len(strList(d["protection"])) > 0) || act || hdr || pay || (method != "" && method != "GET") || d["requestHeadOnly"] != nil {
			l.Skipped++
			continue
		}
		tmpl := str("urlProbe")
		if tmpl == "" {
			tmpl = str("url")
		}
		repl := strings.NewReplacer("{urlMain}", str("urlMain"), "{urlSubpath}", str("urlSubpath"), "{username}", "{account}")
		check := repl.Replace(tmpl)
		pretty := repl.Replace(str("url"))
		rx, rxOK := compileRx(str("regexCheck"))
		if !validSite(check) || !rxOK {
			l.Skipped++
			continue
		}
		s := Site{Name: name, URICheck: check, URIPretty: pretty, ECode: 200, Category: "maigret", Regex: rx, Ignore403: d["ignore403"] == true}
		if r, ok := d["alexaRank"].(float64); ok && r > 0 {
			s.Rank = int(r)
		}
		if tags := strList(d["tags"]); len(tags) > 0 {
			s.Category = tags[0]
		}
		switch str("checkType") {
		case "status_code":
			s.Any2xx = true
			s.MCode = 404
		case "message":
			s.EStrings, s.MStrings = strList(d["presenseStrs"]), strList(d["absenceStrs"])
			s.Any2xx = true
			if len(s.EStrings)+len(s.MStrings) == 0 {
				l.Skipped++
				continue
			}
		case "response_url":
			s.ErrURL = repl.Replace(str("errorUrl"))
			s.Any2xx, s.NoRedirect = true, true
		default:
			l.Skipped++
			continue
		}
		s.Valid = true
		l.Sites = append(l.Sites, s)
	}
	if len(l.Sites) == 0 {
		return nil, errors.New("listede kullanılabilir site yok")
	}
	return l, nil
}
