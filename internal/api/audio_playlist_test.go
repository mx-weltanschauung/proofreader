package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"proofreader/internal/models"
)

type fakePlaylistWorks struct{ WorkStore }

func (fakePlaylistWorks) GetByID(_ context.Context, id int64) (*models.Work, error) {
	if id == 47 {
		return &models.Work{ID: 47, Title: "Капитал, т. 1"}, nil
	}
	return nil, errors.New("work not found")
}

type fakePlaylistChapters struct{}

func (fakePlaylistChapters) GetByID(_ context.Context, id int64) (*models.Chapter, error) {
	if id == 5 {
		return &models.Chapter{ID: 5, WorkID: 47, Title: "Глава первая", StartPage: 10, EndPage: 20}, nil
	}
	return nil, errors.New("chapter not found")
}

func playlistHandler(f *fakeAudio) *DownloadHandler {
	return NewDownloadHandler(nil).WithAudio(
		NewAudioPlaylist(f, f, fakePlaylistWorks{}, fakePlaylistChapters{}, "https://lib.example"))
}

func TestChapterM3UStartsWithHumanRecordings(t *testing.T) {
	f := &fakeAudio{
		tracks: []models.AudioTrack{{ID: 2, Title: "Глава первая", StartPage: 10, EndPage: 20, DurationMS: 60_000},
			{ID: 3, Title: "Глава вторая", StartPage: 21, EndPage: 30}},
		recs: []models.AudioRecording{{ID: 7, ChapterID: 5, ChapterTitle: "Глава первая",
			ChapterStartPage: 10, ChapterEndPage: 20, Position: 1, DurationMS: 1000}},
	}
	req := mux.SetURLVars(httptest.NewRequest("GET", "/api/works/47/chapters/5/download?format=m3u", nil),
		map[string]string{"workId": "47", "id": "5"})
	rec := httptest.NewRecorder()
	playlistHandler(f).Chapter(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	iRec, iTrack := strings.Index(body, "/api/audio/rec/7"), strings.Index(body, "/api/audio/2.opus")
	if !strings.HasPrefix(body, "#EXTM3U\n") || iRec < 0 || iTrack < 0 || iRec > iTrack ||
		strings.Contains(body, "/api/audio/3.opus") || !strings.Contains(body, "https://lib.example/") {
		t.Errorf("плейлист главы:\n%s", body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "audio/x-mpegurl") {
		t.Errorf("Content-Type = %q", ct)
	}
}

func TestWorkM3UWithoutSoundIs404(t *testing.T) {
	req := mux.SetURLVars(httptest.NewRequest("GET", "/api/works/47/download?format=m3u", nil),
		map[string]string{"id": "47"})
	rec := httptest.NewRecorder()
	playlistHandler(&fakeAudio{}).Work(rec, req)
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "звука") {
		t.Errorf("%d %q", rec.Code, rec.Body)
	}
}

func TestUnknownFormatListsM3U(t *testing.T) {
	req := mux.SetURLVars(httptest.NewRequest("GET", "/api/works/47/download?format=zip", nil),
		map[string]string{"id": "47"})
	rec := httptest.NewRecorder()
	playlistHandler(&fakeAudio{}).Work(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "m3u") {
		t.Errorf("%d %q", rec.Code, rec.Body)
	}
}

func TestChapterOfAnotherWorkM3UIs404(t *testing.T) {
	f := &fakeAudio{
		tracks: []models.AudioTrack{{ID: 2, Title: "Глава первая", StartPage: 10, EndPage: 20}},
		recs: []models.AudioRecording{{ID: 7, ChapterID: 5, ChapterTitle: "Глава первая",
			ChapterStartPage: 10, ChapterEndPage: 20, Position: 1}},
	}
	req := mux.SetURLVars(httptest.NewRequest("GET", "/api/works/48/chapters/5/download?format=m3u", nil),
		map[string]string{"workId": "48", "id": "5"})
	rec := httptest.NewRecorder()
	playlistHandler(f).Chapter(rec, req)
	if rec.Code != http.StatusNotFound || strings.Contains(rec.Body.String(), "/api/audio/") {
		t.Errorf("%d %q", rec.Code, rec.Body)
	}
}

func TestCollectionM3UIs400(t *testing.T) {
	req := mux.SetURLVars(httptest.NewRequest("GET", "/api/collections/x/download?format=m3u", nil),
		map[string]string{"slug": "x"})
	rec := httptest.NewRecorder()
	playlistHandler(&fakeAudio{}).Collection(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("%d %q", rec.Code, rec.Body)
	}
}

func TestChapterM3UContentDisposition(t *testing.T) {
	f := &fakeAudio{tracks: []models.AudioTrack{{ID: 2, Title: "Глава первая", StartPage: 10, EndPage: 20}}}
	req := mux.SetURLVars(httptest.NewRequest("GET", "/api/works/47/chapters/5/download?format=m3u", nil),
		map[string]string{"workId": "47", "id": "5"})
	rec := httptest.NewRecorder()
	playlistHandler(f).Chapter(rec, req)
	cd := rec.Header().Get("Content-Disposition")
	if !strings.Contains(cd, ".m3u") || !strings.Contains(cd, "filename*=UTF-8''") {
		t.Errorf("Content-Disposition = %q", cd)
	}
}
