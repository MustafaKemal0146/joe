// Package rx, Türkçe metinde doğru çalışan düzenli ifadeler üretir. Go'nun \b sınırı yalnızca
// ASCII harflerini sözcük sayar; "ölmek", "şiddet", "hiç" gibi Türkçe harfle başlayan ya da biten
// sözcüklerde \b yanlış davranır. Burada \b, Unicode harf/rakam sınırıyla değiştirilir.
package rx

import (
	"regexp"
	"strings"
)

// boundary: metin başı/sonu ya da harf, rakam, alt çizgi olmayan bir karakter. Eşleşme bu
// karakteri de tüketir; çağıranlar yalnızca eşleşme konumunu kullandığı için sorun olmaz.
const boundary = `(?:^|$|[^\p{L}\p{N}_])`

// Compile (?i) ile derler ve her \b'yi Unicode farkında sınırla değiştirir.
func Compile(pattern string) *regexp.Regexp {
	return regexp.MustCompile(`(?i)` + strings.ReplaceAll(pattern, `\b`, boundary))
}
