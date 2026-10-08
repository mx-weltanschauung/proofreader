package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/jackc/pgx/v5/pgconn"

	"proofreader/internal/auth"
	"proofreader/internal/models"
	"proofreader/internal/pagecache"
	"proofreader/internal/repository"
	"proofreader/pkg/markdown"
)

type fakeCollectionStore struct {
	collections []*models.Collection
	rows        []repository.ItemRow
	chapters    []*models.Chapter
	item        *models.CollectionItem

	addedKind      string
	addedChapterID *int64
	addedSnapshot  string
	movedTo        int
	deletedItemID  int64

	// deletedCollectionID запоминает, какую подборку снесли — тесты рычага
	// администратора проверяют по нему, а не по факту отсутствия ошибки.
	deletedCollectionID int64

	// publishedCounts повторяет договор боевого PublishWithinLimit: предел
	// считается по отметке адреса (ipHash), а не по подборке — так же, как
	// считает Postgres-репозиторий (count(*) WHERE publish_ip_hash = ...).
	publishedCounts map[string]int
}

// Create воспроизводит нарушение составного индекса collections_author_slug_key
// тем же типом ошибки, что реальный pgx, — иначе isCollectionSlugTakenError
// (errors.As на *pgconn.PgError) в тесте 409 проверял бы себя же, а не
// поведение обработчика на настоящем контракте ошибки.
func (f *fakeCollectionStore) Create(ctx context.Context, c *models.Collection) error {
	for _, existing := range f.collections {
		if existing.AuthorNickname == c.AuthorNickname && existing.Slug == c.Slug {
			return &pgconn.PgError{Code: "23505", ConstraintName: collectionSlugUniqueConstraint}
		}
	}
	c.ID = 7
	f.collections = append(f.collections, c)
	return nil
}

func (f *fakeCollectionStore) GetBySlug(ctx context.Context, slug string) (*models.Collection, error) {
	return f.GetByAuthorSlug(ctx, "", slug)
}

// GetByAuthorSlug — тот же поиск, что у боевого репозитория: пара (ник,
// слаг) целиком, а не один слаг, — иначе подставной склад не отличал бы
// читательскую подборку от сотруднической с тем же слагом.
func (f *fakeCollectionStore) GetByAuthorSlug(ctx context.Context, nickname, slug string) (*models.Collection, error) {
	for _, c := range f.collections {
		if c.AuthorNickname == nickname && c.Slug == slug {
			return c, nil
		}
	}
	return nil, context.Canceled
}

// List повторяет фильтр боевого репозитория (author_nickname = ” AND
// published_at IS NOT NULL) — витрина сотрудническая, без черновиков и без
// читательских подборок. Фильтровать здесь, а не отдавать всё подряд,
// обязательно: подставной склад обязан врать репозиторию только в
// подключении к базе, не в семантике — иначе тест по нему зелен, а боевой код
// сломан (см. известный сквозной случай в истории проекта).
func (f *fakeCollectionStore) List(ctx context.Context) ([]*models.Collection, error) {
	var out []*models.Collection
	for _, c := range f.collections {
		if c.AuthorNickname == "" && c.PublishedAt != nil {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *fakeCollectionStore) ListByOwner(ctx context.Context, ownerID int64) ([]*models.Collection, error) {
	var out []*models.Collection
	for _, c := range f.collections {
		if c.OwnerID != nil && *c.OwnerID == ownerID {
			out = append(out, c)
		}
	}
	return out, nil
}

// Update воспроизводит то же нарушение индекса, что и Create, — правка
// подборки в слаг, уже занятый ДРУГОЙ подборкой того же автора, бьёт в тот
// же collections_author_slug_key, и обработчик обязан отвечать на неё так
// же (409), а не 500.
func (f *fakeCollectionStore) Update(ctx context.Context, c *models.Collection) error {
	for _, existing := range f.collections {
		if existing.ID != c.ID && existing.AuthorNickname == c.AuthorNickname && existing.Slug == c.Slug {
			return &pgconn.PgError{Code: "23505", ConstraintName: collectionSlugUniqueConstraint}
		}
	}
	return nil
}

func (f *fakeCollectionStore) Delete(ctx context.Context, id int64) error {
	f.deletedCollectionID = id
	return nil
}

// PublishWithinLimit — тот же договор, что у боевого метода: (false, zero,
// nil) на исчерпанный предел, иначе подборка публикуется и предел
// увеличивается. Отметка адреса на самой подборке (PublishIPHash) тоже
// проставляется — Get и Unpublish читают именно её.
func (f *fakeCollectionStore) PublishWithinLimit(
	ctx context.Context, id int64, ipHash string, limit int, since time.Time,
) (bool, time.Time, error) {
	if f.publishedCounts == nil {
		f.publishedCounts = map[string]int{}
	}
	if f.publishedCounts[ipHash] >= limit {
		return false, time.Time{}, nil
	}
	for _, c := range f.collections {
		if c.ID == id {
			now := time.Now()
			c.PublishedAt = &now
			c.PublishIPHash = ipHash
			f.publishedCounts[ipHash]++
			return true, now, nil
		}
	}
	return false, time.Time{}, context.Canceled
}

// Unpublish обнуляет published_at и, как боевой метод, НЕ трогает
// PublishIPHash — это единственный признак «было и снято» в базе.
func (f *fakeCollectionStore) Unpublish(ctx context.Context, id int64) error {
	for _, c := range f.collections {
		if c.ID == id {
			c.PublishedAt = nil
			return nil
		}
	}
	return context.Canceled
}

func (f *fakeCollectionStore) AddItem(
	ctx context.Context, collectionID int64, kind string,
	chapterID, workID *int64, authorOverride string,
) (*models.CollectionItem, error) {
	f.addedKind = kind
	f.addedChapterID = chapterID
	// Снимок берёт стор, из базы; тело запроса на него не влияет.
	f.addedSnapshot = "заголовок из базы"
	return &models.CollectionItem{
		ID: 11, CollectionID: collectionID, Kind: kind, ChapterID: chapterID,
		SnapshotTitle: f.addedSnapshot, AuthorOverride: authorOverride, OrderNumber: 1,
	}, nil
}

func (f *fakeCollectionStore) UpdateItemAuthor(ctx context.Context, collectionID, itemID int64, a string) error {
	return nil
}

func (f *fakeCollectionStore) DeleteItem(ctx context.Context, collectionID, itemID int64) error {
	f.deletedItemID = itemID
	return nil
}

func (f *fakeCollectionStore) MoveItem(ctx context.Context, collectionID, itemID int64, newOrder int) error {
	f.movedTo = newOrder
	return nil
}

func (f *fakeCollectionStore) ItemRows(ctx context.Context, collectionID int64) ([]repository.ItemRow, error) {
	return f.rows, nil
}

func (f *fakeCollectionStore) ChaptersForWorks(ctx context.Context, ids []int64) ([]*models.Chapter, error) {
	return f.chapters, nil
}

func (f *fakeCollectionStore) ItemByID(ctx context.Context, collectionID, itemID int64) (*models.CollectionItem, error) {
	if f.item == nil {
		return nil, context.Canceled
	}
	return f.item, nil
}

// ptrTime — адрес временного значения, аналог ptrInt64 (work_volume_test.go)
// для Collection.PublishedAt.
func ptrTime(t time.Time) *time.Time { return &t }

// Клеймы для тестов владения. Разными UserID, а не ролью — mayEdit сверяет
// владельца по id, роль читателя у владельца и постороннего одна и та же.
// testCollectionEditorClaims нужен сотрудническим строкам (пустой
// AuthorNickname, заведённым напрямую в фикстурах, минуя Create) — по mayEdit
// ими правит любой сотрудник. Читательские фикстуры обязаны нести и OwnerID,
// и непустой AuthorNickname разом — иначе они моделируют состояние, которого
// Create никогда не производит (см. mayEdit).
var (
	testCollectionOwnerClaims    = &auth.Claims{UserID: 501, Role: models.RoleReader, Nickname: "хранитель"}
	testCollectionStrangerClaims = &auth.Claims{UserID: 502, Role: models.RoleReader, Nickname: "чужой"}
	testCollectionEditorClaims   = &auth.Claims{UserID: 9, Role: models.RoleEditor}
	testCollectionAdminClaims    = &auth.Claims{UserID: 10, Role: models.RoleAdministrator}
)

func collectionFixture() *fakeCollectionStore {
	return &fakeCollectionStore{
		collections: []*models.Collection{{
			ID: 1, Title: "Материализм", Slug: "materializm",
			// Легаси-строка без владельца (заведена сотрудником до читательской
			// оси) — уже опубликована, иначе Get отдавал бы её как черновик
			// постороннему запросу без клеймов.
			PublishedAt: ptrTime(time.Now().Add(-time.Hour)),
		}},
		rows: []repository.ItemRow{chapterRow(1, 1, 100, 41)},
		chapters: []*models.Chapter{
			{ID: 100, WorkID: 41, Title: "Людвиг Фейербах", OrderNumber: 1, StartPage: 265, EndPage: 313},
		},
	}
}

func TestCollectionHandlerGetBuildsTOC(t *testing.T) {
	h := NewCollectionHandler(collectionFixture(), nil, nil, NewRangeCache(pagecache.New("")), "", false)

	req := httptest.NewRequest(http.MethodGet, "/api/collections/materializm", nil)
	req = mux.SetURLVars(req, map[string]string{"slug": "materializm"})
	rec := httptest.NewRecorder()
	h.Get(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, ожидался 200; тело: %s", rec.Code, rec.Body.String())
	}

	var got models.Collection
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("ответ не разобрался: %v", err)
	}
	if len(got.Items) != 1 {
		t.Fatalf("строк оглавления %d, ожидалась 1", len(got.Items))
	}
	if got.Items[0].Source.PageStart != 269 {
		t.Errorf("страница %d, ожидалась печатная 269", got.Items[0].Source.PageStart)
	}
}

func TestCollectionHandlerListNeverReturnsNull(t *testing.T) {
	// Известный сквозной дефект проекта: пустая выборка приезжает nil и
	// кодируется как null. Новый маршрут его не наследует.
	h := NewCollectionHandler(&fakeCollectionStore{}, nil, nil, NewRangeCache(pagecache.New("")), "", false)

	req := httptest.NewRequest(http.MethodGet, "/api/collections", nil)
	rec := httptest.NewRecorder()
	h.List(rec, req)

	if body := rec.Body.String(); body != "[]\n" {
		t.Errorf("тело %q, ожидался пустой список", body)
	}
}

func TestListSkipsReaderCollections(t *testing.T) {
	published := ptrTime(time.Now().Add(-time.Hour))
	store := &fakeCollectionStore{
		collections: []*models.Collection{
			{ID: 1, Title: "Витрина", Slug: "vitrina", PublishedAt: published},
			{ID: 2, Title: "Подборка читателя", Slug: "chitatel", AuthorNickname: "чтец", PublishedAt: published},
			{ID: 3, Title: "Черновик сотрудника", Slug: "draft-staff"}, // published_at пуст
		},
	}
	h := NewCollectionHandler(store, nil, nil, NewRangeCache(pagecache.New("")), "", false)

	req := httptest.NewRequest(http.MethodGet, "/api/collections", nil)
	rec := httptest.NewRecorder()
	h.List(rec, req)

	var got []models.Collection
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("ответ не разобрался: %v", err)
	}
	if len(got) != 1 || got[0].Slug != "vitrina" {
		t.Fatalf("список %+v — витрина должна остаться единственной опубликованной сотруднической подборкой", got)
	}
}

func TestCreateCollectionStampsAuthorNickname(t *testing.T) {
	store := &fakeCollectionStore{}
	h := NewCollectionHandler(store, nil, nil, NewRangeCache(pagecache.New("")), "", false)

	body := bytes.NewBufferString(`{"title":"Моя подборка","slug":"moya-podborka"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/collections", body)
	rec := httptest.NewRecorder()
	withClaims(testCollectionOwnerClaims, h.Create)(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("код %d, ожидался 201; тело: %s", rec.Code, rec.Body.String())
	}
	if len(store.collections) != 1 {
		t.Fatalf("подборка не записалась в стор")
	}
	got := store.collections[0]
	if got.AuthorNickname != testCollectionOwnerClaims.Nickname {
		t.Errorf("author_nickname %q, ожидался ник владельца %q", got.AuthorNickname, testCollectionOwnerClaims.Nickname)
	}
	if got.OwnerID == nil || *got.OwnerID != testCollectionOwnerClaims.UserID {
		t.Errorf("owner_id %v, ожидался %d", got.OwnerID, testCollectionOwnerClaims.UserID)
	}
}

func TestUpdateCollectionRejectsStranger(t *testing.T) {
	owner := testCollectionOwnerClaims.UserID
	store := &fakeCollectionStore{
		collections: []*models.Collection{{
			ID: 2, Title: "Чужая", Slug: "chuzhaya", OwnerID: &owner,
			AuthorNickname: testCollectionOwnerClaims.Nickname,
		}},
	}
	h := NewCollectionHandler(store, nil, nil, NewRangeCache(pagecache.New("")), "", false)

	body := bytes.NewBufferString(`{"title":"Захват","slug":"chuzhaya"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/collections/chuzhaya", body)
	req = mux.SetURLVars(req, map[string]string{"nickname": testCollectionOwnerClaims.Nickname, "slug": "chuzhaya"})
	rec := httptest.NewRecorder()
	withClaims(testCollectionStrangerClaims, h.Update)(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("код %d, ожидался 403; тело: %s", rec.Code, rec.Body.String())
	}
	if store.collections[0].Title != "Чужая" {
		t.Errorf("заголовок стал %q — правка постороннего не должна была примениться", store.collections[0].Title)
	}
}

// TestUpdateCollectionRejectsEditorForOrphanedOwner проверяет осиротевшую
// читательскую подборку: OwnerID обнулён (ON DELETE SET NULL при удалении
// учётной записи автора), а AuthorNickname — ник-снимок — остался. Такую
// строку не должен править никто из персонала: mayEdit различает
// сотрудническую подборку не по пустому OwnerID (он легитимно пуст и у
// осиротевшей читательской), а по пустому AuthorNickname. У этой строки ник
// непустой, значит она читательская, а значит правит только владелец —
// которого больше нет. Итог — правка отклонена и редактору тоже.
func TestUpdateCollectionRejectsEditorForOrphanedOwner(t *testing.T) {
	store := &fakeCollectionStore{
		collections: []*models.Collection{{
			ID: 2, Title: "Осиротевшая", Slug: "osirotevshaya",
			OwnerID:        nil,
			AuthorNickname: testCollectionOwnerClaims.Nickname,
		}},
	}
	h := NewCollectionHandler(store, nil, nil, NewRangeCache(pagecache.New("")), "", false)

	body := bytes.NewBufferString(`{"title":"Захват","slug":"osirotevshaya"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/collections/osirotevshaya", body)
	req = mux.SetURLVars(req, map[string]string{"nickname": testCollectionOwnerClaims.Nickname, "slug": "osirotevshaya"})
	rec := httptest.NewRecorder()
	withClaims(testCollectionEditorClaims, h.Update)(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("код %d, ожидался 403; тело: %s", rec.Code, rec.Body.String())
	}
	if store.collections[0].Title != "Осиротевшая" {
		t.Errorf("заголовок стал %q — правка редактором осиротевшей читательской подборки не должна была примениться",
			store.collections[0].Title)
	}
}

func TestDeleteCollectionAllowsAdministrator(t *testing.T) {
	owner := testCollectionOwnerClaims.UserID
	store := &fakeCollectionStore{
		collections: []*models.Collection{{
			ID: 3, Title: "Подборка читателя", Slug: "chitatel-podborka", OwnerID: &owner,
			AuthorNickname: testCollectionOwnerClaims.Nickname,
		}},
	}
	h := NewCollectionHandler(store, nil, nil, NewRangeCache(pagecache.New("")), "", false)

	req := httptest.NewRequest(http.MethodDelete, "/api/collections/chitatel-podborka", nil)
	req = mux.SetURLVars(req, map[string]string{"nickname": testCollectionOwnerClaims.Nickname, "slug": "chitatel-podborka"})
	rec := httptest.NewRecorder()
	withClaims(testCollectionAdminClaims, h.Delete)(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("код %d, ожидался 204 — администратору доступно удаление чужой подборки; тело: %s",
			rec.Code, rec.Body.String())
	}
	if store.deletedCollectionID != 3 {
		t.Errorf("удалена подборка %d, ожидалась 3", store.deletedCollectionID)
	}
}

func TestDeleteCollectionRejectsEditor(t *testing.T) {
	// Зеркало предыдущего теста: у редактора рычага снятия чужой подборки
	// нет — только у администратора. «Мы размещаем, автор собирает».
	owner := testCollectionOwnerClaims.UserID
	store := &fakeCollectionStore{
		collections: []*models.Collection{{
			ID: 4, Title: "Подборка читателя", Slug: "chitatel-2", OwnerID: &owner,
			AuthorNickname: testCollectionOwnerClaims.Nickname,
		}},
	}
	h := NewCollectionHandler(store, nil, nil, NewRangeCache(pagecache.New("")), "", false)

	req := httptest.NewRequest(http.MethodDelete, "/api/collections/chitatel-2", nil)
	req = mux.SetURLVars(req, map[string]string{"nickname": testCollectionOwnerClaims.Nickname, "slug": "chitatel-2"})
	rec := httptest.NewRecorder()
	withClaims(testCollectionEditorClaims, h.Delete)(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("код %d, ожидался 403 — редактор не вправе снять чужую подборку; тело: %s",
			rec.Code, rec.Body.String())
	}
}

func TestDraftCollectionHiddenFromStranger(t *testing.T) {
	owner := testCollectionOwnerClaims.UserID
	store := &fakeCollectionStore{
		collections: []*models.Collection{{
			ID: 5, Title: "Черновик", Slug: "chernovik", OwnerID: &owner,
			AuthorNickname: testCollectionOwnerClaims.Nickname,
			// PublishedAt и PublishIPHash оба пусты — подборка никогда не публиковалась.
		}},
	}
	h := NewCollectionHandler(store, nil, nil, NewRangeCache(pagecache.New("")), "", false)

	req := httptest.NewRequest(http.MethodGet, "/api/collections/chernovik", nil)
	// Подборка читательская: без "nickname" в mux.Vars GetByAuthorSlug её не
	// найдёт вовсе, и 404 придёт из ветки «нет такой строки», а не из ветки
	// «черновик постороннему» — то есть тест перестанет сторожить то, что
	// заявлено в его имени. Ник обязателен, чтобы дойти до настоящей ветки.
	req = mux.SetURLVars(req, map[string]string{"nickname": testCollectionOwnerClaims.Nickname, "slug": "chernovik"})
	rec := httptest.NewRecorder()
	withClaims(testCollectionStrangerClaims, h.Get)(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("код %d, ожидался 404 — черновик не должен выдавать даже факт своего существования; тело: %s",
			rec.Code, rec.Body.String())
	}
}

func TestDraftCollectionVisibleToOwner(t *testing.T) {
	owner := testCollectionOwnerClaims.UserID
	store := &fakeCollectionStore{
		collections: []*models.Collection{{
			ID: 6, Title: "Мой черновик", Slug: "moy-chernovik", OwnerID: &owner,
			AuthorNickname: testCollectionOwnerClaims.Nickname,
		}},
	}
	h := NewCollectionHandler(store, nil, nil, NewRangeCache(pagecache.New("")), "", false)

	req := httptest.NewRequest(http.MethodGet, "/api/collections/moy-chernovik", nil)
	req = mux.SetURLVars(req, map[string]string{"nickname": testCollectionOwnerClaims.Nickname, "slug": "moy-chernovik"})
	rec := httptest.NewRecorder()
	withClaims(testCollectionOwnerClaims, h.Get)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, ожидался 200 — владелец видит свой черновик; тело: %s", rec.Code, rec.Body.String())
	}
}

// TestCollectionsMineRejectsAnonymous calls the handler directly, bypassing
// the router entirely, so it exercises the claims check inside Mine on its
// own. On the real router (reader subrouter, AuthMiddleware +
// RequireRole) that check is a defensive backstop, same as Mine on
// PageSuggestionHandler — the actual gate for an anonymous request is
// AuthMiddleware, see TestCollectionsMineRouteNotSwallowedByGenericSlug in
// router_access_test.go. Referenced by name in router_roles_test.go's
// routeAccess comment for "GET /api/collections/mine".
func TestCollectionsMineRejectsAnonymous(t *testing.T) {
	h := NewCollectionHandler(&fakeCollectionStore{}, nil, nil, NewRangeCache(pagecache.New("")), "", false)

	req := httptest.NewRequest(http.MethodGet, "/api/collections/mine", nil)
	rec := httptest.NewRecorder()
	h.Mine(rec, req) // без withClaims — как если бы гость обошёл AuthMiddleware

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("код %d, ожидался 401; тело: %s", rec.Code, rec.Body.String())
	}
}

// TestCollectionsMineListsOwnDraftsAndPublished — раздел «мои подборки»
// обязан показать и черновик, и опубликованную строку того же владельца, и
// не показать подборку постороннего читателя.
func TestCollectionsMineListsOwnDraftsAndPublished(t *testing.T) {
	owner := testCollectionOwnerClaims.UserID
	stranger := testCollectionStrangerClaims.UserID
	store := &fakeCollectionStore{
		collections: []*models.Collection{
			{ID: 20, Title: "Черновик", Slug: "chernovik-moy", OwnerID: &owner, AuthorNickname: testCollectionOwnerClaims.Nickname},
			{
				ID: 21, Title: "Опубликована", Slug: "opublikovana-moya", OwnerID: &owner,
				AuthorNickname: testCollectionOwnerClaims.Nickname, PublishedAt: ptrTime(time.Now()),
			},
			{ID: 22, Title: "Чужая", Slug: "chuzhaya", OwnerID: &stranger, AuthorNickname: testCollectionStrangerClaims.Nickname},
		},
	}
	h := NewCollectionHandler(store, nil, nil, NewRangeCache(pagecache.New("")), "", false)

	req := httptest.NewRequest(http.MethodGet, "/api/collections/mine", nil)
	rec := httptest.NewRecorder()
	withClaims(testCollectionOwnerClaims, h.Mine)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, ожидался 200; тело: %s", rec.Code, rec.Body.String())
	}
	var got []*models.Collection
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("bad JSON: %v; body: %s", err, rec.Body.String())
	}
	if len(got) != 2 {
		t.Fatalf("длина списка = %d, ожидалось 2 (черновик + опубликованная, без чужой); тело: %s",
			len(got), rec.Body.String())
	}
	for _, c := range got {
		if c.ID == 22 {
			t.Fatalf("чужая подборка (id=22) попала в «мои подборки»")
		}
	}
}

func TestPublishStampsPublishedAt(t *testing.T) {
	owner := testCollectionOwnerClaims.UserID
	store := &fakeCollectionStore{
		collections: []*models.Collection{{
			ID: 8, Title: "Черновик", Slug: "budet-opublikovana", OwnerID: &owner,
			AuthorNickname: testCollectionOwnerClaims.Nickname,
		}},
	}
	h := NewCollectionHandler(store, nil, nil, NewRangeCache(pagecache.New("")), "секрет", false)

	req := httptest.NewRequest(http.MethodPost, "/api/collections/budet-opublikovana/publish", nil)
	req = mux.SetURLVars(req, map[string]string{"nickname": testCollectionOwnerClaims.Nickname, "slug": "budet-opublikovana"})
	rec := httptest.NewRecorder()
	withClaims(testCollectionOwnerClaims, h.Publish)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, ожидался 200; тело: %s", rec.Code, rec.Body.String())
	}
	if store.collections[0].PublishedAt == nil {
		t.Fatalf("published_at не выставился в сторе")
	}

	var got models.Collection
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("ответ не разобрался: %v", err)
	}
	if got.PublishedAt == nil {
		t.Errorf("ответ не несёт published_at")
	}
}

func TestPublishRejectsStranger(t *testing.T) {
	owner := testCollectionOwnerClaims.UserID
	store := &fakeCollectionStore{
		collections: []*models.Collection{{
			ID: 9, Title: "Черновик", Slug: "chuzhoy-chernovik", OwnerID: &owner,
			AuthorNickname: testCollectionOwnerClaims.Nickname,
		}},
	}
	h := NewCollectionHandler(store, nil, nil, NewRangeCache(pagecache.New("")), "", false)

	req := httptest.NewRequest(http.MethodPost, "/api/collections/chuzhoy-chernovik/publish", nil)
	req = mux.SetURLVars(req, map[string]string{"nickname": testCollectionOwnerClaims.Nickname, "slug": "chuzhoy-chernovik"})
	rec := httptest.NewRecorder()
	withClaims(testCollectionStrangerClaims, h.Publish)(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("код %d, ожидался 403; тело: %s", rec.Code, rec.Body.String())
	}
	if store.collections[0].PublishedAt != nil {
		t.Errorf("публикация постороннего не должна была примениться")
	}
}

// TestPublishExemptsStaffFromRateLimit — находка рецензии: предел в 3
// публикации в сутки на IP рассчитан на читателя, но бил и по сотруднику,
// правящему сотрудническую подборку (пустой AuthorNickname, см. mayEdit) с
// общего редакционного адреса. Четвёртая публикация подряд с одного и того
// же адреса читателя была бы отклонена 429-м — здесь она обязана пройти,
// потому что клеймы редактора освобождают Publish от предела вовсе.
func TestPublishExemptsStaffFromRateLimit(t *testing.T) {
	store := &fakeCollectionStore{
		collections: []*models.Collection{{
			ID: 20, Title: "Сотрудническая", Slug: "sotrudnicheskaya",
			// AuthorNickname пуст — сотрудническая строка, mayEdit пускает
			// любого editor/administrator (см. testCollectionEditorClaims).
		}},
	}
	h := NewCollectionHandler(store, nil, nil, NewRangeCache(pagecache.New("")), "секрет", false)

	req := httptest.NewRequest(http.MethodPost, "/api/collections/sotrudnicheskaya/publish", nil)
	req = mux.SetURLVars(req, map[string]string{"slug": "sotrudnicheskaya"})
	req.RemoteAddr = "203.0.113.10:1234"

	// Читательский предел — 3 в сутки; четыре публикации подряд с одного и
	// того же адреса под клеймами редактора обязаны пройти все до одной.
	for i := 0; i < collectionPublishesPerDay+1; i++ {
		rec := httptest.NewRecorder()
		withClaims(testCollectionEditorClaims, h.Publish)(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("публикация %d: код %d, ожидался 200 (сотрудник вне предела); тело: %s",
				i+1, rec.Code, rec.Body.String())
		}
	}
}

func TestUnpublishedCollectionAnswersGone(t *testing.T) {
	owner := testCollectionOwnerClaims.UserID
	store := &fakeCollectionStore{
		collections: []*models.Collection{{
			ID: 10, Title: "Была и снята", Slug: "byla-i-snyata", OwnerID: &owner,
			AuthorNickname: testCollectionOwnerClaims.Nickname,
			// PublishedAt пуст (снята), PublishIPHash непуст (была опубликована) —
			// ровно комбинация, которую Get обязан отличать от черновика.
			PublishIPHash: "была-отметка",
		}},
	}
	h := NewCollectionHandler(store, nil, nil, NewRangeCache(pagecache.New("")), "", false)

	req := httptest.NewRequest(http.MethodGet, "/api/collections/byla-i-snyata", nil)
	req = mux.SetURLVars(req, map[string]string{"nickname": testCollectionOwnerClaims.Nickname, "slug": "byla-i-snyata"})
	rec := httptest.NewRecorder()
	withClaims(testCollectionStrangerClaims, h.Get)(rec, req)

	if rec.Code != http.StatusGone {
		t.Fatalf("код %d, ожидался 410 — «было и снято»; тело: %s", rec.Code, rec.Body.String())
	}
}

func TestUnpublishKeepsPublishIPHashForOwner(t *testing.T) {
	// Сквозная проверка ровно того сценария, который различают 404 и 410:
	// Publish ставит отметку, Unpublish снимает published_at, но не отметку.
	owner := testCollectionOwnerClaims.UserID
	store := &fakeCollectionStore{
		collections: []*models.Collection{{
			ID: 11, Title: "Уйдёт в черновик", Slug: "uydet-v-chernovik", OwnerID: &owner,
			AuthorNickname: testCollectionOwnerClaims.Nickname,
		}},
	}
	h := NewCollectionHandler(store, nil, nil, NewRangeCache(pagecache.New("")), "секрет", false)

	publishReq := httptest.NewRequest(http.MethodPost, "/api/collections/uydet-v-chernovik/publish", nil)
	publishReq = mux.SetURLVars(publishReq, map[string]string{"nickname": testCollectionOwnerClaims.Nickname, "slug": "uydet-v-chernovik"})
	withClaims(testCollectionOwnerClaims, h.Publish)(httptest.NewRecorder(), publishReq)

	unpublishReq := httptest.NewRequest(http.MethodPost, "/api/collections/uydet-v-chernovik/unpublish", nil)
	unpublishReq = mux.SetURLVars(unpublishReq, map[string]string{"nickname": testCollectionOwnerClaims.Nickname, "slug": "uydet-v-chernovik"})
	rec := httptest.NewRecorder()
	withClaims(testCollectionOwnerClaims, h.Unpublish)(rec, unpublishReq)

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, ожидался 200; тело: %s", rec.Code, rec.Body.String())
	}
	if store.collections[0].PublishedAt != nil {
		t.Errorf("published_at должен обнулиться")
	}
	if store.collections[0].PublishIPHash == "" {
		t.Errorf("publish_ip_hash не должен стираться — иначе Get перепутает снятую подборку с черновиком")
	}
}

func TestCollectionHandlerAddItemIgnoresClientSnapshot(t *testing.T) {
	store := collectionFixture()
	h := NewCollectionHandler(store, nil, nil, NewRangeCache(pagecache.New("")), "", false)

	// Клиент пытается подсунуть снимок — он обязан быть проигнорирован.
	body := bytes.NewBufferString(
		`{"kind":"chapter","chapter_id":100,"snapshot_title":"враньё","snapshot_author":"враньё"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/collections/materializm/items", body)
	req = mux.SetURLVars(req, map[string]string{"slug": "materializm"})
	rec := httptest.NewRecorder()
	// Фикстура — легаси-строка без владельца, ею правит сотрудник.
	withClaims(testCollectionEditorClaims, h.AddItem)(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("код %d, ожидался 201; тело: %s", rec.Code, rec.Body.String())
	}

	var item models.CollectionItem
	if err := json.NewDecoder(rec.Body).Decode(&item); err != nil {
		t.Fatalf("ответ не разобрался: %v", err)
	}
	if item.SnapshotTitle != "заголовок из базы" {
		t.Errorf("снимок %q — он обязан приходить из базы, а не из тела запроса",
			item.SnapshotTitle)
	}
}

func TestCollectionHandlerAddItemRejectsUnknownKind(t *testing.T) {
	h := NewCollectionHandler(collectionFixture(), nil, nil, NewRangeCache(pagecache.New("")), "", false)

	body := bytes.NewBufferString(`{"kind":"page_range","chapter_id":100}`)
	req := httptest.NewRequest(http.MethodPost, "/api/collections/materializm/items", body)
	req = mux.SetURLVars(req, map[string]string{"slug": "materializm"})
	rec := httptest.NewRecorder()
	// Проверка вида элемента отвечает раньше проверки владения — клеймов
	// намеренно нет, запрос обязан упасть в 400 и без них.
	h.AddItem(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("код %d, ожидался 400 на неизвестный вид элемента", rec.Code)
	}
}

func TestCollectionHandlerAddItemWorkClearsChapterID(t *testing.T) {
	// Правка к брифу: клиент вправе прислать оба поля (chapter_id и work_id),
	// и для kind=work chapter_id обязан быть обнулён перед вызовом стора —
	// иначе вставку отвергнет CHECK-констрейнт collection_items_target_check.
	store := collectionFixture()
	h := NewCollectionHandler(store, nil, nil, NewRangeCache(pagecache.New("")), "", false)

	body := bytes.NewBufferString(`{"kind":"work","work_id":42,"chapter_id":100}`)
	req := httptest.NewRequest(http.MethodPost, "/api/collections/materializm/items", body)
	req = mux.SetURLVars(req, map[string]string{"slug": "materializm"})
	rec := httptest.NewRecorder()
	withClaims(testCollectionEditorClaims, h.AddItem)(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("код %d, ожидался 201; тело: %s", rec.Code, rec.Body.String())
	}
	if store.addedChapterID != nil {
		t.Errorf("chapter_id ушёл в стор как %v, ожидался nil для kind=work", *store.addedChapterID)
	}
}

func TestCollectionHandlerItemPagesGoneWhenSourceDeleted(t *testing.T) {
	// Разница между «элемента нет» и «элемент есть, но источник пропал» —
	// ровно то, что нужно показать составителю.
	store := collectionFixture()
	store.item = &models.CollectionItem{
		ID: 11, CollectionID: 1, Kind: models.CollectionItemKindChapter,
		ChapterID: nil, SnapshotTitle: "снимок",
	}
	h := NewCollectionHandler(store, nil, nil, NewRangeCache(pagecache.New("")), "", false)

	req := httptest.NewRequest(http.MethodGet, "/api/collections/materializm/items/11/pages", nil)
	req = mux.SetURLVars(req, map[string]string{"slug": "materializm", "itemId": "11"})
	rec := httptest.NewRecorder()
	h.ItemPages(rec, req)

	if rec.Code != http.StatusGone {
		t.Fatalf("код %d, ожидался 410", rec.Code)
	}
}

// collectionVisibilityFixture — читательская подборка владельца
// testCollectionOwnerClaims с одним живым элементом-главой (том 41, глава
// 100, полосы 265—313 внутренние), готовым отдать 200 при снятом гейте.
// published/everPublished управляют PublishedAt/PublishIPHash — три
// состояния, которые различает collectionVisibleTo.
func collectionVisibilityFixture(published, everPublished bool) *fakeCollectionStore {
	owner := testCollectionOwnerClaims.UserID
	c := &models.Collection{
		ID: 1, Title: "Подборка читателя", Slug: "chitatel-podborka",
		OwnerID: &owner, AuthorNickname: testCollectionOwnerClaims.Nickname,
	}
	if published {
		c.PublishedAt = ptrTime(time.Now().Add(-time.Hour))
	}
	if everPublished {
		c.PublishIPHash = "была-отметка"
	}
	return &fakeCollectionStore{
		collections: []*models.Collection{c},
		rows:        []repository.ItemRow{chapterRow(11, 1, 100, 41)},
		chapters: []*models.Chapter{
			{ID: 100, WorkID: 41, Title: "Людвиг Фейербах", OrderNumber: 1, StartPage: 265, EndPage: 313},
		},
		item: &models.CollectionItem{
			ID: 11, CollectionID: 1, Kind: models.CollectionItemKindChapter,
			ChapterID: ptrInt64(100), WorkID: ptrInt64(41), SnapshotTitle: "снимок",
		},
	}
}

func itemPagesVisibilityRequest() *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/api/collections/хранитель/chitatel-podborka/items/11/pages", nil)
	return mux.SetURLVars(req, map[string]string{
		"nickname": testCollectionOwnerClaims.Nickname, "slug": "chitatel-podborka", "itemId": "11",
	})
}

// TestItemPagesHidesDraftFromStranger — БЛОКЕР рецензии: ItemPages не гейтил
// видимость вовсе, и черновик (никогда не публиковался) отдавал полосы
// постороннему в обход 404 у Get.
func TestItemPagesHidesDraftFromStranger(t *testing.T) {
	store := collectionVisibilityFixture(false, false)
	h := NewCollectionHandler(store, &fakePageStore{}, markdown.NewRenderer(), NewRangeCache(pagecache.New("")), "", false)

	rec := httptest.NewRecorder()
	withClaims(testCollectionStrangerClaims, h.ItemPages)(rec, itemPagesVisibilityRequest())

	if rec.Code != http.StatusNotFound {
		t.Fatalf("код %d, ожидался 404 — черновик не должен выдавать полосы постороннему; тело: %s",
			rec.Code, rec.Body.String())
	}
}

// TestItemPagesHidesUnpublishedFromStranger — тот же блокер, вторая половина:
// снятая с публикации подборка (PublishIPHash непуст, PublishedAt пуст)
// обязана отвечать 410, а не отдавать полосы.
func TestItemPagesHidesUnpublishedFromStranger(t *testing.T) {
	store := collectionVisibilityFixture(false, true)
	h := NewCollectionHandler(store, &fakePageStore{}, markdown.NewRenderer(), NewRangeCache(pagecache.New("")), "", false)

	rec := httptest.NewRecorder()
	withClaims(testCollectionStrangerClaims, h.ItemPages)(rec, itemPagesVisibilityRequest())

	if rec.Code != http.StatusGone {
		t.Fatalf("код %d, ожидался 410 — снятая с публикации подборка не должна отдавать полосы; тело: %s",
			rec.Code, rec.Body.String())
	}
}

// TestItemPagesVisibleToOwnerDraft — владелец обязан по-прежнему видеть свой
// черновик через ItemPages, гейт не должен закрыть маршрут и ему.
func TestItemPagesVisibleToOwnerDraft(t *testing.T) {
	store := collectionVisibilityFixture(false, false)
	pageStore := &fakePageStore{
		getPageRangeFn: func(_ context.Context, workID int64, s, e int) ([]*models.Page, error) {
			return []*models.Page{{ID: 1, WorkID: workID, PageNumber: s, ContentMarkdown: "текст"}}, nil
		},
	}
	h := NewCollectionHandler(store, pageStore, markdown.NewRenderer(), NewRangeCache(pagecache.New("")), "", false)

	rec := httptest.NewRecorder()
	withClaims(testCollectionOwnerClaims, h.ItemPages)(rec, itemPagesVisibilityRequest())

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, ожидался 200 — владелец видит свой черновик; тело: %s", rec.Code, rec.Body.String())
	}
}

func TestCollectionHandlerItemPagesWorkItemUsesTopLevelChapterBounds(t *testing.T) {
	// Незакрытое место брифа: у элемента-работы границы берутся из глав
	// верхнего уровня этой работы (ChaptersForWorks), а не только из
	// собственных границ главы (тех у элемента-работы просто нет).
	store := collectionFixture()
	store.item = &models.CollectionItem{
		ID: 9, CollectionID: 1, Kind: models.CollectionItemKindWork,
		WorkID: ptrInt64(42), SnapshotTitle: "снимок",
	}
	store.chapters = []*models.Chapter{
		{ID: 300, WorkID: 42, Title: "Очерк первый", OrderNumber: 1, StartPage: 5, EndPage: 40},
		{ID: 301, WorkID: 42, ParentID: ptrInt64(300), Title: "Вводные замечания",
			OrderNumber: 1, StartPage: 5, EndPage: 12},
		{ID: 302, WorkID: 42, Title: "Очерк второй", OrderNumber: 2, StartPage: 41, EndPage: 90},
	}

	var gotWorkID int64
	var gotStart, gotEnd int
	pageStore := &fakePageStore{
		getPageRangeFn: func(_ context.Context, workID int64, startPage, endPage int) ([]*models.Page, error) {
			gotWorkID, gotStart, gotEnd = workID, startPage, endPage
			return []*models.Page{{ID: 1, WorkID: workID, PageNumber: startPage, ContentMarkdown: "текст"}}, nil
		},
	}

	h := NewCollectionHandler(store, pageStore, markdown.NewRenderer(), NewRangeCache(pagecache.New("")), "", false)

	req := httptest.NewRequest(http.MethodGet, "/api/collections/materializm/items/9/pages", nil)
	req = mux.SetURLVars(req, map[string]string{"slug": "materializm", "itemId": "9"})
	rec := httptest.NewRecorder()
	h.ItemPages(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, ожидался 200 (страницы, а не 410); тело: %s", rec.Code, rec.Body.String())
	}
	if gotWorkID != 42 || gotStart != 5 || gotEnd != 90 {
		t.Errorf("границы (том=%d, %d—%d), ожидались (42, 5—90) — по крайним главам верхнего уровня",
			gotWorkID, gotStart, gotEnd)
	}

	// Форма провода та же, что у страниц главы: слим-полосы, без markdown.
	// Читает их один и тот же ReadingSurface, и разъехаться формам нельзя.
	var got struct {
		Pages []struct {
			PageNumber int    `json:"page_number"`
			HTML       string `json:"html"`
		} `json:"pages"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("bad JSON: %v; тело: %s", err, rec.Body.String())
	}
	if len(got.Pages) != 1 || got.Pages[0].PageNumber != 5 || !strings.Contains(got.Pages[0].HTML, "текст") {
		t.Errorf("слим-полоса не сложилась: %+v", got.Pages)
	}
	if strings.Contains(rec.Body.String(), "content_markdown") {
		t.Errorf("content_markdown не должен уезжать в ответ: %s", rec.Body.String())
	}
}

func TestCollectionHandlerItemPagesWorkItemFallsBackToWorkPages(t *testing.T) {
	// У работы вовсе нет глав (ещё не расставлены toc-chapters) — границы
	// берутся по первой и последней странице PageStore.ListPageMap (лёгкая
	// карта страниц, без content_markdown — см. Important-2 в ревью задачи 5).
	store := collectionFixture()
	store.item = &models.CollectionItem{
		ID: 9, CollectionID: 1, Kind: models.CollectionItemKindWork,
		WorkID: ptrInt64(42), SnapshotTitle: "снимок",
	}
	store.chapters = nil

	var gotStart, gotEnd int
	pageStore := &fakePageStore{
		listPageMapFn: func(_ context.Context, workID int64) ([]models.PageMapEntry, error) {
			return []models.PageMapEntry{
				{PageNumber: 3},
				{PageNumber: 4},
				{PageNumber: 5},
			}, nil
		},
		getPageRangeFn: func(_ context.Context, workID int64, startPage, endPage int) ([]*models.Page, error) {
			gotStart, gotEnd = startPage, endPage
			return []*models.Page{{ID: 1, WorkID: workID, PageNumber: startPage}}, nil
		},
	}

	h := NewCollectionHandler(store, pageStore, markdown.NewRenderer(), NewRangeCache(pagecache.New("")), "", false)

	req := httptest.NewRequest(http.MethodGet, "/api/collections/materializm/items/9/pages", nil)
	req = mux.SetURLVars(req, map[string]string{"slug": "materializm", "itemId": "9"})
	rec := httptest.NewRecorder()
	h.ItemPages(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, ожидался 200; тело: %s", rec.Code, rec.Body.String())
	}
	if gotStart != 3 || gotEnd != 5 {
		t.Errorf("границы %d—%d, ожидались 3—5 — по первой и последней странице тома", gotStart, gotEnd)
	}
}

func TestCollectionHandlerMoveItem(t *testing.T) {
	store := collectionFixture()
	h := NewCollectionHandler(store, nil, nil, NewRangeCache(pagecache.New("")), "", false)

	body := bytes.NewBufferString(`{"order_number":3}`)
	req := httptest.NewRequest(http.MethodPatch, "/api/collections/materializm/items/11/move", body)
	req = mux.SetURLVars(req, map[string]string{"slug": "materializm", "itemId": "11"})
	rec := httptest.NewRecorder()
	withClaims(testCollectionEditorClaims, h.MoveItem)(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("код %d, ожидался 204; тело: %s", rec.Code, rec.Body.String())
	}
	if store.movedTo != 3 {
		t.Errorf("элемент поехал на место %d, ожидалось 3", store.movedTo)
	}
}

// --- Второй вид адреса подборки: /collections/{ник}/{слаг} ---

// TestCollectionResolvesByNicknameAndSlug — Get разбирает читательский адрес:
// оба сегмента пути передаются, GetByAuthorSlug находит подборку по паре.
func TestCollectionResolvesByNicknameAndSlug(t *testing.T) {
	owner := testCollectionOwnerClaims.UserID
	store := &fakeCollectionStore{
		collections: []*models.Collection{{
			ID: 2, Title: "Подборка читателя", Slug: "moya-podborka",
			OwnerID: &owner, AuthorNickname: testCollectionOwnerClaims.Nickname,
			PublishedAt: ptrTime(time.Now().Add(-time.Hour)),
		}},
	}
	h := NewCollectionHandler(store, nil, nil, NewRangeCache(pagecache.New("")), "", false)

	req := httptest.NewRequest(http.MethodGet, "/api/collections/хранитель/moya-podborka", nil)
	req = mux.SetURLVars(req, map[string]string{"nickname": testCollectionOwnerClaims.Nickname, "slug": "moya-podborka"})
	rec := httptest.NewRecorder()
	h.Get(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, ожидался 200; тело: %s", rec.Code, rec.Body.String())
	}
	var got models.Collection
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("ответ не разобрался: %v", err)
	}
	if got.ID != 2 {
		t.Errorf("id %d, ожидался 2 (подборка читателя)", got.ID)
	}
}

// TestStaffCollectionStaysOnBareSlug — сотрудническая подборка (пустой ник)
// остаётся на прежнем, односегментном адресе: nickname в mux.Vars просто
// отсутствует, collectionKey отдаёт "" — тот же путь, что и до этой задачи.
func TestStaffCollectionStaysOnBareSlug(t *testing.T) {
	h := NewCollectionHandler(collectionFixture(), nil, nil, NewRangeCache(pagecache.New("")), "", false)

	req := httptest.NewRequest(http.MethodGet, "/api/collections/materializm", nil)
	req = mux.SetURLVars(req, map[string]string{"slug": "materializm"})
	rec := httptest.NewRecorder()
	h.Get(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, ожидался 200; тело: %s", rec.Code, rec.Body.String())
	}
	var got models.Collection
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("ответ не разобрался: %v", err)
	}
	if got.Slug != "materializm" {
		t.Errorf("slug %q, ожидался materializm", got.Slug)
	}
}

// TestBareSlugDoesNotResolveReaderCollection — пункт ревью сверх брифа:
// голый адрес /collections/{слаг} обязан находить ТОЛЬКО сотруднические
// подборки. Если бы GetBySlug снова искал по одному слагу без учёта ника,
// эта читательская подборка отдалась бы и здесь — то есть один и тот же
// слаг у разных читателей давал бы недетерминированный ответ.
func TestBareSlugDoesNotResolveReaderCollection(t *testing.T) {
	owner := testCollectionOwnerClaims.UserID
	store := &fakeCollectionStore{
		collections: []*models.Collection{{
			ID: 3, Title: "Подборка читателя", Slug: "taken",
			OwnerID: &owner, AuthorNickname: testCollectionOwnerClaims.Nickname,
			PublishedAt: ptrTime(time.Now().Add(-time.Hour)),
		}},
	}
	h := NewCollectionHandler(store, nil, nil, NewRangeCache(pagecache.New("")), "", false)

	req := httptest.NewRequest(http.MethodGet, "/api/collections/taken", nil)
	req = mux.SetURLVars(req, map[string]string{"slug": "taken"})
	rec := httptest.NewRecorder()
	h.Get(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("код %d, ожидался 404 — голый слаг не должен находить читательскую подборку; тело: %s",
			rec.Code, rec.Body.String())
	}
}

// TestSameSlugUnderDifferentAuthors — одинаковый слаг у двух читателей не
// перехватывает друг друга: разводит пара (ник, слаг), а не один слаг.
func TestSameSlugUnderDifferentAuthors(t *testing.T) {
	petya := int64(601)
	vanya := int64(602)
	store := &fakeCollectionStore{
		collections: []*models.Collection{
			{
				ID: 11, Title: "Подборка Пети", Slug: "obshiy",
				OwnerID: &petya, AuthorNickname: "петя",
				PublishedAt: ptrTime(time.Now().Add(-time.Hour)),
			},
			{
				ID: 12, Title: "Подборка Вани", Slug: "obshiy",
				OwnerID: &vanya, AuthorNickname: "ваня",
				PublishedAt: ptrTime(time.Now().Add(-time.Hour)),
			},
		},
	}
	h := NewCollectionHandler(store, nil, nil, NewRangeCache(pagecache.New("")), "", false)

	get := func(nickname string) *models.Collection {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/collections/"+nickname+"/obshiy", nil)
		req = mux.SetURLVars(req, map[string]string{"nickname": nickname, "slug": "obshiy"})
		rec := httptest.NewRecorder()
		h.Get(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("nickname=%q: код %d, ожидался 200; тело: %s", nickname, rec.Code, rec.Body.String())
		}
		var got models.Collection
		if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
			t.Fatalf("nickname=%q: ответ не разобрался: %v", nickname, err)
		}
		return &got
	}

	if got := get("петя"); got.ID != 11 {
		t.Errorf("петя/obshiy отдал id %d, ожидался 11", got.ID)
	}
	if got := get("ваня"); got.ID != 12 {
		t.Errorf("ваня/obshiy отдал id %d, ожидался 12", got.ID)
	}
}

// TestCreateCollectionDuplicateSlugConflicts — пункт ревью сверх брифа: тот
// же автор, тот же слаг второй раз — 409 с понятным сообщением, а не
// проброс нарушения уникального индекса в 500.
func TestCreateCollectionDuplicateSlugConflicts(t *testing.T) {
	owner := testCollectionOwnerClaims.UserID
	store := &fakeCollectionStore{
		collections: []*models.Collection{{
			ID: 20, Title: "Уже есть", Slug: "moya-podborka",
			OwnerID: &owner, AuthorNickname: testCollectionOwnerClaims.Nickname,
		}},
	}
	h := NewCollectionHandler(store, nil, nil, NewRangeCache(pagecache.New("")), "", false)

	body := bytes.NewBufferString(`{"title":"Вторая попытка","slug":"moya-podborka"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/collections", body)
	rec := httptest.NewRecorder()
	withClaims(testCollectionOwnerClaims, h.Create)(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("код %d, ожидался 409; тело: %s", rec.Code, rec.Body.String())
	}
	var got map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("ответ не разобрался: %v", err)
	}
	if got["message"] == "" {
		t.Errorf("тело ответа не несёт понятного сообщения: %v", got)
	}
	if len(store.collections) != 1 {
		t.Errorf("после отказа в сторе %d подборок, ожидалась 1 (вставки не было)", len(store.collections))
	}
}

// TestCreateCollectionSameSlugDifferentAuthorSucceeds — тот же слаг, но
// другой автор, конфликтом не считается: уникальность держит пара
// (author_nickname, slug), а не голый slug.
func TestCreateCollectionSameSlugDifferentAuthorSucceeds(t *testing.T) {
	stranger := testCollectionStrangerClaims.UserID
	store := &fakeCollectionStore{
		collections: []*models.Collection{{
			ID: 20, Title: "Уже есть", Slug: "moya-podborka",
			OwnerID: &stranger, AuthorNickname: testCollectionStrangerClaims.Nickname,
		}},
	}
	h := NewCollectionHandler(store, nil, nil, NewRangeCache(pagecache.New("")), "", false)

	body := bytes.NewBufferString(`{"title":"Моя","slug":"moya-podborka"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/collections", body)
	rec := httptest.NewRecorder()
	withClaims(testCollectionOwnerClaims, h.Create)(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("код %d, ожидался 201 — тот же слаг у другого автора не конфликт; тело: %s", rec.Code, rec.Body.String())
	}
}

// TestUpdateCollectionDuplicateSlugConflicts — то же свойство, что у Create
// (пункт ревью: «повтор слага под одним ником отвечает 409»), приложенное к
// переименованию: владелец правит ОДНУ из своих двух подборок в адрес
// ДРУГОЙ своей же — тоже бьёт в collections_author_slug_key и тоже должно
// отвечать понятным 409, а не пробрасывать нарушение индекса в 500.
func TestUpdateCollectionDuplicateSlugConflicts(t *testing.T) {
	owner := testCollectionOwnerClaims.UserID
	store := &fakeCollectionStore{
		collections: []*models.Collection{
			{ID: 30, Title: "Первая", Slug: "pervaya", OwnerID: &owner, AuthorNickname: testCollectionOwnerClaims.Nickname},
			{ID: 31, Title: "Вторая", Slug: "vtoraya", OwnerID: &owner, AuthorNickname: testCollectionOwnerClaims.Nickname},
		},
	}
	h := NewCollectionHandler(store, nil, nil, NewRangeCache(pagecache.New("")), "", false)

	body := bytes.NewBufferString(`{"title":"Вторая","slug":"pervaya"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/collections/vtoraya", body)
	req = mux.SetURLVars(req, map[string]string{"nickname": testCollectionOwnerClaims.Nickname, "slug": "vtoraya"})
	rec := httptest.NewRecorder()
	withClaims(testCollectionOwnerClaims, h.Update)(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("код %d, ожидался 409; тело: %s", rec.Code, rec.Body.String())
	}
	var got map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("ответ не разобрался: %v", err)
	}
	if got["message"] == "" {
		t.Errorf("тело ответа не несёт понятного сообщения: %v", got)
	}
}
