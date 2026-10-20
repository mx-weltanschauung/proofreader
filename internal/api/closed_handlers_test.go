package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"proofreader/internal/models"
	"proofreader/internal/pagecache"
	"proofreader/pkg/markdown"
	"proofreader/pkg/storage"
)

// closedFixture — настоящий роутер (NewRouter…WithClosed…Setup) над
// подставными хранилищами: вложенные подроутеры и порядок промежуточных слоёв
// те же, что в проде. Работа 7 закрыта, 8 открыта; глава 5 — в 7, глава 6 — в 8.
type closedFixture struct {
	handler http.Handler
	cache   *pagecache.Store
	audio   *fakeAudio
	works   *fakeWorkRepo
}

func newClosedFixture(t *testing.T) *closedFixture {
	t.Helper()
	chapters := &fakeChapterStore{getByIDFn: func(_ context.Context, id int64) (*models.Chapter, error) {
		switch id {
		case 5:
			return &models.Chapter{ID: 5, WorkID: 7, StartPage: 1, EndPage: 2}, nil
		case 6:
			return &models.Chapter{ID: 6, WorkID: 8, StartPage: 1, EndPage: 2}, nil
		}
		return nil, context.Canceled
	}}
	pages := &fakePageStore{getPageRangeFn: func(_ context.Context, w int64, _, _ int) ([]*models.Page, error) {
		return []*models.Page{
			{ID: 1, WorkID: w, PageNumber: 1, ContentMarkdown: "Первая."},
			{ID: 2, WorkID: w, PageNumber: 2, ContentMarkdown: "Вторая."},
		}, nil
	}}
	store := pagecache.New(t.TempDir())
	ch := NewChapterHandler(chapters, pages, markdown.NewRenderer(), NewRangeCache(store))

	works := &fakeWorkRepo{}
	wh := NewWorkHandler(works, nil, markdown.NewRenderer(), storage.NewMemoryStorage(), time.Minute, nil, nil)

	af := &fakeAudio{
		tracks: []models.AudioTrack{{ID: 3, WorkID: 7, S3Key: "works/7/r/1-2-p.opus"}},
		recs:   []models.AudioRecording{{ID: 9, WorkID: 7, S3Key: "works/7/rec/5/x.mp3"}},
	}
	closed := &fakeClosedStore{works: map[int64]string{7: models.ClosedByWork}}
	ah := NewAudioHandler(af, af, af, fakeAudioChapters{}, storage.NewMemoryStorage(), closed)

	rt := createTestRouter()
	r := NewRouter(nil, wh, nil, ch, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		rt.cacheHandler, nil, rt.authService).WithAudio(ah).WithClosed(closed)
	return &closedFixture{handler: r.Setup(), cache: store, audio: af, works: works}
}

func (f *closedFixture) get(path, token string) int {
	return gateGet(f.handler, path, token)
}

func TestChapterOfAnotherWorkIs404(t *testing.T) {
	f := newClosedFixture(t)
	for _, token := range []string{"", editorToken(t)} {
		// Глава 6 — тома 8; путь называет открытый том 8 → 200, а том 9 → 404.
		for _, path := range []string{"/api/works/9/chapters/6", "/api/works/9/chapters/6/pages"} {
			if code := f.get(path, token); code != http.StatusNotFound {
				t.Errorf("токен %q, %s: код %d, ждали 404", token, path, code)
			}
		}
		for _, path := range []string{"/api/works/8/chapters/6", "/api/works/8/chapters/6/pages"} {
			if code := f.get(path, token); code != http.StatusOK {
				t.Errorf("токен %q, %s: код %d, ждали 200", token, path, code)
			}
		}
	}
}

func TestStaffWarmedChapterCacheNotServedToAnonymous(t *testing.T) {
	f := newClosedFixture(t)
	path := "/api/works/7/chapters/5/pages"
	if code := f.get(path, editorToken(t)); code != http.StatusOK {
		t.Fatalf("редактор: код %d, ждали 200", code)
	}
	if _, _, ok := f.cache.Open(pagecache.Key{WorkID: 7, Start: 1, End: 2}); !ok {
		t.Fatal("после запроса редактора файл в кэше не появился")
	}
	if code := f.get(path, ""); code != http.StatusNotFound {
		t.Errorf("аноним: код %d, ждали 404", code)
	}
	if code := f.get(path, readerToken(t)); code != http.StatusNotFound {
		t.Errorf("читатель: код %d, ждали 404", code)
	}
}

func TestAudioRedirectOfClosedWorkIs404(t *testing.T) {
	f := newClosedFixture(t)
	for _, path := range []string{"/api/audio/3.opus", "/api/audio/rec/9"} {
		for name, token := range map[string]string{"аноним": "", "читатель": readerToken(t)} {
			if code := f.get(path, token); code != http.StatusNotFound {
				t.Errorf("%s %s: код %d, ждали 404", name, path, code)
			}
		}
		if code := f.get(path, editorToken(t)); code != http.StatusFound {
			t.Errorf("редактор %s: код %d, ждали 302", path, code)
		}
	}
}

// Адрес несуществующей дорожки по-прежнему 410, а не 404: закрытость не
// должна менять ответ на «нет такой строки».
func TestAudioRedirectMissingStaysGoneWithClosedStore(t *testing.T) {
	f := newClosedFixture(t)
	if code := f.get("/api/audio/99.opus", ""); code != http.StatusGone {
		t.Errorf("код %d, ждали 410", code)
	}
}

func TestWorkChildrenHideClosed(t *testing.T) {
	f := newClosedFixture(t)
	f.works.children = map[int64][]*models.Work{8: {
		{ID: 80, Title: "титул", ClosedBy: ""},
		{ID: 81, Title: "закрытое содержание", ClosedBy: models.ClosedByWork},
	}}
	count := func(token string) int {
		req := httptest.NewRequest("GET", "/api/works/8", nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		f.handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("код %d", rec.Code)
		}
		return countChildren(t, rec.Body.Bytes())
	}
	if n := count(""); n != 1 {
		t.Errorf("аноним: детей %d, ждали 1", n)
	}
	if n := count(readerToken(t)); n != 1 {
		t.Errorf("читатель: детей %d, ждали 1", n)
	}
	if n := count(editorToken(t)); n != 2 {
		t.Errorf("редактор: детей %d, ждали 2", n)
	}
}

func countChildren(t *testing.T, body []byte) int {
	t.Helper()
	var resp struct {
		Children []struct {
			ID int64 `json:"id"`
		} `json:"children"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatal(err)
	}
	return len(resp.Children)
}
