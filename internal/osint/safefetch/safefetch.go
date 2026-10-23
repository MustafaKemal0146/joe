// Package safefetch, dışarıdaki sayfaları SSRF'e karşı güvenli biçimde okur.
//
// Hedef IP, DNS çözümlemesi ile bağlantı arasındaki boşluğa (DNS rebinding) düşmemek için
// bağlantı kurulurken, yani soketin gerçekten bağlanacağı adres üzerinde doğrulanır
// (net.Dialer.Control). Her yönlendirme yeniden doğrulanır, ortam proxy'leri yok sayılır ve
// gövde akışlı okunup sınırda kesilir.
package safefetch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// Policy bir IP'ye bağlanmaya izin verilip verilmediğini söyler.
type Policy func(netip.Addr) bool

// ErrBlocked hedefin ilkeyle reddedildiğini bildirir.
var ErrBlocked = errors.New("hedef adres izinli değil")

var blockedPrefixes = func() []netip.Prefix {
	var out []netip.Prefix
	for _, p := range []string{
		"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12",
		"192.0.0.0/24", "192.0.2.0/24", "192.88.99.0/24", "192.168.0.0/16", "198.18.0.0/15",
		"198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4",
		"::/128", "::1/128", "64:ff9b::/96", "100::/64", "2001:db8::/32", "fc00::/7", "fe80::/10", "ff00::/8",
	} {
		out = append(out, netip.MustParsePrefix(p))
	}
	return out
}()

// PublicOnly yalnızca genel internet adreslerine izin verir. IPv4-mapped IPv6 adresleri
// ("::ffff:127.0.0.1") önce IPv4'e çözülür; böylece haritalama ile atlatma mümkün olmaz.
func PublicOnly(a netip.Addr) bool {
	a = a.Unmap()
	if !a.IsValid() || a.IsUnspecified() || a.IsLoopback() || a.IsPrivate() || a.IsLinkLocalUnicast() ||
		a.IsLinkLocalMulticast() || a.IsInterfaceLocalMulticast() || a.IsMulticast() {
		return false
	}
	for _, p := range blockedPrefixes {
		if p.Contains(a) {
			return false
		}
	}
	return true
}

// Page okunan sayfanın özetidir.
type Page struct {
	URL         string
	Status      int
	Title       string
	Text        string
	ContentType string
	SHA256      string
	Bytes       int
	Truncated   bool
}

// Client güvenli getirici.
type Client struct {
	// Policy nil ise PublicOnly kullanılır.
	Policy       Policy
	MaxBytes     int64
	MaxRedirects int
	Timeout      time.Duration
	UserAgent    string
}

// New makul varsayılanlarla bir istemci üretir.
func New() *Client {
	return &Client{MaxBytes: 2 << 20, MaxRedirects: 4, Timeout: 20 * time.Second, UserAgent: "JoeResearch/0.1 (+local-source-observation)"}
}

func (c *Client) policy() Policy {
	if c.Policy != nil {
		return c.Policy
	}
	return PublicOnly
}

func (c *Client) httpClient() *http.Client {
	pol := c.policy()
	dialer := &net.Dialer{
		Timeout: 8 * time.Second,
		// Control, soket bağlanmadan hemen önce çözülmüş gerçek adresle çağrılır.
		Control: func(network, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return ErrBlocked
			}
			ap, err := netip.ParseAddr(host)
			if err != nil || !pol(ap) {
				return fmt.Errorf("%w: %s", ErrBlocked, host)
			}
			return nil
		},
	}
	tr := &http.Transport{
		Proxy:                 nil, // ortam proxy'si politikayı atlatmasın
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   8 * time.Second,
		ResponseHeaderTimeout: c.Timeout,
		DisableKeepAlives:     true,
	}
	return &http.Client{
		Transport: tr,
		Timeout:   c.Timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > c.MaxRedirects {
				return errors.New("yönlendirme sınırı aşıldı")
			}
			return checkURL(req.URL)
		},
	}
}

func checkURL(u *url.URL) error {
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("yalnızca http/https okunur, %q değil", u.Scheme)
	}
	if u.Hostname() == "" {
		return errors.New("adres boş")
	}
	if u.User != nil {
		return errors.New("kimlik bilgisi içeren adres reddedildi")
	}
	return nil
}

var (
	reScript = regexp.MustCompile(`(?is)<(script|style|noscript)\b[^>]*>.*?</(script|style|noscript)>`)
	reTag    = regexp.MustCompile(`<[^>]+>`)
	reTitle  = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	reSpace  = regexp.MustCompile(`\s+`)
)

// Fetch bir sayfayı okur. Hata, ağ hatasından ya da ilkeden gelir; asla boş sonuç döndürmez.
func (c *Client) Fetch(ctx context.Context, rawURL string) (*Page, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("geçersiz adres: %w", err)
	}
	if err := checkURL(u); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("Accept", "text/html,text/plain;q=0.9,*/*;q=0.1")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("kaynak HTTP %d döndürdü", resp.StatusCode)
	}
	// Akışlı okuma: sınırdan bir bayt fazlası okunup kesildi mi anlaşılır; gövdenin tamamı indirilmez.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, c.MaxBytes+1))
	if err != nil {
		return nil, err
	}
	truncated := int64(len(raw)) > c.MaxBytes
	if truncated {
		raw = raw[:c.MaxBytes]
	}
	html := strings.ToValidUTF8(string(raw), "�")
	title := ""
	if m := reTitle.FindStringSubmatch(html); m != nil {
		title = clean(reTag.ReplaceAllString(m[1], " "))
		if len(title) > 500 {
			title = title[:500]
		}
	}
	text := clean(reTag.ReplaceAllString(reScript.ReplaceAllString(html, " "), " "))
	sum := sha256.Sum256(raw)
	return &Page{
		URL: resp.Request.URL.String(), Status: resp.StatusCode, Title: title, Text: text,
		ContentType: resp.Header.Get("Content-Type"), SHA256: hex.EncodeToString(sum[:]), Bytes: len(raw), Truncated: truncated,
	}, nil
}

func clean(s string) string { return strings.TrimSpace(reSpace.ReplaceAllString(s, " ")) }

// Probe bir adrese GET isteği atar ve HTTP hata kodlarını (404, 403…) hata saymaz: durum kodu ve
// gövde döner. Kullanıcı adı taraması gibi "sayfa var mı" sorularında durum kodunun kendisi cevaptır.
// SSRF korumaları Fetch ile aynıdır (bağlantı anında IP doğrulama, yönlendirme denetimi, sınırlı okuma).
type ProbeResult struct {
	Status    int
	Body      string
	Truncated bool
	FinalURL  string
}

// Probe bkz. ProbeResult.
func (c *Client) Probe(ctx context.Context, rawURL string) (*ProbeResult, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("geçersiz adres: %w", err)
	}
	if err := checkURL(u); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("Accept", "text/html,application/json;q=0.9,*/*;q=0.1")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, c.MaxBytes+1))
	if err != nil {
		return nil, err
	}
	trunc := int64(len(raw)) > c.MaxBytes
	if trunc {
		raw = raw[:c.MaxBytes]
	}
	return &ProbeResult{Status: resp.StatusCode, Body: strings.ToValidUTF8(string(raw), "\uFFFD"), Truncated: trunc, FinalURL: resp.Request.URL.String()}, nil
}
