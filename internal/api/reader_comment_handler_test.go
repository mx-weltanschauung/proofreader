package api

import (
	"context"
	"github.com/gorilla/mux"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"proofreader/internal/models"
	"proofreader/internal/repository"
)

type commentFake struct {
	CommentStore
	err error
}

func (f commentFake) Review(context.Context, models.CommentViewer, int64, int, bool, string) error {
	return f.err
}

func TestReaderCommentReviewStaleRevision(t *testing.T) {
	h := NewReaderCommentHandler(commentFake{err: repository.ErrCommentConflict}, nil, nil, nil, "", false)
	req := mux.SetURLVars(httptest.NewRequest("POST", "/", strings.NewReader(`{"revision":2}`)), map[string]string{"id": "7"})
	rec := httptest.NewRecorder()
	withClaims(testReaderClaims, h.Approve)(rec, req)
	if rec.Code != 409 {
		t.Fatalf("устаревшее решение: %d %s", rec.Code, rec.Body)
	}
}

func (f commentFake) Save(_ context.Context, v models.CommentViewer, _ int64, in models.MarginNoteInput, _ string, _ int64, _ int) (*models.ReaderComment, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &models.ReaderComment{MarginNote: models.MarginNote{Body: in.Body}, Status: "на_рассмотрении"}, nil
}
func TestReaderCommentCreateValidation(t *testing.T) {
	for _, tt := range []struct {
		body string
		err  error
		code int
	}{
		{`{"target":"work","body":""}`, nil, 400},
		{`{"target":"work","body":"` + strings.Repeat("я", 2001) + `"}`, nil, 400},
		{`{"target":"work","body":"мысль"}`, nil, 201},
		{`{"target":"quote","body":"мысль","start_page":1,"end_page":2,"quote_key":"x"}`, nil, 404},
		{`{"target":"work","body":"мысль"}`, repository.ErrCommentRate, 429},
		{`{"target":"work","body":"мысль"}`, repository.ErrCommentBanned, 403},
	} {
		h := NewReaderCommentHandler(commentFake{err: tt.err}, fakeMarginWorks{ids: map[int64]bool{7: true}}, fakeMarginPages{}, fakeMarginChapters{}, "secret", false)
		req := httptest.NewRequest("POST", "/", strings.NewReader(tt.body))
		req = mux.SetURLVars(req, map[string]string{"workId": "7"})
		rec := httptest.NewRecorder()
		withClaims(testReaderClaims, h.Create)(rec, req)
		if rec.Code != tt.code {
			t.Errorf("%s: %d %s", tt.body, rec.Code, rec.Body)
		}
	}
	h := NewReaderCommentHandler(nil, nil, nil, nil, "", false)
	rec := httptest.NewRecorder()
	h.Create(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{}`)))
	if rec.Code != 401 {
		t.Fatalf("без сессии: %d", rec.Code)
	}
}
