// Package chat, Joe'nun sohbet katmanıdır: kullanıcı mesajını anlar, sınırlı ve tipli araçlarla
// uygulamayı yönetir. Güvenlik kuralları yapısaldır, istemle sağlanmaz:
//   - Salt-okuma araçları hemen çalışır ve yalnızca özet metrik döndürür (ham yazışma metni asla).
//   - Yazan ve geri alınamaz araçları model ÇALIŞTIRAMAZ; yalnızca öneri (bekleyen eylem) üretir,
//     yürütme kullanıcının ayrı onay isteğiyle olur.
//   - Sayfa açma araçları sadece bir bağlantı kartı üretir, para harcamaz.
package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Kind aracın türüdür.
type Kind int

const (
	Read     Kind = iota // hemen çalışır, özet döndürür
	Navigate             // yalnızca bağlantı kartı üretir
	Write                // kullanıcı onayı ister
	Danger               // geri alınamaz; kırmızı onay ister
)

// Env araçların uygulamaya eriştiği arayüzdür (sunucu uygular, testler sahtesini verir).
type Env interface {
	ListPeople() []map[string]any
	ListSources() []map[string]any
	ProfileSummary(personID int64) (map[string]any, error)
	Compare(a, b int64) (map[string]any, error)
	SearchCount(q string) (map[string]any, error)

	CreatePerson(name string, identities []string) (string, error)
	AddIdentity(personID int64, value string) (string, error)
	SetOwner(sourceID int64, name, handle string) (string, error)
	AddNote(personID int64, text, source string) (string, error)
	DeleteSource(sourceID int64) (string, error)
}

// Result bir aracın sonucudur.
type Result struct {
	// Text modele geri verilen (ve geçmişe yazılan) sonuç metnidir.
	Text string
	// Nav doluysa arayüz bu yola giden bir bağlantı kartı gösterir.
	Nav *Nav
}

// Nav bir bağlantı kartıdır.
type Nav struct {
	Label string `json:"label"`
	To    string `json:"to"`
}

// Tool bir araç tanımıdır.
type Tool struct {
	Name        string
	Description string
	Schema      string
	Kind        Kind
	// parse argümanları doğrular ve çalıştırılabilir bir değere çevirir.
	parse func(raw string) (any, error)
	// summary onay kartında gösterilecek açıklamadır.
	summary func(v any) string
	run     func(ctx context.Context, env Env, v any, source string) (Result, error)
}

var errArgs = errors.New("geçersiz araç argümanı")

func strict(raw string, v any) error {
	if strings.TrimSpace(raw) == "" {
		raw = "{}"
	}
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("%w: %v", errArgs, err)
	}
	return nil
}

func jsonText(v any) string {
	b, _ := json.MarshalIndent(v, "", " ")
	if len(b) > 6000 {
		return string(b[:6000]) + "\n… (kısaltıldı)"
	}
	return string(b)
}

func positive(id int64, name string) error {
	if id <= 0 {
		return fmt.Errorf("%w: %s pozitif olmalı", errArgs, name)
	}
	return nil
}

func nonEmpty(s, name string, max int) error {
	if strings.TrimSpace(s) == "" {
		return fmt.Errorf("%w: %s boş olamaz", errArgs, name)
	}
	if len([]rune(s)) > max {
		return fmt.Errorf("%w: %s en çok %d karakter olabilir", errArgs, name, max)
	}
	return nil
}

var pages = map[string]string{"kisiler": "/kisiler", "harita": "/harita", "karsilastir": "/karsilastir", "konsey": "/konsey", "kaynaklar": "/kaynaklar", "ayarlar": "/ayarlar", "ara": "/ara"}

// Tools araç kaydıdır.
func Tools() []Tool {
	type idArg struct {
		PersonID int64 `json:"person_id"`
	}
	return []Tool{
		{Name: "list_people", Kind: Read, Description: "Kişi dosyalarını listeler (kimlik, ad, kimlik sayısı, 'ben' mi).", Schema: `{"type":"object","properties":{},"additionalProperties":false}`,
			parse: func(raw string) (any, error) { var v struct{}; return v, strict(raw, &v) },
			run: func(_ context.Context, e Env, _ any, _ string) (Result, error) {
				return Result{Text: jsonText(e.ListPeople())}, nil
			}},
		{Name: "list_sources", Kind: Read, Description: "İçe aktarılmış kaynakları listeler (platform, sahip, mesaj sayısı).", Schema: `{"type":"object","properties":{},"additionalProperties":false}`,
			parse: func(raw string) (any, error) { var v struct{}; return v, strict(raw, &v) },
			run: func(_ context.Context, e Env, _ any, _ string) (Result, error) {
				return Result{Text: jsonText(e.ListSources())}, nil
			}},
		{Name: "get_profile_summary", Kind: Read, Description: "Bir kişinin deterministik metrik özetini verir (mesaj sayısı, ritim, yanıt süresi, sohbet karşıları). Ham mesaj metni içermez.",
			Schema: `{"type":"object","properties":{"person_id":{"type":"integer"}},"required":["person_id"],"additionalProperties":false}`,
			parse: func(raw string) (any, error) {
				var v idArg
				if err := strict(raw, &v); err != nil {
					return nil, err
				}
				return v, positive(v.PersonID, "person_id")
			},
			run: func(_ context.Context, e Env, v any, _ string) (Result, error) {
				m, err := e.ProfileSummary(v.(idArg).PersonID)
				if err != nil {
					return Result{}, err
				}
				return Result{Text: jsonText(m)}, nil
			}},
		{Name: "compare_people", Kind: Read, Description: "İki kişinin benzerlik ve denge göstergelerini verir (hüküm değil).",
			Schema: `{"type":"object","properties":{"a":{"type":"integer"},"b":{"type":"integer"}},"required":["a","b"],"additionalProperties":false}`,
			parse: func(raw string) (any, error) {
				var v struct{ A, B int64 }
				if err := strict(raw, &v); err != nil {
					return nil, err
				}
				if err := positive(v.A, "a"); err != nil {
					return nil, err
				}
				if v.A == v.B {
					return nil, fmt.Errorf("%w: iki farklı kişi gerekir", errArgs)
				}
				return v, positive(v.B, "b")
			},
			run: func(_ context.Context, e Env, v any, _ string) (Result, error) {
				p := v.(struct{ A, B int64 })
				m, err := e.Compare(p.A, p.B)
				if err != nil {
					return Result{}, err
				}
				return Result{Text: jsonText(m)}, nil
			}},
		{Name: "search_count", Kind: Read, Description: "Bir sözcüğün mesajlarda kaç kez ve hangi sohbetlerde geçtiğini söyler. Mesaj metnini döndürmez.",
			Schema: `{"type":"object","properties":{"query":{"type":"string"}},"required":["query"],"additionalProperties":false}`,
			parse: func(raw string) (any, error) {
				var v struct {
					Query string `json:"query"`
				}
				if err := strict(raw, &v); err != nil {
					return nil, err
				}
				return v, nonEmpty(v.Query, "query", 200)
			},
			run: func(_ context.Context, e Env, v any, _ string) (Result, error) {
				m, err := e.SearchCount(v.(struct {
					Query string `json:"query"`
				}).Query)
				if err != nil {
					return Result{}, err
				}
				return Result{Text: jsonText(m)}, nil
			}},

		{Name: "open_page", Kind: Navigate, Description: "Kullanıcıya bir sayfaya giden bağlantı gösterir: kisiler, harita, karsilastir, konsey, kaynaklar, ayarlar, ara.",
			Schema: `{"type":"object","properties":{"page":{"type":"string","enum":["kisiler","harita","karsilastir","konsey","kaynaklar","ayarlar","ara"]}},"required":["page"],"additionalProperties":false}`,
			parse: func(raw string) (any, error) {
				var v struct {
					Page string `json:"page"`
				}
				if err := strict(raw, &v); err != nil {
					return nil, err
				}
				if _, ok := pages[v.Page]; !ok {
					return nil, fmt.Errorf("%w: bilinmeyen sayfa %q", errArgs, v.Page)
				}
				return v, nil
			},
			run: func(_ context.Context, _ Env, v any, _ string) (Result, error) {
				page := v.(struct {
					Page string `json:"page"`
				}).Page
				return Result{Text: "Bağlantı kullanıcıya gösterildi.", Nav: &Nav{Label: "Sayfayı aç", To: pages[page]}}, nil
			}},
		{Name: "prepare_council", Kind: Navigate, Description: "Konsey sayfasını bir kişi için hazırlanmış olarak açan bağlantı gösterir. Maliyet onayı o sayfada kullanıcıdan alınır; analiz BURADA başlamaz.",
			Schema: `{"type":"object","properties":{"person_id":{"type":"integer"}},"required":["person_id"],"additionalProperties":false}`,
			parse: func(raw string) (any, error) {
				var v idArg
				if err := strict(raw, &v); err != nil {
					return nil, err
				}
				return v, positive(v.PersonID, "person_id")
			},
			run: func(_ context.Context, _ Env, v any, _ string) (Result, error) {
				return Result{Text: "Konsey bağlantısı kullanıcıya gösterildi; analiz henüz başlamadı, maliyet onayı kullanıcıdan alınacak.",
					Nav: &Nav{Label: "Konsey'i hazırla", To: fmt.Sprintf("/konsey?kisi=%d", v.(idArg).PersonID)}}, nil
			}},

		{Name: "create_person", Kind: Write, Description: "Yeni bir kişi dosyası açar (kullanıcı onayı gerekir).",
			Schema: `{"type":"object","properties":{"name":{"type":"string"},"identities":{"type":"array","items":{"type":"string"}}},"required":["name"],"additionalProperties":false}`,
			parse: func(raw string) (any, error) {
				var v struct {
					Name       string   `json:"name"`
					Identities []string `json:"identities"`
				}
				if err := strict(raw, &v); err != nil {
					return nil, err
				}
				if len(v.Identities) > 10 {
					return nil, fmt.Errorf("%w: en çok 10 kimlik", errArgs)
				}
				for _, id := range v.Identities {
					if err := nonEmpty(id, "kimlik", 100); err != nil {
						return nil, err
					}
				}
				return v, nonEmpty(v.Name, "name", 100)
			},
			summary: func(v any) string {
				p := v.(struct {
					Name       string   `json:"name"`
					Identities []string `json:"identities"`
				})
				return fmt.Sprintf("«%s» için kişi dosyası aç (kimlikler: %s)", p.Name, strings.Join(append([]string{p.Name}, p.Identities...), ", "))
			},
			run: func(_ context.Context, e Env, v any, _ string) (Result, error) {
				p := v.(struct {
					Name       string   `json:"name"`
					Identities []string `json:"identities"`
				})
				msg, err := e.CreatePerson(p.Name, p.Identities)
				return Result{Text: msg}, err
			}},
		{Name: "add_identity", Kind: Write, Description: "Bir kişiye ad ya da kullanıcı adı ekler (kimlik birleştirme; kullanıcı onayı gerekir).",
			Schema: `{"type":"object","properties":{"person_id":{"type":"integer"},"value":{"type":"string"}},"required":["person_id","value"],"additionalProperties":false}`,
			parse: func(raw string) (any, error) {
				var v struct {
					PersonID int64  `json:"person_id"`
					Value    string `json:"value"`
				}
				if err := strict(raw, &v); err != nil {
					return nil, err
				}
				if err := positive(v.PersonID, "person_id"); err != nil {
					return nil, err
				}
				return v, nonEmpty(v.Value, "value", 100)
			},
			summary: func(v any) string {
				p := v.(struct {
					PersonID int64  `json:"person_id"`
					Value    string `json:"value"`
				})
				return fmt.Sprintf("#%d numaralı kişiye «%s» kimliğini ekle", p.PersonID, p.Value)
			},
			run: func(_ context.Context, e Env, v any, _ string) (Result, error) {
				p := v.(struct {
					PersonID int64  `json:"person_id"`
					Value    string `json:"value"`
				})
				msg, err := e.AddIdentity(p.PersonID, p.Value)
				return Result{Text: msg}, err
			}},
		{Name: "set_owner", Kind: Write, Description: "Bir kaynağın hesap sahibini belirler (kullanıcı onayı gerekir).",
			Schema: `{"type":"object","properties":{"source_id":{"type":"integer"},"name":{"type":"string"},"handle":{"type":"string"}},"required":["source_id","name"],"additionalProperties":false}`,
			parse: func(raw string) (any, error) {
				var v struct {
					SourceID int64  `json:"source_id"`
					Name     string `json:"name"`
					Handle   string `json:"handle"`
				}
				if err := strict(raw, &v); err != nil {
					return nil, err
				}
				if err := positive(v.SourceID, "source_id"); err != nil {
					return nil, err
				}
				if v.Handle != "" {
					if err := nonEmpty(v.Handle, "handle", 100); err != nil {
						return nil, err
					}
				}
				return v, nonEmpty(v.Name, "name", 100)
			},
			summary: func(v any) string {
				p := v.(struct {
					SourceID int64  `json:"source_id"`
					Name     string `json:"name"`
					Handle   string `json:"handle"`
				})
				return fmt.Sprintf("#%d numaralı kaynağın sahibini «%s» olarak belirle", p.SourceID, p.Name)
			},
			run: func(_ context.Context, e Env, v any, _ string) (Result, error) {
				p := v.(struct {
					SourceID int64  `json:"source_id"`
					Name     string `json:"name"`
					Handle   string `json:"handle"`
				})
				msg, err := e.SetOwner(p.SourceID, p.Name, p.Handle)
				return Result{Text: msg}, err
			}},
		{Name: "add_note", Kind: Write, Description: "Kullanıcı açıkça 'not al / hatırla' dediğinde bir kişi dosyasına not ekler (kullanıcı onayı gerekir; kaynağı sohbet olarak kaydedilir).",
			Schema: `{"type":"object","properties":{"person_id":{"type":"integer"},"text":{"type":"string"}},"required":["person_id","text"],"additionalProperties":false}`,
			parse: func(raw string) (any, error) {
				var v struct {
					PersonID int64  `json:"person_id"`
					Text     string `json:"text"`
				}
				if err := strict(raw, &v); err != nil {
					return nil, err
				}
				if err := positive(v.PersonID, "person_id"); err != nil {
					return nil, err
				}
				return v, nonEmpty(v.Text, "text", 1000)
			},
			summary: func(v any) string {
				p := v.(struct {
					PersonID int64  `json:"person_id"`
					Text     string `json:"text"`
				})
				return fmt.Sprintf("#%d numaralı kişiye şu notu ekle: «%s»", p.PersonID, p.Text)
			},
			run: func(_ context.Context, e Env, v any, source string) (Result, error) {
				p := v.(struct {
					PersonID int64  `json:"person_id"`
					Text     string `json:"text"`
				})
				msg, err := e.AddNote(p.PersonID, p.Text, source)
				return Result{Text: msg}, err
			}},

		{Name: "delete_source", Kind: Danger, Description: "Bir kaynağı ve ona bağlı bütün mesaj/etkinlikleri siler. GERİ ALINAMAZ; kırmızı onay gerekir.",
			Schema: `{"type":"object","properties":{"source_id":{"type":"integer"}},"required":["source_id"],"additionalProperties":false}`,
			parse: func(raw string) (any, error) {
				var v struct {
					SourceID int64 `json:"source_id"`
				}
				if err := strict(raw, &v); err != nil {
					return nil, err
				}
				return v, positive(v.SourceID, "source_id")
			},
			summary: func(v any) string {
				return fmt.Sprintf("#%d numaralı kaynağı ve ona bağlı TÜM mesaj ve etkinlikleri kalıcı olarak sil", v.(struct {
					SourceID int64 `json:"source_id"`
				}).SourceID)
			},
			run: func(_ context.Context, e Env, v any, _ string) (Result, error) {
				msg, err := e.DeleteSource(v.(struct {
					SourceID int64 `json:"source_id"`
				}).SourceID)
				return Result{Text: msg}, err
			}},
	}
}

// ByName araç kaydında ada göre arar.
func ByName(name string) (Tool, bool) {
	for _, t := range Tools() {
		if t.Name == name {
			return t, true
		}
	}
	return Tool{}, false
}
