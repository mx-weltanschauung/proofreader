package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"math"
	"net/http"
	"time"

	"proofreader/internal/auth"
	"proofreader/internal/middleware"
	"proofreader/internal/models"
	"proofreader/internal/repository"
)

// documentSubmitsPerDay — предел частоты отправки разборов на модерацию: 3 в
// сутки по отметке адреса, тем же рельсом (ip_hash + pg_advisory_xact_lock,
// счёт и запись одной транзакцией), что у публикации подборки. Число взято
// по её образцу: очередь разбирает человек, и наплыв стоит ему рабочего дня.
//
// Сотрудник от предела освобождён вовсе — как и у подборок: редакционный
// адрес общий на всех, и счёт по IP делили бы между собой все редакторы
// сразу.
const documentSubmitsPerDay = 3

// DocumentReviewHandler — модерация разбора: отправка автором, очередь
// редактору, приём и отказ с причиной, снятие с публикации, «моё».
type DocumentReviewHandler struct {
	documents  DocumentStore
	jwtSecret  string
	trustProxy bool
	// cache — краулерская половина ServingCache. Снятие с публикации — рычаг
	// по жалобе (см. DropCrawler), и nil здесь означает «кэшей нет», как и
	// везде, где ServingCache используется, — так собраны тестовые
	// конструкторы.
	cache *ServingCache
}

func NewDocumentReviewHandler(documents DocumentStore, jwtSecret string, trustProxy bool, cache *ServingCache) *DocumentReviewHandler {
	return &DocumentReviewHandler{documents: documents, jwtSecret: jwtSecret, trustProxy: trustProxy, cache: cache}
}

// load читает разбор из адреса, разводя «нет такого» и «сбой хранилища»: 404
// обязан значить «разбора нет», а не «база недоступна прямо сейчас» — тот же
// приём, что в document_key.go (loadDocumentByKey), которому эта функция
// теперь и делегирует целиком.
func (h *DocumentReviewHandler) load(w http.ResponseWriter, r *http.Request) (*models.Document, bool) {
	return loadDocumentByKey(w, r, h.documents)
}

// Submit отправляет черновик на рассмотрение.
//
// Сотрудник своим же разбором в очередь не идёт: он и есть модератор, и ждать
// самого себя — ритуал, а не контроль (тот же довод, что освобождает staff от
// предела частоты у подборок). Его отправка публикует сразу.
func (h *DocumentReviewHandler) Submit(w http.ResponseWriter, r *http.Request) {
	document, ok := h.load(w, r)
	if !ok {
		return
	}
	claims, _ := middleware.GetUserFromContext(r.Context())
	if !mayEditDocument(claims, document) {
		writeError(w, http.StatusForbidden, "Отправить на проверку можно только свой разбор")
		return
	}
	if document.ReviewStatus == models.DocumentPending {
		writeError(w, http.StatusConflict, "Разбор уже ждёт рассмотрения")
		return
	}

	ctx := context.Background()

	// Сотруднический разбор (подписи нет, править вправе весь персонал)
	// публикуется сразу — PublishOwn, а не Approve: Approve с фикс-раунда 1
	// задачи 6 гвардирован состоянием «на_рассмотрении» (реальная модерация
	// чужого поданного), а тут черновик публикует сам себе тот, кто и есть
	// модератор — ждать очереди не от кого.
	if document.AuthorNickname == "" && isStaffClaims(claims) {
		if err := h.documents.PublishOwn(ctx, document.ID, claims.UserID); err != nil {
			writeError(w, http.StatusInternalServerError, "Не удалось опубликовать разбор")
			return
		}
		h.respondWith(w, ctx, claims, document.ID)
		return
	}

	limit := documentSubmitsPerDay
	if isStaffClaims(claims) {
		limit = math.MaxInt
	}
	ipHash := hashIP(clientIP(r, h.trustProxy), h.jwtSecret)
	submitted, err := h.documents.SubmitWithinLimit(
		ctx, document.ID, ipHash, limit, time.Now().Add(-24*time.Hour))
	if err != nil {
		log.Printf("submit document: %v", err)
		writeError(w, http.StatusInternalServerError, "Не удалось отправить разбор на проверку")
		return
	}
	if !submitted {
		writeError(w, http.StatusTooManyRequests,
			"С этого адреса сегодня уже отправляли разборы. Попробуйте завтра.")
		return
	}
	h.respondWith(w, ctx, claims, document.ID)
}

// Queue — очередь модератора: разборы, ждущие рассмотрения, в порядке
// поступления. Отдаётся как есть, без documentForViewer: модератор судит
// именно о черновике, который ему подали (mayReadDraft это и разрешает).
func (h *DocumentReviewHandler) Queue(w http.ResponseWriter, r *http.Request) {
	documents, err := h.documents.ListForReview(context.Background())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось получить очередь")
		return
	}
	if documents == nil {
		documents = []*models.Document{}
	}
	writeJSON(w, documents)
}

// Approve одобряет ПОДАННЫЙ разбор — реальная модерация чужой правки.
//
// Гвардировано на двух уровнях (фикс-раунд 1 задачи 6): здесь, до похода в
// репозиторий, — против заведомо неверного запроса (черновик ещё не
// отправлен, или уже рассмотрен и это не гонка, а стухшая вкладка); в
// репозитории, атомарно в WHERE (repository.ErrDocumentNotPending) — против
// НАСТОЯЩЕЙ гонки: два модератора с открытой очередью, один успел одобрить, у
// второго в устаревшей вкладке всё ещё кнопка «отклонить». Один уровень тут
// не достаточен — проверка здесь и запись в базе разнесены по времени, и
// ровно в эту щель попадает второй модератор.
func (h *DocumentReviewHandler) Approve(w http.ResponseWriter, r *http.Request) {
	document, ok := h.load(w, r)
	if !ok {
		return
	}
	if msg, status, ok := documentReviewGuard(document); !ok {
		writeError(w, status, msg)
		return
	}
	claims, _ := middleware.GetUserFromContext(r.Context())
	ctx := context.Background()
	if err := h.documents.Approve(ctx, document.ID, claims.UserID); err != nil {
		if errors.Is(err, repository.ErrDocumentNotPending) {
			writeError(w, http.StatusConflict, "Разбор уже рассмотрен")
			return
		}
		writeError(w, http.StatusInternalServerError, "Не удалось одобрить разбор")
		return
	}
	h.respondWithOwnDecision(w, ctx, document.ID)
}

// rejectRequest — тело отказа. Причина из ЗАКРЫТОГО списка: ответить
// модератору автор не может, и свободная причина превратила бы модерацию в
// переписку в один конец (тот же довод, что у отказа предложению правки).
type rejectRequest struct {
	Reason models.DocumentRejectReason `json:"reason"`
}

// Reject — зеркало Approve, тот же двухуровневый guard и то же основание
// (см. комментарий над Approve): стухшая вкладка второго модератора не
// перезаписывает чужое решение, а разбор, который никто не подавал, отказом
// не закрыть.
func (h *DocumentReviewHandler) Reject(w http.ResponseWriter, r *http.Request) {
	document, ok := h.load(w, r)
	if !ok {
		return
	}
	var req rejectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Некорректное тело запроса")
		return
	}
	if !models.ValidDocumentRejectReason(req.Reason) {
		writeError(w, http.StatusBadRequest, "Укажите причину отказа из списка")
		return
	}
	if msg, status, ok := documentReviewGuard(document); !ok {
		writeError(w, status, msg)
		return
	}
	claims, _ := middleware.GetUserFromContext(r.Context())
	ctx := context.Background()
	if err := h.documents.Reject(ctx, document.ID, claims.UserID, req.Reason); err != nil {
		if errors.Is(err, repository.ErrDocumentNotPending) {
			writeError(w, http.StatusConflict, "Разбор уже рассмотрен")
			return
		}
		writeError(w, http.StatusInternalServerError, "Не удалось отклонить разбор")
		return
	}
	h.respondWithOwnDecision(w, ctx, document.ID)
}

// documentReviewGuard — уровень обработчика двухуровневого guard'а
// Approve/Reject (фикс-раунд 1 задачи 6): требует, чтобы разбор СЕЙЧАС был
// «на_рассмотрении», и различает читателю ДВЕ разные причины отказа — «уже
// рассмотрен» (одобрено/отклонено) от «не подавался вовсе» (черновик), это
// разные действия для того, кто нажал кнопку (ждать решения другого
// модератора vs. сперва дождаться отправки автором). Второй, атомарный
// уровень — в самом запросе к репозиторию (repository.ErrDocumentNotPending);
// этот уровень ловит заведомо неверный запрос ДО похода в базу, но гонку
// между чтением здесь и записью там не закрывает — для неё и нужен второй.
func documentReviewGuard(document *models.Document) (message string, status int, ok bool) {
	switch document.ReviewStatus {
	case models.DocumentPending:
		return "", 0, true
	case models.DocumentDraft:
		return "Разбор не подавался на проверку", http.StatusConflict, false
	default:
		return "Разбор уже рассмотрен", http.StatusConflict, false
	}
}

// Unpublish убирает разбор с людей, оставляя автору его черновик.
//
// Доступно владельцу (убрать своё со стены) и сотруднику (рычаг снятия по
// жалобе — ровно как удаление чужой подборки администратором). Правку чужого
// разбора это не открывает: mayEditDocument здесь ни при чём.
func (h *DocumentReviewHandler) Unpublish(w http.ResponseWriter, r *http.Request) {
	document, ok := h.load(w, r)
	if !ok {
		return
	}
	claims, _ := middleware.GetUserFromContext(r.Context())
	if !mayEditDocument(claims, document) && !isStaffClaims(claims) {
		writeError(w, http.StatusForbidden, "Снять с публикации может автор или редакция")
		return
	}
	// Фикс-раунд 1 задачи 6: снимать с публикации то, чего на ней уже нет, —
	// бессмысленное действие, и заодно единственная поверхность, на которой
	// сотрудник тыкался бы в чужой неопубликованный черновик через Unpublish
	// (mayEditDocument его сюда не пускает, а isStaffClaims — пускает).
	if document.PublishedAt == nil {
		writeError(w, http.StatusConflict, "Разбор и так не опубликован")
		return
	}
	ctx := context.Background()
	if err := h.documents.Unpublish(ctx, document.ID); err != nil {
		if errors.Is(err, repository.ErrDocumentNotPublished) {
			writeError(w, http.StatusConflict, "Разбор и так не опубликован")
			return
		}
		writeError(w, http.StatusInternalServerError, "Не удалось снять разбор с публикации")
		return
	}
	// Задача 10: снятие — рычаг по жалобе, и час, пока краулер отдаёт снятый
	// текст из своего кэша, стоит здесь дороже, чем у обычной правки полосы.
	// Сброс тотальный (см. ServingCache.DropCrawler) и не возвращает ошибку —
	// запись уже прошла, отвечать отказом про неё было бы неверно.
	h.cache.DropCrawler()
	h.respondWith(w, ctx, claims, document.ID)
}

// Mine — «моё» автора: черновики, поданное, одобренное и отклонённое с
// причиной. Личные данные, поэтому на подроутере reader; проверка claims
// здесь страховка на случай переезда маршрута, как в CollectionHandler.Mine.
func (h *DocumentReviewHandler) Mine(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "Нужно записаться в читальню")
		return
	}
	documents, err := h.documents.ListByOwner(context.Background(), claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось получить список разборов")
		return
	}
	if documents == nil {
		documents = []*models.Document{}
	}
	writeJSON(w, documents)
}

// respondWith перечитывает разбор и отдаёт его целиком: клиент рисует по
// ответу новое состояние (какая редакция на людях, ждёт ли правка), и
// собирать это на клиенте из голого кода 200 значит разойтись с сервером при
// первом же расхождении.
//
// Фикс-раунд 1 задачи 6: пропускает перечитанный разбор через
// documentForViewer, а не отдаёт сырую строку. Для очереди и для отправки
// автором это ничего не меняет — оба и так вправе видеть черновик. Но
// Unpublish доступен и сотруднику на ЧУЖОМ разборе (рычаг снятия по жалобе),
// и до этой правки сырой ответ утекал Title/MarkdownContent приватного
// черновика постороннему сотруднику, который к правке доступа не имеет.
//
// Approve и Reject этой функцией НЕ пользуются — см. respondWithOwnDecision.
func (h *DocumentReviewHandler) respondWith(w http.ResponseWriter, ctx context.Context, claims *auth.Claims, id int64) {
	document, err := h.documents.GetByID(ctx, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось перечитать разбор")
		return
	}
	writeJSON(w, documentForViewer(claims, document))
}

// respondWithOwnDecision перечитывает разбор и отдаёт его БЕЗ фильтра
// documentForViewer — для Approve и Reject, где действующий только что сам
// изменил состояние этого разбора.
//
// Фикс-раунд 2 задачи 6: фильтр из фикс-раунда 1 был рассчитан на Unpublish
// (сотрудник снимает ЧУЖОЙ черновик и не должен увидеть его текст), но,
// применённый и к Approve/Reject, гасил решение самого модератора — после
// одобрения или отказа документ уже не «на_рассмотрении», mayReadDraft для
// не-автора возвращает false, и documentForViewer стирал бы ReviewStatus,
// RejectReason, ModeratorID и ReviewedAt из ответа тому, кто их только что
// проставил.
//
// Это безопасно ровно потому, что право на действие и право на чтение здесь
// совпадают по построению, а не по случайности: documentReviewGuard (см.
// выше) пускает к Approve/Reject только разбор в состоянии «на_рассмотрении»
// — а это ТО ЖЕ САМОЕ состояние, в котором mayReadDraft разрешает модератору
// читать черновик целиком. Одобрить или отклонить можно только то, что
// модератору и так было дозволено прочесть ДО его действия — отдавать это же
// содержимое обратно в подтверждении нечего опасаться.
//
// Submit и Unpublish продолжают идти через отфильтрованный respondWith: там
// действующий (автор при отправке; сотрудник на чужом черновике при снятии)
// не обязательно имеет право видеть черновик целиком, и это разное решение
// — трогать его эта правка не должна.
func (h *DocumentReviewHandler) respondWithOwnDecision(w http.ResponseWriter, ctx context.Context, id int64) {
	document, err := h.documents.GetByID(ctx, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось перечитать разбор")
		return
	}
	writeJSON(w, document)
}
