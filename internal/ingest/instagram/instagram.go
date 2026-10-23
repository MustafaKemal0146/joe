// Package instagram, Instagram "Bilgilerini indir" JSON dışa aktarımını kanonik modele çevirir.
//
// Dışa aktarımın klasör düzeni sürümden sürüme değiştiği için dosyalar sabit yol yerine dosya
// adı ve üst klasör ipuçlarıyla bulunur; kayıtlar ise anahtar adlarına göre (string_list_data,
// string_map_data, label_values) genel bir ayrıştırıcıyla okunur. Tanınmayan dosyalar sessizce
// yutulmaz, uyarı olarak raporlanır.
package instagram

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MustafaKemal0146/joe/internal/exif"
	"github.com/MustafaKemal0146/joe/internal/ingest/zipfs"
	"github.com/MustafaKemal0146/joe/internal/model"
)

const maxJSONBytes = 512 << 20

// FixMojibake, Instagram'ın UTF-8 baytları latin-1 kaçışlarıyla yazması yüzünden bozulan metni
// ("Ã¼" → "ü") onarır. Metin zaten doğruysa olduğu gibi döner.
func FixMojibake(s string) string {
	high := false
	for _, r := range s {
		if r > 0xFF {
			return s
		}
		if r >= 0x80 {
			high = true
		}
	}
	if !high {
		return s
	}
	b := make([]byte, 0, len(s))
	for _, r := range s {
		b = append(b, byte(r))
	}
	if !utf8.Valid(b) {
		return s
	}
	return string(b)
}

// tsOf saniye ya da milisaniye cinsinden zaman damgasını time.Time'a çevirir.
func tsOf(v int64) time.Time {
	if v <= 0 {
		return time.Time{}
	}
	if v > 1e12 {
		return time.UnixMilli(v).UTC()
	}
	return time.Unix(v, 0).UTC()
}

// ---- Genel kayıt ayrıştırıcı ----

type field struct {
	Value string
	Href  string
	TS    int64
}

type entry struct {
	Title  string
	Value  string
	Href   string
	TS     int64
	Fields map[string]field // küçük harf anahtar
}

func (e entry) get(keys ...string) (field, bool) {
	for _, k := range keys {
		if f, ok := e.Fields[strings.ToLower(k)]; ok {
			return f, true
		}
	}
	return field{}, false
}

func (e entry) str(keys ...string) string {
	f, _ := e.get(keys...)
	return FixMojibake(f.Value)
}

// handle bir kaydın hesap adını döndürür: değer, başlık ya da bağlantının son parçası.
func (e entry) handle() string {
	for _, c := range []string{e.Value, e.Title} {
		if c = strings.TrimSpace(FixMojibake(c)); c != "" {
			return strings.TrimPrefix(c, "@")
		}
	}
	if e.Href != "" {
		h := strings.TrimRight(e.Href, "/")
		if i := strings.LastIndex(h, "/"); i >= 0 && i+1 < len(h) {
			return h[i+1:]
		}
	}
	return ""
}

func (e entry) when() time.Time {
	if e.TS > 0 {
		return tsOf(e.TS)
	}
	for _, f := range e.Fields {
		if f.TS > 0 {
			return tsOf(f.TS)
		}
	}
	return time.Time{}
}

type rawObj = map[string]any

func asInt(v any) int64 {
	switch t := v.(type) {
	case float64:
		return int64(t)
	case json.Number:
		n, _ := t.Int64()
		return n
	}
	return 0
}

func asStr(v any) string {
	s, _ := v.(string)
	return s
}

func asList(v any) []any { l, _ := v.([]any); return l }
func asObj(v any) rawObj { o, _ := v.(map[string]any); return o }

// parseEntries kök dizi ya da {"anahtar":[...]} biçimindeki bir dosyadan kayıtları çıkarır.
func parseEntries(data []byte) ([]entry, error) {
	var root any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	var arrays [][]any
	switch r := root.(type) {
	case []any:
		arrays = append(arrays, r)
	case map[string]any:
		keys := make([]string, 0, len(r))
		for k := range r {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if l := asList(r[k]); l != nil {
				arrays = append(arrays, l)
			}
		}
	}
	var out []entry
	for _, arr := range arrays {
		for _, item := range arr {
			obj := asObj(item)
			if obj == nil {
				continue
			}
			out = append(out, entriesFromObject(obj)...)
		}
	}
	return out, nil
}

func entriesFromObject(o rawObj) []entry {
	title := asStr(o["title"])
	var out []entry
	if sld := asList(o["string_list_data"]); len(sld) > 0 {
		for _, it := range sld {
			m := asObj(it)
			out = append(out, entry{Title: title, Value: asStr(m["value"]), Href: asStr(m["href"]), TS: asInt(m["timestamp"])})
		}
		return out
	}
	fields := map[string]field{}
	if smd := asObj(o["string_map_data"]); smd != nil {
		for k, v := range smd {
			m := asObj(v)
			fields[strings.ToLower(k)] = field{Value: asStr(m["value"]), Href: asStr(m["href"]), TS: asInt(m["timestamp"])}
		}
	}
	if lv := asList(o["label_values"]); lv != nil {
		for _, it := range lv {
			m := asObj(it)
			label := asStr(m["label"])
			if label == "" {
				// Yeni biçimde iç içe gruplar vardır: {"title":"Owner","dict":[{"dict":[{"label":"Username",…}]}]}.
				// Alanlar "grup.etiket" anahtarıyla düzleştirilir (ör. "owner.username").
				if t := strings.ToLower(asStr(m["title"])); t != "" {
					flattenGroup(fields, t, asList(m["dict"]), 0)
				}
				continue
			}
			fields[strings.ToLower(label)] = field{Value: asStr(m["value"]), Href: asStr(m["href"]), TS: asInt(m["timestamp_value"])}
		}
	}
	e := entry{Title: title, Value: asStr(o["value"]), Href: asStr(o["href"]), TS: asInt(o["timestamp"]), Fields: fields}
	if len(fields) > 0 || e.Value != "" || e.Title != "" || e.TS > 0 {
		out = append(out, e)
	}
	return out
}

// flattenGroup iç içe etiket gruplarını "grup.etiket" alanlarına açar. Derinlik sınırlıdır: kötü
// niyetli ya da bozuk bir dosya sonsuz iç içe yapı kuramaz.
func flattenGroup(fields map[string]field, prefix string, items []any, depth int) {
	if depth > 4 {
		return
	}
	for _, it := range items {
		m := asObj(it)
		if label := asStr(m["label"]); label != "" {
			key := prefix + "." + strings.ToLower(label)
			if _, dup := fields[key]; !dup { // ilk değer kalır (ör. çok sayıda hashtag)
				fields[key] = field{Value: asStr(m["value"]), Href: asStr(m["href"]), TS: asInt(m["timestamp_value"])}
			}
			continue
		}
		flattenGroup(fields, prefix, asList(m["dict"]), depth+1)
	}
}

// displayName kaydın görünen adını döndürür ("Name" ya da iç içe "Owner.Name" etiketi); yoksa "".
// Sohbet listesindeki görünen adlarla kesin eşleştirme için olayın metnine yazılır.
func (e entry) displayName() string {
	for _, k := range []string{"owner.name", "name"} {
		if f, ok := e.Fields[k]; ok {
			if v := strings.TrimSpace(FixMojibake(f.Value)); v != "" {
				return v
			}
		}
	}
	return ""
}

// account kaydın ilgili olduğu hesabı bulur: iç içe "Owner.Username", düz "Username" etiketi, sonra
// değer/başlık/bağlantı sırasıyla (eski biçimler).
func (e entry) account() string {
	for _, k := range []string{"owner.username", "username"} {
		if f, ok := e.Fields[k]; ok {
			if v := strings.TrimSpace(strings.TrimPrefix(FixMojibake(f.Value), "@")); v != "" {
				return v
			}
		}
	}
	if f, ok := e.Fields["url"]; ok && e.Value == "" && e.Title == "" && f.Href == "" {
		// yalnızca "URL" etiketi taşıyan kayıt: bağlantının son parçası hesap adıdır
		h := strings.TrimRight(f.Value, "/")
		if i := strings.LastIndex(h, "/"); i >= 0 && i+1 < len(h) && strings.Contains(h, "instagram.com/") && !strings.Contains(h, "/p/") && !strings.Contains(h, "/reel/") {
			return h[i+1:]
		}
	}
	// Eski biçimde başlık hesabın adıdır, değer ise başka bir şey olabilir (ör. beğeni simgesi);
	// bu yüzden başlık değerden önce gelir.
	if t := strings.TrimSpace(strings.TrimPrefix(FixMojibake(e.Title), "@")); t != "" {
		return t
	}
	return e.handle()
}

// ---- Dosya sınıflandırma ----

type fileClass int

const (
	clsUnknown fileClass = iota
	clsMessage
	clsProfile
	clsRelationship
	clsLikePost
	clsLikeComment
	clsComment
	clsSaved
	clsStoryLike
	clsStoryInteract
	clsSearchAccount
	clsSearchKeyword
	clsLogin
	clsMedia
	clsProfileChange
	clsInterest
	clsView
)

type fileRule struct {
	cls  fileClass
	kind string
	test func(base, dir string) bool
}

func has(sub ...string) func(base, dir string) bool {
	return func(base, dir string) bool {
		for _, s := range sub {
			if strings.Contains(base, s) {
				return true
			}
		}
		return false
	}
}

var rules = []fileRule{
	{clsRelationship, model.EvUnfollowed, has("recently_unfollowed")},
	{clsRelationship, model.EvFollowRequest, has("pending_follow_requests", "follow_requests_sent", "recent_follow_requests")},
	{clsRelationship, model.EvCloseFriend, has("close_friends")},
	{clsRelationship, model.EvBlocked, has("blocked_profiles")},
	{clsRelationship, model.EvRestricted, has("restricted_profiles")},
	{clsRelationship, model.EvFollower, func(b, _ string) bool { return strings.HasPrefix(b, "followers") }},
	{clsRelationship, model.EvFollowing, func(b, _ string) bool { return b == "following.json" }},
	{clsLikeComment, model.EvLikeComment, has("liked_comments")},
	{clsLikePost, model.EvLikePost, has("liked_posts")},
	{clsComment, model.EvComment, func(b, _ string) bool {
		return strings.Contains(b, "post_comments") || strings.Contains(b, "reels_comments")
	}},
	{clsSaved, model.EvSaved, has("saved_posts")},
	{clsStoryLike, model.EvStoryLike, has("story_likes")},
	{clsStoryInteract, model.EvStoryView, has("stories_viewed")},
	{clsStoryInteract, model.EvStoryInteraction, has("emoji_sliders", "polls.json", "questions.json", "quizzes.json", "countdowns")},
	{clsSearchAccount, model.EvSearchAccount, has("account_searches", "profile_searches")},
	{clsSearchKeyword, model.EvSearchKeyword, has("word_or_phrase_searches")},
	{clsLogin, model.EvLogin, func(b, d string) bool { return b == "login_activity.json" }},
	{clsMedia, model.EvPost, func(b, d string) bool {
		return strings.HasPrefix(b, "posts_") && (strings.Contains(d, "/media") || strings.Contains(d, "/content"))
	}},
	{clsMedia, model.EvStory, func(b, d string) bool {
		return b == "stories.json" && (strings.Contains(d, "/media") || strings.Contains(d, "/content"))
	}},
	{clsMedia, model.EvReel, func(b, d string) bool {
		return b == "reels.json" && (strings.Contains(d, "/media") || strings.Contains(d, "/content"))
	}},
	{clsProfileChange, model.EvProfileChange, has("profile_changes.json")},
	{clsInterest, model.EvInterest, has("your_topics.json")},
	{clsView, model.EvPostView, has("posts_viewed")},
	{clsView, model.EvVideoView, has("videos_watched")},
	{clsView, model.EvSuggestionView, has("suggested_accounts_viewed")},
	{clsProfile, "", has("personal_information.json")},
}

func classify(p string) (fileClass, string) {
	low := strings.ToLower(p)
	base, dir := path.Base(low), path.Dir(low)
	if strings.HasSuffix(base, ".json") && strings.HasPrefix(base, "message_") &&
		(strings.Contains(dir, "/inbox/") || strings.Contains(dir, "/archived_threads/") ||
			strings.Contains(dir, "/filtered_threads/") || strings.Contains(dir, "/message_requests/")) {
		return clsMessage, ""
	}
	if !strings.HasSuffix(base, ".json") {
		return clsUnknown, ""
	}
	for _, r := range rules {
		if r.test(base, dir) {
			return r.cls, r.kind
		}
	}
	return clsUnknown, ""
}

// ---- Ayrıştırma ----

// Parse Instagram dışa aktarımını okur.
func Parse(src *zipfs.Source) (*model.Import, error) {
	imp := &model.Import{Platform: "instagram", Label: src.Name, Stats: map[string]int{}, Warnings: append([]string(nil), src.Warnings...)}
	fsys := src.FS

	var files []string
	htmlCount := 0
	if err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		files = append(files, p)
		if l := strings.ToLower(p); strings.HasSuffix(l, ".html") && strings.Contains(l, "messages") {
			htmlCount++
		}
		return nil
	}); err != nil {
		return nil, err
	}
	sort.Strings(files)

	threads := map[string][]string{}
	var profile entry
	hasProfile := false
	var skipped []string
	unknown := map[string]int{}

	for _, p := range files {
		cls, kind := classify(p)
		switch cls {
		case clsUnknown:
			if strings.HasSuffix(strings.ToLower(p), ".json") {
				unknown[path.Base(strings.ToLower(p))]++
			}
			continue
		case clsMessage:
			threads[path.Dir(p)] = append(threads[path.Dir(p)], p)
			continue
		}
		data, err := zipfs.ReadFileLimited(fsys, p, maxJSONBytes)
		if err != nil {
			skipped = append(skipped, fmt.Sprintf("%s: %v", p, err))
			continue
		}
		if cls == clsMedia {
			evs, err := parseMedia(data, kind)
			if err != nil {
				skipped = append(skipped, fmt.Sprintf("%s: geçersiz JSON", p))
				continue
			}
			imp.Events = append(imp.Events, evs...)
			imp.Stats["event:"+kind] += len(evs)
			continue
		}
		entries, err := parseEntries(data)
		if err != nil {
			skipped = append(skipped, fmt.Sprintf("%s: geçersiz JSON", p))
			continue
		}
		if cls == clsProfile {
			for _, e := range entries {
				if _, ok := e.get("username"); ok {
					profile, hasProfile = e, true
					break
				}
			}
			continue
		}
		n := addEvents(imp, cls, kind, entries)
		imp.Stats["event:"+kind] += n
	}

	scanMediaEXIF(imp, fsys, files)

	for dir, parts := range threads {
		conv, err := parseThread(fsys, parts)
		if err != nil {
			skipped = append(skipped, fmt.Sprintf("%s: %v", dir, err))
			continue
		}
		if len(conv.Messages) > 0 {
			imp.Conversations = append(imp.Conversations, conv)
		}
	}
	sort.Slice(imp.Conversations, func(i, j int) bool { return len(imp.Conversations[i].Messages) > len(imp.Conversations[j].Messages) })

	if hasProfile {
		imp.OwnerName = profile.str("name")
		imp.OwnerHandle = strings.TrimPrefix(profile.str("username"), "@")
	}
	if imp.OwnerName == "" {
		imp.OwnerName = guessOwner(imp.Conversations)
		imp.OwnerGuessed = imp.OwnerName != ""
	}
	if imp.OwnerName == "" && imp.OwnerHandle == "" {
		imp.Warnings = append(imp.Warnings, "Hesap sahibi bulunamadı; içe aktarmadan sonra elle seçilmeli.")
	}

	for i := range imp.Conversations {
		for _, m := range imp.Conversations[i].Messages {
			imp.Stats["messages"]++
			_ = m
		}
		imp.Stats["conversations"]++
	}
	if htmlCount > 0 && len(imp.Conversations) == 0 {
		imp.Warnings = append(imp.Warnings, "Dışa aktarım HTML biçiminde görünüyor; Joe JSON biçimini okur. Instagram'dan 'Biçim: JSON' seçerek yeniden indir.")
	}
	if w := unknownSummary(unknown); w != "" {
		imp.Warnings = append(imp.Warnings, w)
	}
	if len(skipped) > 0 {
		imp.Warnings = append(imp.Warnings, skipped...)
	}
	if len(imp.Conversations) == 0 && len(imp.Events) == 0 {
		return nil, fmt.Errorf("Instagram dışa aktarımı tanınmadı: mesaj ya da etkinlik dosyası bulunamadı")
	}
	sort.SliceStable(imp.Events, func(i, j int) bool { return imp.Events[i].TS.Before(imp.Events[j].TS) })
	return imp, nil
}

func guessOwner(convs []Conv) string {
	count := map[string]int{}
	for _, c := range convs {
		for _, p := range c.Participants {
			count[p]++
		}
	}
	best, bestN := "", 0
	for p, n := range count {
		if n > bestN || (n == bestN && p < best) {
			best, bestN = p, n
		}
	}
	if bestN < 2 {
		return ""
	}
	return best
}

// Conv yerel takma ad (guessOwner imzasını kısa tutar).
type Conv = model.Conversation

func addEvents(imp *model.Import, cls fileClass, kind string, entries []entry) int {
	n := 0
	add := func(ev model.Event) {
		imp.Events = append(imp.Events, ev)
		n++
	}
	for _, e := range entries {
		t := e.when()
		switch cls {
		case clsRelationship:
			h := e.account()
			if h == "" {
				continue
			}
			ev := model.Event{Kind: kind, TS: t, Actor: model.Owner, Counterpart: h, Text: e.displayName()}
			if kind == model.EvFollower {
				ev.Actor, ev.Counterpart = h, model.Owner
			}
			add(ev)
		case clsLikePost, clsLikeComment, clsStoryLike:
			h := e.account()
			if h == "" {
				continue
			}
			add(model.Event{Kind: kind, TS: t, Actor: model.Owner, Counterpart: h, Text: e.displayName(), Meta: hrefMeta(e.Href)})
		case clsComment:
			owner := e.str("media owner", "owner")
			add(model.Event{Kind: kind, TS: t, Actor: model.Owner, Counterpart: strings.TrimPrefix(owner, "@"), Text: e.str("comment")})
		case clsSaved:
			owner := e.Title
			if owner == "" {
				owner = e.account()
			}
			f, _ := e.get("saved on")
			ts := tsOf(f.TS)
			if f.TS == 0 {
				ts = t
			}
			add(model.Event{Kind: kind, TS: ts, Actor: model.Owner, Counterpart: FixMojibake(owner), Text: e.displayName(), Meta: hrefMeta(f.Href)})
		case clsStoryInteract:
			h := e.account()
			add(model.Event{Kind: kind, TS: t, Actor: model.Owner, Counterpart: h, Text: firstNonEmpty(e.displayName(), e.str("answer", "response", "emoji", "question", "text"))})
		case clsView:
			// Yazar bilgisi her biçimde yoktur; olay yine de sayılır (izleme yoğunluğu için).
			add(model.Event{Kind: kind, TS: t, Actor: model.Owner, Counterpart: e.account()})
		case clsSearchAccount, clsSearchKeyword:
			v := e.str("search")
			if v == "" {
				v = e.account()
			}
			if v == "" {
				continue
			}
			add(model.Event{Kind: kind, TS: t, Actor: model.Owner, Counterpart: strings.TrimPrefix(v, "@"), Text: v})
		case clsProfileChange:
			what := e.str("changed", "field")
			from, to := e.str("previous value", "old value"), e.str("new value")
			if what == "" && from == "" && to == "" {
				continue
			}
			add(model.Event{Kind: kind, TS: t, Actor: model.Owner, Text: what, Meta: map[string]any{"from": from, "to": to}})
		case clsInterest:
			name := e.str("name")
			if name == "" {
				name = e.handle()
			}
			if name == "" {
				continue
			}
			add(model.Event{Kind: kind, Actor: model.Owner, Text: name})
		case clsLogin:
			meta := map[string]any{}
			if v := e.str("ip address", "ip"); v != "" {
				meta["ip"] = v
			}
			if v := e.str("user agent", "user-agent"); v != "" {
				meta["user_agent"] = v
			}
			if v := e.str("language code"); v != "" {
				meta["language"] = v
			}
			if v := e.str("cookie name"); v != "" {
				meta["cookie"] = v
			}
			if len(meta) == 0 && t.IsZero() {
				continue
			}
			add(model.Event{Kind: kind, TS: t, Actor: model.Owner, Meta: meta})
		}
	}
	return n
}

func hrefMeta(h string) map[string]any {
	if h == "" {
		return nil
	}
	return map[string]any{"href": h}
}

// parseMedia kendi gönderi/hikâye/reels dosyalarından paylaşım zamanlarını çıkarır. Kök dizi ya da
// {"ig_stories": [...]} gibi bir nesne olabilir; her öğede media[] ve creation_timestamp bulunur.
func parseMedia(data []byte, kind string) ([]model.Event, error) {
	var root any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	var items []any
	switch r := root.(type) {
	case []any:
		items = r
	case map[string]any:
		keys := make([]string, 0, len(r))
		for k := range r {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			items = append(items, asList(r[k])...)
		}
	}
	var out []model.Event
	for _, it := range items {
		o := asObj(it)
		if o == nil {
			continue
		}
		media := asList(o["media"])
		ts := asInt(o["creation_timestamp"])
		caption := FixMojibake(asStr(o["title"]))
		if ts == 0 || caption == "" {
			for _, m := range media {
				mo := asObj(m)
				if ts == 0 {
					ts = asInt(mo["creation_timestamp"])
				}
				if caption == "" {
					caption = FixMojibake(asStr(mo["title"]))
				}
			}
		}
		if ts == 0 {
			continue
		}
		n := len(media)
		if n == 0 {
			n = 1
		}
		out = append(out, model.Event{Kind: kind, TS: tsOf(ts), Actor: model.Owner, Text: caption, Meta: map[string]any{"media": n}})
	}
	return out, nil
}

const (
	maxEXIFFiles = 5000
	exifHeadSize = 128 << 10
)

// scanMediaEXIF arşivdeki JPEG dosyalarının yalnızca ilk baytlarını okuyup EXIF özeti çıkarır.
// Instagram çoğu paylaşımın EXIF'ini siler; bu yüzden bulunmaması olağandır ve uyarı değildir.
func scanMediaEXIF(imp *model.Import, fsys fs.FS, files []string) {
	scanned, found, gps := 0, 0, 0
	for _, p := range files {
		l := strings.ToLower(p)
		if !(strings.HasSuffix(l, ".jpg") || strings.HasSuffix(l, ".jpeg")) {
			continue
		}
		if scanned >= maxEXIFFiles {
			imp.Warnings = append(imp.Warnings, fmt.Sprintf("Medya metadata taraması ilk %d dosyayla sınırlandı.", maxEXIFFiles))
			break
		}
		scanned++
		f, err := fsys.Open(p)
		if err != nil {
			continue
		}
		head := make([]byte, exifHeadSize)
		n, _ := io.ReadFull(f, head)
		f.Close()
		info, err := exif.Parse(head[:n])
		if err != nil {
			continue
		}
		found++
		meta := map[string]any{"file": p}
		if info.Make != "" {
			meta["make"] = info.Make
		}
		if info.Model != "" {
			meta["model"] = info.Model
		}
		if info.Software != "" {
			meta["software"] = info.Software
		}
		if info.HasGPS {
			gps++
			meta["lat"], meta["lon"] = info.Lat, info.Lon
		}
		ev := model.Event{Kind: model.EvMediaMeta, Actor: model.Owner, Text: strings.TrimSpace(info.Make + " " + info.Model), Meta: meta}
		if info.HasTaken {
			ev.TS = info.Taken
		}
		imp.Events = append(imp.Events, ev)
	}
	if scanned > 0 {
		imp.Stats["media_scanned"] = scanned
	}
	if found > 0 {
		imp.Stats["event:"+model.EvMediaMeta] = found
		imp.Stats["media_gps"] = gps
	}
}

func firstNonEmpty(v ...string) string {
	for _, x := range v {
		if x != "" {
			return x
		}
	}
	return ""
}

// unknownSummary okunmayan JSON dosyalarının adlarını (içerik değil) özetler; hangi verinin atlandığı görünsün diye.
func unknownSummary(m map[string]int) string {
	if len(m) == 0 {
		return ""
	}
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool {
		return m[names[i]] > m[names[j]] || (m[names[i]] == m[names[j]] && names[i] < names[j])
	})
	total := 0
	for _, c := range m {
		total += c
	}
	if len(names) > 12 {
		names = names[:12]
	}
	parts := make([]string, len(names))
	for i, n := range names {
		parts[i] = fmt.Sprintf("%s ×%d", n, m[n])
	}
	return fmt.Sprintf("Okunmayan %d JSON dosyası var (içerikleri analize girmedi); en çok: %s.", total, strings.Join(parts, ", "))
}
