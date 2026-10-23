// Package directory kamuya açık geliştirici profil dizinlerinde (GitHub, GitLab, Codeberg, Stack
// Overflow) kullanıcı adı ve tam ad araması yapar. Sonuçlar aday profildir: ad ya da kullanıcı adı
// benzerliği sahiplik kanıtı değildir. Hız sınırı ve engeller "bulunamadı" değil, Failed olarak döner.
package directory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"

	"github.com/MustafaKemal0146/joe/internal/osint/safefetch"
	"github.com/MustafaKemal0146/joe/internal/osint/scan"
)

// Kind arama türüdür.
type Kind string

const (
	Username Kind = "username"
	FullName Kind = "full_name"
)

// Endpoints dizin API tabanlarıdır; testlerde yerel sunucuya yönlendirilir.
type Endpoints struct{ GitHub, GitLab, Codeberg, StackExchange string }

// Defaults gerçek servis adresleridir.
var Defaults = Endpoints{
	GitHub: "https://api.github.com", GitLab: "https://gitlab.com", Codeberg: "https://codeberg.org", StackExchange: "https://api.stackexchange.com",
}

const perSource = 10

// Validate sorguyu türüne göre denetler.
func Validate(q string, k Kind) error {
	q = strings.TrimSpace(q)
	switch k {
	case Username:
		return scan.ValidUsername(q)
	case FullName:
		if n := len([]rune(q)); n < 2 || n > 120 {
			return errors.New("tam ad 2–120 karakter olmalı")
		}
		return nil
	}
	return errors.New("bilinmeyen arama türü")
}

type source struct {
	name string
	url  func(e Endpoints, q string, k Kind) string // "" = bu tür için yok
	hits func(body []byte, q string, k Kind) ([]hit, error)
}

type hit struct{ label, url string }

func esc(s string) string { return url.QueryEscape(s) }

func sources() []source {
	return []source{
		{"GitHub", func(e Endpoints, q string, k Kind) string {
			if k == Username {
				return e.GitHub + "/users/" + url.PathEscape(q)
			}
			return e.GitHub + "/search/users?per_page=10&q=" + esc(`"`+q+`" in:fullname type:user`)
		}, func(b []byte, q string, k Kind) ([]hit, error) {
			if k == Username {
				var one struct {
					Login   string `json:"login"`
					HTMLURL string `json:"html_url"`
				}
				if err := json.Unmarshal(b, &one); err != nil {
					return nil, err
				}
				if one.HTMLURL == "" {
					return nil, nil
				}
				return []hit{{one.Login, one.HTMLURL}}, nil
			}
			var r struct {
				Items []struct {
					Login   string `json:"login"`
					HTMLURL string `json:"html_url"`
				} `json:"items"`
			}
			if err := json.Unmarshal(b, &r); err != nil {
				return nil, err
			}
			var out []hit
			for _, i := range r.Items {
				out = append(out, hit{i.Login, i.HTMLURL})
			}
			return out, nil
		}},
		{"GitLab", func(e Endpoints, q string, k Kind) string {
			if k == Username {
				return e.GitLab + "/api/v4/users?per_page=10&username=" + esc(q)
			}
			return e.GitLab + "/api/v4/users?per_page=10&search=" + esc(q)
		}, func(b []byte, q string, k Kind) ([]hit, error) {
			var raw []struct {
				Username string `json:"username"`
				Name     string `json:"name"`
				WebURL   string `json:"web_url"`
			}
			if err := json.Unmarshal(b, &raw); err != nil {
				return nil, err
			}
			var out []hit
			for _, i := range raw {
				if k == Username && !strings.EqualFold(i.Username, q) {
					continue
				}
				out = append(out, hit{label(i.Username, i.Name), i.WebURL})
			}
			return out, nil
		}},
		{"Codeberg", func(e Endpoints, q string, k Kind) string {
			if k == Username {
				return e.Codeberg + "/api/v1/users/" + url.PathEscape(q)
			}
			return e.Codeberg + "/api/v1/users/search?limit=10&q=" + esc(q)
		}, func(b []byte, q string, k Kind) ([]hit, error) {
			type u struct {
				Login    string `json:"login"`
				FullName string `json:"full_name"`
				HTMLURL  string `json:"html_url"`
			}
			if k == Username {
				var one u
				if err := json.Unmarshal(b, &one); err != nil {
					return nil, err
				}
				if one.HTMLURL == "" {
					return nil, nil
				}
				return []hit{{label(one.Login, one.FullName), one.HTMLURL}}, nil
			}
			var r struct {
				Data []u `json:"data"`
			}
			if err := json.Unmarshal(b, &r); err != nil {
				return nil, err
			}
			var out []hit
			for _, i := range r.Data {
				out = append(out, hit{label(i.Login, i.FullName), i.HTMLURL})
			}
			return out, nil
		}},
		{"Stack Overflow", func(e Endpoints, q string, k Kind) string {
			if k != FullName {
				return ""
			}
			return e.StackExchange + "/2.3/users?site=stackoverflow&pagesize=10&inname=" + esc(q)
		}, func(b []byte, q string, k Kind) ([]hit, error) {
			var r struct {
				Items []struct {
					DisplayName string `json:"display_name"`
					Link        string `json:"link"`
				} `json:"items"`
			}
			if err := json.Unmarshal(b, &r); err != nil {
				return nil, err
			}
			var out []hit
			for _, i := range r.Items {
				out = append(out, hit{i.DisplayName, i.Link})
			}
			return out, nil
		}},
	}
}

func label(login, name string) string {
	if name != "" && !strings.EqualFold(name, login) {
		return login + " (" + name + ")"
	}
	return login
}

// Sites bu arama türünde sorgulanacak dizinlerin adlarıdır.
func Sites(k Kind) []string {
	var out []string
	for _, s := range sources() {
		if s.url(Defaults, "x", k) != "" {
			out = append(out, s.name)
		}
	}
	return out
}

// Search tüm dizinleri paralel sorgular. Dönen sonuçlar Found (aday profil) ve Failed (denetlenemedi)
// içerir; bulunamayan dizinler yazılmaz. Hiçbir dizine ulaşılamazsa hata döner.
func Search(ctx context.Context, c *safefetch.Client, e Endpoints, q string, k Kind, progress func(scan.Progress)) ([]scan.Result, error) {
	q = strings.TrimSpace(q)
	if err := Validate(q, k); err != nil {
		return nil, err
	}
	var srcs []source
	for _, s := range sources() {
		if s.url(e, q, k) != "" {
			srcs = append(srcs, s)
		}
	}
	var mu sync.Mutex
	var out []scan.Result
	p := scan.Progress{Total: len(srcs)}
	reached := 0
	var wg sync.WaitGroup
	for _, s := range srcs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rs, ok := query(ctx, c, s, e, q, k)
			mu.Lock()
			defer mu.Unlock()
			p.Done++
			if ok {
				reached++
			}
			for _, r := range rs {
				switch r.Outcome {
				case scan.Found:
					p.Found++
				case scan.Failed:
					p.Failed++
				}
			}
			out = append(out, rs...)
			if progress != nil {
				progress(p)
			}
		}()
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if reached == 0 {
		return out, errors.New("hiçbir dizine ulaşılamadı")
	}
	return out, nil
}

func query(ctx context.Context, c *safefetch.Client, s source, e Endpoints, q string, k Kind) ([]scan.Result, bool) {
	fail := func(d string) ([]scan.Result, bool) {
		return []scan.Result{{Site: s.name, Category: "dizin", URL: s.url(e, q, k), Outcome: scan.Failed, Detail: d}}, false
	}
	pr, err := c.Probe(ctx, s.url(e, q, k))
	if err != nil {
		return fail(strings.Join(strings.Fields(err.Error()), " "))
	}
	switch {
	case pr.Status == 404:
		return nil, true
	case pr.Status == 429 || (pr.Status == 403 && strings.Contains(strings.ToLower(pr.Body), "rate limit")):
		return fail("hız sınırı (HTTP " + fmt.Sprint(pr.Status) + ")")
	case pr.Status != 200:
		return fail(fmt.Sprintf("beklenmeyen yanıt (HTTP %d)", pr.Status))
	case pr.Truncated:
		return fail("yanıt beklenenden büyük")
	}
	hs, err := s.hits([]byte(pr.Body), q, k)
	if err != nil {
		return fail("yanıt okunamadı")
	}
	var out []scan.Result
	for i, h := range hs {
		if i >= perSource {
			break
		}
		if u, err := url.Parse(h.url); err != nil || (u.Scheme != "https" && u.Scheme != "http") {
			continue
		}
		out = append(out, scan.Result{Site: s.name, Category: "dizin", URL: h.url, Outcome: scan.Found, Detail: h.label})
	}
	return out, true
}
