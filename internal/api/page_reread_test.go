package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"proofreader/internal/auth"
	"proofreader/internal/middleware"
	"proofreader/internal/models"
	"proofreader/pkg/markdown"
	"proofreader/pkg/storage"
)

// editedAt — дата, которую хранилище проставляет полосе ПОСЛЕ первой правки.
var editedAt = time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)

// rereadPageStore — склад полос, ведущий себя как настоящий репозиторий в том
// единственном, что здесь важно: GetByID отдаёт НОВУЮ структуру и считает
// text_edited_at в момент чтения (в PageRepository это подзапрос по
// page_versions). Поэтому структура, прочитанная ДО правки, так и остаётся с
// прежней датой — сколько бы её потом ни правили по указателю.
//
// Общий фейк пакета отдаёт одну и ту же структуру на каждое чтение, и на нём
// этот дефект невыразим: устаревший ответ и свежий там неразличимы.
func rereadPageStore(stored *models.Page) *fakePageStore {
	pages := &fakePageStore{}
	pages.getByIDFn = func(ctx context.Context, id int64) (*models.Page, error) {
		if id != stored.ID {
			return nil, fmt.Errorf("page %d not found", id)
		}
		fresh := *stored
		if len(pages.savedVersions) > 0 {
			at := editedAt
			fresh.TextEditedAt = &at
		}
		return &fresh, nil
	}
	pages.saveEditFn = func(ctx context.Context, p *models.Page, v *models.PageVersion) error {
		pages.savedVersions = append(pages.savedVersions, v)
		stored.ContentMarkdown = p.ContentMarkdown
		stored.Status = p.Status
		return nil
	}
	return pages
}

func editorRequest(req *http.Request, vars map[string]string) *http.Request {
	req = mux.SetURLVars(req, vars)
	return req.WithContext(context.WithValue(req.Context(), middleware.UserContextKey,
		&auth.Claims{UserID: 5, Role: models.RoleEditor}))
}

// decodedPage разбирает тело ответа как полосу.
func decodedPage(t *testing.T, rec *httptest.ResponseRecorder) models.Page {
	t.Helper()
	var got models.Page
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("тело ответа не разбирается как полоса: %s", rec.Body.String())
	}
	return got
}

// Оба пути правки текста обязаны отвечать ПЕРЕЧИТАННОЙ полосой.
//
// text_edited_at считается подзапросом при чтении из базы, поэтому у
// структуры, прочитанной до правки и пролежавшей в памяти, оно остаётся
// прежним: ответ на запрос, который эту дату только что изменил, врёт о ней.
// У PUT это было починено в другой волне, но без теста; у возврата к версии
// не чинилось вовсе. Проверяются оба — иначе починенная половина отъедет
// назад так же тихо, как отъехала бы эта.
func TestPageWritePathsAnswerWithRereadPage(t *testing.T) {
	t.Run("put", func(t *testing.T) {
		stored := &models.Page{
			ID: 42, WorkID: 14, PageNumber: 730,
			ContentMarkdown: "текст после машинной вычитки",
			Status:          models.PageStatusMachineProofread,
		}
		pages := rereadPageStore(stored)
		h := NewPageHandler(pages, &fakePageVersions{}, newFakeFragmentStore(), &fakeDocumentCutStore{},
			markdown.NewRenderer(), storage.NewMemoryStorage(), time.Minute)

		body, _ := json.Marshal(map[string]string{
			"content_markdown": "правка редактора",
			"status":           string(models.PageStatusProofread),
		})
		req := editorRequest(
			httptest.NewRequest(http.MethodPut, "/api/works/14/pages/42", bytes.NewReader(body)),
			map[string]string{"workId": "14", "pageId": "42"})
		rec := httptest.NewRecorder()
		h.Update(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("код %d; тело %s", rec.Code, rec.Body.String())
		}
		got := decodedPage(t, rec)
		if got.TextEditedAt == nil {
			t.Fatal("в ответе нет text_edited_at: отдана полоса, прочитанная ДО правки")
		}
		if !got.TextEditedAt.Equal(editedAt) {
			t.Errorf("text_edited_at = %v, ожидалось %v", got.TextEditedAt, editedAt)
		}
		if got.ContentMarkdown != "правка редактора" {
			t.Errorf("в ответе текст %q", got.ContentMarkdown)
		}
	})

	t.Run("restore", func(t *testing.T) {
		stored := &models.Page{
			ID: 42, WorkID: 14, PageNumber: 730,
			ContentMarkdown: "текст после машинной вычитки",
			Status:          models.PageStatusMachineProofread,
		}
		pages := rereadPageStore(stored)
		versions := &fakePageVersions{version: &models.PageVersion{
			ID: 7, PageID: 42, VersionNumber: 1,
			ContentMarkdown: "текст, каким он был до вычитки",
		}}
		h := NewPageHandler(pages, versions, newFakeFragmentStore(), &fakeDocumentCutStore{},
			markdown.NewRenderer(), storage.NewMemoryStorage(), time.Minute)

		req := editorRequest(
			httptest.NewRequest(http.MethodPost, "/api/works/14/pages/42/versions/7/restore", nil),
			map[string]string{"workId": "14", "pageId": "42", "versionId": "7"})
		rec := httptest.NewRecorder()
		h.RestoreVersion(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("код %d; тело %s", rec.Code, rec.Body.String())
		}
		got := decodedPage(t, rec)
		if got.TextEditedAt == nil {
			t.Fatal("в ответе нет text_edited_at: отдана полоса, прочитанная ДО возврата")
		}
		if !got.TextEditedAt.Equal(editedAt) {
			t.Errorf("text_edited_at = %v, ожидалось %v", got.TextEditedAt, editedAt)
		}
		if got.ContentMarkdown != "текст, каким он был до вычитки" {
			t.Errorf("в ответе текст %q", got.ContentMarkdown)
		}
	})
}

// Перечитывание может не удаться — и тогда ответ обязан быть отказом, а не
// молча устаревшей полосой: тело, собранное «как-нибудь», хуже честного 500,
// потому что клиент принимает его за состояние после правки.
func TestRestoreVersionFailsLoudlyWhenRereadFails(t *testing.T) {
	stored := &models.Page{
		ID: 42, WorkID: 14, PageNumber: 730,
		ContentMarkdown: "текст после машинной вычитки",
		Status:          models.PageStatusMachineProofread,
	}
	pages := rereadPageStore(stored)
	inner := pages.getByIDFn
	pages.getByIDFn = func(ctx context.Context, id int64) (*models.Page, error) {
		// Первое чтение (pageOfWork) удаётся, второе — уже нет.
		if len(pages.savedVersions) > 0 {
			return nil, fmt.Errorf("база отвалилась")
		}
		return inner(ctx, id)
	}
	versions := &fakePageVersions{version: &models.PageVersion{
		ID: 7, PageID: 42, VersionNumber: 1, ContentMarkdown: "прежний текст",
	}}
	h := NewPageHandler(pages, versions, newFakeFragmentStore(), &fakeDocumentCutStore{},
		markdown.NewRenderer(), storage.NewMemoryStorage(), time.Minute)

	req := editorRequest(
		httptest.NewRequest(http.MethodPost, "/api/works/14/pages/42/versions/7/restore", nil),
		map[string]string{"workId": "14", "pageId": "42", "versionId": "7"})
	rec := httptest.NewRecorder()
	h.RestoreVersion(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("код %d, ожидался 500; тело %s", rec.Code, rec.Body.String())
	}
}
