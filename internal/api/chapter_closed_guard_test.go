package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gorilla/mux"

	"proofreader/internal/models"
)

type fakeClosedSpans struct{ ids map[int64]bool }

func (f fakeClosedSpans) ClosedSpans(ctx context.Context, workID int64) ([]models.PageSpan, error) {
	return nil, nil
}

func (f fakeClosedSpans) ClosedChapterIDs(ctx context.Context, workID int64) (map[int64]bool, error) {
	return f.ids, nil
}

// Дерево тома 1: 10 закрыта, её подглава 11 закрыта через родителя;
// 20 — открытая соседка с общей полосой; 30 — открытый родитель
// закрытой подглавы 31; 40 — открытая глава.
func closedGuardHandler() (*ChapterHandler, *int) {
	chapters := map[int64]*models.Chapter{
		10: {ID: 10, WorkID: 1, Title: "Закрытая", Type: "chapter", StartPage: 1, EndPage: 5},
		11: {ID: 11, WorkID: 1, Title: "Подглава", Type: "chapter", StartPage: 2, EndPage: 3, ParentID: i64(10)},
		20: {ID: 20, WorkID: 1, Title: "Соседка", Type: "chapter", StartPage: 5, EndPage: 9},
		30: {ID: 30, WorkID: 1, Title: "Родитель", Type: "chapter", StartPage: 10, EndPage: 20},
		31: {ID: 31, WorkID: 1, Title: "Закрытая внутри", Type: "chapter", StartPage: 11, EndPage: 12, ParentID: i64(30)},
		40: {ID: 40, WorkID: 1, Title: "Открытая", Type: "chapter", StartPage: 21, EndPage: 30},
	}
	writes := 0
	store := &fakeChapterStore{
		getByIDFn: func(ctx context.Context, id int64) (*models.Chapter, error) {
			c, ok := chapters[id]
			if !ok {
				return nil, context.Canceled
			}
			cp := *c
			return &cp, nil
		},
		listHierarchicalFn: func(ctx context.Context, workID int64) ([]*models.Chapter, error) {
			c10 := &models.Chapter{ID: 10, Children: []*models.Chapter{{ID: 11}}}
			c30 := &models.Chapter{ID: 30, Children: []*models.Chapter{{ID: 31}}}
			return []*models.Chapter{c10, {ID: 20}, c30, {ID: 40}}, nil
		},
		updateFn: func(ctx context.Context, c *models.Chapter) error { writes++; return nil },
		moveFn: func(ctx context.Context, id int64, p *int64, o int) error {
			writes++
			return nil
		},
		deleteFn: func(ctx context.Context, id int64) error { writes++; return nil },
	}
	h := NewChapterHandler(store, &fakePageStore{}, nil, nil).
		WithClosedChapters(fakeClosedSpans{ids: map[int64]bool{10: true, 11: true, 31: true}})
	return h, &writes
}

func doChapter(t *testing.T, call func(http.ResponseWriter, *http.Request), method string, id int64, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(method, "/api/works/1/chapters/"+strconv.FormatInt(id, 10), bytes.NewReader(raw))
	req = mux.SetURLVars(req, map[string]string{"workId": "1", "id": strconv.FormatInt(id, 10)})
	rec := httptest.NewRecorder()
	call(rec, req)
	return rec
}

func want409(t *testing.T, rec *httptest.ResponseRecorder, writes *int) {
	t.Helper()
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body %s", rec.Code, rec.Body)
	}
	var e struct {
		Message string `json:"message"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &e)
	if e.Message != "Глава закрыта — сначала откройте её" {
		t.Errorf("message = %q", e.Message)
	}
	if *writes != 0 {
		t.Errorf("запись состоялась: %d", *writes)
	}
}

func updBody(title string, start, end int, parent *int64) CreateChapterRequest {
	return CreateChapterRequest{Title: title, Type: "chapter", StartPage: start, EndPage: end, ParentID: parent}
}

func TestUpdateClosedChapter409(t *testing.T) {
	h, w := closedGuardHandler()
	want409(t, doChapter(t, h.Update, "PUT", 10, updBody("Новое", 1, 5, nil)), w)
}

func TestUpdateDescendantOfClosed409(t *testing.T) {
	h, w := closedGuardHandler()
	want409(t, doChapter(t, h.Update, "PUT", 11, updBody("Новое", 2, 3, i64(10))), w)
}

func TestMoveIntoClosedSubtree409(t *testing.T) {
	h, w := closedGuardHandler()
	want409(t, doChapter(t, h.Move, "PATCH", 40, MoveChapterRequest{ParentID: i64(10)}), w)
}

func TestMoveOutOfClosedSubtree409(t *testing.T) {
	h, w := closedGuardHandler()
	// 11 — потомок закрытой 10; перенос под открытую 40.
	want409(t, doChapter(t, h.Move, "PATCH", 11, MoveChapterRequest{ParentID: i64(40)}), w)
}

func TestDeleteClosedChapter409(t *testing.T) {
	h, w := closedGuardHandler()
	want409(t, doChapter(t, h.Delete, "DELETE", 10, nil), w)
}

func TestDeleteParentOfClosed409(t *testing.T) {
	h, w := closedGuardHandler()
	want409(t, doChapter(t, h.Delete, "DELETE", 30, nil), w)
}

func TestUpdateOpenNeighborAllowed(t *testing.T) {
	h, w := closedGuardHandler()
	rec := doChapter(t, h.Update, "PUT", 20, updBody("Соседка 2", 5, 10, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body)
	}
	if *w != 1 {
		t.Errorf("writes = %d", *w)
	}
	// Открытая глава без закрытых потомков удаляется.
	if rec := doChapter(t, h.Delete, "DELETE", 40, nil); rec.Code != http.StatusNoContent {
		t.Errorf("delete open = %d", rec.Code)
	}
}
