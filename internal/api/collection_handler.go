package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"github.com/jackc/pgx/v5/pgconn"

	"proofreader/internal/auth"
	"proofreader/internal/middleware"
	"proofreader/internal/models"
	"proofreader/internal/pagecache"
	"proofreader/internal/repository"
	"proofreader/pkg/markdown"
)

// collectionPublishesPerDay — предел частоты публикации: 3 в сутки по
// отметке адреса, тот же рельс, что у остальных пределов (readerSignupsPerDay,
// PageSuggestion CreateWithinLimit). Держит только читателей — сотрудник
// (editor/administrator) от него освобождён вовсе, см. Publish.
const collectionPublishesPerDay = 3

// mayEdit решает, вправе ли пришедший править эту подборку.
//
// Редактор чужую (читательскую) подборку НЕ правит: это то же разделение
// «мы размещаем, автор собирает», на котором стоит вся ось читательского.
// Администратору остаётся удаление — рычаг снятия, а не правки (см. Delete).
//
// Различие идёт по AuthorNickname, а НЕ по OwnerID: пустой ник и есть признак
// сотруднической подборки (тот же признак, что у List() отделяет витрину от
// читательских), а OwnerID тем временем законно бывает nil и у читательской
// строки — ON DELETE SET NULL при удалении учётной записи. Если бы правило
// смотрело на OwnerID == nil, удаление аккаунта автора молча превращало бы
// его подборку в сотрудническую, которую правит любой редактор: ник-снимок
// остаётся, а владельца больше нет ни у кого. Различая по нику, такая
// строка просто перестаёт быть редактируемой вовсе — ни владельцем (его
// нет), ни персоналом (подборка не их) — что и есть правильный исход для
// осиротевшей читательской подборки.
func mayEdit(claims *auth.Claims, c *models.Collection) bool {
	if claims == nil {
		return false
	}
	if c.AuthorNickname == "" {
		return claims.Role == models.RoleEditor || claims.Role == models.RoleAdministrator
	}
	return c.OwnerID != nil && *c.OwnerID == claims.UserID
}

// collectionVisibleTo решает, вправе ли пришедший читать эту подборку —
// черновик (никогда не публиковался) и снятую с публикации выдаёт только
// тот, кому mayEdit разрешает правку (владелец, или сотрудник для
// легаси-строки без ника). Это единственное место, решающее видимость
// подборки, и звать его обязаны все три публичных маршрута, отдающих её
// содержимое: Get, ItemPages и скачивание (DownloadHandler.Collection) —
// иначе снятая с публикации подборка остаётся доступна по одному из них
// (см. историю: ItemPages и download отдавали 200 после 410 на Get).
//
// ok=false — показывать нельзя; status/message тогда уже готовы для
// writeError (Get, ItemPages) или http.Error (DownloadHandler.Collection,
// который в другом файле того же пакета и claims достаёт сам, до вызова
// h.serve — DownloadSource claims не видит вовсе).
func collectionVisibleTo(claims *auth.Claims, c *models.Collection) (status int, message string, ok bool) {
	if c.PublishedAt != nil {
		return 0, "", true
	}
	if mayEdit(claims, c) {
		return 0, "", true
	}
	if c.PublishIPHash == "" {
		// Никогда не публиковалась — черновик не выдаёт даже факт своего
		// существования.
		return http.StatusNotFound, "Подборка не найдена", false
	}
	// Была опубликована и снята — «было и снято», отличимое от «никогда не
	// было».
	return http.StatusGone, "Подборка снята с публикации", false
}

// collectionKey достаёт из пути пару «ник, слаг». Ник пуст, если адрес
// односегментный: /collections/{слаг} — сотрудническая подборка.
func collectionKey(r *http.Request) (nickname, slug string) {
	vars := mux.Vars(r)
	return vars["nickname"], vars["slug"]
}

// collectionSlugUniqueConstraint — имя составного индекса (миграция 000024),
// которое держит уникальность пары (author_nickname, slug). Проверяется по
// имени, а не только по коду 23505, тем же приёмом, что isVolumeTakenError:
// голого кода мало, вставка может упереться и в другой уникальный индекс.
const collectionSlugUniqueConstraint = "collections_author_slug_key"

// isCollectionSlugTakenError отличает «такой слаг уже занят этим автором» от
// прочих ошибок вставки — чтобы Create отвечал понятным 409, а не 500.
func isCollectionSlugTakenError(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == "23505" && pgErr.ConstraintName == collectionSlugUniqueConstraint
}

// CollectionHandler handles collection endpoints
type CollectionHandler struct {
	collectionStore CollectionStore
	pageStore       PageStore
	renderer        *markdown.Renderer
	cache           *RangeCache
	jwtSecret       string
	trustProxy      bool
}

// NewCollectionHandler creates a new collection handler
func NewCollectionHandler(
	collectionStore CollectionStore,
	pageStore PageStore,
	renderer *markdown.Renderer,
	cache *RangeCache,
	jwtSecret string,
	trustProxy bool,
) *CollectionHandler {
	return &CollectionHandler{
		collectionStore: collectionStore,
		pageStore:       pageStore,
		renderer:        renderer,
		cache:           cache,
		jwtSecret:       jwtSecret,
		trustProxy:      trustProxy,
	}
}

// CollectionRequest is the body of POST/PUT /api/collections.
type CollectionRequest struct {
	Title       string `json:"title"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
}

// CollectionItemRequest is the body of POST /api/collections/{slug}/items.
//
// Снимка заголовка здесь намеренно нет: его пишет репозиторий, из базы.
type CollectionItemRequest struct {
	Kind           string `json:"kind"`
	ChapterID      *int64 `json:"chapter_id"`
	WorkID         *int64 `json:"work_id"`
	AuthorOverride string `json:"author_override"`
}

// CollectionItemAuthorRequest is the body of PUT /api/collections/{slug}/items/{itemId}.
type CollectionItemAuthorRequest struct {
	AuthorOverride string `json:"author_override"`
}

// CollectionMoveRequest is the body of PATCH .../items/{itemId}/move.
type CollectionMoveRequest struct {
	OrderNumber int `json:"order_number"`
}

// List retrieves all collections
func (h *CollectionHandler) List(w http.ResponseWriter, r *http.Request) {
	collections, err := h.collectionStore.List(context.Background())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось получить список подборок")
		return
	}

	// Пустая выборка приезжает из репозитория nil-слайсом и кодируется как
	// null. Клиент ждёт список.
	if collections == nil {
		collections = []*models.Collection{}
	}

	writeJSON(w, collections)
}

// Mine отдаёт подборки вошедшего читателя — черновики и опубликованные,
// для экрана «моё». Витринному List() показывать черновики нельзя, поэтому
// это отдельный маршрут, а не фильтр над ним.
//
// Висит на подроутере reader (AuthMiddleware + RequireRole), а не public —
// личные данные, включая черновики. От захвата общим GET /collections/{slug}
// (объявлен на public) защищает не порядок строк HandleFunc, а то, что сам
// подроутер reader объявлен раньше public (см. комментарий у его объявления
// в router.go и TestCollectionsMineRouteNotSwallowedByGenericSlug). Проверка
// claims здесь поэтому страховка «на случай переезда маршрута», как и в
// PageSuggestionHandler.Mine, — RequireRole уже не пускает анонимный запрос
// дальше.
func (h *CollectionHandler) Mine(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "Нужно записаться в читальню")
		return
	}

	collections, err := h.collectionStore.ListByOwner(context.Background(), claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось получить список подборок")
		return
	}
	// Тот же известный дефект, что и у остальных списочных маршрутов: пустой
	// срез из репозитория кодируется как null, а клиент ждёт [].
	if collections == nil {
		collections = []*models.Collection{}
	}

	writeJSONStatus(w, http.StatusOK, collections)
}

// Get retrieves one collection by slug, with its assembled table of contents.
//
// Черновик постороннему отдаёт 404 — не выдаёт даже факт существования.
// Снятая с публикации подборка отдаёт 410: «было и снято», отличимое от
// «никогда не было» — см. Collection.PublishIPHash. Владелец (и легаси-строка
// без владельца — сотрудник) видит и черновик, и снятую подборку как обычно.
func (h *CollectionHandler) Get(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()

	nickname, slug := collectionKey(r)
	collection, err := h.collectionStore.GetByAuthorSlug(ctx, nickname, slug)
	if err != nil || collection == nil {
		writeError(w, http.StatusNotFound, "Подборка не найдена")
		return
	}

	claims, _ := middleware.GetUserFromContext(r.Context())
	if status, message, ok := collectionVisibleTo(claims, collection); !ok {
		writeError(w, status, message)
		return
	}

	rows, err := h.collectionStore.ItemRows(ctx, collection.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось получить состав подборки")
		return
	}

	chapters, err := h.collectionStore.ChaptersForWorks(ctx, collectionWorkIDs(rows))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось получить главы")
		return
	}

	// buildCollectionTOC всегда возвращает make([]..., 0, len(rows)), не nil —
	// у пустой подборки Items остаётся пустым срезом. Из-за json:"items,omitempty"
	// это значит, что ключ "items" в ответе отсутствует вовсе (не []); потребитель
	// обязан читать отсутствие ключа как пустой состав.
	collection.Items = buildCollectionTOC(rows, chapters)

	writeJSON(w, collection)
}

// Create creates a new collection
func (h *CollectionHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req CollectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Некорректное тело запроса")
		return
	}

	if req.Title == "" || req.Slug == "" {
		writeError(w, http.StatusBadRequest, "Укажите название и адрес подборки (slug)")
		return
	}

	collection := &models.Collection{
		Title:       req.Title,
		Slug:        req.Slug,
		Description: req.Description,
	}
	// Владелец — тот, кто создал подборку (читатель или сотрудник); маршрут
	// теперь висит на подроутере reader, токен обязателен. AuthorNickname —
	// снимок ника, пустой у сотрудника (claims.Nickname пуст не у читателя) —
	// им List() отличает сотрудническую витрину от читательских подборок, а
	// UNIQUE(author_nickname, slug) — читательские адреса от сотруднических.
	if claims, ok := middleware.GetUserFromContext(r.Context()); ok {
		userID := claims.UserID
		collection.OwnerID = &userID
		collection.AuthorNickname = claims.Nickname
	}

	if err := h.collectionStore.Create(context.Background(), collection); err != nil {
		if isCollectionSlugTakenError(err) {
			writeError(w, http.StatusConflict, "Такой адрес подборки у вас уже занят. Выберите другой.")
			return
		}
		writeError(w, http.StatusInternalServerError, "Не удалось создать подборку")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(collection)
}

// Update updates a collection
func (h *CollectionHandler) Update(w http.ResponseWriter, r *http.Request) {
	var req CollectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Некорректное тело запроса")
		return
	}

	if req.Title == "" || req.Slug == "" {
		writeError(w, http.StatusBadRequest, "Укажите название и адрес подборки (slug)")
		return
	}

	ctx := context.Background()
	nickname, slug := collectionKey(r)
	collection, err := h.collectionStore.GetByAuthorSlug(ctx, nickname, slug)
	if err != nil || collection == nil {
		writeError(w, http.StatusNotFound, "Подборка не найдена")
		return
	}

	claims, _ := middleware.GetUserFromContext(r.Context())
	if !mayEdit(claims, collection) {
		writeError(w, http.StatusForbidden, "Правка чужой подборки недоступна")
		return
	}

	collection.Title = req.Title
	collection.Slug = req.Slug
	collection.Description = req.Description

	if err := h.collectionStore.Update(ctx, collection); err != nil {
		// Переименование в слаг, уже занятый этим же автором (например, в его
		// другой подборке), бьёт в тот же составной индекс, что и Create —
		// бриф называет свойство «повтор слага под одним ником отвечает 409»
		// как таковое, а не только для точки входа Create.
		if isCollectionSlugTakenError(err) {
			writeError(w, http.StatusConflict, "Такой адрес подборки у вас уже занят. Выберите другой.")
			return
		}
		writeError(w, http.StatusInternalServerError, "Не удалось обновить подборку")
		return
	}

	writeJSON(w, collection)
}

// Delete deletes a collection. Администратору доступно удаление чужой
// подборки (рычаг снятия), но не правка — см. mayEdit.
func (h *CollectionHandler) Delete(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()

	nickname, slug := collectionKey(r)
	collection, err := h.collectionStore.GetByAuthorSlug(ctx, nickname, slug)
	if err != nil || collection == nil {
		writeError(w, http.StatusNotFound, "Подборка не найдена")
		return
	}

	claims, _ := middleware.GetUserFromContext(r.Context())
	if !mayEdit(claims, collection) && (claims == nil || claims.Role != models.RoleAdministrator) {
		writeError(w, http.StatusForbidden, "Удаление чужой подборки недоступно")
		return
	}

	if err := h.collectionStore.Delete(ctx, collection.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось удалить подборку")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Publish переводит подборку из черновика в опубликованную. Предел частоты —
// 3 в сутки по отметке адреса, тот же рельс, что у остальных пределов; ключ
// хранится в publish_ip_hash и после снятия с публикации остаётся —
// см. Collection.PublishIPHash.
func (h *CollectionHandler) Publish(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()

	nickname, slug := collectionKey(r)
	collection, err := h.collectionStore.GetByAuthorSlug(ctx, nickname, slug)
	if err != nil || collection == nil {
		writeError(w, http.StatusNotFound, "Подборка не найдена")
		return
	}

	claims, _ := middleware.GetUserFromContext(r.Context())
	if !mayEdit(claims, collection) {
		writeError(w, http.StatusForbidden, "Публикация чужой подборки недоступна")
		return
	}

	// Предел рассчитан на читателя: одно частное лицо публикует со своего
	// адреса. Сотрудник правит сотрудническую подборку (mayEdit пускает сюда
	// только staff-строку с пустым AuthorNickname) с общего редакционного
	// адреса — там счёт по IP делят между собой все редакторы и
	// администраторы сразу, и штатная публикация нескольких подборок за день
	// упиралась бы в чужой лимит. Находка рецензии: staff освобождается от
	// предела вовсе, а не получает его выше — читательский рельс остаётся
	// как есть.
	limit := collectionPublishesPerDay
	if claims.Role == models.RoleEditor || claims.Role == models.RoleAdministrator {
		limit = math.MaxInt
	}

	ipHash := hashIP(clientIP(r, h.trustProxy), h.jwtSecret)
	published, publishedAt, err := h.collectionStore.PublishWithinLimit(
		ctx, collection.ID, ipHash, limit, time.Now().Add(-24*time.Hour))
	if err != nil {
		log.Printf("publish collection: %v", err)
		writeError(w, http.StatusInternalServerError, "Не удалось опубликовать подборку")
		return
	}
	if !published {
		writeError(w, http.StatusTooManyRequests,
			"С этого адреса сегодня уже публиковали подборки. Попробуйте завтра.")
		return
	}

	collection.PublishedAt = &publishedAt
	writeJSON(w, collection)
}

// Unpublish возвращает подборку в черновик. publish_ip_hash не стирается —
// см. Collection.PublishIPHash и PublishWithinLimit.
func (h *CollectionHandler) Unpublish(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()

	nickname, slug := collectionKey(r)
	collection, err := h.collectionStore.GetByAuthorSlug(ctx, nickname, slug)
	if err != nil || collection == nil {
		writeError(w, http.StatusNotFound, "Подборка не найдена")
		return
	}

	claims, _ := middleware.GetUserFromContext(r.Context())
	if !mayEdit(claims, collection) {
		writeError(w, http.StatusForbidden, "Правка чужой подборки недоступна")
		return
	}

	if err := h.collectionStore.Unpublish(ctx, collection.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось снять подборку с публикации")
		return
	}

	collection.PublishedAt = nil
	writeJSON(w, collection)
}

// AddItem appends an item to the collection.
func (h *CollectionHandler) AddItem(w http.ResponseWriter, r *http.Request) {
	var req CollectionItemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Некорректное тело запроса")
		return
	}

	switch req.Kind {
	case models.CollectionItemKindChapter:
		if req.ChapterID == nil {
			writeError(w, http.StatusBadRequest, "Укажите главу (chapter_id)")
			return
		}
	case models.CollectionItemKindWork:
		if req.WorkID == nil {
			writeError(w, http.StatusBadRequest, "Укажите том (work_id)")
			return
		}
		// Клиент вправе прислать оба поля разом; для kind=work chapter_id
		// обнуляется здесь — иначе вставку отвергнет CHECK-констрейнт
		// collection_items_target_check, и вместо осмысленного ответа
		// пользователь получит 500.
		req.ChapterID = nil
	default:
		writeError(w, http.StatusBadRequest, "Вид элемента должен быть «chapter» или «work»")
		return
	}

	ctx := context.Background()
	nickname, slug := collectionKey(r)
	collection, err := h.collectionStore.GetByAuthorSlug(ctx, nickname, slug)
	if err != nil || collection == nil {
		writeError(w, http.StatusNotFound, "Подборка не найдена")
		return
	}

	claims, _ := middleware.GetUserFromContext(r.Context())
	if !mayEdit(claims, collection) {
		writeError(w, http.StatusForbidden, "Правка чужой подборки недоступна")
		return
	}

	item, err := h.collectionStore.AddItem(
		ctx, collection.ID, req.Kind, req.ChapterID, req.WorkID, req.AuthorOverride)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось добавить элемент в подборку")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(item)
}

// UpdateItem changes the author override of an item.
func (h *CollectionHandler) UpdateItem(w http.ResponseWriter, r *http.Request) {
	itemID, err := strconv.ParseInt(mux.Vars(r)["itemId"], 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Некорректный идентификатор элемента")
		return
	}

	var req CollectionItemAuthorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Некорректное тело запроса")
		return
	}

	ctx := context.Background()
	nickname, slug := collectionKey(r)
	collection, err := h.collectionStore.GetByAuthorSlug(ctx, nickname, slug)
	if err != nil || collection == nil {
		writeError(w, http.StatusNotFound, "Подборка не найдена")
		return
	}

	claims, _ := middleware.GetUserFromContext(r.Context())
	if !mayEdit(claims, collection) {
		writeError(w, http.StatusForbidden, "Правка чужой подборки недоступна")
		return
	}

	if err := h.collectionStore.UpdateItemAuthor(ctx, collection.ID, itemID, req.AuthorOverride); err != nil {
		writeError(w, http.StatusNotFound, "Элемент подборки не найден")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// DeleteItem removes an item from the collection.
func (h *CollectionHandler) DeleteItem(w http.ResponseWriter, r *http.Request) {
	itemID, err := strconv.ParseInt(mux.Vars(r)["itemId"], 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Некорректный идентификатор элемента")
		return
	}

	ctx := context.Background()
	nickname, slug := collectionKey(r)
	collection, err := h.collectionStore.GetByAuthorSlug(ctx, nickname, slug)
	if err != nil || collection == nil {
		writeError(w, http.StatusNotFound, "Подборка не найдена")
		return
	}

	claims, _ := middleware.GetUserFromContext(r.Context())
	if !mayEdit(claims, collection) {
		writeError(w, http.StatusForbidden, "Правка чужой подборки недоступна")
		return
	}

	if err := h.collectionStore.DeleteItem(ctx, collection.ID, itemID); err != nil {
		writeError(w, http.StatusNotFound, "Элемент подборки не найден")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// MoveItem puts an item at the requested position.
func (h *CollectionHandler) MoveItem(w http.ResponseWriter, r *http.Request) {
	itemID, err := strconv.ParseInt(mux.Vars(r)["itemId"], 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Некорректный идентификатор элемента")
		return
	}

	var req CollectionMoveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Некорректное тело запроса")
		return
	}

	ctx := context.Background()
	nickname, slug := collectionKey(r)
	collection, err := h.collectionStore.GetByAuthorSlug(ctx, nickname, slug)
	if err != nil || collection == nil {
		writeError(w, http.StatusNotFound, "Подборка не найдена")
		return
	}

	claims, _ := middleware.GetUserFromContext(r.Context())
	if !mayEdit(claims, collection) {
		writeError(w, http.StatusForbidden, "Правка чужой подборки недоступна")
		return
	}

	if err := h.collectionStore.MoveItem(ctx, collection.ID, itemID, req.OrderNumber); err != nil {
		writeError(w, http.StatusNotFound, "Элемент подборки не найден")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ItemPages returns the pages of one item with rendered HTML and aggregated
// footnotes — the same shape as ChapterHandler.ListPages, because the reading
// UI consumes per-page sections: page markers, sub-chapter anchors and the
// saved reading position all hang off them.
func (h *CollectionHandler) ItemPages(w http.ResponseWriter, r *http.Request) {
	itemID, err := strconv.ParseInt(mux.Vars(r)["itemId"], 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Некорректный идентификатор элемента")
		return
	}

	ctx := context.Background()
	nickname, slug := collectionKey(r)
	collection, err := h.collectionStore.GetByAuthorSlug(ctx, nickname, slug)
	if err != nil || collection == nil {
		writeError(w, http.StatusNotFound, "Подборка не найдена")
		return
	}

	// Тот же гейт видимости, что у Get — черновик и снятую подборку страницы
	// элемента не должны выдавать в обход адреса самой подборки.
	claims, _ := middleware.GetUserFromContext(r.Context())
	if status, message, ok := collectionVisibleTo(claims, collection); !ok {
		writeError(w, status, message)
		return
	}

	item, err := h.collectionStore.ItemByID(ctx, collection.ID, itemID)
	if err != nil || item == nil {
		writeError(w, http.StatusNotFound, "Элемент подборки не найден")
		return
	}

	// Источник пересоздан или удалён. 410, а не 404: элемент на месте, читать
	// нечего — и составителю это надо показать по-разному.
	if item.WorkID == nil ||
		(item.Kind == models.CollectionItemKindChapter && item.ChapterID == nil) {
		writeError(w, http.StatusGone, "Источник элемента удалён")
		return
	}

	start, end, ok, err := h.resolveItemPageRange(ctx, collection.ID, item)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось определить границы элемента")
		return
	}
	if !ok {
		writeError(w, http.StatusGone, "Источник элемента удалён")
		return
	}

	// Ключ — сам диапазон полос (work_id, start, end), не элемент и не
	// подборка: глава и элемент подборки, накрывающие один диапазон,
	// обязаны читать один и тот же файл.
	key := pagecache.Key{WorkID: *item.WorkID, Start: start, End: end}
	h.cache.Serve(w, r, key, func() (any, error) {
		pages, err := h.pageStore.GetPageRange(ctx, *item.WorkID, start, end)
		if err != nil {
			return nil, err
		}
		pageHTML, notes := renderPageRange(h.renderer, pages)
		return ChapterPagesResponse{
			Pages:         chapterPagesFromRange(pages, pageHTML),
			FootnotesHTML: markdown.RenderNotes(notes),
		}, nil
	})
}

// resolveItemPageRange отдаёт внутренние (не печатные) границы элемента —
// по ним ходит GetPageRange.
//
// У главы границы свои и уже приезжают вместе с ItemRows (см. itemPageRange).
// У элемента-работы own границ нет вовсе: chapter_id у него пуст, и запрос
// ItemRows не подтягивает по нему ни одной главы. Поэтому для kind=work
// границы берутся отдельно: сперва по крайним страницам глав верхнего
// уровня этой работы (ChaptersForWorks), а если таких глав нет вовсе
// (том ещё не размечен toc-chapters) — по первой и последней странице
// работы через PageStore.ListPageMap: он отдаёт только номер страницы и
// статус, тогда как ListByWork тянул бы content_markdown всех страниц тома
// целиком ради двух номеров — на большом томе это лишние мегабайты на
// публичном маршруте.
func (h *CollectionHandler) resolveItemPageRange(
	ctx context.Context, collectionID int64, item *models.CollectionItem,
) (start, end int, ok bool, err error) {
	if item.Kind == models.CollectionItemKindChapter {
		rows, err := h.collectionStore.ItemRows(ctx, collectionID)
		if err != nil {
			return 0, 0, false, err
		}
		start, end, ok := itemPageRange(rows, item.ID)
		return start, end, ok, nil
	}

	// item.Kind == models.CollectionItemKindWork (единственный другой вид;
	// item.WorkID уже проверен вызывающим кодом на не-nil).
	chapters, err := h.collectionStore.ChaptersForWorks(ctx, []int64{*item.WorkID})
	if err != nil {
		return 0, 0, false, err
	}
	if start, end, ok := topLevelPageRange(chapters, *item.WorkID); ok {
		return start, end, true, nil
	}

	pageMap, err := h.pageStore.ListPageMap(ctx, *item.WorkID)
	if err != nil {
		return 0, 0, false, err
	}
	if len(pageMap) == 0 {
		return 0, 0, false, nil
	}

	return pageMap[0].PageNumber, pageMap[len(pageMap)-1].PageNumber, true, nil
}

// itemPageRange — внутренние границы элемента-главы: собственные start/end
// главы, которые ItemRows уже подтянул джойном. Для элемента-работы своих
// границ у строки нет (chapter_id пуст) — эта ветка отдельная, см.
// resolveItemPageRange.
func itemPageRange(rows []repository.ItemRow, itemID int64) (start, end int, ok bool) {
	for _, row := range rows {
		if row.Item.ID != itemID {
			continue
		}
		if row.ChapterStartPage != nil && row.ChapterEndPage != nil {
			return *row.ChapterStartPage, *row.ChapterEndPage, true
		}
	}

	return 0, 0, false
}

// topLevelPageRange — минимальная StartPage и максимальная EndPage среди
// глав работы workID без родителя (глав верхнего уровня). ok=false, если
// таких глав нет вовсе.
func topLevelPageRange(chapters []*models.Chapter, workID int64) (start, end int, ok bool) {
	for _, c := range chapters {
		if c.WorkID != workID || c.ParentID != nil {
			continue
		}
		if !ok || c.StartPage < start {
			start = c.StartPage
		}
		if !ok || c.EndPage > end {
			end = c.EndPage
		}
		ok = true
	}

	return start, end, ok
}
