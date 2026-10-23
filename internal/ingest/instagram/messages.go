package instagram

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/MustafaKemal0146/joe/internal/ingest/zipfs"
	"github.com/MustafaKemal0146/joe/internal/model"
)

type igThread struct {
	Participants []struct {
		Name string `json:"name"`
	} `json:"participants"`
	Messages []igMessage `json:"messages"`
	Title    string      `json:"title"`
}

type igMessage struct {
	SenderName  string `json:"sender_name"`
	TimestampMS int64  `json:"timestamp_ms"`
	Content     string `json:"content"`
	IsUnsent    bool   `json:"is_unsent"`
	CallSeconds *int64 `json:"call_duration"`
	Reactions   []struct {
		Reaction string `json:"reaction"`
		Actor    string `json:"actor"`
	} `json:"reactions"`
	Share *struct {
		Link      string `json:"link"`
		ShareText string `json:"share_text"`
		Owner     string `json:"original_content_owner"`
	} `json:"share"`
	Photos     []json.RawMessage `json:"photos"`
	Videos     []json.RawMessage `json:"videos"`
	AudioFiles []json.RawMessage `json:"audio_files"`
	Gifs       []json.RawMessage `json:"gifs"`
	Files      []json.RawMessage `json:"files"`
	Sticker    json.RawMessage   `json:"sticker"`
}

var unsentPhrases = []string{
	"unsent a message", "mesajın gönderimini geri aldı", "bir mesajı geri aldı", "bu mesaj silindi", "mesaj gönderimi geri alındı",
}

var missedCallPhrases = []string{"missed a call", "missed a video call", "cevapsız", "arayamadı"}

func containsAny(s string, subs []string) bool {
	l := strings.ToLower(s)
	for _, x := range subs {
		if strings.Contains(l, x) {
			return true
		}
	}
	return false
}

func parseThread(fsys fs.FS, parts []string) (model.Conversation, error) {
	sort.Strings(parts)
	conv := model.Conversation{Platform: "instagram"}
	seen := map[string]bool{}
	for _, p := range parts {
		data, err := zipfs.ReadFileLimited(fsys, p, maxJSONBytes)
		if err != nil {
			return conv, err
		}
		var th igThread
		if err := json.Unmarshal(data, &th); err != nil {
			return conv, fmt.Errorf("%s: geçersiz JSON", p)
		}
		if conv.Title == "" {
			conv.Title = FixMojibake(th.Title)
		}
		for _, pt := range th.Participants {
			n := FixMojibake(pt.Name)
			if n != "" && !seen[n] {
				seen[n] = true
				conv.Participants = append(conv.Participants, n)
			}
		}
		for _, m := range th.Messages {
			conv.Messages = append(conv.Messages, convertMessage(m))
		}
	}
	sort.SliceStable(conv.Messages, func(i, j int) bool { return conv.Messages[i].TS.Before(conv.Messages[j].TS) })
	for _, m := range conv.Messages {
		if m.Sender != "" && !seen[m.Sender] {
			seen[m.Sender] = true
			conv.Participants = append(conv.Participants, m.Sender)
		}
	}
	conv.IsGroup = len(conv.Participants) > 2
	return conv, nil
}

func convertMessage(m igMessage) model.Message {
	out := model.Message{Sender: FixMojibake(m.SenderName), TS: tsOf(m.TimestampMS), Text: FixMojibake(m.Content)}
	for _, r := range m.Reactions {
		out.Reactions = append(out.Reactions, model.Reaction{Actor: FixMojibake(r.Actor), Emoji: FixMojibake(r.Reaction)})
	}
	meta := map[string]any{}
	switch {
	case m.IsUnsent || (out.Text != "" && containsAny(out.Text, unsentPhrases) && len(out.Text) < 120):
		out.Kind, out.Text = model.KindUnsent, ""
	case m.CallSeconds != nil:
		out.Kind = model.KindCall
		meta["seconds"] = *m.CallSeconds
		if *m.CallSeconds == 0 {
			out.Kind = model.KindMissedCall
		}
	case containsAny(out.Text, missedCallPhrases) && len(out.Text) < 120:
		out.Kind = model.KindMissedCall
	case m.Share != nil:
		out.Kind = model.KindShare
		if m.Share.Link != "" {
			meta["link"] = m.Share.Link
		}
		if m.Share.Owner != "" {
			meta["owner"] = FixMojibake(m.Share.Owner)
		}
		if out.Text == "" {
			out.Text = FixMojibake(m.Share.ShareText)
		}
	case len(m.Photos)+len(m.Videos)+len(m.AudioFiles)+len(m.Gifs)+len(m.Files) > 0 || len(m.Sticker) > 0:
		out.Kind = model.KindMedia
		switch {
		case len(m.Photos) > 0:
			meta["media"] = "photo"
		case len(m.Videos) > 0:
			meta["media"] = "video"
		case len(m.AudioFiles) > 0:
			meta["media"] = "audio"
		case len(m.Gifs) > 0:
			meta["media"] = "gif"
		case len(m.Sticker) > 0:
			meta["media"] = "sticker"
		default:
			meta["media"] = "file"
		}
	case isReactionText(out.Text):
		out.Kind = model.KindReaction
	case out.Text == "":
		out.Kind = model.KindSystem
	default:
		out.Kind = model.KindText
	}
	if len(meta) > 0 {
		out.Meta = meta
	}
	return out
}

// isReactionText eski dışa aktarımlarda tepkinin mesaj olarak yazıldığı kalıpları yakalar.
func isReactionText(s string) bool {
	l := strings.ToLower(s)
	return len(l) < 160 && (strings.HasPrefix(l, "reacted ") || strings.HasSuffix(l, "to your message") ||
		strings.HasSuffix(l, "bir mesaja tepki verdi") || strings.Contains(l, "mesajınıza tepki verdi") ||
		strings.HasPrefix(l, "liked a message"))
}
