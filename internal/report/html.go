package report

import (
	"html"
	"regexp"
	"strings"
)

var (
	reBold = regexp.MustCompile(`\*\*(.+?)\*\*`)
	reCode = regexp.MustCompile("`([^`]+)`")
)

// inline satır içi biçimleri uygular: önce HTML kaçışı, sonra **kalın** ve `kod`.
func inline(s string) string {
	s = html.EscapeString(s)
	s = reBold.ReplaceAllString(s, "<strong>$1</strong>")
	return reCode.ReplaceAllString(s, "<code>$1</code>")
}

// splitRow bir tablo satırını hücrelere böler; \| kaçışlı dikey çizgi hücrenin içinde kalır.
func splitRow(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	var cells []string
	var cur strings.Builder
	for i := 0; i < len(line); i++ {
		if line[i] == '\\' && i+1 < len(line) && line[i+1] == '|' {
			cur.WriteByte('|')
			i++
			continue
		}
		if line[i] == '|' {
			cells = append(cells, strings.TrimSpace(cur.String()))
			cur.Reset()
			continue
		}
		cur.WriteByte(line[i])
	}
	return append(cells, strings.TrimSpace(cur.String()))
}

func isSeparator(line string) bool {
	t := strings.Trim(strings.TrimSpace(line), "|")
	t = strings.NewReplacer("-", "", ":", "", " ", "", "|", "").Replace(t)
	return t == "" && strings.Contains(line, "-")
}

const css = `
:root{color-scheme:light}
*{box-sizing:border-box}
body{font:14px/1.55 -apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,"Helvetica Neue",Arial,sans-serif;color:#1d1d1f;max-width:820px;margin:32px auto;padding:0 24px}
h1{font-size:26px;margin:0 0 12px;letter-spacing:-.02em}
h2{font-size:17px;margin:28px 0 8px;padding-bottom:4px;border-bottom:1px solid #e5e5ea;break-after:avoid}
blockquote{margin:10px 0;padding:8px 14px;border-left:3px solid #ff9f0a;background:#fff8ec;border-radius:0 8px 8px 0;font-size:13px}
table{border-collapse:collapse;width:100%;margin:10px 0;font-size:12.5px;break-inside:avoid}
th,td{border:1px solid #d1d1d6;padding:5px 8px;text-align:left;vertical-align:top}
th{background:#f5f5f7}
ul{padding-left:20px}li{margin:2px 0}li.sub{margin-left:20px;list-style:circle;color:#48484a}
h3{font-size:15px;margin:20px 0 6px}h4{font-size:14px;margin:16px 0 4px;color:#3a3a3c}h5{font-size:13px;margin:12px 0 2px}
code{background:#f2f2f7;border-radius:4px;padding:1px 5px;font-size:12px}
hr{border:0;border-top:1px solid #e5e5ea;margin:24px 0}
.bar{display:flex;gap:8px;align-items:center;margin-bottom:18px;padding:10px 14px;background:#f5f5f7;border-radius:12px;font-size:13px}
button{font:inherit;padding:6px 14px;border-radius:999px;border:0;background:#0071e3;color:#fff;cursor:pointer}
@media print{.bar{display:none}body{margin:0;max-width:none;padding:0}@page{margin:16mm}}
`

// HTML raporun yazdırmaya uygun, kendi başına açılabilen HTML sürümünü üretir. İçerik Markdown
// ile birebir aynıdır (aynı girdiyle aynı çıktı); PDF için tarayıcının "Yazdır → PDF olarak kaydet"i kullanılır.
func HTML(in Input) string {
	md := Markdown(in)
	var b strings.Builder
	title := html.EscapeString(in.Person.Name) + " — kişi dosyası raporu"
	b.WriteString("<!doctype html>\n<html lang=\"tr\"><head><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width,initial-scale=1\"><meta name=\"robots\" content=\"noindex\">")
	b.WriteString("<title>" + title + "</title><style>" + css + "</style></head><body>\n")
	b.WriteString(`<div class="bar"><button id="print" type="button">PDF olarak kaydet</button><span>Açılan pencerede hedef olarak "PDF olarak kaydet"i seç.</span></div>` + "\n")

	lines := strings.Split(md, "\n")
	for i := 0; i < len(lines); i++ {
		ln := lines[i]
		switch {
		case strings.TrimSpace(ln) == "":
		case strings.HasPrefix(ln, "# "):
			b.WriteString("<h1>" + inline(ln[2:]) + "</h1>\n")
		case strings.HasPrefix(ln, "##### "):
			b.WriteString("<h5>" + inline(ln[6:]) + "</h5>\n")
		case strings.HasPrefix(ln, "#### "):
			b.WriteString("<h4>" + inline(ln[5:]) + "</h4>\n")
		case strings.HasPrefix(ln, "### "):
			b.WriteString("<h3>" + inline(ln[4:]) + "</h3>\n")
		case strings.HasPrefix(ln, "## "):
			b.WriteString("<h2>" + inline(ln[3:]) + "</h2>\n")
		case ln == "---":
			b.WriteString("<hr>\n")
		case strings.HasPrefix(ln, ">"):
			b.WriteString("<blockquote>")
			for ; i < len(lines) && strings.HasPrefix(lines[i], ">"); i++ {
				b.WriteString(inline(strings.TrimSpace(strings.TrimPrefix(lines[i], ">"))) + "<br>")
			}
			i--
			b.WriteString("</blockquote>\n")
		case strings.HasPrefix(ln, "|"):
			b.WriteString("<table>")
			header := true
			for ; i < len(lines) && strings.HasPrefix(lines[i], "|"); i++ {
				if isSeparator(lines[i]) {
					header = false
					continue
				}
				tag := "td"
				if header {
					tag = "th"
				}
				b.WriteString("<tr>")
				for _, c := range splitRow(lines[i]) {
					b.WriteString("<" + tag + ">" + inline(c) + "</" + tag + ">")
				}
				b.WriteString("</tr>")
			}
			i--
			b.WriteString("</table>\n")
		case strings.HasPrefix(ln, "- "):
			b.WriteString("<ul>")
			for ; i < len(lines) && (strings.HasPrefix(lines[i], "- ") || strings.HasPrefix(lines[i], "  - ")); i++ {
				if strings.HasPrefix(lines[i], "  - ") {
					b.WriteString("<li class=\"sub\">" + inline(lines[i][4:]) + "</li>")
					continue
				}
				b.WriteString("<li>" + inline(lines[i][2:]) + "</li>")
			}
			i--
			b.WriteString("</ul>\n")
		default:
			b.WriteString("<p>" + inline(ln) + "</p>\n")
		}
	}
	b.WriteString("<script src=\"/api/report-print.js\"></script>\n</body></html>\n")
	return b.String()
}

// PrintJS yazdır düğmesini bağlayan betiktir (CSP satır içi betiğe izin vermediği için ayrı dosya).
const PrintJS = `document.getElementById('print').addEventListener('click', function () { window.print() });
if (new URLSearchParams(location.search).get('print') === '1') { window.addEventListener('load', function () { setTimeout(function () { window.print() }, 300) }) }
`
