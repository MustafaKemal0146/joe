package analytics

import (
	"regexp"
	"strings"
)

// Yakınlık ve çatışma dili sözlükleri YALNIZCA ifade sayımıdır: bir mesajda bu ifadelerden biri
// geçiyorsa sayılır. Anlam (şaka, alıntı, olumsuzlama, ironi) yorumlanmaz; sonuç bir puan ya da
// ilişki hükmü değildir. Cinsel içerik için bilerek bir sözlük yoktur.
var (
	affectionRx    = regexp.MustCompile(`(?:^|[^a-z])(canim|askim|hayatim|tatlim|guzelim|bebegim|sevgilim|gulum|bitanem|canimin ici|kalbim|tanem|melegim|seni seviyorum|seni cok seviyorum|ozledim|ozlettin|opuyorum|opucuk|sana asigim|iyi ki varsin)(?:[^a-z]|$)`)
	affectionEmoji = []string{"❤", "😘", "🥰", "😍", "💕", "💖", "💗", "💞", "😚", "💋", "🫶", "💘"}

	conflictRx = regexp.MustCompile(`(?:^|[^a-z])(yeter|bikt[a-z]*|nefret[a-z]*|sinirl[a-z]*|sinirim bozuk|kizgin[a-z]*|kizdim|kirildim|kirdin|aptal[a-z]*|salak[a-z]*|gerizekali[a-z]*|git basimdan|konusmak istemiyorum|bir daha arama|engelliyorum|yalan soyl[a-z]*|guvenmiyorum|rezil[a-z]*|defol|kavga[a-z]*|tartis(?:ma|iyoruz|mayalim|ma)[a-z]*|hakaret[a-z]*|ihanet[a-z]*)(?:[^a-z]|$)`)

	// Yalnızca anne/baba ve büyük aile hitapları: "abi/kardeşim/dayı" arkadaşlar arasında da yaygın olduğu için eklenmedi.
	kinshipAddr = map[string]string{
		"anne": "anne", "annecim": "anne", "annem": "anne", "anneciğim": "anne", "annecigim": "anne",
		"baba": "baba", "babacim": "baba", "babam": "baba", "babacığım": "baba", "babacigim": "baba",
		"anneanne": "aile", "babaanne": "aile", "dede": "aile", "nine": "aile",
	}
)

// affectionIn / conflictIn mesaj metninde ifade olup olmadığını söyler.
func affectionIn(text string) bool {
	if affectionRx.MatchString(foldASCII(text)) {
		return true
	}
	for _, e := range affectionEmoji {
		if strings.Contains(text, e) {
			return true
		}
	}
	return false
}

func conflictIn(text string) bool { return conflictRx.MatchString(foldASCII(text)) }

// firstWord mesajın ilk sözcüğünü (küçük harf, noktalamasız) döndürür.
func firstWord(text string) string {
	f := strings.Fields(foldASCII(text))
	if len(f) == 0 {
		return ""
	}
	return strings.Trim(f[0], ".,!?;:\"'()")
}
