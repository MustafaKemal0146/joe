package instagram

import (
	"archive/zip"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MustafaKemal0146/joe/internal/exif/exiftest"
	"github.com/MustafaKemal0146/joe/internal/ingest/zipfs"
	"github.com/MustafaKemal0146/joe/internal/model"
)

// mojibake, Instagram'ın yaptığı gibi UTF-8 baytlarını latin-1 karakterleri sayarak yazar.
func mojibake(s string) string {
	r := make([]rune, 0, len(s))
	for _, b := range []byte(s) {
		r = append(r, rune(b))
	}
	return string(r)
}

func buildZip(t *testing.T, files map[string]any) *zipfs.Source {
	t.Helper()
	p := filepath.Join(t.TempDir(), "ig.zip")
	f, _ := os.Create(p)
	zw := zip.NewWriter(f)
	for name, v := range files {
		w, _ := zw.Create(name)
		json.NewEncoder(w).Encode(v)
	}
	zw.Close()
	f.Close()
	src, err := zipfs.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { src.Close() })
	return src
}

func TestFixMojibake(t *testing.T) {
	cases := map[string]string{
		mojibake("Gülşen çok güzel"): "Gülşen çok güzel",
		"zaten doğru İ":              "zaten doğru İ",
		"plain ascii":                "plain ascii",
		mojibake("😀"):                "😀",
	}
	for in, want := range cases {
		if got := FixMojibake(in); got != want {
			t.Errorf("FixMojibake(%q)=%q, want %q", in, got, want)
		}
	}
}

func TestParseMessagesAndOwner(t *testing.T) {
	thread := func(other string, n int) map[string]any {
		msgs := []map[string]any{}
		for i := 0; i < n; i++ {
			sender := "Ben Sahip"
			if i%2 == 1 {
				sender = other
			}
			msgs = append(msgs, map[string]any{"sender_name": mojibake(sender), "timestamp_ms": 1_700_000_000_000 + int64(i)*60_000, "content": mojibake("merhaba şu an")})
		}
		msgs = append(msgs,
			map[string]any{"sender_name": other, "timestamp_ms": 1_700_000_900_000, "is_unsent": true},
			map[string]any{"sender_name": "Ben Sahip", "timestamp_ms": 1_700_001_000_000, "call_duration": 0},
			map[string]any{"sender_name": other, "timestamp_ms": 1_700_001_100_000, "photos": []any{map[string]any{"uri": "x.jpg"}}},
			map[string]any{"sender_name": other, "timestamp_ms": 1_700_001_200_000, "share": map[string]any{"link": "https://i.example/p/1"}},
		)
		return map[string]any{
			"participants": []map[string]any{{"name": mojibake("Ben Sahip")}, {"name": mojibake(other)}},
			"messages":     msgs, "title": mojibake(other),
		}
	}
	src := buildZip(t, map[string]any{
		"your_instagram_activity/messages/inbox/ayse_1/message_1.json": thread("Ayşe", 4),
		"your_instagram_activity/messages/inbox/can_2/message_1.json":  thread("Can", 6),
	})
	imp, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	if imp.OwnerName != "Ben Sahip" || !imp.OwnerGuessed {
		t.Fatalf("sahip tahmini yanlış: %q guessed=%v", imp.OwnerName, imp.OwnerGuessed)
	}
	if len(imp.Conversations) != 2 || imp.Conversations[0].Title != "Can" {
		t.Fatalf("sohbetler: %+v", imp.Conversations)
	}
	kinds := map[string]int{}
	for _, m := range imp.Conversations[0].Messages {
		kinds[m.Kind]++
	}
	for _, k := range []string{model.KindText, model.KindUnsent, model.KindMissedCall, model.KindMedia, model.KindShare} {
		if kinds[k] == 0 {
			t.Errorf("tür bulunamadı: %s (%v)", k, kinds)
		}
	}
	if imp.Conversations[0].IsGroup {
		t.Fatal("ikili sohbet grup sayılmamalı")
	}
	if imp.Conversations[0].Messages[0].Text != "merhaba şu an" {
		t.Fatalf("Türkçe karakter onarılmadı: %q", imp.Conversations[0].Messages[0].Text)
	}
}

func TestParseRelationshipsAndActivity(t *testing.T) {
	sld := func(title, value string, ts int64) map[string]any {
		return map[string]any{"title": title, "string_list_data": []any{map[string]any{"value": value, "href": "https://www.instagram.com/" + value, "timestamp": ts}}}
	}
	src := buildZip(t, map[string]any{
		"personal_information/personal_information/personal_information.json": map[string]any{"profile_user": []any{map[string]any{"string_map_data": map[string]any{
			"Username": map[string]any{"value": "benim"}, "Name": map[string]any{"value": mojibake("Ben Sahip")}}}}},
		"connections/followers_and_following/followers_1.json": []any{sld("", "ayse_k", 1_700_000_000)},
		"connections/followers_and_following/following.json": map[string]any{"relationships_following": []any{
			map[string]any{"title": "can_y", "string_list_data": []any{map[string]any{"href": "https://www.instagram.com/_u/can_y", "timestamp": 1_700_000_100}}}}},
		"connections/followers_and_following/recently_unfollowed_profiles.json": map[string]any{"relationships_unfollowed_users": []any{sld("", "eski_biri", 1_700_000_200)}},
		"your_instagram_activity/likes/liked_posts.json":                        map[string]any{"likes_media_likes": []any{sld("ayse_k", "", 1_700_000_300)}},
		"logged_information/recent_searches/account_searches.json": map[string]any{"searches_user": []any{map[string]any{"string_map_data": map[string]any{
			"Search": map[string]any{"value": "gizemli.biri"}, "Time": map[string]any{"timestamp": 1_700_000_400}}}}},
		"security_and_login_information/login_and_account_creation/login_activity.json": map[string]any{"account_history_login_history": []any{map[string]any{"title": "", "string_map_data": map[string]any{
			"IP Address": map[string]any{"value": "203.0.113.7"}, "Time": map[string]any{"timestamp": 1_700_000_500}}}}},
	})
	imp, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	if imp.OwnerName != "Ben Sahip" || imp.OwnerHandle != "benim" || imp.OwnerGuessed {
		t.Fatalf("profil sahibi: %q @%q guessed=%v", imp.OwnerName, imp.OwnerHandle, imp.OwnerGuessed)
	}
	by := map[string]model.Event{}
	for _, e := range imp.Events {
		by[e.Kind] = e
	}
	if e := by[model.EvFollower]; e.Actor != "ayse_k" || e.Counterpart != model.Owner {
		t.Errorf("takipçi yönü yanlış: %+v", e)
	}
	if e := by[model.EvFollowing]; e.Counterpart != "can_y" || e.Actor != model.Owner {
		t.Errorf("takip edilen (başlıktan): %+v", e)
	}
	if by[model.EvUnfollowed].Counterpart != "eski_biri" {
		t.Errorf("takipten çıkma: %+v", by[model.EvUnfollowed])
	}
	if by[model.EvLikePost].Counterpart != "ayse_k" {
		t.Errorf("beğeni: %+v", by[model.EvLikePost])
	}
	if by[model.EvSearchAccount].Counterpart != "gizemli.biri" {
		t.Errorf("arama: %+v", by[model.EvSearchAccount])
	}
	if by[model.EvLogin].Meta["ip"] != "203.0.113.7" {
		t.Errorf("giriş: %+v", by[model.EvLogin])
	}
	for i := 1; i < len(imp.Events); i++ {
		if imp.Events[i].TS.Before(imp.Events[i-1].TS) {
			t.Fatal("olaylar zamana göre sıralı olmalı")
		}
	}
}

func TestUnrecognisedExportFails(t *testing.T) {
	src := buildZip(t, map[string]any{"readme.json": map[string]any{"x": 1}})
	if _, err := Parse(src); err == nil {
		t.Fatal("tanınmayan arşiv hata vermeli")
	}
}

func TestParseOwnPostsProfileChangesAndInterests(t *testing.T) {
	src := buildZip(t, map[string]any{
		"your_instagram_activity/media/posts_1.json": []any{
			map[string]any{"media": []any{map[string]any{"uri": "a.jpg", "creation_timestamp": 1_700_000_100, "title": mojibake("Güzel gün")}, map[string]any{"uri": "b.jpg", "creation_timestamp": 1_700_000_100}}},
		},
		"your_instagram_activity/media/stories.json": map[string]any{"ig_stories": []any{
			map[string]any{"uri": "s.jpg", "creation_timestamp": 1_700_000_200, "title": ""},
		}},
		"your_instagram_activity/media/reels.json": map[string]any{"ig_reels_media": []any{
			map[string]any{"media": []any{map[string]any{"creation_timestamp": 1_700_000_300}}},
		}},
		"personal_information/personal_information/profile_changes.json": map[string]any{"profile_profile_change": []any{
			map[string]any{"string_map_data": map[string]any{
				"Changed":        map[string]any{"value": "Username"},
				"Previous Value": map[string]any{"value": "eski_ad"},
				"New Value":      map[string]any{"value": "yeni_ad"},
				"Change Date":    map[string]any{"timestamp": 1_700_000_400},
			}}}},
		"preferences/your_topics/your_topics.json": map[string]any{"topics_your_topics": []any{
			map[string]any{"string_map_data": map[string]any{"Name": map[string]any{"value": mojibake("Müzik")}}},
		}},
		"personal_information/personal_information/personal_information.json": map[string]any{"profile_user": []any{map[string]any{"string_map_data": map[string]any{
			"Username": map[string]any{"value": "benim"}, "Name": map[string]any{"value": "Ben"}}}}},
	})
	imp, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]model.Event{}
	for _, e := range imp.Events {
		by[e.Kind] = e
	}
	if e := by[model.EvPost]; e.Text != "Güzel gün" || e.Meta["media"] != 2 || e.TS.Unix() != 1_700_000_100 {
		t.Errorf("gönderi: %+v", e)
	}
	if e := by[model.EvStory]; e.TS.Unix() != 1_700_000_200 {
		t.Errorf("hikâye: %+v", e)
	}
	if e := by[model.EvReel]; e.TS.Unix() != 1_700_000_300 {
		t.Errorf("reels (media içindeki zaman): %+v", e)
	}
	if e := by[model.EvProfileChange]; e.Text != "Username" || e.Meta["from"] != "eski_ad" || e.Meta["to"] != "yeni_ad" {
		t.Errorf("profil değişimi: %+v", e)
	}
	if e := by[model.EvInterest]; e.Text != "Müzik" || !e.TS.IsZero() {
		t.Errorf("ilgi alanı: %+v", e)
	}
}

func TestScanMediaEXIFExtractsDeviceTimeAndGPS(t *testing.T) {
	p := filepath.Join(t.TempDir(), "ig.zip")
	f, _ := os.Create(p)
	zw := zip.NewWriter(f)
	add := func(name string, data []byte) { w, _ := zw.Create(name); w.Write(data) }
	add("media/posts/202603/a.jpg", exiftest.JPEG("Apple", "iPhone 13", "2026:03:14 21:45:10", true, [3]uint32{41, 0, 18}, "N", [3]uint32{28, 58, 30}, "E"))
	add("media/posts/202603/b.jpeg", exiftest.JPEG("Samsung", "SM-G991B", "2026:03:15 08:00:00", false, [3]uint32{}, "N", [3]uint32{}, "E"))
	add("media/posts/202603/c.jpg", []byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 4, 'J', 'F', 0xFF, 0xD9}) // EXIF'siz
	add("media/posts/202603/d.jpg", []byte("bozuk dosya"))
	add("personal_information/personal_information/personal_information.json", []byte(`{"profile_user":[{"string_map_data":{"Username":{"value":"ben"},"Name":{"value":"Ben"}}}]}`))
	zw.Close()
	f.Close()
	src, err := zipfs.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	imp, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	if imp.Stats["media_scanned"] != 4 || imp.Stats["event:"+model.EvMediaMeta] != 2 || imp.Stats["media_gps"] != 1 {
		t.Fatalf("istatistikler: %v", imp.Stats)
	}
	var withGPS, without *model.Event
	for i := range imp.Events {
		e := &imp.Events[i]
		if e.Kind != model.EvMediaMeta {
			continue
		}
		if _, ok := e.Meta["lat"]; ok {
			withGPS = e
		} else {
			without = e
		}
	}
	if withGPS == nil || without == nil {
		t.Fatalf("iki medya olayı beklenir: %+v", imp.Events)
	}
	if withGPS.Text != "Apple iPhone 13" || withGPS.TS.Format("2006-01-02 15:04") != "2026-03-14 21:45" {
		t.Fatalf("cihaz/zaman: %+v", withGPS)
	}
	if lat := withGPS.Meta["lat"].(float64); lat < 41.004 || lat > 41.006 {
		t.Fatalf("enlem: %v", lat)
	}
	if without.Text != "Samsung SM-G991B" {
		t.Fatalf("GPS'siz cihaz: %+v", without)
	}
	for _, w := range imp.Warnings {
		if strings.Contains(w, "EXIF") {
			t.Fatalf("EXIF bulunmaması uyarı olmamalı: %s", w)
		}
	}
}

// Yeni (Meta) dışa aktarım biçimi: kökte dizi, her kayıt {timestamp, label_values:[…], fbid}. Hesap, ya düz
// "Username" etiketindedir ya da iç içe "Owner" grubunun içindedir. Kullanıcı adları uydurmadır.
func TestParseLabelValuesFormatFindsAccountsInFlatAndNestedGroups(t *testing.T) {
	lv := func(label, value string) map[string]any { return map[string]any{"label": label, "value": value} }
	flat := func(ts int64, user string) map[string]any {
		return map[string]any{"timestamp": ts, "media": []any{}, "fbid": "1", "label_values": []any{lv("URL", "https://www.instagram.com/"+user), lv("Name", "Ad"), lv("Username", user)}}
	}
	owned := func(ts int64, owner string) map[string]any {
		return map[string]any{"timestamp": ts, "media": []any{}, "fbid": "2", "label_values": []any{
			map[string]any{"label": "URL", "value": "https://www.instagram.com/p/XYZ/", "href": "https://www.instagram.com/p/XYZ/"},
			map[string]any{"title": "Owner", "dict": []any{map[string]any{"title": "", "dict": []any{lv("URL", "https://www.instagram.com/"+owner), lv("Name", "Ad"), lv("Username", owner)}}}},
			map[string]any{"title": "Hashtags", "dict": []any{map[string]any{"title": "", "dict": []any{lv("Name", "etiket1")}}, map[string]any{"title": "", "dict": []any{lv("Name", "etiket2")}}}},
		}}
	}
	src := buildZip(t, map[string]any{
		"personal_information/personal_information/personal_information.json": map[string]any{"profile_user": []any{map[string]any{"string_map_data": map[string]any{
			"Username": map[string]any{"value": "benim"}, "Name": map[string]any{"value": "Ben Sahip"}}}}},
		"connections/followers_and_following/close_friends.json":                []any{flat(1_700_000_000, "yakin_biri"), flat(1_700_000_001, "yakin_iki")},
		"connections/followers_and_following/blocked_profiles.json":             []any{flat(1_700_000_010, "engelli_biri")},
		"connections/followers_and_following/recent_follow_requests.json":       []any{flat(1_700_000_020, "istek_atilan")},
		"connections/followers_and_following/recently_unfollowed_profiles.json": []any{flat(1_700_000_030, "birakilan")},
		"your_instagram_activity/likes/liked_posts.json":                        []any{owned(1_700_000_100, "gonderi_sahibi"), owned(1_700_000_101, "gonderi_sahibi")},
		"your_instagram_activity/story_interactions/story_likes.json":           []any{owned(1_700_000_200, "hikaye_sahibi")},
		"your_instagram_activity/story_interactions/stories_viewed.json":        []any{owned(1_700_000_300, "izlenen_biri"), owned(1_700_000_301, "izlenen_biri"), owned(1_700_000_302, "izlenen_biri")},
		"your_instagram_activity/saved/saved_posts.json":                        []any{owned(1_700_000_400, "kaydedilen_sahip")},
		"logged_information/recent_searches/profile_searches.json":              []any{flat(1_700_000_500, "aranan_hesap")},
	})
	imp, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	count := map[string]map[string]int{}
	for _, e := range imp.Events {
		if count[e.Kind] == nil {
			count[e.Kind] = map[string]int{}
		}
		count[e.Kind][e.Counterpart]++
	}
	want := map[string]map[string]int{
		model.EvCloseFriend:   {"yakin_biri": 1, "yakin_iki": 1},
		model.EvBlocked:       {"engelli_biri": 1},
		model.EvFollowRequest: {"istek_atilan": 1},
		model.EvUnfollowed:    {"birakilan": 1},
		model.EvLikePost:      {"gonderi_sahibi": 2},
		model.EvStoryLike:     {"hikaye_sahibi": 1},
		model.EvStoryView:     {"izlenen_biri": 3},
		model.EvSaved:         {"kaydedilen_sahip": 1},
		model.EvSearchAccount: {"aranan_hesap": 1},
	}
	for kind, w := range want {
		for who, n := range w {
			if count[kind][who] != n {
				t.Errorf("%s/%s: %d olay bekleniyordu, %d bulundu (tümü: %v)", kind, who, n, count[kind][who], count[kind])
			}
		}
	}
	for kind, m := range count {
		if m[""] > 0 {
			t.Errorf("%s: hesabı boş olay üretilmemeli: %v", kind, m)
		}
	}
	for _, e := range imp.Events {
		if (e.Kind == model.EvCloseFriend || e.Kind == model.EvLikePost) && e.Text != "Ad" {
			t.Errorf("%s olayının metni görünen ad olmalı: %q", e.Kind, e.Text)
		}
	}
	if imp.Stats["event:"+model.EvLikePost] != 2 || imp.Stats["event:"+model.EvStoryView] != 3 {
		t.Errorf("istatistikler: %v", imp.Stats)
	}
}

func TestFlattenGroupDepthIsBounded(t *testing.T) {
	var node any = []any{map[string]any{"label": "Username", "value": "cok_derin"}}
	for i := 0; i < 50; i++ {
		node = []any{map[string]any{"dict": node}}
	}
	fields := map[string]field{}
	flattenGroup(fields, "owner", node.([]any), 0) // çok derin iç içe yapı çökertmemeli
	if _, ok := fields["owner.username"]; ok {
		t.Fatal("sınırın ötesindeki alan okunmamalı")
	}
}

// Eski biçimde beğeni kaydında değer bir simge olabilir; hesap başlıktadır.
func TestOldFormatLikedCommentUsesTitleNotValue(t *testing.T) {
	src := buildZip(t, map[string]any{
		"personal_information/personal_information/personal_information.json": map[string]any{"profile_user": []any{map[string]any{"string_map_data": map[string]any{
			"Username": map[string]any{"value": "benim"}, "Name": map[string]any{"value": "Ben Sahip"}}}}},
		"your_instagram_activity/likes/liked_comments.json": map[string]any{"likes_comment_likes": []any{
			map[string]any{"title": "yorum_sahibi", "string_list_data": []any{map[string]any{"href": "https://www.instagram.com/p/A/", "value": "❤", "timestamp": 1_700_000_000}}}}},
		"connections/followers_and_following/followers_1.json": []any{
			map[string]any{"title": "", "string_list_data": []any{map[string]any{"value": "takipci_biri", "href": "https://www.instagram.com/takipci_biri", "timestamp": 1_700_000_100}}}},
	})
	imp, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, e := range imp.Events {
		got[e.Kind] = e.Counterpart + "|" + e.Actor
	}
	if got[model.EvLikeComment] != "yorum_sahibi|"+model.Owner {
		t.Errorf("yorum beğenisi hesabı başlıktan gelmeli: %q", got[model.EvLikeComment])
	}
	if got[model.EvFollower] != model.Owner+"|takipci_biri" {
		t.Errorf("takipçi hesabı değerden gelmeli (başlık boş): %q", got[model.EvFollower])
	}
}

func TestParseViewHistoryAndUnknownSummary(t *testing.T) {
	view := func(author string, ts int64) map[string]any {
		return map[string]any{"string_map_data": map[string]any{"Author": map[string]any{"value": author}, "Time": map[string]any{"timestamp": ts}}}
	}
	src := buildZip(t, map[string]any{
		"personal_information/personal_information/personal_information.json": map[string]any{"profile_user": []any{map[string]any{"string_map_data": map[string]any{
			"Username": map[string]any{"value": "benim"}, "Name": map[string]any{"value": "Ben Sahip"}}}}},
		"ads_information/ads_and_topics/posts_viewed.json":   map[string]any{"impressions_history_posts_seen": []any{view("a", 1_700_000_000), view("b", 1_700_000_100)}},
		"ads_information/ads_and_topics/videos_watched.json": map[string]any{"impressions_history_videos_watched": []any{view("a", 1_700_000_200)}},
		"some_folder/ilgisiz_dosya.json":                     map[string]any{"x": 1},
		"some_folder/ilgisiz_dosya2.json":                    map[string]any{"x": 1},
	})
	imp, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	if imp.Stats["event:"+model.EvPostView] != 2 || imp.Stats["event:"+model.EvVideoView] != 1 {
		t.Fatalf("izleme olayları: %v", imp.Stats)
	}
	found := false
	for _, w := range imp.Warnings {
		if strings.Contains(w, "Okunmayan 2 JSON") && strings.Contains(w, "ilgisiz_dosya.json") {
			found = true
		}
	}
	if !found {
		t.Fatalf("okunmayan dosya özeti yok: %v", imp.Warnings)
	}
}
