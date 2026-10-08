package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"proofreader/internal/auth"
	"proofreader/internal/middleware"
	"proofreader/internal/models"
	"proofreader/internal/repository"
	"proofreader/pkg/markdown"
)

// fakeDocumentStore — подставное хранилище разборов. GetByID на чужой id
// отвечает ошибкой, как отвечает настоящий репозиторий (pgx.ErrNoRows там
// переводится в "document not found"), а не nil-ом без ошибки.
// getErr, если выставлен, отдаётся из GetByID вместо обычной проверки
// «нет строки» — тем самым отличает в тестах сбой хранилища (текст ошибки
// без суффикса "not found") от честного отсутствия документа (с суффиксом),
// не заводя второго фейка ради круга правок 1 задачи 9.
type fakeDocumentStore struct {
	doc    *models.Document
	getErr error

	// docs — дополнительные разборы для методов-выборок (List*) и для
	// модерации (задача 6), адресуемые по ID документа, а не позиционно:
	// круг модерации (Submit → Approve/Reject) достаёт документ обратно по
	// id, который сам назначил Create, и позиционный индекс срывался бы при
	// первом же несовпадении порядка. doc, если задан, участвует в выборках
	// наравне с docs — тем самым существующие тесты, заполняющие только doc,
	// не меняют поведение.
	docs map[int64]*models.Document

	listPublishedErr error
	listByOwnerErr   error
	listForReviewErr error
	// submitDenied — SubmitWithinLimit отвечает (false, nil), как настоящий
	// репозиторий отвечает при исчерпанном пределе, не задевая записи.
	submitDenied bool
	submitErr    error
	approveErr   error
	rejectErr    error
	unpublishErr error
	// submitted — SubmitWithinLimit был вызван хотя бы раз. Отличает круг
	// «сотрудник публикует свой разбор сразу, минуя очередь» от «отправлен
	// в очередь»: оба кода ответа 200, но только явный флаг ловит попытку
	// сотрудника всё-таки уйти в SubmitWithinLimit, а не в Approve напрямую.
	submitted bool
}

// newFakeDocumentStore строит фейк с готовым набором документов, ключ —
// собственный ID документа (а не порядок в списке аргументов): круг
// модерации в document_review_test.go достаёт документ обратно по этому ID
// (store.docs[1]), а не по позиции.
func newFakeDocumentStore(docs ...*models.Document) *fakeDocumentStore {
	store := &fakeDocumentStore{docs: make(map[int64]*models.Document, len(docs))}
	for _, d := range docs {
		store.docs[d.ID] = d
	}
	return store
}

// Create пишет строку и проставляет то, что проставила бы БД через
// `RETURNING` реального INSERT (только title/markdown_content/owner_id/
// author_nickname идут в сам INSERT — остальные колонки берут табличные
// умолчания 000028: review_status='черновик', published_at NULL,
// was_published=false, и так далее). Id — следующий свободный среди уже
// известных фейку, как BIGSERIAL. Документ дописывается в f.docs, чтобы
// последующий GetByID/List этого же теста его нашёл — до фикс-раунда 2
// задачи 4 фейк был немым no-op, и «создал → тут же вижу в списке» было бы
// незаметно сломано для первого же теста, который на это положился бы
// (см. task-4-report.md, фикс-раунд 2).
func (f *fakeDocumentStore) Create(_ context.Context, d *models.Document) error {
	var maxID int64
	for _, existing := range f.allDocs() {
		if existing.ID > maxID {
			maxID = existing.ID
		}
	}
	d.ID = maxID + 1
	now := time.Now()
	d.CreatedAt = now
	d.UpdatedAt = now
	d.PublishedTitle = ""
	d.PublishedMarkdown = ""
	d.PublishedAt = nil
	d.WasPublished = false
	d.ReviewStatus = models.DocumentDraft
	d.RejectReason = nil
	d.ModeratorID = nil
	d.ReviewedAt = nil
	d.SubmittedAt = nil
	d.SubmitIPHash = ""
	if f.docs == nil {
		f.docs = map[int64]*models.Document{}
	}
	f.docs[d.ID] = d
	return nil
}

// GetByID ищет по ВСЕМ известным фейку документам (doc и docs — allDocs()),
// а не только по doc: до фикс-раунда 2 задачи 4 поиск шёл лишь по doc, и
// документ, дописанный в docs (например, только что созданный Create),
// был бы для GetByID невидим, хотя настоящий SELECT по id находит любую
// строку таблицы вне зависимости от того, как её завели.
func (f *fakeDocumentStore) GetByID(_ context.Context, id int64) (*models.Document, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	for _, d := range f.allDocs() {
		if d.ID == id {
			return d, nil
		}
	}
	return nil, fmt.Errorf("document not found")
}

// GetByAuthorSlug ищет тем же способом, что GetByID, — по ВСЕМ известным
// фейку документам (allDocs()), сравнивая пару «подпись + слаг» точно, как
// настоящий репозиторий. Отсутствие — ошибка с суффиксом "not found", а не
// (nil, nil): фейк, отдающий находку мягче репозитория, уже дважды прятал
// сломанный обработчик за зелёными тестами (см. память проекта).
func (f *fakeDocumentStore) GetByAuthorSlug(_ context.Context, nickname, slug string) (*models.Document, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	for _, d := range f.allDocs() {
		if d.AuthorNickname == nickname && d.Slug == slug {
			return d, nil
		}
	}
	return nil, fmt.Errorf("document not found")
}

// List отдаёт ВСЕ разборы — черновики и снятое включительно, — сужая по
// ownerID, когда тот задан: тот же фильтр, что реальный SQL
// `WHERE 1=1 [AND owner_id = $n] ORDER BY created_at DESC LIMIT/OFFSET`.
// До задачи 4 у метода не было ни одного вызывающего кода (обработчик List
// звал ListPublished), и заглушка `return nil, nil` осталась незамеченной —
// ровно тот класс расхождения, из-за которого мутация 2 (замена
// ListPublished на List в обработчике) валила тест по неверной причине
// («список пуст»), а не по нужной («в списке черновик»). Смотри
// фикс-раунд 1 задачи 4.
func (f *fakeDocumentStore) List(_ context.Context, limit, offset int, ownerID *int64) ([]*models.Document, error) {
	if limit <= 0 {
		return nil, nil
	}
	var out []*models.Document
	for _, d := range f.allDocs() {
		if ownerID != nil && (d.OwnerID == nil || *d.OwnerID != *ownerID) {
			continue
		}
		out = append(out, d)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if offset >= len(out) {
		return nil, nil
	}
	end := len(out)
	if offset+limit < end {
		end = offset + limit
	}
	return out[offset:end], nil
}

// allDocs — doc и docs вместе, doc первым. С фикс-раунда 2 задачи 4
// GetByID/Create/Update/Delete тоже читают его: Create дописывает сюда
// новый документ, Update/Delete по нему же ищут id.
func (f *fakeDocumentStore) allDocs() []*models.Document {
	out := make([]*models.Document, 0, len(f.docs)+1)
	if f.doc != nil {
		out = append(out, f.doc)
	}
	for _, d := range f.docs {
		out = append(out, d)
	}
	return out
}

// ListPublished фильтрует сам — только published_at IS NOT NULL, свежим
// вперёд — а не отдаёт всё подряд: настоящий репозиторий именно так и
// устроен, и фейк, отдающий всё, спрятал бы черновик на витрине в тесте.
// limit <= 0 отдаёт ноль строк — так же, как настоящий `LIMIT $1` на
// limit=0 (отрицательный limit сюда не приходит: обработчик его не
// пропускает, а настоящий Postgres на нём просто откажет запросом).
func (f *fakeDocumentStore) ListPublished(_ context.Context, limit, offset int) ([]*models.Document, error) {
	if f.listPublishedErr != nil {
		return nil, f.listPublishedErr
	}
	if limit <= 0 {
		return nil, nil
	}
	var out []*models.Document
	for _, d := range f.allDocs() {
		if d.PublishedAt != nil {
			out = append(out, d)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].PublishedAt.After(*out[j].PublishedAt) })
	if offset >= len(out) {
		return nil, nil
	}
	end := len(out)
	if offset+limit < end {
		end = offset + limit
	}
	return out[offset:end], nil
}

func (f *fakeDocumentStore) ListByOwner(_ context.Context, ownerID int64) ([]*models.Document, error) {
	if f.listByOwnerErr != nil {
		return nil, f.listByOwnerErr
	}
	var out []*models.Document
	for _, d := range f.allDocs() {
		if d.OwnerID != nil && *d.OwnerID == ownerID {
			out = append(out, d)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}

func (f *fakeDocumentStore) ListForReview(_ context.Context) ([]*models.Document, error) {
	if f.listForReviewErr != nil {
		return nil, f.listForReviewErr
	}
	var out []*models.Document
	for _, d := range f.allDocs() {
		if d.ReviewStatus == models.DocumentPending {
			out = append(out, d)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].SubmittedAt == nil || out[j].SubmittedAt == nil {
			return false
		}
		return out[i].SubmittedAt.Before(*out[j].SubmittedAt)
	})
	return out, nil
}

// SubmitWithinLimit отвечает (false, "document not found") на чужой id —
// как настоящий репозиторий после фикс-раунда 2: там false без ошибки
// значит «упёрся в предел частоты» (submitDenied ниже), а false с ошибкой,
// оканчивающейся на "not found", — «разбора не существует». Путать их
// нельзя: молчаливый true на несуществующем id утверждал бы автору, что
// правка отправлена на рассмотрение, хотя в очереди её нет.
func (f *fakeDocumentStore) SubmitWithinLimit(_ context.Context, id int64, ipHash string, _ int, _ time.Time) (bool, error) {
	f.submitted = true
	if f.submitErr != nil {
		return false, f.submitErr
	}
	if f.submitDenied {
		return false, nil
	}
	for _, d := range f.allDocs() {
		if d.ID == id {
			now := time.Now()
			d.ReviewStatus = models.DocumentPending
			d.SubmittedAt = &now
			d.SubmitIPHash = ipHash
			d.RejectReason = nil
			return true, nil
		}
	}
	return false, fmt.Errorf("document not found")
}

// Approve, Reject, Unpublish отвечают "document not found" на несуществующий
// id — так же, как настоящий репозиторий после фикс-раунда 1 задачи 4
// (RowsAffected() == 0). Раньше цикл просто не находил совпадения и молча
// возвращал nil; фейк, добрее настоящего SQL, спрятал бы этот путь от тестов
// обработчика.
//
// С фикс-раунда 1 задачи 6 Approve/Reject/Unpublish на СУЩЕСТВУЮЩЕЙ, но не в
// том состоянии строке отвечают repository.ErrDocumentNotPending /
// ErrDocumentNotPublished — зеркало атомарного guard'а в WHERE настоящего
// SQL. Без этого фейк снова был бы добрее репозитория и прятал бы гонку двух
// модераторов от тестов обработчика.
func (f *fakeDocumentStore) Approve(_ context.Context, id, moderatorID int64) error {
	if f.approveErr != nil {
		return f.approveErr
	}
	for _, d := range f.allDocs() {
		if d.ID == id {
			if d.ReviewStatus != models.DocumentPending {
				return repository.ErrDocumentNotPending
			}
			f.publish(d, moderatorID)
			return nil
		}
	}
	return fmt.Errorf("document not found")
}

// PublishOwn — сотруднический самопубликующийся путь Submit: тот же перенос
// в показываемую редакцию, что и Approve, но БЕЗ проверки review_status —
// сотрудник публикует прямо из черновика, минуя очередь (см.
// repository.DocumentRepository.PublishOwn).
func (f *fakeDocumentStore) PublishOwn(_ context.Context, id, moderatorID int64) error {
	if f.approveErr != nil {
		return f.approveErr
	}
	for _, d := range f.allDocs() {
		if d.ID == id {
			f.publish(d, moderatorID)
			return nil
		}
	}
	return fmt.Errorf("document not found")
}

// publish — общее тело Approve/PublishOwn: перенос черновика в показываемую
// редакцию.
func (f *fakeDocumentStore) publish(d *models.Document, moderatorID int64) {
	now := time.Now()
	d.PublishedTitle = d.Title
	d.PublishedMarkdown = d.MarkdownContent
	d.PublishedAt = &now
	d.WasPublished = true
	d.ReviewStatus = models.DocumentApproved
	d.RejectReason = nil
	d.ModeratorID = &moderatorID
	d.ReviewedAt = &now
}

func (f *fakeDocumentStore) Reject(_ context.Context, id, moderatorID int64, reason models.DocumentRejectReason) error {
	if f.rejectErr != nil {
		return f.rejectErr
	}
	for _, d := range f.allDocs() {
		if d.ID == id {
			if d.ReviewStatus != models.DocumentPending {
				return repository.ErrDocumentNotPending
			}
			now := time.Now()
			d.ReviewStatus = models.DocumentRejected
			d.RejectReason = &reason
			d.ModeratorID = &moderatorID
			d.ReviewedAt = &now
			return nil
		}
	}
	return fmt.Errorf("document not found")
}

func (f *fakeDocumentStore) Unpublish(_ context.Context, id int64) error {
	if f.unpublishErr != nil {
		return f.unpublishErr
	}
	for _, d := range f.allDocs() {
		if d.ID == id {
			if d.PublishedAt == nil {
				return repository.ErrDocumentNotPublished
			}
			d.PublishedAt = nil
			// Снятие отзывает и заявку — ровно как настоящий репозиторий
			// (I2): иначе фейк отдавал бы форму, которой репозиторий больше
			// не возвращает, и обработчик проверялся бы против выдумки.
			if d.ReviewStatus == models.DocumentPending {
				d.ReviewStatus = models.DocumentDraft
				d.SubmittedAt = nil
			}
			return nil
		}
	}
	return fmt.Errorf("document not found")
}

// Update правит title/markdown_content и проставляет updated_at — как
// настоящий `UPDATE ... RETURNING updated_at`; published_* не трогает
// (правка черновика не меняет того, что на людях). Несуществующий id
// отвечает "document not found", как настоящий репозиторий на
// RowsAffected()==0 (pgx.ErrNoRows).
//
// С фикс-раунда 1 задачи 6: правка разбора в состоянии «на_рассмотрении»
// возвращает его в «черновик» и стирает след отправки — зеркало CASE в
// настоящем SQL (см. комментарий над DocumentRepository.Update). Черновик и
// уже рассмотренное правятся как раньше, без побочных полей.
func (f *fakeDocumentStore) Update(_ context.Context, document *models.Document) error {
	for _, d := range f.allDocs() {
		if d.ID == document.ID {
			d.Title = document.Title
			d.MarkdownContent = document.MarkdownContent
			if d.ReviewStatus == models.DocumentPending {
				d.ReviewStatus = models.DocumentDraft
				d.SubmittedAt = nil
				d.SubmitIPHash = ""
			}
			d.UpdatedAt = time.Now()
			document.UpdatedAt = d.UpdatedAt
			document.ReviewStatus = d.ReviewStatus
			return nil
		}
	}
	return fmt.Errorf("document not found")
}

// Delete снимает строку целиком — как настоящий `DELETE ... WHERE id = $1`.
// Несуществующий id отвечает "document not found", тем же суффиксом, что и
// настоящий репозиторий на RowsAffected()==0.
func (f *fakeDocumentStore) Delete(_ context.Context, id int64) error {
	if f.doc != nil && f.doc.ID == id {
		f.doc = nil
		return nil
	}
	if _, ok := f.docs[id]; ok {
		delete(f.docs, id)
		return nil
	}
	return fmt.Errorf("document not found")
}

func createTestDocumentHandler() *DocumentHandler {
	return &DocumentHandler{
		documentRepo: nil,
		renderer:     markdown.NewRenderer(),
		cuts:         &fakeDocumentCutStore{},
		works:        &fakeWorkStore{},
	}
}

// newTestDocumentHandlerWithCuts строит обработчик поверх fakeDocumentStore,
// заполненного docs: первый достаётся полю doc (GetByID/Get/View читают
// именно его по совпадению ID), остальные — полю docs (выборки List*
// проходят по ним обоим через allDocs()). cuts передаётся явно — задачи 5-7
// заводят вклейки поверх той же фабрики, не переписывая её.
func newTestDocumentHandlerWithCuts(t *testing.T, cuts DocumentCutStore, docs ...*models.Document) *DocumentHandler {
	t.Helper()
	store := &fakeDocumentStore{}
	if len(docs) > 0 {
		store.doc = docs[0]
		if len(docs) > 1 {
			store.docs = make(map[int64]*models.Document, len(docs)-1)
			for _, d := range docs[1:] {
				store.docs[d.ID] = d
			}
		}
	}
	return &DocumentHandler{
		documentRepo: store,
		renderer:     markdown.NewRenderer(),
		cuts:         cuts,
		works:        &fakeWorkStore{},
	}
}

func newTestDocumentHandler(t *testing.T, docs ...*models.Document) *DocumentHandler {
	t.Helper()
	return newTestDocumentHandlerWithCuts(t, &fakeDocumentCutStore{}, docs...)
}

// doGetVars/doPostVars/doPutVars/doDeleteVars — одно семейство тестовых
// помощников на весь пакет API-обработчиков разбора: переменные пути кладутся
// явно через mux.SetURLVars (обработчики читают их оттуда, без этого
// mux.Vars(r)["id"] вернёт пустую строку), claims — тем же способом, каким их
// кладёт middleware.AuthMiddleware (см. withClaims в
// page_suggestion_handler_test.go).
func doGetVars(t *testing.T, h http.HandlerFunc, path string, vars map[string]string, claims *auth.Claims) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if vars != nil {
		req = mux.SetURLVars(req, vars)
	}
	rec := httptest.NewRecorder()
	withClaims(claims, h)(rec, req)
	return rec
}

func doPostVars(t *testing.T, h http.HandlerFunc, path, body string, vars map[string]string, claims *auth.Claims) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	if vars != nil {
		req = mux.SetURLVars(req, vars)
	}
	rec := httptest.NewRecorder()
	withClaims(claims, h)(rec, req)
	return rec
}

func doPutVars(t *testing.T, h http.HandlerFunc, path, body string, vars map[string]string, claims *auth.Claims) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, path, strings.NewReader(body))
	if vars != nil {
		req = mux.SetURLVars(req, vars)
	}
	rec := httptest.NewRecorder()
	withClaims(claims, h)(rec, req)
	return rec
}

func doDeleteVars(t *testing.T, h http.HandlerFunc, path string, vars map[string]string, claims *auth.Claims) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodDelete, path, nil)
	if vars != nil {
		req = mux.SetURLVars(req, vars)
	}
	rec := httptest.NewRecorder()
	withClaims(claims, h)(rec, req)
	return rec
}

// Правка опубликованного разбора не меняет того, что на людях, пока её не
// одобрили. Это главный признак готовности ветки: без него модерация —
// театр (пропустить безобидное, потом переписать).
func TestPendingRevisionStaysOffPublicRead(t *testing.T) {
	doc := &models.Document{
		ID: 1, Title: "Новое заглавие",
		Slug:              "novoe-zaglavie",
		MarkdownContent:   "новое тело, ещё не одобрено",
		PublishedTitle:    "Старое заглавие",
		PublishedMarkdown: "старое тело, одобрено",
		PublishedAt:       timePtr(time.Now()), WasPublished: true,
		AuthorNickname: "чтец", OwnerID: ptrInt64(5),
		ReviewStatus: models.DocumentPending,
	}
	h := newTestDocumentHandler(t, doc)
	vars := map[string]string{"nickname": "чтец", "slug": "novoe-zaglavie"}

	for _, route := range []struct {
		name    string
		handler http.HandlerFunc
		path    string
	}{
		{"карточка", h.Get, "/api/documents/чтец/novoe-zaglavie"},
		{"сборка", h.View, "/api/documents/чтец/novoe-zaglavie/view"},
	} {
		rr := doGetVars(t, route.handler, route.path, vars, nil /* аноним */)
		if rr.Code != http.StatusOK {
			t.Fatalf("%s: код %d", route.name, rr.Code)
		}
		body := rr.Body.String()
		if strings.Contains(body, "ещё не одобрено") {
			t.Errorf("%s: неодобренный черновик уехал анониму", route.name)
		}
		if !strings.Contains(body, "старое тело") {
			t.Errorf("%s: одобренная редакция не показана: %s", route.name, body)
		}
	}
}

// Витрина показывает только то, что на людях. Черновик в публичном списке —
// та самая утечка, ради которой модерация и заводится.
func TestListShowsOnlyPublished(t *testing.T) {
	published := &models.Document{ID: 1, Title: "На людях",
		PublishedTitle: "На людях", PublishedMarkdown: "тело",
		PublishedAt: timePtr(time.Now()), WasPublished: true}
	draft := &models.Document{ID: 2, Title: "Тайный черновик",
		MarkdownContent: "тело черновика", AuthorNickname: "чтец", OwnerID: ptrInt64(5)}
	h := newTestDocumentHandler(t, published, draft)

	rr := doGetVars(t, h.List, "/api/documents", nil, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("код %d", rr.Code)
	}
	body := rr.Body.String()

	// Текстовая проверка ловит утечку СОДЕРЖИМОГО — она по-прежнему
	// осмысленна для соседнего случая (опубликованный разбор с ждущей
	// правкой: объект в списке быть обязан, а неодобренный текст — нет,
	// см. TestPublishedDocumentWithRejectedRevisionShowsApprovedOnly для
	// Get/View). Для ЭТОГО черновика — никогда не публиковавшегося — она
	// проходит и до, и после фикса ниже: documentForViewer маскирует
	// title/markdown_content любого непубличного разбора в пустую строку
	// (PublishedTitle/PublishedMarkdown у него и так пустые), поэтому
	// секретный текст не появляется в ответе НИКОГДА, вне зависимости от
	// того, обязан ли сам документ быть в списке. Одна эта проверка
	// утечку факта присутствия черновика не ловит — для неё отдельная
	// проверка состава ниже (см. task-4-report.md, фикс-раунд 2).
	if strings.Contains(body, "Тайный черновик") {
		t.Error("черновик попал в публичную витрину")
	}
	if !strings.Contains(body, "На людях") {
		t.Error("опубликованный разбор пропал из витрины")
	}

	// Проверка СОСТАВА — граница, которую эта мутация реально нарушает:
	// черновика не должно быть в массиве вообще, а не только в его тексте.
	// Видимость строк списка решает ListPublished (выбор строк), а не
	// documentForViewer (маскировка полей уже отобранной строки) — им
	// нельзя подменять друг друга.
	var shown []struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &shown); err != nil {
		t.Fatalf("разбор ответа: %v: %s", err, body)
	}
	if len(shown) != 1 || shown[0].ID != published.ID {
		t.Fatalf("витрина обязана содержать ровно один опубликованный разбор (id=%d), получила %+v",
			published.ID, shown)
	}
}

// Сочетание, которое просила рецензия задачи 3: разбор опубликован, а его
// ТЕКУЩАЯ редакция отклонена. Посторонний обязан видеть одобренную редакцию
// и не видеть ни отклонённого черновика, ни причины отказа — documentVisibleTo
// решает по PublishedAt, а не по ReviewStatus, и это поведение регрессией не
// застраховано.
func TestPublishedDocumentWithRejectedRevisionShowsApprovedOnly(t *testing.T) {
	reason := models.DocumentRejectAbuse
	doc := &models.Document{
		ID: 3, Title: "Отклонённое заглавие",
		Slug:              "otklonennoe-zaglavie",
		MarkdownContent:   "отклонённое тело",
		PublishedTitle:    "Опубликованное заглавие",
		PublishedMarkdown: "опубликованное тело",
		PublishedAt:       timePtr(time.Now()), WasPublished: true,
		AuthorNickname: "чтец", OwnerID: ptrInt64(5),
		ReviewStatus: models.DocumentRejected,
		RejectReason: &reason,
	}
	h := newTestDocumentHandler(t, doc)
	vars := map[string]string{"nickname": "чтец", "slug": "otklonennoe-zaglavie"}

	for _, route := range []struct {
		name    string
		handler http.HandlerFunc
		path    string
	}{
		{"карточка", h.Get, "/api/documents/чтец/otklonennoe-zaglavie"},
		{"сборка", h.View, "/api/documents/чтец/otklonennoe-zaglavie/view"},
	} {
		rr := doGetVars(t, route.handler, route.path, vars, nil /* аноним */)
		if rr.Code != http.StatusOK {
			t.Fatalf("%s: код %d", route.name, rr.Code)
		}
		body := rr.Body.String()
		if strings.Contains(body, "отклонённое тело") || strings.Contains(body, "Отклонённое заглавие") {
			t.Errorf("%s: отклонённый черновик уехал анониму: %s", route.name, body)
		}
		if !strings.Contains(body, "опубликованное тело") {
			t.Errorf("%s: одобренная редакция не показана: %s", route.name, body)
		}
		if strings.Contains(body, string(models.DocumentRejectAbuse)) || strings.Contains(body, "reject_reason") {
			t.Errorf("%s: причина отказа уехала постороннему: %s", route.name, body)
		}
	}
}

// Сбой хранилища при чтении разбора — не то же самое, что честное отсутствие:
// 404 обязан значить «разбора нет», а не «база недоступна прямо сейчас».
// Тот же приём (getErr в fakeDocumentStore), что уже проверяет это различие
// у DocumentCutHandler (TestCreateCutDocumentStorageErrorIsServerError).
func TestDocumentReadStorageErrorIsServerErrorNotNotFound(t *testing.T) {
	failing := &fakeDocumentStore{getErr: errors.New("connection reset")}
	// getErr отвечает раньше любого сравнения ника/слага (см. fakeDocumentStore
	// .GetByAuthorSlug) — значение "slug" тут произвольно, важно только само
	// наличие ключа, которого ждёт documentKey.
	vars := map[string]string{"slug": "1"}

	for _, route := range []struct {
		name    string
		handler func(http.ResponseWriter, *http.Request)
		path    string
	}{
		{"Get", (&DocumentHandler{documentRepo: failing, renderer: markdown.NewRenderer(), cuts: &fakeDocumentCutStore{}, works: &fakeWorkStore{}}).Get, "/api/documents/1"},
		{"View", (&DocumentHandler{documentRepo: failing, renderer: markdown.NewRenderer(), cuts: &fakeDocumentCutStore{}, works: &fakeWorkStore{}}).View, "/api/documents/1/view"},
		{"Delete", (&DocumentHandler{documentRepo: failing, renderer: markdown.NewRenderer(), cuts: &fakeDocumentCutStore{}, works: &fakeWorkStore{}}).Delete, "/api/documents/1"},
	} {
		rr := doGetVars(t, route.handler, route.path, vars, editorClaims())
		assertServerErrorWithMessage(t, rr)
	}
}

func TestNewDocumentHandler(t *testing.T) {
	renderer := markdown.NewRenderer()
	handler := NewDocumentHandler(nil, renderer, &fakeDocumentCutStore{}, &fakeWorkStore{}, nil)

	if handler == nil {
		t.Fatal("Expected handler to be created")
	}
	if handler.renderer != renderer {
		t.Error("Expected renderer to be set")
	}
}

// До адреса-пары строка "invalid" в {id} не парсилась как число и обработчик
// отвечал 400 до похода в хранилище. По адресу-паре {slug} — свободная
// строка, парсить нечего: "invalid" такой же кандидат в слаг, как любой
// другой, и единственный честный ответ на несуществующий слаг — 404, тем же
// путём (loadDocumentByKey), что и любой другой ненайденный разбор. Это
// НАЙДЕННОЕ ПРИ ПРАВКЕ изменение ожидания теста (400 → 404), а не подгонка
// фикстуры, — см. задачу 3, task-3-report.md. Хранилище здесь обязано быть
// не-nil (пустой fakeDocumentStore), иначе loadDocumentByKey падает паникой
// на разыменовании nil-интерфейса — при старой проверке ParseInt хранилище
// не трогалось вовсе, поэтому createTestDocumentHandler() с documentRepo:
// nil было безопасно.
func TestDocumentHandler_Get_UnknownSlug(t *testing.T) {
	handler := createTestDocumentHandler()
	handler.documentRepo = &fakeDocumentStore{}

	req := httptest.NewRequest(http.MethodGet, "/api/documents/invalid", nil)
	req = mux.SetURLVars(req, map[string]string{"slug": "invalid"})
	rec := httptest.NewRecorder()

	handler.Get(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestDocumentHandler_Create_InvalidJSON(t *testing.T) {
	handler := createTestDocumentHandler()

	req := httptest.NewRequest(http.MethodPost, "/api/documents", bytes.NewBufferString("invalid json"))
	rec := httptest.NewRecorder()

	handler.Create(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestDocumentHandler_Create_NoUserContext(t *testing.T) {
	handler := createTestDocumentHandler()

	body := CreateDocumentRequest{
		Title:           "Test Document",
		MarkdownContent: "# Test",
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/documents", bytes.NewBuffer(jsonBody))
	rec := httptest.NewRecorder()

	handler.Create(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

// См. комментарий у TestDocumentHandler_Get_UnknownSlug — то же изменение
// ожидания (400 → 404) по той же причине: адрес-пара не парсит {slug} как
// число, "invalid" такой же кандидат в слаг, как любой другой, и до проверки
// авторства Update не доходит вовсе — документ не найден раньше.
func TestDocumentHandler_Update_UnknownSlug(t *testing.T) {
	handler := createTestDocumentHandler()
	handler.documentRepo = &fakeDocumentStore{}

	// Тело обязано разобраться как валидный JSON — иначе Update отвечает 400
	// ещё до чтения адреса (декодирование тела идёт первым шагом), и тест
	// проверял бы не то, что заявлено в имени.
	body := UpdateDocumentRequest{Title: "Заглавие", MarkdownContent: "Текст"}
	jsonBody, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPut, "/api/documents/invalid", bytes.NewBuffer(jsonBody))
	req = mux.SetURLVars(req, map[string]string{"slug": "invalid"})
	rec := httptest.NewRecorder()

	handler.Update(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestDocumentHandler_Update_InvalidJSON(t *testing.T) {
	handler := createTestDocumentHandler()

	req := httptest.NewRequest(http.MethodPut, "/api/documents/1", bytes.NewBufferString("invalid json"))
	req = mux.SetURLVars(req, map[string]string{"slug": "1"})
	rec := httptest.NewRecorder()

	handler.Update(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

// См. комментарий у TestDocumentHandler_Get_UnknownSlug — то же изменение
// ожидания (400 → 404) по той же причине.
func TestDocumentHandler_Delete_UnknownSlug(t *testing.T) {
	handler := createTestDocumentHandler()
	handler.documentRepo = &fakeDocumentStore{}

	req := httptest.NewRequest(http.MethodDelete, "/api/documents/invalid", nil)
	req = mux.SetURLVars(req, map[string]string{"slug": "invalid"})
	rec := httptest.NewRecorder()

	handler.Delete(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// Задача 10 (тот же дефект, что у Unpublish — .scratch/razbor-vkleyka/issues/
// 03-…): удаление разбора отдаётся краулеру тот же час, что и снятие с
// публикации, из того же кэша. Наблюдаемое здесь — что Delete зовёт сброс
// краулерского кэша ровно один раз.
func TestDocumentHandler_Delete_PurgesCrawlerCache(t *testing.T) {
	doc := &models.Document{
		ID: 1, Slug: "razbor", AuthorNickname: "чтец", OwnerID: ptrInt64(5),
		PublishedAt: timePtr(time.Now()), WasPublished: true,
	}
	handler := newTestDocumentHandler(t, doc)
	crawler := &fakeCrawlerCaches{}
	handler.cache = NewServingCache(nil, crawler)

	rr := doDeleteVars(t, handler.Delete, "/api/documents/чтец/razbor",
		map[string]string{"nickname": "чтец", "slug": "razbor"}, readerClaims(5, "чтец"))
	if rr.Code != http.StatusNoContent {
		t.Fatalf("Status = %d, want %d (%s)", rr.Code, http.StatusNoContent, rr.Body.String())
	}
	if crawler.calls != 1 {
		t.Errorf("краулерский кэш сброшен %d раз, ожидался 1", crawler.calls)
	}
}

// См. комментарий у TestDocumentHandler_Get_UnknownSlug — то же изменение
// ожидания (400 → 404) по той же причине.
func TestDocumentHandler_View_UnknownSlug(t *testing.T) {
	handler := createTestDocumentHandler()
	handler.documentRepo = &fakeDocumentStore{}

	req := httptest.NewRequest(http.MethodGet, "/api/documents/invalid/view", nil)
	req = mux.SetURLVars(req, map[string]string{"slug": "invalid"})
	rec := httptest.NewRecorder()

	handler.View(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// Разбор отдаётся рендером markdown в момент чтения, а не сохранённым снимком:
// колонки html_content больше нет, и правка полосы обязана доезжать до вклейки.
func TestDocumentHandler_View_RendersAtReadTime(t *testing.T) {
	// Опубликован тем же телом, что и черновик: тест проверяет сборку HTML из
	// markdown при каждом чтении, а не решение о видимости (это отдельные
	// тесты, TestPendingRevisionStaysOffPublicRead и соседи) — без публикации
	// анониму, которым идёт этот запрос, показывать было бы нечего.
	handler := &DocumentHandler{
		documentRepo: &fakeDocumentStore{doc: &models.Document{
			ID:                7,
			Title:             "Разбор",
			Slug:              "razbor",
			MarkdownContent:   "# Заголовок\n\nТекст разбора.",
			PublishedTitle:    "Разбор",
			PublishedMarkdown: "# Заголовок\n\nТекст разбора.",
			PublishedAt:       timePtr(time.Now()),
			WasPublished:      true,
			OwnerID:           int64Ptr(1),
		}},
		renderer: markdown.NewRenderer(),
		cuts:     &fakeDocumentCutStore{},
		works:    &fakeWorkStore{},
	}

	// Сотруднический разбор — короткий адрес, пустой ник: та же пара
	// (AuthorNickname="", Slug="razbor"), что и в фикстуре.
	req := httptest.NewRequest(http.MethodGet, "/api/documents/razbor/view", nil)
	req = mux.SetURLVars(req, map[string]string{"slug": "razbor"})
	rec := httptest.NewRecorder()

	handler.View(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Status = %d, want %d", rec.Code, http.StatusOK)
	}
	var got struct {
		HTMLContent string `json:"html_content"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("разбор ответа: %v", err)
	}
	if !strings.Contains(got.HTMLContent, "<h1") || !strings.Contains(got.HTMLContent, "Текст разбора.") {
		t.Errorf("html_content собран не из markdown: %q", got.HTMLContent)
	}
}

// Финальная находка ревью ветки 3: ответ View обязан нести slug — вторую
// половину адреса разбора (вместе с author_nickname выбирает форму
// /documents/{слаг} или /documents/{ник}/{слаг}). Без него фронтовая
// documentEditPath (frontend/src/utils/documentPath.ts) не может построить
// адрес правки, и кнопка «Править» на странице самого разбора уводит на
// .../undefined/edit — единственная поверхность правки, где так происходит:
// «моё» и очередь модерации получают slug из другого источника (списочные
// методы), а не из View. Обе формы адреса — сотрудническая (пустой ник) и
// читательская — проверяются здесь одним тестом на срез.
func TestDocumentHandler_View_ResponseCarriesSlug(t *testing.T) {
	cases := []struct {
		name     string
		doc      *models.Document
		vars     map[string]string
		wantSlug string
	}{
		{
			name: "сотруднический разбор — короткий адрес",
			doc: &models.Document{
				ID: 1, Title: "Разбор", Slug: "razbor",
				MarkdownContent: "Текст.", PublishedTitle: "Разбор",
				PublishedMarkdown: "Текст.", PublishedAt: timePtr(time.Now()),
				WasPublished: true,
			},
			vars:     map[string]string{"slug": "razbor"},
			wantSlug: "razbor",
		},
		{
			name: "читательский разбор — адрес с ником",
			doc: &models.Document{
				ID: 2, Title: "Разбор читателя", Slug: "moy-razbor",
				AuthorNickname:    "чтец",
				MarkdownContent:   "Текст.",
				PublishedTitle:    "Разбор читателя",
				PublishedMarkdown: "Текст.",
				PublishedAt:       timePtr(time.Now()),
				WasPublished:      true,
				OwnerID:           ptrInt64(5),
			},
			vars:     map[string]string{"nickname": "чтец", "slug": "moy-razbor"},
			wantSlug: "moy-razbor",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler := &DocumentHandler{
				documentRepo: &fakeDocumentStore{doc: tc.doc},
				renderer:     markdown.NewRenderer(),
				cuts:         &fakeDocumentCutStore{},
				works:        &fakeWorkStore{},
			}

			path := "/api/documents/" + tc.doc.Slug + "/view"
			if tc.doc.AuthorNickname != "" {
				path = "/api/documents/" + tc.doc.AuthorNickname + "/" + tc.doc.Slug + "/view"
			}
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req = mux.SetURLVars(req, tc.vars)
			rec := httptest.NewRecorder()

			handler.View(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("Status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
			}
			var got struct {
				Slug string `json:"slug"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("разбор ответа: %v", err)
			}
			if got.Slug != tc.wantSlug {
				t.Errorf("slug = %q, want %q", got.Slug, tc.wantSlug)
			}
		})
	}
}

// Плейсхолдер на несуществующую или чужую вклейку — отказ сохранения с
// названной причиной. Молчащий рендер здесь недопустим: автор увидел бы дыру
// там, где рассчитывал на опору.
func TestDocumentUpdateRejectsUnknownCutPlaceholder(t *testing.T) {
	doc := &models.Document{ID: 5, Title: "Разбор", Slug: "razbor", MarkdownContent: "было"}
	h := &DocumentHandler{
		documentRepo: &fakeDocumentStore{doc: doc},
		cuts:         newFakeDocumentCutStore(), // вклеек у разбора нет вовсе
		renderer:     markdown.NewRenderer(),
	}

	body, _ := json.Marshal(UpdateDocumentRequest{
		Title:           "Разбор",
		MarkdownContent: "Текст.\n\n<cut id=\"777\">\n",
	})
	req := httptest.NewRequest(http.MethodPut, "/api/documents/razbor", bytes.NewReader(body))
	req = mux.SetURLVars(req, map[string]string{"slug": "razbor"})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserContextKey, editorClaims()))
	rec := httptest.NewRecorder()

	h.Update(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("код %d, ожидался 400", rec.Code)
	}
	var payload struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil || payload.Message == "" {
		t.Fatalf("ошибка пришла не как {\"message\": …}: %s", rec.Body.String())
	}
	// Проверка обязана стоять ДО присвоения полей: иначе тело уже испорчено,
	// даже если запись не дошла до репозитория.
	if doc.MarkdownContent != "было" {
		t.Fatal("тело разбора изменено, хотя ссылается на несуществующую вклейку")
	}
}

// Вклейки, которых тело больше не называет, уходят при сохранении: иначе они
// копятся невидимым мусором и переякориваются на каждой правке полосы.
func TestDocumentUpdatePrunesUnreferencedCuts(t *testing.T) {
	doc := &models.Document{ID: 5, Slug: "razbor", MarkdownContent: "<cut id=\"1\">\n\n<cut id=\"2\">"}
	cuts := newFakeDocumentCutStore(
		&models.DocumentCut{ID: 1, DocumentID: 5},
		&models.DocumentCut{ID: 2, DocumentID: 5},
	)
	h := &DocumentHandler{
		documentRepo: &fakeDocumentStore{doc: doc},
		cuts:         cuts,
		renderer:     markdown.NewRenderer(),
	}

	body, _ := json.Marshal(UpdateDocumentRequest{
		Title:           "Разбор",
		MarkdownContent: "Осталась одна.\n\n<cut id=\"1\">\n",
	})
	req := httptest.NewRequest(http.MethodPut, "/api/documents/razbor", bytes.NewReader(body))
	req = mux.SetURLVars(req, map[string]string{"slug": "razbor"})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserContextKey, editorClaims()))
	rec := httptest.NewRecorder()

	h.Update(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	kept := cuts.keptAfterPrune()
	if len(kept) != 1 || kept[0] != 1 {
		t.Fatalf("оставлены вклейки %v, ожидалась только 1", kept)
	}
}

// containsInt64 — тонкая проверка принадлежности, без завода зависимости
// ради одного среза.
func containsInt64(xs []int64, x int64) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// Сборка мусора вклеек обязана щадить ОБЕ редакции. Автор убирает вклейку из
// черновика — на людях при этом ещё стоит одобренная редакция, которая её
// называет. Снеси строку document_cuts — и опубликованный разбор покажет
// «Вклейка не найдена» на месте куска корпуса, причём молча: сохранение
// пройдёт, а увидит это только читатель.
func TestUpdateKeepsCutsReferencedByPublishedEdition(t *testing.T) {
	both := "текст\n\n<cut id=\"1\">\n\nещё текст\n\n<cut id=\"2\">\n"
	doc := &models.Document{
		ID: 1, Title: "Разбор", Slug: "razbor", MarkdownContent: both,
		PublishedTitle: "Разбор", PublishedMarkdown: both,
		PublishedAt: timePtr(time.Now()), WasPublished: true,
		AuthorNickname: "чтец", OwnerID: ptrInt64(5),
	}
	cuts := newFakeDocumentCutStore(
		&models.DocumentCut{ID: 1, DocumentID: 1},
		&models.DocumentCut{ID: 2, DocumentID: 1},
	)
	h := newTestDocumentHandlerWithCuts(t, cuts, doc)

	// В черновике осталась только первая вклейка.
	body, _ := json.Marshal(UpdateDocumentRequest{
		Title:           "Разбор",
		MarkdownContent: "текст\n\n<cut id=\"1\">\n",
	})
	rr := doPutVars(t, h.Update, "/api/documents/чтец/razbor", string(body),
		map[string]string{"nickname": "чтец", "slug": "razbor"}, readerClaims(5, "чтец"))
	if rr.Code != http.StatusOK {
		t.Fatalf("сохранение: код %d (%s)", rr.Code, rr.Body.String())
	}
	kept := cuts.keptAfterPrune()
	if !containsInt64(kept, 2) {
		t.Fatalf("сборка мусора снесла вклейку, на которую ссылается одобренная редакция; keep=%v", kept)
	}
	if !containsInt64(kept, 1) {
		t.Fatalf("сборка мусора снесла вклейку черновика; keep=%v", kept)
	}
}

// Симптом, а не механизм: тот же сценарий, что и
// TestUpdateKeepsCutsReferencedByPublishedEdition (автор убирает вклейку из
// черновика, одобренная редакция на людях всё ещё её называет), но проверка
// здесь не заглядывает во внутренний список сохранённых id — она собирает
// разбор ТЕМ ЖЕ путём, каким его получает посторонний читатель (View), и
// смотрит на готовый html_content. Внутренний тест говорит, ЧТО именно
// разъехалось (список keep); этот — заметно ли это вообще читателю. Если
// сборка мусора когда-нибудь снова снесёт вклейку, на которую опирается
// опубликованная редакция, этот тест увидит ровно то, что увидел бы читатель:
// «Вклейка не найдена» на месте куска корпуса.
func TestUpdateKeepsPublishedCutVisibleToReader(t *testing.T) {
	const corpusText = "Единственный в тесте кусок корпуса, который должен остаться виден."
	both := "текст\n\n<cut id=\"1\">\n\nещё текст\n\n<cut id=\"2\">\n"
	doc := &models.Document{
		ID: 1, Title: "Разбор", Slug: "razbor", MarkdownContent: both,
		PublishedTitle: "Разбор", PublishedMarkdown: both,
		PublishedAt: timePtr(time.Now()), WasPublished: true,
		AuthorNickname: "чтец", OwnerID: ptrInt64(5),
	}
	// Вклейка 2 — не голая заглушка, как в первом тесте: у неё есть настоящий
	// том, живой якорь и своя полоса, чтобы при живом чтении на её месте
	// стоял действительно кусок корпуса, а не просто «не битая запись».
	page := &models.Page{ID: 200, WorkID: 10, PageNumber: 42, ContentMarkdown: corpusText}
	cut2 := &models.DocumentCut{
		ID: 2, DocumentID: 1, WorkID: ptrInt64(10),
		Anchor: models.Anchor{
			StartPageID: 200, StartOffset: 0,
			EndPageID: 200, EndOffset: len(corpusText),
		},
		Status:      models.CutStatusOK,
		SourceTitle: "Источник два",
	}
	cuts := newFakeDocumentCutStore(
		&models.DocumentCut{ID: 1, DocumentID: 1},
		cut2,
	)
	cuts.pagesByCutID = map[int64][]*models.Page{2: {page}}
	h := &DocumentHandler{
		documentRepo: &fakeDocumentStore{doc: doc},
		renderer:     markdown.NewRenderer(),
		cuts:         cuts,
		works: &fakeWorkStore{getFn: func(ctx context.Context, id int64) (*models.Work, error) {
			return &models.Work{ID: id, PageOffset: 0}, nil
		}},
	}

	// В черновике осталась только первая вклейка — то же редактирование, что
	// и в первом тесте.
	body, _ := json.Marshal(UpdateDocumentRequest{
		Title:           "Разбор",
		MarkdownContent: "текст\n\n<cut id=\"1\">\n",
	})
	rr := doPutVars(t, h.Update, "/api/documents/чтец/razbor", string(body),
		map[string]string{"nickname": "чтец", "slug": "razbor"}, readerClaims(5, "чтец"))
	if rr.Code != http.StatusOK {
		t.Fatalf("сохранение: код %d (%s)", rr.Code, rr.Body.String())
	}

	// Читает посторонний, без claims — так же, как GET /api/documents/чтец/razbor/view
	// доходит до анонима, которому показывается ОДОБРЕННАЯ редакция
	// (documentForViewer), а не черновик автора.
	viewRR := doGetVars(t, h.View, "/api/documents/чтец/razbor/view",
		map[string]string{"nickname": "чтец", "slug": "razbor"}, nil)
	if viewRR.Code != http.StatusOK {
		t.Fatalf("чтение: код %d (%s)", viewRR.Code, viewRR.Body.String())
	}
	var got struct {
		HTMLContent string `json:"html_content"`
	}
	if err := json.Unmarshal(viewRR.Body.Bytes(), &got); err != nil {
		t.Fatalf("разбор ответа: %v", err)
	}
	if strings.Contains(got.HTMLContent, "Вклейка не найдена") {
		t.Fatalf("читатель видит «Вклейка не найдена» на месте куска корпуса: %q", got.HTMLContent)
	}
	if !strings.Contains(got.HTMLContent, corpusText) {
		t.Fatalf("кусок корпуса не показан читателю: %q", got.HTMLContent)
	}
}

// Тег, вклеенный внутри строки (не отдельной строкой), gomarkdown закрывает
// сам как inline HTML — рендер соберёт разбор с порванной вёрсткой, и это не
// лечится на чтении. Отказ здесь — единственный момент, где это можно
// поймать, и сообщение обязано отличаться от «вклейки нет», чтобы автор не
// стал искать несуществующую пропажу id.
func TestDocumentUpdateRejectsInlineCutPlaceholder(t *testing.T) {
	doc := &models.Document{ID: 5, Slug: "razbor", MarkdownContent: "было"}
	cuts := newFakeDocumentCutStore(&models.DocumentCut{ID: 1, DocumentID: 5})
	h := &DocumentHandler{
		documentRepo: &fakeDocumentStore{doc: doc},
		cuts:         cuts,
		renderer:     markdown.NewRenderer(),
	}

	body, _ := json.Marshal(UpdateDocumentRequest{
		Title:           "Разбор",
		MarkdownContent: "Текст перед вклейкой <cut id=\"1\"> и после.",
	})
	req := httptest.NewRequest(http.MethodPut, "/api/documents/razbor", bytes.NewReader(body))
	req = mux.SetURLVars(req, map[string]string{"slug": "razbor"})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserContextKey, editorClaims()))
	rec := httptest.NewRecorder()

	h.Update(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("код %d, ожидался 400: %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil || payload.Message == "" {
		t.Fatalf("ошибка пришла не как {\"message\": …}: %s", rec.Body.String())
	}
	if !strings.Contains(payload.Message, "отдельной строкой") {
		t.Fatalf("сообщение не отличает встроенный тег от неизвестной вклейки: %q", payload.Message)
	}
}

// Абзац в markdown задаёт ПУСТАЯ СТРОКА вокруг, а не просто перевод строки.
// Тег на своей строке сразу после текста (без пустой строки перед ним)
// остаётся частью того же абзаца — парсер проведёт его как встроенный HTML,
// и подстановка блока вклейки порвёт вёрстку (`<p>текст<div>…</div>` без
// закрытого `<p>`). Найдено мутацией при ревью ветки: построчный регэксп
// без проверки пустых строк засчитывал такой тег «своей строкой» и пропускал
// сохранение (I1).
func TestDocumentUpdateRejectsCutPlaceholderWithoutBlankLineBefore(t *testing.T) {
	doc := &models.Document{ID: 5, Slug: "razbor", MarkdownContent: "было"}
	cuts := newFakeDocumentCutStore(&models.DocumentCut{ID: 1, DocumentID: 5})
	h := &DocumentHandler{
		documentRepo: &fakeDocumentStore{doc: doc},
		cuts:         cuts,
		renderer:     markdown.NewRenderer(),
	}

	body, _ := json.Marshal(UpdateDocumentRequest{
		Title:           "Разбор",
		MarkdownContent: "Текст сразу перед тегом.\n<cut id=\"1\">\n",
	})
	req := httptest.NewRequest(http.MethodPut, "/api/documents/razbor", bytes.NewReader(body))
	req = mux.SetURLVars(req, map[string]string{"slug": "razbor"})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserContextKey, editorClaims()))
	rec := httptest.NewRecorder()

	h.Update(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("код %d, ожидался 400 (тег без пустой строки перед ним — не блок): %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil || !strings.Contains(payload.Message, "отдельной строкой") {
		t.Fatalf("сообщение не про отдельную строку: %s", rec.Body.String())
	}
}

// Два тега подряд, разделённые только переводом строки (без пустой строки
// между ними), — тоже не два отдельных блока: gomarkdown соберёт их в один
// абзац и допишет висячие </cut></p>. Найдено мутацией (I1).
func TestDocumentUpdateRejectsTwoCutTagsWithoutBlankLineBetween(t *testing.T) {
	doc := &models.Document{ID: 5, Slug: "razbor", MarkdownContent: "было"}
	cuts := newFakeDocumentCutStore(
		&models.DocumentCut{ID: 1, DocumentID: 5},
		&models.DocumentCut{ID: 2, DocumentID: 5},
	)
	h := &DocumentHandler{
		documentRepo: &fakeDocumentStore{doc: doc},
		cuts:         cuts,
		renderer:     markdown.NewRenderer(),
	}

	body, _ := json.Marshal(UpdateDocumentRequest{
		Title:           "Разбор",
		MarkdownContent: "<cut id=\"1\">\n<cut id=\"2\">\n",
	})
	req := httptest.NewRequest(http.MethodPut, "/api/documents/razbor", bytes.NewReader(body))
	req = mux.SetURLVars(req, map[string]string{"slug": "razbor"})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserContextKey, editorClaims()))
	rec := httptest.NewRecorder()

	h.Update(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("код %d, ожидался 400 (два тега без пустой строки между ними — не два блока): %s", rec.Code, rec.Body.String())
	}
}

// CRLF не должен ломать распознавание отдельного блока: тег, честно стоящий
// собственным абзацем, но с \r\n-переводами строк, обязан проходить так же,
// как с \n (отложенная находка задачи 9, часть I1).
func TestDocumentUpdateAcceptsCutPlaceholderWithCRLF(t *testing.T) {
	doc := &models.Document{ID: 5, Slug: "razbor", MarkdownContent: "было"}
	cuts := newFakeDocumentCutStore(&models.DocumentCut{ID: 1, DocumentID: 5})
	h := &DocumentHandler{
		documentRepo: &fakeDocumentStore{doc: doc},
		cuts:         cuts,
		renderer:     markdown.NewRenderer(),
	}

	body, _ := json.Marshal(UpdateDocumentRequest{
		Title:           "Разбор",
		MarkdownContent: "a\r\n\r\n<cut id=\"1\">\r\n\r\nb",
	})
	req := httptest.NewRequest(http.MethodPut, "/api/documents/razbor", bytes.NewReader(body))
	req = mux.SetURLVars(req, map[string]string{"slug": "razbor"})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserContextKey, editorClaims()))
	rec := httptest.NewRecorder()

	h.Update(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, ожидался 200 (тег отдельным блоком, CRLF не должен мешать): %s", rec.Code, rec.Body.String())
	}
}

// Новый разбор не может ссылаться ни на одну вклейку — она физически не
// могла быть заведена раньше самого разбора. Сентинел id=0 в Create обязан
// отвергать её тем же путём, что и Update.
func TestDocumentCreateRejectsCutPlaceholderOnBrandNewDocument(t *testing.T) {
	h := &DocumentHandler{
		documentRepo: &fakeDocumentStore{},
		cuts:         newFakeDocumentCutStore(),
		renderer:     markdown.NewRenderer(),
	}

	body, _ := json.Marshal(CreateDocumentRequest{
		Title:           "Новый разбор",
		MarkdownContent: "Текст.\n\n<cut id=\"1\">\n",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/documents", bytes.NewReader(body))
	claims := &auth.Claims{UserID: 1, Role: models.RoleEditor}
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserContextKey, claims))
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("код %d, ожидался 400: %s", rec.Code, rec.Body.String())
	}
}

// Авторство снимается с claims, а не приходит в теле запроса: подделать
// чужую подпись через JSON нельзя. У читателя ник непустой и попадает в
// AuthorNickname; у сотрудника claims.Nickname пуст — это и есть признак
// сотруднического разбора, которым mayEditDocument отличает его от
// читательского.
func TestCreateStampsReaderNickname(t *testing.T) {
	store := &fakeDocumentStore{}
	h := NewDocumentHandler(store, markdown.NewRenderer(), &fakeDocumentCutStore{}, &fakeWorkStore{}, nil)

	rr := doPostVars(t, h.Create, "/api/documents",
		`{"title":"Мой разбор","markdown_content":"тело"}`, nil, readerClaims(5, "чтец"))
	if rr.Code != http.StatusCreated {
		t.Fatalf("код %d: %s", rr.Code, rr.Body.String())
	}
	if len(store.docs) != 1 {
		t.Fatalf("документ не создан: %d записей в фейке", len(store.docs))
	}
	created := store.docs[1]
	if created.AuthorNickname != "чтец" {
		t.Errorf("подпись не снята с учётной записи: %q", created.AuthorNickname)
	}
	if created.OwnerID == nil || *created.OwnerID != 5 {
		t.Error("владелец не проставлен")
	}

	// Сотрудник подписи не оставляет: пустой ник и есть признак
	// сотруднического разбора.
	rr = doPostVars(t, h.Create, "/api/documents",
		`{"title":"Редакционный","markdown_content":"тело"}`, nil, editorClaims())
	if rr.Code != http.StatusCreated {
		t.Fatalf("код %d", rr.Code)
	}
	if len(store.docs) != 2 {
		t.Fatalf("второй документ не создан: %d записей в фейке", len(store.docs))
	}
	if staffCreated := store.docs[2]; staffCreated.AuthorNickname != "" {
		t.Errorf("сотруднику проставлена подпись: %q", staffCreated.AuthorNickname)
	}
}

// Чужую правку не пропускает ни посторонний читатель, ни редактор — «мы
// размещаем, автор собирает», то же разделение, что у подборок читателя.
func TestUpdateRejectsForeignDocument(t *testing.T) {
	doc := readerDoc(5, "чтец")
	doc.Slug = "razbor"
	h := newTestDocumentHandler(t, doc)
	vars := map[string]string{"nickname": "чтец", "slug": "razbor"}

	rr := doPutVars(t, h.Update, "/api/documents/чтец/razbor",
		`{"title":"Взлом","markdown_content":"чужое"}`,
		vars, readerClaims(6, "другой"))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("чужая правка прошла с кодом %d: %s", rr.Code, rr.Body.String())
	}
	// И редактором тоже: «мы размещаем, автор собирает».
	rr = doPutVars(t, h.Update, "/api/documents/чтец/razbor",
		`{"title":"Правка","markdown_content":"редакторское"}`,
		vars, editorClaims())
	if rr.Code != http.StatusForbidden {
		t.Fatalf("правка читательского разбора редактором прошла с кодом %d: %s", rr.Code, rr.Body.String())
	}
}

// TestUpdateOnPendingDocumentRevertsToDraft — фикс-раунд 1 задачи 6: правка
// разбора, ждущего рассмотрения, отзывает отправку и возвращает его в
// «черновик» — заявка не должна пережить изменение тела, которое одобрили бы
// не глядя. Место в очереди теряется, автор отправляет заново.
func TestUpdateOnPendingDocumentRevertsToDraft(t *testing.T) {
	now := time.Now()
	doc := &models.Document{
		ID: 7, OwnerID: ptrInt64(5), AuthorNickname: "чтец",
		Title: "Черновик", Slug: "chernovik", MarkdownContent: "тело",
		ReviewStatus: models.DocumentPending,
		SubmittedAt:  &now, SubmitIPHash: "хэш",
	}
	h := newTestDocumentHandler(t, doc)

	rr := doPutVars(t, h.Update, "/api/documents/чтец/chernovik",
		`{"title":"Опечатка исправлена","markdown_content":"тело поправлено"}`,
		map[string]string{"nickname": "чтец", "slug": "chernovik"}, readerClaims(5, "чтец"))
	if rr.Code != http.StatusOK {
		t.Fatalf("правка: код %d (%s)", rr.Code, rr.Body.String())
	}
	if doc.ReviewStatus != models.DocumentDraft {
		t.Fatalf("состояние после правки %q, ожидался «черновик»", doc.ReviewStatus)
	}
	if doc.SubmittedAt != nil {
		t.Fatal("отметка отправки пережила откат в черновик")
	}
	if doc.SubmitIPHash != "" {
		t.Fatal("отметка адреса отправки пережила откат в черновик")
	}

	var resp models.Document
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("тело ответа не разобралось: %v", err)
	}
	if resp.ReviewStatus != models.DocumentDraft {
		t.Fatalf("ответ несёт прежнее состояние %q — клиент не узнает об откате", resp.ReviewStatus)
	}
}

// TestUpdateOnDraftOrResolvedDocumentLeavesReviewStatusAlone — та же правка
// НЕ трогает review_status черновика (нечего откатывать) и уже рассмотренного
// (отклонённого) разбора: откат в черновик — не побочный эффект любого PUT, а
// реакция ровно на «висит в очереди».
func TestUpdateOnDraftOrResolvedDocumentLeavesReviewStatusAlone(t *testing.T) {
	cases := []models.DocumentReviewStatus{models.DocumentDraft, models.DocumentRejected}
	for _, status := range cases {
		doc := &models.Document{
			ID: 7, OwnerID: ptrInt64(5), AuthorNickname: "чтец",
			Title: "Было", Slug: "razbor", MarkdownContent: "тело", ReviewStatus: status,
		}
		h := newTestDocumentHandler(t, doc)
		rr := doPutVars(t, h.Update, "/api/documents/чтец/razbor",
			`{"title":"Стало","markdown_content":"новое тело"}`,
			map[string]string{"nickname": "чтец", "slug": "razbor"}, readerClaims(5, "чтец"))
		if rr.Code != http.StatusOK {
			t.Fatalf("[%s] правка: код %d (%s)", status, rr.Code, rr.Body.String())
		}
		if doc.ReviewStatus != status {
			t.Fatalf("[%s] правка сменила состояние на %q", status, doc.ReviewStatus)
		}
	}
}

func TestCreateDocumentRequest(t *testing.T) {
	req := CreateDocumentRequest{
		Title:           "My Document",
		MarkdownContent: "# Hello\n\nWorld",
	}

	if req.Title != "My Document" {
		t.Errorf("Title = %s, want My Document", req.Title)
	}
	if req.MarkdownContent != "# Hello\n\nWorld" {
		t.Errorf("MarkdownContent mismatch")
	}
}

func TestUpdateDocumentRequest(t *testing.T) {
	req := UpdateDocumentRequest{
		Title:           "Updated Title",
		MarkdownContent: "# Updated\n\nContent",
	}

	if req.Title != "Updated Title" {
		t.Errorf("Title = %s, want Updated Title", req.Title)
	}
}

func TestCreateDocumentRequest_JSON(t *testing.T) {
	jsonStr := `{"title":"Test Doc","markdown_content":"# Test\n\nContent"}`

	var req CreateDocumentRequest
	if err := json.Unmarshal([]byte(jsonStr), &req); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if req.Title != "Test Doc" {
		t.Errorf("Title = %s, want Test Doc", req.Title)
	}
}

func TestDocumentModel(t *testing.T) {
	doc := models.Document{
		ID:              1,
		Title:           "Test",
		MarkdownContent: "# Test",
		OwnerID:         int64Ptr(1),
	}

	if doc.ID != 1 {
		t.Errorf("ID = %d, want 1", doc.ID)
	}
	if doc.Title != "Test" {
		t.Errorf("Title = %s, want Test", doc.Title)
	}
}

func TestCreateGivesDocumentSlugFromTitle(t *testing.T) {
	h := newTestDocumentHandler(t)
	rec := doPostVars(t, h.Create, "/documents",
		`{"title":"Что делать?","markdown_content":""}`, nil,
		&auth.Claims{UserID: 7, Nickname: "chitatel", Role: models.RoleReader})
	if rec.Code != http.StatusCreated {
		t.Fatalf("код %d: %s", rec.Code, rec.Body)
	}
	var got models.Document
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Slug != "chto-delat" {
		t.Fatalf("слаг созданного разбора: %q", got.Slug)
	}
}
