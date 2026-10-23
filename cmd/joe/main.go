// joe: kişi dosyası, OSINT ve psikanalitik konsey için yerel çalışma alanı.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/MustafaKemal0146/joe/internal/config"
	"github.com/MustafaKemal0146/joe/internal/ingest"
	"github.com/MustafaKemal0146/joe/internal/ingest/zipfs"
	"github.com/MustafaKemal0146/joe/internal/server"
	"github.com/MustafaKemal0146/joe/internal/store"
	"github.com/MustafaKemal0146/joe/internal/version"
	"github.com/MustafaKemal0146/joe/web"
)

const usage = `joe — kişi dosyası ve konsey çalışma alanı

Kullanım:
  joe [serve] [--addr 127.0.0.1:8777] [--no-browser]   arayüzü başlatır
  joe import <zip|dizin|dosya> [parça…]                 bir kaynağı içe aktarır (çok parçalı ZIP için hepsini ver)
  joe eval [--personas jung,freud] [--yes] [--out r.md]  sentetik vakalarla persona kalitesini ölçer (para harcar)
  joe export <dosya.json>                               bütün veriyi JSON olarak dışa aktarır
  joe wipe [--yes] [--ayarlar]                          bütün veriyi siler (onay ister)
  joe version

Veri klasörü: $JOE_HOME ya da kullanıcı ayar klasörü/joe
Saat dilimi:  $JOE_TZ (varsayılan Europe/Istanbul)
`

func location() *time.Location {
	name := os.Getenv("JOE_TZ")
	if name == "" {
		name = "Europe/Istanbul"
	}
	if l, err := time.LoadLocation(name); err == nil {
		return l
	}
	return time.Local
}

func main() {
	args := os.Args[1:]
	cmd := "serve"
	if len(args) > 0 && len(args[0]) > 0 && args[0][0] != '-' {
		cmd, args = args[0], args[1:]
	}
	var err error
	switch cmd {
	case "serve":
		err = serve(args)
	case "import":
		err = importCmd(args)
	case "eval":
		err = evalCmd(args)
	case "export":
		err = exportCmd(args)
	case "wipe":
		err = wipeCmd(args, os.Stdin)
	case "version", "--version", "-v":
		fmt.Println(version.String())
	case "help":
		fmt.Print(usage)
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "joe:", err)
		os.Exit(1)
	}
}

func open() (*store.Store, *config.Holder, string, error) {
	dir, err := config.Dir()
	if err != nil {
		return nil, nil, "", err
	}
	st, err := store.Open(filepath.Join(dir, "joe.db"))
	if err != nil {
		return nil, nil, "", err
	}
	cfg, err := config.Load(filepath.Join(dir, "config.enc"))
	if err != nil {
		st.Close()
		return nil, nil, "", fmt.Errorf("ayarlar okunamadı: %w", err)
	}
	return st, cfg, dir, nil
}

func importCmd(args []string) error {
	if len(args) < 1 {
		return errors.New("kullanım: joe import <zip|dizin|dosya> [diğer parçalar…]")
	}
	st, _, _, err := open()
	if err != nil {
		return err
	}
	defer st.Close()
	src, err := zipfs.OpenMulti(args)
	if err != nil {
		return err
	}
	defer src.Close()
	imp, err := ingest.Parse(src, location())
	if err != nil {
		return err
	}
	id, err := st.SaveImport(imp, src.SHA256)
	var dup store.ErrDuplicate
	if errors.As(err, &dup) {
		fmt.Printf("Bu kaynak zaten içe aktarılmış (kaynak #%d).\n", dup.SourceID)
		return nil
	}
	if err != nil {
		return err
	}
	fmt.Printf("İçe aktarıldı: kaynak #%d (%s)\n", id, imp.Platform)
	if row, err := st.Source(id); err == nil && row.OwnerConfirmed {
		if p, err := st.EnsureOwnerPerson(row); err == nil {
			fmt.Printf("  kişi dosyası: %s\n", p.Name)
		}
	}
	fmt.Printf("  sohbet: %d, mesaj: %d, olay: %d\n", len(imp.Conversations), imp.Stats["messages"], len(imp.Events))
	if imp.OwnerName != "" {
		note := ""
		if imp.OwnerGuessed {
			note = " (tahmin)"
		}
		fmt.Printf("  hesap sahibi: %s%s\n", imp.OwnerName, note)
	} else {
		fmt.Printf("  hesap sahibi belirlenemedi; arayüzden seç. Adaylar: %v\n", imp.Candidates)
	}
	for _, w := range imp.Warnings {
		fmt.Println("  uyarı:", w)
	}
	return nil
}

func serve(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	addr := fs.String("addr", "127.0.0.1:8777", "dinleme adresi (yalnızca loopback)")
	noBrowser := fs.Bool("no-browser", false, "tarayıcıyı açma")
	if err := fs.Parse(args); err != nil {
		return err
	}
	host, _, err := net.SplitHostPort(*addr)
	if err != nil {
		return fmt.Errorf("geçersiz adres: %w", err)
	}
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return fmt.Errorf("güvenlik: Joe yalnızca loopback adresinde dinler (127.0.0.1); %q reddedildi", host)
	}
	st, cfg, dir, err := open()
	if err != nil {
		return err
	}
	defer st.Close()
	// Önceki çalışmada yarıda kalan konsey oturumları devam ettirilebilir olarak işaretlenir;
	// kullanıcı onayı olmadan otomatik para harcanmaz.
	if err := st.InterruptRunning(); err != nil {
		return err
	}
	if err := st.InterruptOsint(); err != nil {
		return err
	}
	tmp := filepath.Join(dir, "tmp")
	if err := os.MkdirAll(tmp, 0o700); err != nil {
		return err
	}
	srv := &server.Server{Store: st, Cfg: cfg, UI: web.FS(), Loc: location(), TempDir: tmp, DataDir: dir, PersonaDir: filepath.Join(dir, "personas")}
	hs := &http.Server{Addr: *addr, Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		return err
	}
	url := "http://" + ln.Addr().String()
	fmt.Println("joe çalışıyor:", url)
	fmt.Println("veri klasörü:", dir)
	if !*noBrowser {
		go openBrowser(url)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	go func() {
		<-ctx.Done()
		sh, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		hs.Shutdown(sh)
		srv.StopCouncils()
		srv.StopOSINT()
	}()
	if err := hs.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func openBrowser(url string) {
	var c *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		c = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		c = exec.Command("open", url)
	default:
		c = exec.Command("xdg-open", url)
	}
	_ = c.Start()
}

func exportCmd(args []string) error {
	if len(args) != 1 {
		return errors.New("kullanım: joe export <dosya.json>")
	}
	st, _, _, err := open()
	if err != nil {
		return err
	}
	defer st.Close()
	if _, err := os.Stat(args[0]); err == nil {
		return fmt.Errorf("%s zaten var; üzerine yazılmaz", args[0])
	}
	f, err := os.OpenFile(args[0], os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if err := st.ExportJSON(f, version.Version); err != nil {
		f.Close()
		os.Remove(args[0])
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	fmt.Println("Dışa aktarıldı:", args[0], "(yalnızca sen okuyabilirsin: izin 0600)")
	return nil
}

// confirm kullanıcıdan tam olarak beklenen kelimeyi yazmasını ister.
func confirm(in io.Reader, out io.Writer, prompt, want string) bool {
	fmt.Fprint(out, prompt)
	line, _ := bufio.NewReader(in).ReadString('\n')
	return strings.TrimSpace(line) == want
}

func wipeCmd(args []string, in io.Reader) error {
	fs := flag.NewFlagSet("wipe", flag.ContinueOnError)
	yes := fs.Bool("yes", false, "onay sormadan sil")
	settings := fs.Bool("ayarlar", false, "ayarları ve şifreleme anahtarını da sil")
	if err := fs.Parse(args); err != nil {
		return err
	}
	st, _, dir, err := open()
	if err != nil {
		return err
	}
	defer st.Close()
	if !*yes && !confirm(in, os.Stdout, "Bütün kaynaklar, mesajlar, olaylar ve kişi dosyaları silinecek. Geri alınamaz.\nOnaylamak için SİL yaz: ", "SİL") {
		return errors.New("onay verilmedi, hiçbir şey silinmedi")
	}
	if err := st.Wipe(); err != nil {
		return err
	}
	fmt.Println("Bütün veri silindi.")
	if *settings {
		for _, n := range []string{"config.enc", "config.enc.key"} {
			if err := os.Remove(filepath.Join(dir, n)); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
		fmt.Println("Ayarlar ve şifreleme anahtarı silindi.")
	}
	fmt.Println("Not: bu güvenli silme (secure erase) değildir; diskte iz kalmaması için disk şifrelemesi kullan.")
	return nil
}
