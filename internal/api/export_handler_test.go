package api

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"proofreader/internal/models"
	"proofreader/internal/repository"
)

// Настоящий репозиторий обязан удовлетворять WorkStore — иначе проводка
// в cmd/server/main.go перестанет компилироваться.
var _ WorkStore = (*repository.WorkRepository)(nil)

// fakeWorkStore — подставной WorkStore. Невыставленные поля-функции
// возвращают пусто.
type fakeWorkStore struct {
	listFn func(ctx context.Context, limit, offset int, status *models.WorkStatus, ownerID *int64) ([]*models.Work, error)
	getFn  func(ctx context.Context, id int64) (*models.Work, error)
}

func (f *fakeWorkStore) List(ctx context.Context, limit, offset int, status *models.WorkStatus, ownerID *int64) ([]*models.Work, error) {
	if f.listFn != nil {
		return f.listFn(ctx, limit, offset, status, ownerID)
	}
	return nil, nil
}

func (f *fakeWorkStore) GetByID(ctx context.Context, id int64) (*models.Work, error) {
	if f.getFn != nil {
		return f.getFn(ctx, id)
	}
	return nil, nil
}

func TestExportStreamsReadableArchive(t *testing.T) {
	works := &fakeWorkStore{
		listFn: func(ctx context.Context, limit, offset int, status *models.WorkStatus, ownerID *int64) ([]*models.Work, error) {
			if offset > 0 {
				return nil, nil
			}
			return []*models.Work{{ID: 1, Title: "Том 1", Author: "А", Language: "ru", Country: "ru", Status: models.WorkStatusInProgress}}, nil
		},
	}
	pages := &fakePageStore{
		listByWorkFn: func(ctx context.Context, workID int64) ([]*models.Page, error) {
			return []*models.Page{{ID: 1, PageNumber: 1, Status: models.PageStatusProofread, ContentMarkdown: "текст"}}, nil
		},
	}
	h := NewExportHandler(works, &fakeChapterStore{}, pages)

	rec := httptest.NewRecorder()
	h.Export(rec, httptest.NewRequest(http.MethodGet, "/api/export", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/zip" {
		t.Errorf("Content-Type = %q, want application/zip", got)
	}
	if got := rec.Header().Get("Content-Disposition"); got != `attachment; filename="proofreader-export.zip"` {
		t.Errorf("Content-Disposition = %q", got)
	}

	raw := rec.Body.Bytes()
	r, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("body is not a readable zip: %v", err)
	}
	if len(r.File) != 4 { // works.md + work.md + chapters.md + одна страница
		t.Errorf("archive has %d entries, want 4", len(r.File))
	}
}

func TestExportStoreErrorLeavesArchiveInvalid(t *testing.T) {
	works := &fakeWorkStore{
		listFn: func(ctx context.Context, limit, offset int, status *models.WorkStatus, ownerID *int64) ([]*models.Work, error) {
			return nil, errors.New("database is gone")
		},
	}
	h := NewExportHandler(works, &fakeChapterStore{}, &fakePageStore{})

	rec := httptest.NewRecorder()
	h.Export(rec, httptest.NewRequest(http.MethodGet, "/api/export", nil))

	raw := rec.Body.Bytes()
	if _, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw))); err == nil {
		t.Error("a failed export must not produce a readable archive")
	}
}

func TestExportPagesThroughAllWorks(t *testing.T) {
	var offsets []int
	works := &fakeWorkStore{
		listFn: func(ctx context.Context, limit, offset int, status *models.WorkStatus, ownerID *int64) ([]*models.Work, error) {
			offsets = append(offsets, offset)
			if offset > 0 {
				return nil, nil // вторая страница пуста — обход должен остановиться
			}
			batch := make([]*models.Work, exportPageSize) // полная страница ⇒ нужен ещё запрос
			for i := range batch {
				batch[i] = &models.Work{ID: int64(i + 1), Status: models.WorkStatusDraft}
			}
			return batch, nil
		},
	}
	h := NewExportHandler(works, &fakeChapterStore{}, &fakePageStore{})

	rec := httptest.NewRecorder()
	h.Export(rec, httptest.NewRequest(http.MethodGet, "/api/export", nil))

	if len(offsets) != 2 || offsets[0] != 0 || offsets[1] != exportPageSize {
		t.Errorf("offsets = %v, want [0 %d]", offsets, exportPageSize)
	}
}
