package audio

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"proofreader/internal/models"
)

// Общая с Python фикстура: расхождение в переводах строк или нормализации
// сделало бы всю озвучку stale сразу при регистрации.
func TestPagesSHA256MatchesSharedFixture(t *testing.T) {
	raw, err := os.ReadFile("testdata/pages_sha256.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Pages  []string `json:"pages"`
		SHA256 string   `json:"sha256"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) < 5 {
		t.Fatalf("фикстура урезана: %d случаев", len(cases))
	}
	for _, c := range cases {
		if got := PagesSHA256(c.Pages); got != c.SHA256 {
			t.Errorf("PagesSHA256(%q) = %s, ждали %s", c.Pages, got, c.SHA256)
		}
	}
}

const (
	shaA = "aaaaaaaa11111111111111111111111111111111111111111111111111111111"
	shaB = "bbbbbbbb22222222222222222222222222222222222222222222222222222222"
	shaC = "cccccccc33333333333333333333333333333333333333333333333333333333"
)

func TestTrackKeyChangesWithText(t *testing.T) {
	k1 := TrackKey(47, shaA, shaB, 43, 93, "Глава")
	// sha256("Глава")[:8] = 0af3fff3
	if k1 != "works/47/aaaaaaaa/43-93-bbbbbbbb-0af3fff3.opus" {
		t.Errorf("TrackKey = %q", k1)
	}
	// Review Focus 1: тот же рецепт и диапазон, другой текст — другой ключ.
	if k2 := TrackKey(47, shaA, shaC, 43, 93, "Глава"); k2 == k1 {
		t.Errorf("ключ не зависит от текста полос: %q", k2)
	}
	// Тикет 05: заголовок произносится, переименованная глава
	// переозвучивается — под своим ключом, а не поверх живого объекта.
	if k3 := TrackKey(47, shaA, shaB, 43, 93, "Глава первая"); k3 == k1 {
		t.Errorf("ключ не зависит от заголовка: %q", k3)
	}
}

func TestValidSHA256(t *testing.T) {
	if !ValidSHA256(shaA) {
		t.Error("верный отвергнут")
	}
	for _, bad := range []string{"", "abc", strings.ToUpper(shaA), shaA[:63] + "g", "../" + shaA[3:]} {
		if ValidSHA256(bad) {
			t.Errorf("принят %q", bad)
		}
	}
}

func TestPlaylistPutsHumanRecordingsFirstWithinChapter(t *testing.T) {
	tracks := []models.AudioTrack{
		{ID: 1, Title: "Предисловие", StartPage: 1, EndPage: 9, DurationMS: 60_000},
		{ID: 2, Title: "Глава первая", StartPage: 10, EndPage: 20, DurationMS: 120_400},
		{ID: 3, Title: "Глава вторая", StartPage: 21, EndPage: 30, DurationMS: 1000},
	}
	recs := []models.AudioRecording{
		{ID: 8, ChapterTitle: "Глава первая", ChapterStartPage: 10, ChapterEndPage: 20, Position: 2, DurationMS: 5000},
		{ID: 7, ChapterTitle: "Глава первая", ChapterStartPage: 10, ChapterEndPage: 20, Position: 1, Reader: "Иванов", DurationMS: 4000},
	}
	got := Playlist(tracks, recs, 10, 20, "https://lib.example")
	var urls []string
	for _, e := range got {
		urls = append(urls, e.URL)
	}
	want := []string{
		"https://lib.example/api/audio/rec/7",
		"https://lib.example/api/audio/rec/8",
		"https://lib.example/api/audio/2.opus",
	}
	if strings.Join(urls, " ") != strings.Join(want, " ") {
		t.Errorf("порядок главы = %v, ждали %v", urls, want)
	}
	if !strings.Contains(got[0].Title, "Иванов") || !strings.Contains(got[0].Title, "1 из 2") {
		t.Errorf("подпись записи человека = %q", got[0].Title)
	}

	whole := Playlist(tracks, recs, 0, 0, "https://lib.example")
	if len(whole) != 5 || whole[0].URL != "https://lib.example/api/audio/1.opus" ||
		whole[1].URL != "https://lib.example/api/audio/rec/7" {
		t.Errorf("том = %+v", whole)
	}
}

func recURLs(entries []Entry) string {
	var urls []string
	for _, e := range entries {
		urls = append(urls, e.URL)
	}
	return strings.Join(urls, " ")
}

// Тикет 03: глава и её первая подглава начинаются на одной полосе — их записи
// не перемешиваются по position, объемлющая глава идёт первой. Номер главы у
// объемлющей нарочно больше: порядок обязан держаться концом диапазона, а не
// случайностью номеров.
func TestPlaylistKeepsChapterBeforeItsFirstSubchapter(t *testing.T) {
	recs := []models.AudioRecording{
		{ID: 1, ChapterID: 4, ChapterTitle: "§ 1", ChapterStartPage: 1, ChapterEndPage: 5, Position: 1},
		{ID: 2, ChapterID: 9, ChapterTitle: "Глава", ChapterStartPage: 1, ChapterEndPage: 10, Position: 1},
		{ID: 3, ChapterID: 4, ChapterTitle: "§ 1", ChapterStartPage: 1, ChapterEndPage: 5, Position: 2},
		{ID: 4, ChapterID: 9, ChapterTitle: "Глава", ChapterStartPage: 1, ChapterEndPage: 10, Position: 2},
	}
	want := "/api/audio/rec/2 /api/audio/rec/4 /api/audio/rec/1 /api/audio/rec/3"
	if got := recURLs(Playlist(nil, recs, 0, 0, "")); got != want {
		t.Errorf("порядок = %s, ждали %s", got, want)
	}
}

// Тикет 03: у двух глав совпал весь диапазон — записи всё равно идут главой.
func TestPlaylistGroupsRecordingsOfChaptersWithSameRange(t *testing.T) {
	recs := []models.AudioRecording{
		{ID: 1, ChapterID: 5, ChapterStartPage: 1, ChapterEndPage: 5, Position: 1},
		{ID: 2, ChapterID: 6, ChapterStartPage: 1, ChapterEndPage: 5, Position: 1},
		{ID: 3, ChapterID: 5, ChapterStartPage: 1, ChapterEndPage: 5, Position: 2},
		{ID: 4, ChapterID: 6, ChapterStartPage: 1, ChapterEndPage: 5, Position: 2},
	}
	want := "/api/audio/rec/1 /api/audio/rec/3 /api/audio/rec/2 /api/audio/rec/4"
	if got := recURLs(Playlist(nil, recs, 0, 0, "")); got != want {
		t.Errorf("порядок = %s, ждали %s", got, want)
	}
}

func TestPlaylistChapterTakesOverlappingTracksOnly(t *testing.T) {
	tracks := []models.AudioTrack{
		{ID: 1, StartPage: 1, EndPage: 12}, // захватывает начало главы
		{ID: 2, StartPage: 13, EndPage: 30},
		{ID: 3, StartPage: 31, EndPage: 40},
	}
	got := Playlist(tracks, nil, 10, 30, "")
	if len(got) != 2 || got[0].URL != "/api/audio/1.opus" || got[1].URL != "/api/audio/2.opus" {
		t.Errorf("пересечение = %+v", got)
	}
}

func TestWriteM3U(t *testing.T) {
	var b strings.Builder
	err := WriteM3U(&b, []Entry{{Title: "Глава\nпервая", DurationMS: 61_600, URL: "https://x/api/audio/2.opus"}})
	if err != nil {
		t.Fatal(err)
	}
	want := "#EXTM3U\n#EXTINF:62,Глава первая\nhttps://x/api/audio/2.opus\n"
	if b.String() != want {
		t.Errorf("m3u =\n%q\nждали\n%q", b.String(), want)
	}
}

func TestIsAudioKey(t *testing.T) {
	r := "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	for key, want := range map[string]bool{
		TrackKey(47, r, r, 1, 2, "Глава"):              true,
		"works/47/rec/9/abc123.mp3":                    true,
		"works/47/rec/9/abc123.opus":                   true,
		"works/47/rec/9/abc123.exe":                    false,
		"works/48/rec/9/abc123.mp3":                    false,
		TrackKey(48, r, r, 1, 2, "Глава"):              false,
		"works/47/pages/page_1.png":                    false,
		"works/47/original/book.pdf":                   false,
		"works/47/rec/9/../../original/x.mp3":          false,
		"works/47/ABCDEF01/1-2-abcdef01-0af3fff3.opus": false,
		// форма до тикета 05, без заголовка
		"works/47/abcdef01/1-2-abcdef01.opus": false,
	} {
		if got := IsAudioKey(47, key); got != want {
			t.Errorf("%q: %v, ждали %v", key, got, want)
		}
	}
}
