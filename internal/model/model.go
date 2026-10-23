// Package model, kaynak biçiminden bağımsız kanonik veri modelidir. Instagram ve WhatsApp
// adaptörleri bu tiplere dönüştürür; analiz ve arayüz yalnız bu tipleri bilir.
package model

import (
	"strings"
	"time"
	"unicode"
)

// Owner, bir olayda hesap sahibini (dışa aktarımı yapan kişiyi) gösteren yer tutucudur.
const Owner = "$owner"

// Mesaj türleri.
const (
	KindText       = "text"
	KindMedia      = "media"
	KindShare      = "share"
	KindCall       = "call"
	KindMissedCall = "missed_call"
	KindUnsent     = "unsent"
	KindDeleted    = "deleted"
	KindSystem     = "system"
	KindReaction   = "reaction_msg"
)

// Olay türleri (mesaj olmayan etkinlik).
const (
	EvFollowing        = "following"       // sahip → karşı taraf takip ediyor
	EvFollower         = "follower"        // karşı taraf → sahibi takip ediyor
	EvUnfollowed       = "unfollowed"      // sahip takibi bıraktı
	EvFollowRequest    = "follow_request"  // bekleyen istek (sahibin gönderdiği)
	EvCloseFriend      = "close_friend"    // yakın arkadaş
	EvBlocked          = "blocked"         // engellenen
	EvRestricted       = "restricted"      // kısıtlanan
	EvLikePost         = "like_post"       // sahip, karşı tarafın gönderisini beğendi
	EvLikeComment      = "like_comment"    // sahip, karşı tarafın yorumunu beğendi
	EvComment          = "comment"         // sahip, karşı tarafın gönderisine yorum yaptı
	EvSaved            = "saved"           // sahip, karşı tarafın gönderisini kaydetti
	EvStoryLike        = "story_like"      // sahip, karşı tarafın hikâyesini beğendi
	EvStoryInteraction = "story_interact"  // anket / soru / quiz / kaydırıcı
	EvStoryView        = "story_view"      // sahip, karşı tarafın hikâyesini izledi
	EvSearchAccount    = "search_account"  // sahip, hesap aradı
	EvSearchKeyword    = "search_keyword"  // sahip, kelime/ifade aradı
	EvLogin            = "login"           // giriş etkinliği
	EvPost             = "post"            // kendi gönderisi
	EvStory            = "story"           // kendi hikâyesi
	EvReel             = "reel"            // kendi reels paylaşımı
	EvProfileChange    = "profile_change"  // kullanıcı adı, e-posta, telefon, biyografi değişimi
	EvInterest         = "interest"        // Instagram'ın atadığı ilgi alanı (zamansız)
	EvPostView         = "post_view"       // sahip bir gönderiyi görüntüledi (izleme geçmişi)
	EvVideoView        = "video_view"      // sahip bir videoyu/reel'i izledi
	EvSuggestionView   = "suggestion_view" // önerilen hesap sahibin ekranında göründü
	EvMediaMeta        = "media_meta"      // fotoğrafın EXIF özeti (cihaz, çekim zamanı, konum)
)

// Reaction bir mesaja verilen tepkidir.
type Reaction struct {
	Actor string `json:"actor"`
	Emoji string `json:"emoji"`
}

// Message tek bir mesajdır.
type Message struct {
	Sender    string         `json:"sender"`
	TS        time.Time      `json:"ts"`
	Kind      string         `json:"kind"`
	Text      string         `json:"text,omitempty"`
	Reactions []Reaction     `json:"reactions,omitempty"`
	Meta      map[string]any `json:"meta,omitempty"`
}

// Conversation bir sohbet dizisidir.
type Conversation struct {
	Title        string    `json:"title"`
	Platform     string    `json:"platform"`
	Participants []string  `json:"participants"`
	IsGroup      bool      `json:"is_group"`
	Messages     []Message `json:"messages"`
}

// Event mesaj dışı bir etkinliktir.
type Event struct {
	Kind        string         `json:"kind"`
	TS          time.Time      `json:"ts"`
	Actor       string         `json:"actor"`
	Counterpart string         `json:"counterpart,omitempty"`
	Text        string         `json:"text,omitempty"`
	Meta        map[string]any `json:"meta,omitempty"`
}

// Import bir kaynağın (ZIP/dizin/dosya) ayrıştırılmış halidir.
type Import struct {
	Platform      string         `json:"platform"` // instagram | whatsapp
	Label         string         `json:"label"`
	OwnerName     string         `json:"owner_name"`
	OwnerHandle   string         `json:"owner_handle,omitempty"`
	OwnerGuessed  bool           `json:"owner_guessed"`
	Candidates    []string       `json:"candidates,omitempty"` // sahip belirsizse aday adlar
	Conversations []Conversation `json:"conversations"`
	Events        []Event        `json:"events"`
	Warnings      []string       `json:"warnings,omitempty"`
	Stats         map[string]int `json:"stats"`
}

var normReplacer = strings.NewReplacer("İ", "i", "I", "ı", "Ş", "ş", "Ğ", "ğ", "Ü", "ü", "Ö", "ö", "Ç", "ç")

// Norm bir ad ya da kullanıcı adını karşılaştırma için sadeleştirir: küçük harf (Türkçe uyumlu),
// harf ve rakam dışındakiler atılır, baştaki @ düşer.
func Norm(s string) string {
	s = normReplacer.Replace(s)
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}
