package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"proofreader/internal/auth"
	"proofreader/internal/models"
)

// Признак «черновик разошёлся с тем, что на людях» (M7) обязан считаться
// ТОЛЬКО для того, кто вправе видеть черновик, и оставаться ложным для
// остальных.
//
// Сторожа на это не было: повторная рецензия прогнала мутацию «считать
// признак всем подряд» — и она прошла зелёной через тридцать два теста.
// Поведение было верным, сторожа не было; здесь он заводится.
//
// Утечка тут не гипотетическая по форме, а по содержанию: сам ФАКТ «у автора
// лежит неотправленная правка» — сведение о черновике, которого посторонний
// знать не должен ровно по тому же доводу, по которому documentForViewer
// гасит ReviewStatus и RejectReason («модерация — не театр»).
func TestViewTellsUnpublishedChangesOnlyToDraftReaders(t *testing.T) {
	newDoc := func() *models.Document {
		return &models.Document{
			ID: 7, AuthorNickname: "чтец", Slug: "razbor", OwnerID: ptrInt64(5),
			Title: "Разбор", MarkdownContent: "новая, ещё не отправленная редакция",
			PublishedTitle: "Разбор", PublishedMarkdown: "прежняя редакция",
			PublishedAt: timePtr(time.Now()), WasPublished: true,
			ReviewStatus: models.DocumentApproved,
		}
	}

	for _, tc := range []struct {
		name   string
		claims *auth.Claims
		want   bool
	}{
		{"автор", readerClaims(5, "чтец"), true},
		{"аноним", nil, false},
		{"посторонний читатель", readerClaims(6, "другой"), false},
		// Разбор «одобрено», не «на_рассмотрении», — mayReadDraft сюда
		// сотрудника не пускает, и признак ему тоже не полагается.
		{"редактор, которому черновик не подавали", editorClaims(), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newTestDocumentHandler(t, newDoc())
			rr := doGetVars(t, h.View, "/api/documents/чтец/razbor/view",
				map[string]string{"nickname": "чтец", "slug": "razbor"}, tc.claims)
			if rr.Code != http.StatusOK {
				t.Fatalf("код %d: %s", rr.Code, rr.Body.String())
			}
			if got := viewFlag(t, rr, "has_unpublished_changes"); got != tc.want {
				t.Errorf("has_unpublished_changes = %v, ожидалось %v", got, tc.want)
			}
		})
	}

	// Контрольная группа: автору БЕЗ расхождения редакций признак тоже
	// ложен — он говорит о расхождении, а не о том, что смотрит автор.
	same := newDoc()
	same.MarkdownContent = same.PublishedMarkdown
	same.Title = same.PublishedTitle
	h := newTestDocumentHandler(t, same)
	rr := doGetVars(t, h.View, "/api/documents/чтец/razbor/view",
		map[string]string{"nickname": "чтец", "slug": "razbor"}, readerClaims(5, "чтец"))
	if viewFlag(t, rr, "has_unpublished_changes") {
		t.Error("признак поднят у автора без расхождения редакций")
	}

	// И модератору ПОДАННОГО — поднят: он вправе видеть черновик.
	pending := newDoc()
	pending.ReviewStatus = models.DocumentPending
	h = newTestDocumentHandler(t, pending)
	rr = doGetVars(t, h.View, "/api/documents/чтец/razbor/view",
		map[string]string{"nickname": "чтец", "slug": "razbor"}, editorClaims())
	if !viewFlag(t, rr, "has_unpublished_changes") {
		t.Error("модератор поданного не получил признак расхождения")
	}
}

func viewFlag(t *testing.T, rr *httptest.ResponseRecorder, key string) bool {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("ответ не разобрался: %v (%s)", err, rr.Body.String())
	}
	got, ok := body[key]
	if !ok {
		t.Fatalf("в ответе нет поля %q: %s", key, rr.Body.String())
	}
	flag, ok := got.(bool)
	if !ok {
		t.Fatalf("поле %q не булево: %#v", key, got)
	}
	return flag
}
