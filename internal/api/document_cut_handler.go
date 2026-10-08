package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gorilla/mux"

	"proofreader/internal/middleware"
	"proofreader/internal/models"
	"proofreader/pkg/markdown"
)

// DocumentCutHandler обслуживает вклейки разбора: догрузку целиком
// («Развернуть здесь» у подрезанного блока, задача 8), заведение и снятие
// (задача 9). Отдельный маршрут для догрузки, а не второй параметр у View:
// страница разбора уже отрисована, догрузка вставляет свой кусок в готовый
// DOM, а не перезапрашивает разбор целиком.
type DocumentCutHandler struct {
	documents DocumentStore
	cuts      DocumentCutStore
	works     WorkStore
	pages     PageStore
	renderer  *markdown.Renderer
}

func NewDocumentCutHandler(documents DocumentStore, cuts DocumentCutStore, works WorkStore, pages PageStore, renderer *markdown.Renderer) *DocumentCutHandler {
	return &DocumentCutHandler{documents: documents, cuts: cuts, works: works, pages: pages, renderer: renderer}
}

// createCutRequest — тело POST /documents/{id}/cuts. Полосы адресуются
// НОМЕРАМИ, не id: pages.id наружу не ходит (решение 15), а
// GetByWorkAndPageNumber уже есть в PageStore.
type createCutRequest struct {
	WorkID      int64  `json:"work_id"`
	StartPage   int    `json:"start_page"`
	StartOffset int    `json:"start_offset"`
	EndPage     int    `json:"end_page"`
	EndOffset   int    `json:"end_offset"`
	SourceTitle string `json:"source_title"`
}

// sourceTitleMaxRunes — потолок подписи источника. Значение с потолка не
// снималось: 512 знаков — это заведомо больше любой честной библиографической
// ссылки («Ленин. Полн. собр. соч., 5-е изд., т. 6, с. 233—235.»), но
// достаточно, чтобы не пустить в базу абзац по ошибке.
const sourceTitleMaxRunes = 512

// storageNotFound отличает «сущности нет» от сбоя хранилища. Третьего
// способа не заводим: тот же приём (репозитории проекта не заводят
// sentinel-ошибок, на отсутствие строки текст кончается суффиксом
// "not found", на прочих бедах — иначе) уже применён дважды в дереве —
// collectionNotFound в seo_adapter.go и isNotFound в
// internal/seo/source.go — там же и причина, почему это не общая функция:
// internal/seo не может импортировать internal/api (цикл через router.go), а
// делить приватную функцию между пакетами Go не умеет вовсе.
func storageNotFound(err error) bool {
	return err != nil && strings.HasSuffix(err.Error(), "not found")
}

// Create заводит новую вклейку: тело называет полосы и границы, сервер сам
// снимает якорные цитаты и хэши с ТЕКУЩЕГО текста полос — клиенту нельзя
// доверить то, чем вклейка держится за текст (решение контроллера).
// source_title приходит с клиента: его уже собирает
// frontend/src/utils/citation.ts для кнопки «Цитировать», второй сборщик в Go
// был бы третьей парой близнецов в проекте, где одна уже признана дорогой.
func (h *DocumentCutHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createCutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Некорректное тело запроса")
		return
	}

	// Разбор обязан существовать: вклейка без хозяина — противоречие в
	// адресе. loadDocumentByKey (document_key.go) уже разводит честное
	// отсутствие (404) и сбой хранилища (500) — тот же довод, что раньше был
	// расписан здесь построчно для GetByID.
	document, ok := loadDocumentByKey(w, r, h.documents)
	if !ok {
		return
	}
	ctx := context.Background()

	// Вклейка заводится только в своём разборе — те же права, что у правки
	// разбора (mayEditDocument, задача 3): читатель, подавший разбор в
	// модерацию или ещё правящий черновик, решает, что в нём цитируется, а
	// не кто угодно вошедший. Маршрут — на подроутере reader именно поэтому
	// с задачи 5, и до этой правки не проверял ничего.
	claims, _ := middleware.GetUserFromContext(r.Context())
	if !mayEditDocument(claims, document) {
		writeError(w, http.StatusForbidden, "Вклейка заводится только в своём разборе")
		return
	}

	sourceTitle := strings.TrimSpace(req.SourceTitle)
	if sourceTitle == "" {
		writeError(w, http.StatusBadRequest, "Подпись источника не может быть пустой")
		return
	}
	if utf8.RuneCountInString(sourceTitle) > sourceTitleMaxRunes {
		writeError(w, http.StatusBadRequest, "Подпись источника длиннее 512 знаков")
		return
	}

	if req.EndPage < req.StartPage {
		writeError(w, http.StatusBadRequest, "Граница вклейки вне полосы")
		return
	}

	// То же разведение, что и у GetByID разбора выше: полосы могут честно не
	// существовать (неверный номер с клиента — 400), а могут быть недоступны
	// из-за сбоя хранилища (500) — эти два исхода не должен путать клиент,
	// который на 400 показывает автору «такой полосы нет», а не «попробуйте
	// ещё раз».
	startPage, err := h.pages.GetByWorkAndPageNumber(ctx, req.WorkID, req.StartPage)
	if err != nil {
		if storageNotFound(err) {
			writeError(w, http.StatusBadRequest, "Начальная полоса вклейки не найдена")
		} else {
			writeError(w, http.StatusInternalServerError, "Не удалось прочитать начальную полосу вклейки")
		}
		return
	}
	endPage := startPage
	if req.EndPage != req.StartPage {
		endPage, err = h.pages.GetByWorkAndPageNumber(ctx, req.WorkID, req.EndPage)
		if err != nil {
			if storageNotFound(err) {
				writeError(w, http.StatusBadRequest, "Конечная полоса вклейки не найдена")
			} else {
				writeError(w, http.StatusInternalServerError, "Не удалось прочитать конечную полосу вклейки")
			}
			return
		}
	}

	// Границы — против ТЕКУЩЕЙ длины текста полосы, байтовые смещения, как и
	// везде у Anchor (cut_anchor.go). Тот же набор проверок, что buildFragment
	// (concept_stream.go) делает для вырезки понятия — общий приём переякоривания.
	if req.StartOffset < 0 || req.StartOffset > len(startPage.ContentMarkdown) ||
		req.EndOffset < 0 || req.EndOffset > len(endPage.ContentMarkdown) {
		writeError(w, http.StatusBadRequest, "Граница вклейки вне полосы")
		return
	}
	if req.StartPage == req.EndPage && req.EndOffset < req.StartOffset {
		writeError(w, http.StatusBadRequest, "Граница вклейки вне полосы")
		return
	}
	if !offsetOnRuneBoundary(startPage.ContentMarkdown, req.StartOffset) ||
		!offsetOnRuneBoundary(endPage.ContentMarkdown, req.EndOffset) {
		writeError(w, http.StatusBadRequest, "Граница вклейки должна приходиться на границу символа")
		return
	}

	headEnd := len(startPage.ContentMarkdown)
	tailStart := 0
	if req.StartPage == req.EndPage {
		headEnd = req.EndOffset
		tailStart = req.StartOffset
	}

	workID := req.WorkID
	cut := &models.DocumentCut{
		DocumentID: document.ID,
		WorkID:     &workID,
		Anchor: models.Anchor{
			StartPageID: startPage.ID,
			StartOffset: req.StartOffset,
			EndPageID:   endPage.ID,
			EndOffset:   req.EndOffset,
			HeadQuote:   quoteOf(startPage.ContentMarkdown, req.StartOffset, headEnd, true),
			TailQuote:   quoteOf(endPage.ContentMarkdown, tailStart, req.EndOffset, false),
			StartHash:   pageHash(startPage.ContentMarkdown),
			EndHash:     pageHash(endPage.ContentMarkdown),
		},
		Status:      models.CutStatusOK,
		SourceTitle: sourceTitle,
	}

	if err := h.cuts.Create(ctx, cut); err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось сохранить вклейку")
		return
	}

	writeJSONStatus(w, http.StatusCreated, cut)
}

// Delete снимает одну вклейку. Тег <cut id="N"> в теле разбора при этом не
// трогается — если автор забудет вынести его сам, следующее сохранение
// отвергнет тело («Разбор ссылается на вклейку, которой нет», проверка
// DocumentHandler.Update/Create) вместо того, чтобы молча собрать разбор с
// дырой на его месте.
//
// Единичного Delete(id) у DocumentCutStore нет (Task 2, интерфейс фиксирован
// этой веткой) — снятие одной вклейки идёт тем же приёмом, что и сборка
// мусора при сохранении: список ВСЕХ вклеек разбора без снимаемой отдаётся
// DeleteUnreferenced как keep, и она удаляет ровно одну лишнюю строку.
func (h *DocumentCutHandler) Delete(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	cutID, err := strconv.ParseInt(vars["cutId"], 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Некорректный идентификатор вклейки")
		return
	}

	// Тот же приём и то же разведение, что и в Create: разбор обязан быть
	// прочитан и проверен на авторство ДО снятия вклейки — до этой правки
	// Delete вообще не читал разбор, и посторонний читатель мог снести
	// вклейку из чужого разбора, зная только его id и id вклейки.
	document, ok := loadDocumentByKey(w, r, h.documents)
	if !ok {
		return
	}
	ctx := context.Background()
	claims, _ := middleware.GetUserFromContext(r.Context())
	if !mayEditDocument(claims, document) {
		writeError(w, http.StatusForbidden, "Вклейка снимается только в своём разборе")
		return
	}

	// ByIDs сверяет document_id сама — вклейка чужого разбора тут не
	// найдётся так же, как неизвестная, и получит тот же честный отказ.
	existing, err := h.cuts.ByIDs(ctx, document.ID, []int64{cutID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось проверить вклейку")
		return
	}
	if len(existing) == 0 {
		writeError(w, http.StatusNotFound, "Вклейка не найдена")
		return
	}

	all, err := h.cuts.ByDocument(ctx, document.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось прочитать вклейки разбора")
		return
	}
	keep := make([]int64, 0, len(all))
	for _, c := range all {
		if c.ID != cutID {
			keep = append(keep, c.ID)
		}
	}
	if err := h.cuts.DeleteUnreferenced(ctx, document.ID, keep); err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось снять вклейку")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Full отдаёт вклейку целиком, без подрезки: сюда приходит «развернуть
// здесь». Публичный, как и чтение разбора — сама вклейка тоже читается без
// авторизации через View.
//
// 410, не 404: вклейка чужого разбора или уже отвязавшаяся/снятая — состояние
// адреса, а не опечатка в нём. nginx фронта перехватывает 404 и подменяет его
// оболочкой SPA кодом 200 (@spa) — крышка «этого больше нет» до клиента не
// доехала бы вовсе.
func (h *DocumentCutHandler) Full(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	cutID, err := strconv.ParseInt(vars["cutId"], 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Некорректный идентификатор вклейки")
		return
	}

	ctx := context.Background()

	// Разбор читается и проверяется на видимость ПЕРВЫМ шагом — тем же
	// правилом, что и сам разбор (documentVisibleTo, задача 3): иначе
	// снятый/непубликованный разбор остаётся читаемым по кускам через этот
	// маршрут, даже когда View на него уже отвечает 404/410 (тот же класс
	// дыры, что уже был у подборок — ItemPages/download отдавали 200 после
	// 410 на Get).
	//
	// Full НЕ зовёт общий loadDocumentByKey: у него другой словарь ответов —
	// отсутствие разбора здесь обязано остаться 410 («Вклейка снята»), а не
	// стать 404, который отдаёт loadDocumentByKey. Причина в nginx фронта: он
	// перехватывает 404 и подменяет его оболочкой SPA кодом 200 (@spa), и
	// крышка «этого больше нет» до клиента не доехала бы вовсе. Поэтому адрес
	// разбирается напрямую — documentKey + GetByAuthorSlug, — а не через
	// помощник, который выбрал бы за нас неверный код.
	nickname, slug := documentKey(r)
	document, err := h.documents.GetByAuthorSlug(ctx, nickname, slug)
	if err != nil {
		if storageNotFound(err) {
			writeError(w, http.StatusGone, "Вклейка снята")
		} else {
			writeError(w, http.StatusInternalServerError, "Не удалось прочитать разбор")
		}
		return
	}
	claims, _ := middleware.GetUserFromContext(r.Context())
	if _, _, ok := documentVisibleTo(claims, document); !ok {
		// Один код на оба исхода (никогда не публиковался vs снят): сам факт
		// существования вклейки в чужом черновике наружу не выдаётся.
		writeError(w, http.StatusGone, "Вклейка снята")
		return
	}

	// ByIDs сверяет document_id сама (WHERE document_id = $1 AND id = ANY($2)):
	// вклейка чужого разбора не найдётся здесь так же, как неизвестная.
	//
	// Сбой хранилища и честное отсутствие — разные ответы: 410 обязан
	// означать «вклейки больше нет», а не «база сейчас недоступна» — клиент,
	// который на 410 прячет кнопку «развернуть» и перестаёт повторять запрос
	// (см. фронтовую половину этой задачи), закрыл бы её из-за временной
	// неполадки до перезагрузки страницы. Тот же довод и то же разведение
	// уже в DocumentHandler.View (document_handler.go) — 500 через writeError
	// на ошибку хранилища, отдельно от прикладного «не найдено».
	cuts, err := h.cuts.ByIDs(ctx, document.ID, []int64{cutID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось прочитать вклейку")
		return
	}
	if len(cuts) == 0 {
		writeError(w, http.StatusGone, "Вклейка снята")
		return
	}
	cut := cuts[0]

	// Отдаём только вклейку, НАЗВАННУЮ той редакцией, которую видит
	// пришедший. Принадлежности разбору тут мало: у разбора две редакции
	// разом, и вклейка, на которую ссылается только неодобренный черновик,
	// доставалась бы постороннему по перебираемому номеру — он узнал бы, что
	// цитирует ждущая решения правка, а подпись источника (свободный текст
	// автора до 512 знаков) стала бы публично читаемой мимо модерации вовсе.
	//
	// Тело для этого уже под рукой: им же считается порядковый номер вклейки
	// — та самая область имён сносок, которой обязан совпасть догруженный
	// блок с соседями при полной сборке (assembleDocument).
	//
	// Отказ — тем же кодом и теми же словами, что прочие отказы маршрута: сам
	// факт существования вклейки в чужом черновике наружу не выдаётся.
	ordinal, named := cutOrdinal(documentForViewer(claims, document).MarkdownContent, cut.ID)
	if !named {
		writeError(w, http.StatusGone, "Вклейка снята")
		return
	}

	// Тот же guard, что renderCutState ставит перед renderCut: сама renderCut
	// (задача 6) беззащитна намеренно и режет по смещениям как есть. У
	// отвязавшейся (stale) вклейки полосы ещё известны (Broken() тут не
	// сработает), но смещения уже не совпадают с текущим текстом полосы —
	// показать её без этой проверки значит вырезать чужой кусок молча.
	if cut.Broken() || cut.Status == models.CutStatusStale {
		writeError(w, http.StatusGone, "Вклейка снята")
		return
	}

	// То же разведение, что и у ByIDs выше: сбой хранилища — 500, а честно
	// пустой диапазон полос (полосы снесены, но сама вклейка ещё жива) — 410.
	pages, err := h.cuts.PagesOfCut(ctx, cut)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось прочитать полосы вклейки")
		return
	}
	if len(pages) == 0 {
		writeError(w, http.StatusGone, "Вклейка снята")
		return
	}

	// Смещение печатной колонцифры тома — тот же вызов, что делает View для
	// каждого затронутого тома; работа, которую не удалось прочитать, остаётся
	// с offset 0 (folioLabel в этом случае не хуже, чем у View).
	offset := 0
	if work, err := h.works.GetByID(ctx, *cut.WorkID); err == nil && work != nil {
		offset = work.PageOffset
	}

	// ordinal посчитан выше, вместе с проверкой «названа ли вклейка видимой
	// редакцией»: посторонний читатель видит опубликованную редакцию, и
	// нумерация вклеек в ней может отличаться от черновика.
	writeJSONStatus(w, http.StatusOK, map[string]string{
		"html": renderCut(h.renderer, cut, pages, ordinal, offset),
	})
}
