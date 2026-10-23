// Package scan, bir kullanıcı adının herkese açık sitelerde var olup olmadığını WhatsMyName biçimindeki
// bir site listesiyle denetler. Yalnızca herkese açık profil adreslerine GET isteği atılır; hesap
// bilgisi, giriş ya da özel içerik kullanılmaz. Eşleşme KİMLİK DOĞRULAMASI DEĞİLDİR: aynı kullanıcı
// adı farklı kişilere ait olabilir; sonuçlar "doğrulanmadı" aday bulgulardır.
package scan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"sync"

	"github.com/MustafaKemal0146/joe/internal/osint/safefetch"
)

// Site WhatsMyName kayıt biçimidir (kullanılan alanlar).
type Site struct {
	Name      string `json:"name"`
	URICheck  string `json:"uri_check"`
	URIPretty string `json:"uri_pretty"`
	ECode     int    `json:"e_code"`
	EString   string `json:"e_string"`
	MCode     int    `json:"m_code"`
	MString   string `json:"m_string"`
	Category  string `json:"cat"`
	Valid     bool   `json:"-"`

	// Sherlock/Maigret biçimlerinden gelen ek kurallar (WMN listesinde boştur).
	MStrings   []string       `json:"-"` // bunlardan biri varsa hesap YOK
	EStrings   []string       `json:"-"` // doluysa bunlardan biri olmalı, yoksa hesap YOK
	ErrURL     string         `json:"-"` // son adres bununla başlıyorsa hesap YOK (yönlendirme tabanlı)
	Any2xx     bool           `json:"-"` // 200 yerine herhangi bir 2xx "var" sayılır
	NoRedirect bool           `json:"-"` // yönlendirilen yanıt "yok" sayılır (Sherlock response_url)
	Ignore403  bool           `json:"-"` // 403 "erişim engeli" değil, "yok" sayılır
	Regex      *regexp.Regexp `json:"-"` // kullanıcı adı buna uymuyorsa site atlanır
	Rank       int            `json:"-"` // popülerlik sırası (küçük = popüler; 0 = bilinmiyor)
}

// List ayrıştırılmış site listesidir.
type List struct {
	Sites   []Site
	Skipped int
}

const maxListSites = 12000

// ParseList bir wmn-data.json içeriğini doğrular ve geçerli siteleri döndürür. Geçersiz kayıtlar
// (adres kalıbı eksik, http dışı, ayırt edici kural yok) sessizce yutulmaz; Skipped'a sayılır.
func ParseList(data []byte) (*List, error) {
	var doc struct {
		Sites []Site `json:"sites"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("site listesi okunamadı: %w", err)
	}
	if len(doc.Sites) == 0 {
		return nil, errors.New("site listesi boş ya da biçimi tanınmadı (\"sites\" dizisi bekleniyor)")
	}
	if len(doc.Sites) > maxListSites {
		return nil, fmt.Errorf("site listesi çok büyük (%d > %d)", len(doc.Sites), maxListSites)
	}
	l := &List{}
	for _, s := range doc.Sites {
		u, err := url.Parse(strings.ReplaceAll(s.URICheck, "{account}", "x"))
		ok := err == nil && strings.Contains(s.URICheck, "{account}") && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" &&
			strings.TrimSpace(s.Name) != "" && (s.EString != "" || s.MString != "" || s.ECode > 0)
		if !ok {
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

var userRx = regexp.MustCompile(`^[A-Za-z0-9._-]{2,40}$`)

// ValidUsername tarama için kullanıcı adının güvenli ve makul olduğunu denetler.
func ValidUsername(u string) error {
	if !userRx.MatchString(u) {
		return errors.New("kullanıcı adı 2–40 karakter olmalı ve yalnızca harf, rakam, nokta, alt çizgi ve tire içermeli")
	}
	return nil
}

// Outcome bir sitedeki sonuçtur.
type Outcome string

const (
	Found    Outcome = "found"
	NotFound Outcome = "not_found"
	Failed   Outcome = "failed" // ağ hatası, hız sınırı, engelleme: "bulunamadı" DEĞİL
)

// Result bir sitenin sonucudur.
type Result struct {
	Site     string
	Category string
	URL      string // kullanıcıya gösterilen profil adresi
	Outcome  Outcome
	Detail   string
}

// classify bir yanıtı WhatsMyName kurallarına göre sınıflar. Belirsiz durumlar (403/429/5xx ve
// beklenmeyen kodlar) Failed olur: hata, "hesap yok" gibi sunulmaz.
func classify(s Site, status int, body string) (Outcome, string) {
	return classifyAt(s, status, body, "", "")
}

func containsAny(body string, subs []string) bool {
	for _, x := range subs {
		if x != "" && strings.Contains(body, x) {
			return true
		}
	}
	return false
}

// classifyAt classify gibi çalışır; yönlendirme tabanlı kurallar için son adresi (finalURL) ve
// bu kullanıcı adına göre doldurulmuş "yok" adresini (errURL) de alır.
func classifyAt(s Site, status int, body, finalURL, errURL string) (Outcome, string) {
	statusOK := status == s.ECode || (s.Any2xx && status >= 200 && status < 300)
	eOK := statusOK && (s.EString == "" || strings.Contains(body, s.EString)) && (len(s.EStrings) == 0 || containsAny(body, s.EStrings))
	mHit := (s.MString != "" && strings.Contains(body, s.MString)) || containsAny(body, s.MStrings) ||
		(s.MCode > 0 && s.MCode != s.ECode && status == s.MCode) || (errURL != "" && strings.TrimRight(finalURL, "/") == strings.TrimRight(errURL, "/"))
	switch {
	case eOK && !mHit:
		return Found, ""
	case mHit:
		return NotFound, ""
	case status == 404 || status == 410:
		return NotFound, ""
	case statusOK:
		return NotFound, "beklenen işaret yok"
	case status == 403 && s.Ignore403:
		return NotFound, ""
	case status == 429:
		return Failed, "hız sınırı (HTTP 429)"
	case status == 401 || status == 403:
		return Failed, fmt.Sprintf("erişim engellendi (HTTP %d)", status)
	case status >= 500:
		return Failed, fmt.Sprintf("site hatası (HTTP %d)", status)
	}
	return Failed, fmt.Sprintf("beklenmeyen yanıt (HTTP %d)", status)
}

// Progress tarama ilerlemesidir.
type Progress struct{ Done, Total, Found, Failed int }

// Run kullanıcı adını tüm sitelerde sınırlı eşzamanlılıkla arar. İptal edilirse eldeki sonuçlar döner.
func Run(ctx context.Context, client *safefetch.Client, sites []Site, username string, concurrency int, progress func(Progress)) ([]Result, error) {
	return RunStream(ctx, client, sites, username, concurrency, progress, nil)
}

// RunStream Run gibi çalışır; ayrıca her bulgu (var ya da denetlenemedi) bulunur bulunmaz onResult'a verilir.
// onResult kilit altında, sıralı çağrılır: çağıranın ayrıca eşitleme yapmasına gerek yok.
func RunStream(ctx context.Context, client *safefetch.Client, sites []Site, username string, concurrency int, progress func(Progress), onResult func(Result)) ([]Result, error) {
	if err := ValidUsername(username); err != nil {
		return nil, err
	}
	if concurrency <= 0 {
		concurrency = 10
	}
	esc := url.PathEscape(username)
	var mu sync.Mutex
	var out []Result
	p := Progress{Total: len(sites)}
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for _, s := range sites {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			if ctx.Err() != nil {
				return
			}
			if s.Regex != nil && !s.Regex.MatchString(username) {
				mu.Lock()
				p.Done++ // bu site bu kullanıcı adı biçimini kabul etmiyor: sonuç yok, hata da değil
				if progress != nil {
					progress(p)
				}
				mu.Unlock()
				return
			}
			target := strings.ReplaceAll(s.URICheck, "{account}", esc)
			r := Result{Site: s.Name, Category: s.Category, URL: strings.ReplaceAll(firstNonEmpty(s.URIPretty, s.URICheck), "{account}", esc)}
			pr, err := client.Probe(ctx, target)
			if err != nil {
				r.Outcome, r.Detail = Failed, shortErr(err)
			} else {
				r.Outcome, r.Detail = classifyAt(s, pr.Status, pr.Body, pr.FinalURL, strings.ReplaceAll(s.ErrURL, "{account}", esc))
				if s.NoRedirect && r.Outcome == Found && strings.TrimRight(pr.FinalURL, "/") != strings.TrimRight(target, "/") {
					r.Outcome = NotFound // yönlendirildi: Sherlock'a göre hesap yok
				}
			}
			mu.Lock()
			p.Done++
			switch r.Outcome {
			case Found:
				p.Found++
				out = append(out, r)
			case Failed:
				p.Failed++
				out = append(out, r)
			}
			if onResult != nil && (r.Outcome == Found || r.Outcome == Failed) {
				onResult(r)
			}
			// İlerleme geri çağrısı kilit altında, sıralı çağrılır: çağıranın ayrıca eşitleme yapmasına gerek yok.
			if progress != nil {
				progress(p)
			}
			mu.Unlock()
		}()
	}
	wg.Wait()
	return out, ctx.Err()
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func shortErr(err error) string {
	m := strings.Join(strings.Fields(err.Error()), " ")
	if len(m) > 120 {
		m = m[:120] + "…"
	}
	return m
}
