package directory

import (
	"net/url"
	"strings"
)

// Link elle açılacak bir arama ya da profil adayıdır.
type Link struct {
	Engine string `json:"engine,omitempty"`
	Label  string `json:"label"`
	Query  string `json:"query,omitempty"`
	URL    string `json:"url"`
}

// Plan elle araştırma planıdır. Bu bağlantılar için sunucu hiçbir istek atmaz.
type Plan struct {
	Query    string `json:"query"`
	Kind     Kind   `json:"kind"`
	Searches []Link `json:"searches"`
	Direct   []Link `json:"direct"`
	Notice   string `json:"notice"`
}

var engines = []struct{ name, base string }{
	{"Google", "https://www.google.com/search?q="},
	{"Bing", "https://www.bing.com/search?q="},
	{"DuckDuckGo", "https://duckduckgo.com/?q="},
}

// ManualPlan tırnaklı ve siteye özel arama varyantları üretir.
func ManualPlan(q string, k Kind) Plan {
	q = strings.Join(strings.Fields(q), " ")
	var variants []string
	var direct []Link
	if k == Username {
		variants = []string{q, `"` + q + `"`, `"` + q + `" profil`, `"` + q + `" site:github.com OR site:gitlab.com`, `"` + q + `" site:reddit.com OR site:medium.com`}
		for _, d := range []struct{ label, base string }{
			{"GitHub profili", "https://github.com/"}, {"Reddit profili", "https://www.reddit.com/user/"}, {"Keybase profili", "https://keybase.io/"},
		} {
			direct = append(direct, Link{Label: d.label, URL: d.base + url.PathEscape(q)})
		}
	} else {
		variants = []string{`"` + q + `"`, `"` + q + `" site:linkedin.com/in`, `"` + q + `" site:github.com`, `"` + q + `" filetype:pdf`, `"` + q + `" haber OR röportaj OR özgeçmiş`}
	}
	p := Plan{Query: q, Kind: k, Direct: direct, Notice: "Bu bağlantılar kanıt değil, senin doğrulaman gereken araştırma adaylarıdır."}
	for _, v := range variants {
		for _, e := range engines {
			p.Searches = append(p.Searches, Link{Engine: e.name, Label: v, Query: v, URL: e.base + url.QueryEscape(v)})
		}
	}
	return p
}
