package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"proofreader/internal/models"
)

type fakeScansHiddenChapters struct{ ids map[int64]bool }

func (f fakeScansHiddenChapters) ScansHiddenChapterIDs(context.Context, int64) (map[int64]bool, error) {
	return f.ids, nil
}

func TestScansHiddenStructuralMoveRefused(t *testing.T) {
	writes := 0
	store := &fakeChapterStore{
		getByIDFn: func(_ context.Context, id int64) (*models.Chapter, error) {
			parent := i64(10)
			if id == 11 {
				return &models.Chapter{ID: 11, WorkID: 1, ParentID: parent, Title: "подглава", StartPage: 2, EndPage: 3}, nil
			}
			return &models.Chapter{ID: id, WorkID: 1, Title: "глава", StartPage: 1, EndPage: 5}, nil
		},
		listHierarchicalFn: func(context.Context, int64) ([]*models.Chapter, error) {
			return []*models.Chapter{{ID: 10, Children: []*models.Chapter{{ID: 11}}}, {ID: 20}}, nil
		},
		updateFn: func(context.Context, *models.Chapter) error { writes++; return nil },
		moveFn:   func(context.Context, int64, *int64, int) error { writes++; return nil },
		deleteFn: func(context.Context, int64) error { writes++; return nil },
	}
	h := NewChapterHandler(store, &fakePageStore{}, nil, nil).
		WithScansHiddenChapters(fakeScansHiddenChapters{ids: map[int64]bool{10: true, 11: true}})
	for _, c := range []struct {
		call   func(http.ResponseWriter, *http.Request)
		method string
		id     int64
		body   any
	}{
		{h.Update, "PUT", 10, updBody("новая граница", 1, 4, nil)},
		{h.Move, "PATCH", 11, MoveChapterRequest{ParentID: i64(20)}},
		{h.Delete, "DELETE", 10, nil},
	} {
		rec := doChapter(t, c.call, c.method, c.id, c.body)
		if rec.Code != http.StatusConflict || writes != 0 {
			t.Fatalf("%s %d: code=%d writes=%d body=%s", c.method, c.id, rec.Code, writes, rec.Body.String())
		}
	}

	editionID := int64(8)
	workRepo := &fakeWorkRepo{scansHiddenBy: "edition", editionID: &editionID}
	workHandler := NewWorkHandler(workRepo, nil, nil, nil, 0, nil, nil)
	req := httptest.NewRequest(http.MethodPut, "/api/works/7", strings.NewReader(`{"title":"том","edition_id":null}`))
	req = mux.SetURLVars(req, map[string]string{"id": "7"})
	rec := httptest.NewRecorder()
	workHandler.Update(rec, req)
	if rec.Code != http.StatusConflict || workRepo.updates != 0 {
		t.Fatalf("work moved out of hidden edition: code=%d updates=%d body=%s", rec.Code, workRepo.updates, rec.Body.String())
	}
}
