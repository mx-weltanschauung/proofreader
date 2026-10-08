package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gorilla/mux"

	"proofreader/internal/middleware"
	"proofreader/internal/models"
	"proofreader/internal/repository"
)

const (
	// Тело в 768 КБ: 200 000 знаков кириллицы весят около 400 КБ в UTF-8, плюс
	// экранирование в JSON и обёртка запроса. Предел, равный символьному,
	// сделал бы символьную проверку недостижимой — читатель получал бы 413
	// вместо внятного 422.
	suggestionMaxBody = 768 << 10

	// Замер корпуса (27.08.2026, 45 495 полос): средняя 2 137 знаков,
	// 99.99-й процентиль 18 927, длиннее 64 К — ровно две полосы (166 625 и
	// 165 067 при третьей в 24 943). Абсолютный потолок обязан лишь не мешать
	// законной правке; работу против залива делает относительный.
	suggestionMaxChars     = 200000
	suggestionMaxNoteChars = 2000

	// Относительный потолок (4×основа+2000) получает пол. На пустой полосе
	// основа нулевая, и без пола формула схлопывается в 2000 знаков — меньше
	// средней полосы корпуса (2137). Читатель, взявшийся расшифровать пустую
	// полосу по скану — то есть восстановить отсутствующий текст, самый
	// ценный вклад, — упирался бы в отказ первым же обычным по длине текстом.
	// Замер рабочей базы (27.08.2026): 708 полос пусты целиком, ещё 2105
	// короче 500 знаков. Число пола не с потолка: 99.99-й процентиль корпуса
	// — 18 927 знаков; 20 000 берёт его с запасом.
	suggestionRelativeCapFloor = 20000

	suggestionRatePerHour = 5
)

// hasInvisibleRunes сообщает, есть ли в строке символы, которых на экране не
// видно: управляющие (категория Cc) и форматирующие (Cf) — переключатели
// направления письма, нулевая ширина, метка порядка байтов.
//
// Cc и Cf отбиваются по разным причинам, и обе настоящие.
//
// Cc — потому что нулевой байт в TEXT роняет вставку (см. место вызова).
//
// Cf — потому что дифф слов на экране модерации это единственный инструмент
// редактора, а невидимый символ делает его врущим: правка, отличающаяся от
// основы ТОЛЬКО невидимыми символами, показывает <ins>/<del> над визуально
// одинаковыми словами. Редактор видит «слово заменено на то же слово»,
// принимает — и текст с переключателем направления письма уезжает в EPUB,
// FB2 и предметный указатель. unicode.IsControl про Cf отвечает false, так
// что одной проверкой Cc такое не ловится.
//
// \n, \r и \t пропускаются: текст полосы многострочный.
//
// МЯГКИЙ ПЕРЕНОС (U+00AD) пропускается, хотя он тоже Cf, и это не поблажка,
// а замер: в корпусе на 27.08.2026 (45 495 полос) он встречается на 24
// полосах, тогда как нулевая ширина, все переключатели направления,
// соединители, метка порядка байтов и неразрывный пробел — на нуле полос
// каждый. Отбить остальное не стоит ни одной законной правки; отбить мягкий
// перенос — значит закрыть подачу правки для двух десятков живых полос.
func hasInvisibleRunes(s string) bool {
	for _, r := range s {
		if r == '\n' || r == '\r' || r == '\t' || r == '\u00ad' {
			continue
		}
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return true
		}
	}
	return false
}

// pageSHA256 — отпечаток текста полосы. По нему сверяется основа при подаче:
// хэш, а не метка updated_at, потому что нас интересует «текст, который я
// правил, всё ещё тот», а не «строку никто не трогал».
func pageSHA256(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// PageSuggestionHandler handles reader suggestion endpoints
type PageSuggestionHandler struct {
	suggestions PageSuggestionStore
	pages       PageStore
	versions    PageVersionStore
	fragments   FragmentStore
	cuts        DocumentCutStore
	jwtSecret   string
	trustProxy  bool
}

// NewPageSuggestionHandler creates a new suggestion handler
func NewPageSuggestionHandler(
	suggestions PageSuggestionStore,
	pages PageStore,
	versions PageVersionStore,
	fragments FragmentStore,
	cuts DocumentCutStore,
	jwtSecret string,
	trustProxy bool,
) *PageSuggestionHandler {
	return &PageSuggestionHandler{
		suggestions: suggestions,
		pages:       pages,
		versions:    versions,
		fragments:   fragments,
		cuts:        cuts,
		jwtSecret:   jwtSecret,
		trustProxy:  trustProxy,
	}
}

// CreateSuggestionRequest — тело подачи от вошедшего читателя.
type CreateSuggestionRequest struct {
	ProposedMarkdown string `json:"proposed_markdown"`
	Note             string `json:"note"`
	BaseSHA256       string `json:"base_sha256"`
	// BindingRef — ловушка. Имя нарочно вне словарей автозаполнения и не
	// совпадает ни с чем настоящим в этом API.
	BindingRef string `json:"binding_ref"`
}

// Create принимает предложение исправления полосы от вошедшего читателя.
func (h *PageSuggestionHandler) Create(w http.ResponseWriter, r *http.Request) {
	pageID, err := strconv.ParseInt(mux.Vars(r)["pageId"], 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Неверный идентификатор страницы")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, suggestionMaxBody)
	var req CreateSuggestionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "Правка слишком велика")
			return
		}
		writeError(w, http.StatusBadRequest, "Не удалось разобрать запрос")
		return
	}

	claims, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		// До сюда не дойти: маршрут стоит на подроутере reader. Проверка —
		// страховка на случай, если маршрут однажды переедет.
		writeError(w, http.StatusUnauthorized, "Нужно записаться в читальню")
		return
	}

	// Ловушка: поле уведено с экрана, человек его не видит. Отвечаем успехом —
	// код ошибки подсказал бы боту, что ловушка распознана, и следующая версия
	// её обойдёт.
	if strings.TrimSpace(req.BindingRef) != "" {
		writeJSONStatus(w, http.StatusCreated,
			map[string]any{"id": 0, "status": models.SuggestionNew})
		return
	}

	// Невидимые символы отбиваются ДО всякой работы с базой, и это не
	// вкусовщина. Нулевой байт в TEXT роняет вставку, то есть строка не
	// создаётся — а предел частоты считает именно строки. Значит запрос с
	// таким символом не идёт в счёт и повторяется бесконечно, каждый раз
	// стоя базе поиска страницы, подсчёта и упавшей транзакции. Тремя
	// символами получается неограниченная нагрузка в обход предела.
	//
	// Второй, не менее важный повод — врущий дифф на экране модерации; см.
	// комментарий у hasInvisibleRunes.
	if hasInvisibleRunes(req.ProposedMarkdown) || hasInvisibleRunes(req.Note) {
		writeError(w, http.StatusUnprocessableEntity, "В тексте есть недопустимые символы")
		return
	}

	ctx := context.Background()
	page, err := h.pages.GetByID(ctx, pageID)
	if err != nil {
		writeError(w, http.StatusNotFound, "Страница не найдена")
		return
	}

	if pageSHA256(page.ContentMarkdown) != req.BaseSHA256 {
		writeError(w, http.StatusConflict,
			"Полосу успели поправить, пока вы правили её у себя. Обновите основу — ваш текст не потерян.")
		return
	}

	if req.ProposedMarkdown == page.ContentMarkdown {
		writeError(w, http.StatusUnprocessableEntity, "Текст не изменился")
		return
	}

	// Длины — в символах: len() на кириллице вдвое больше и отрезал бы вдвое
	// раньше, чем обещано читателю.
	proposedLen := utf8.RuneCountInString(req.ProposedMarkdown)
	baseLen := utf8.RuneCountInString(page.ContentMarkdown)

	relativeCap := 4*baseLen + 2000
	if relativeCap < suggestionRelativeCapFloor {
		relativeCap = suggestionRelativeCapFloor
	}

	// Два разных отказа с двумя разными числами: читатель, упёршийся в
	// потолок, должен понять, что произошло, а не гадать по фразе без цифр.
	if proposedLen > suggestionMaxChars {
		writeError(w, http.StatusUnprocessableEntity,
			fmt.Sprintf("Правка слишком велика: не больше %d знаков", suggestionMaxChars))
		return
	}
	if proposedLen > relativeCap {
		writeError(w, http.StatusUnprocessableEntity,
			fmt.Sprintf("Правка слишком велика для этой полосы: не больше %d знаков", relativeCap))
		return
	}
	if utf8.RuneCountInString(req.Note) > suggestionMaxNoteChars {
		writeError(w, http.StatusUnprocessableEntity,
			fmt.Sprintf("Записка слишком длинная: не больше %d знаков", suggestionMaxNoteChars))
		return
	}

	ipHash := hashIP(clientIP(r, h.trustProxy), h.jwtSecret)

	s := &models.PageSuggestion{
		PageID: pageID,
		// Основу берём из текущего текста, а не из тела запроса: клиент написал
		// бы что угодно, и дифф на экране модерации стал бы враньём. Совпадение
		// с тем, что видел читатель, уже проверено по base_sha256 выше.
		BaseMarkdown:     page.ContentMarkdown,
		ProposedMarkdown: req.ProposedMarkdown,
		Note:             strings.TrimSpace(req.Note),
		UserID:           &claims.UserID,
		IPHash:           ipHash,
	}

	// Предел частоты и запись — одно действие под блокировкой в базе. Раньше
	// здесь стояли счёт и вставка порознь, и залп параллельных подач
	// проскакивал мимо предела целиком: замер на одноразовой базе — из
	// двадцати одновременных подач при пределе пять прошли восемнадцать.
	// Вместе с ловушкой это вся защита маршрута от залива.
	accepted, err := h.suggestions.CreateWithinLimit(ctx, s,
		suggestionRatePerHour, time.Now().Add(-time.Hour))
	if err != nil {
		log.Printf("create suggestion for page %d: %v", pageID, err)
		writeError(w, http.StatusInternalServerError, "Не удалось принять правку")
		return
	}
	if !accepted {
		writeError(w, http.StatusTooManyRequests,
			"Слишком много предложений за час. Попробуйте позже.")
		return
	}

	writeJSONStatus(w, http.StatusCreated,
		map[string]any{"id": s.ID, "status": s.Status})
}

// Mine отдаёт правки вошедшего читателя.
//
// Автор берётся из claims, а не из заголовка: маршрут стоит на подроутере
// reader, и личность читателя устанавливает вход, а не предъявительский
// секрет.
func (h *PageSuggestionHandler) Mine(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		// До сюда не дойти: та же страховка, что и в Create.
		writeError(w, http.StatusUnauthorized, "Нужно записаться в читальню")
		return
	}

	rows, err := h.suggestions.ListByUserID(context.Background(), claims.UserID)
	if err != nil {
		log.Printf("list reader suggestions: %v", err)
		writeError(w, http.StatusInternalServerError, "Не удалось получить список правок")
		return
	}
	// Известный дефект платформы: списочные маршруты по умолчанию отдают
	// null вместо [] на пустом срезе. Инициализируем явно, а не полагаемся
	// на то, что вернул стор.
	if rows == nil {
		rows = []repository.SuggestionRow{}
	}

	writeJSONStatus(w, http.StatusOK, rows)
}

// List отдаёт очередь модерации. Счётчик новых для значка в шапке берётся
// отсюда же с limit=0 — отдельного маршрута под него не нужно.
func (h *PageSuggestionHandler) List(w http.ResponseWriter, r *http.Request) {
	var status *models.PageSuggestionStatus
	if raw := r.URL.Query().Get("status"); raw != "" {
		s := models.PageSuggestionStatus(raw)
		switch s {
		case models.SuggestionNew, models.SuggestionAccepted, models.SuggestionRejected:
			status = &s
		default:
			writeError(w, http.StatusBadRequest, "Неизвестный статус")
			return
		}
	}

	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 || n > 200 {
			writeError(w, http.StatusBadRequest, "Неверное значение limit")
			return
		}
		limit = n
	}
	offset := 0
	if raw := r.URL.Query().Get("offset"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "Неверное значение offset")
			return
		}
		offset = n
	}

	rows, total, err := h.suggestions.List(context.Background(), status, limit, offset)
	if err != nil {
		log.Printf("list suggestions: %v", err)
		writeError(w, http.StatusInternalServerError, "Не удалось получить очередь")
		return
	}

	writeJSONStatus(w, http.StatusOK, map[string]any{"items": rows, "total": total})
}

// Get отдаёт одно предложение со всеми тремя текстами: основой, предложенным
// и текущим текстом полосы. Без третьего экран модерации не смог бы показать,
// что изменилось, пока предложение ждало.
func (h *PageSuggestionHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Неверный идентификатор предложения")
		return
	}

	d, err := h.suggestions.GetDetail(context.Background(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "Предложение не найдено")
		return
	}

	writeJSONStatus(w, http.StatusOK, d)
}

// AcceptSuggestionRequest — итоговый текст редактора.
//
// Поля status здесь нет намеренно: принятие не трогает статус полосы.
type AcceptSuggestionRequest struct {
	ContentMarkdown string `json:"content_markdown"`
	Comment         string `json:"comment"`
}

// RejectSuggestionRequest — причина отказа из закрытого списка.
type RejectSuggestionRequest struct {
	Reason models.RejectReason `json:"reason"`
}

// Accept применяет итоговый текст редактора и помечает предложение принятым.
//
// Принимается текст редактора, а не флаг «применить предложенное»: итог всегда
// проходит через его руки, и «применить как есть» — просто случай, когда он
// ничего не менял.
func (h *PageSuggestionHandler) Accept(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Неверный идентификатор предложения")
		return
	}

	claims, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "Требуется вход")
		return
	}

	var req AcceptSuggestionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Не удалось разобрать запрос")
		return
	}
	if strings.TrimSpace(req.ContentMarkdown) == "" {
		writeError(w, http.StatusUnprocessableEntity, "Итоговый текст пуст")
		return
	}

	ctx := context.Background()
	d, err := h.suggestions.GetDetail(ctx, id)
	if err != nil {
		writeError(w, http.StatusNotFound, "Предложение не найдено")
		return
	}

	page, err := h.pages.GetByID(ctx, d.PageID)
	if err != nil {
		writeError(w, http.StatusNotFound, "Страница не найдена")
		return
	}

	// Отметку ставим первой: она отказывает на уже разобранном предложении, и
	// отказать надо до того, как текст полосы изменён.
	if err := h.suggestions.Resolve(ctx, id, models.SuggestionAccepted, nil, claims.UserID); err != nil {
		if errors.Is(err, repository.ErrSuggestionResolved) {
			writeError(w, http.StatusConflict, "Предложение уже разобрано")
			return
		}
		log.Printf("resolve suggestion %d: %v", id, err)
		writeError(w, http.StatusInternalServerError, "Не удалось применить правку")
		return
	}

	// Статус полосы не трогаем: page.Status передаётся как есть.
	if err := applyPageEdit(ctx, h.pages, h.versions, h.fragments, h.cuts,
		page, req.ContentMarkdown, page.Status, claims.UserID, req.Comment); err != nil {
		log.Printf("apply suggestion %d to page %d: %v", id, d.PageID, err)
		writeError(w, http.StatusInternalServerError, "Не удалось применить правку")
		return
	}

	writeJSONStatus(w, http.StatusOK, map[string]any{
		"id": id, "status": models.SuggestionAccepted,
	})
}

// Reject помечает предложение отклонённым с причиной из закрытого списка.
func (h *PageSuggestionHandler) Reject(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Неверный идентификатор предложения")
		return
	}

	claims, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "Требуется вход")
		return
	}

	var req RejectSuggestionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Не удалось разобрать запрос")
		return
	}
	// Список закрыт: ответить редактору читатель не может, и свободная причина
	// превратила бы модерацию в переписку в один конец.
	if !models.ValidRejectReason(req.Reason) {
		writeError(w, http.StatusUnprocessableEntity, "Неизвестная причина отказа")
		return
	}

	reason := req.Reason
	err = h.suggestions.Resolve(context.Background(), id,
		models.SuggestionRejected, &reason, claims.UserID)
	if errors.Is(err, repository.ErrSuggestionResolved) {
		writeError(w, http.StatusConflict, "Предложение уже разобрано")
		return
	}
	if err != nil {
		log.Printf("reject suggestion %d: %v", id, err)
		writeError(w, http.StatusInternalServerError, "Не удалось отклонить предложение")
		return
	}

	writeJSONStatus(w, http.StatusOK, map[string]any{
		"id": id, "status": models.SuggestionRejected, "reject_reason": reason,
	})
}

// Delete сносит одно отклонённое предложение.
//
// Удаление физическое: строка исчезает и из «Моих предложений» читателя.
// Для того, что удаляют (мусор и спам), это и есть цель; принятое и
// неразобранное сюда не пускает условие по статусу в самом DELETE.
func (h *PageSuggestionHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Неверный идентификатор предложения")
		return
	}

	err = h.suggestions.Delete(r.Context(), id)
	if errors.Is(err, repository.ErrSuggestionNotDeletable) {
		writeError(w, http.StatusConflict, "Удалить можно только отклонённое предложение")
		return
	}
	if err != nil {
		log.Printf("delete suggestion %d: %v", id, err)
		writeError(w, http.StatusInternalServerError, "Не удалось удалить предложение")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// DeleteRejected сносит все отклонённые предложения разом.
func (h *PageSuggestionHandler) DeleteRejected(w http.ResponseWriter, r *http.Request) {
	n, err := h.suggestions.DeleteRejected(r.Context())
	if err != nil {
		log.Printf("purge rejected suggestions: %v", err)
		writeError(w, http.StatusInternalServerError, "Не удалось удалить отклонённые предложения")
		return
	}

	writeJSONStatus(w, http.StatusOK, map[string]any{"deleted": n})
}
