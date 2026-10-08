package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"proofreader/internal/middleware"
	"proofreader/internal/models"
	"proofreader/pkg/markdown"
)

type DocumentHandler struct {
	documentRepo DocumentStore
	renderer     *markdown.Renderer
	cuts         DocumentCutStore
	works        WorkStore
	// cache — краулерская половина ServingCache (см. ServingCache.DropCrawler):
	// удаление разбора — тот же случай, что снятие с публикации, только без
	// возврата назад. nil означает «кэшей нет», как и у DocumentReviewHandler.
	cache *ServingCache
}

// pageRepo раньше здесь был нужен асинхронному рендеру документа (снесён
// вместе с очередью на asynq/Redis 14.09.2026 — см. CLAUDE.md, «Разбор
// рендерится при чтении»): View собирает HTML прямо в обработчике, читая
// полосы через h.cuts.PagesOfCut, отдельный доступ к PageStore здесь не
// нужен.
func NewDocumentHandler(
	documentRepo DocumentStore,
	renderer *markdown.Renderer,
	cuts DocumentCutStore,
	works WorkStore,
	cache *ServingCache,
) *DocumentHandler {
	return &DocumentHandler{
		documentRepo: documentRepo,
		renderer:     renderer,
		cuts:         cuts,
		works:        works,
		cache:        cache,
	}
}

// documentTitleMaxRunes — потолок заглавия. Число не с потолка: колонка
// documents.title объявлена varchar(500), и Postgres считает её в ЗНАКАХ, а
// не байтах. Без этой проверки 501-й знак ронял вставку ошибкой хранилища, и
// автор получал 500 «Не удалось создать разбор» вместо внятного отказа;
// пустое заглавие проходило вовсе — клиент его не пускает, но клиент не
// единственный вход.
const documentTitleMaxRunes = 500

// validateDocumentTitle отдаёт заглавие в том виде, в каком его пишут в базу
// (без обрамляющих пробелов), и пустое сообщение, если оно годно. Тримминг
// здесь тот же и по той же причине, что у подписи источника вклейки и у ника:
// «   » — не заглавие, а невидимая строка, которую потом никто не найдёт.
func validateDocumentTitle(raw string) (title, message string) {
	title = strings.TrimSpace(raw)
	if title == "" {
		return "", "Нужно заглавие разбора"
	}
	if utf8.RuneCountInString(title) > documentTitleMaxRunes {
		return "", fmt.Sprintf("Заглавие длиннее %d знаков", documentTitleMaxRunes)
	}
	return title, ""
}

type CreateDocumentRequest struct {
	Title           string `json:"title"`
	MarkdownContent string `json:"markdown_content"`
}

type UpdateDocumentRequest struct {
	Title           string `json:"title"`
	MarkdownContent string `json:"markdown_content"`
}

// List — публичная витрина разборов. Отдаёт только то, что на людях: черновик
// и снятое сюда не попадают вовсе через отдельный метод репозитория
// (ListPublished), а не фильтром над общим List — иначе первая же забытая
// проверка в обработчике превращала бы список в утечку черновиков.
func (h *DocumentHandler) List(w http.ResponseWriter, r *http.Request) {
	limitStr := r.URL.Query().Get("limit")
	offsetStr := r.URL.Query().Get("offset")

	limit := 50
	offset := 0

	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil {
			limit = l
		}
	}

	if offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil {
			offset = o
		}
	}

	ctx := context.Background()
	documents, err := h.documentRepo.ListPublished(ctx, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось получить список разборов")
		return
	}

	if documents == nil {
		documents = []*models.Document{}
	}

	// Даже у опубликованного разбора черновик может ждать одобрения — список
	// обязан печатать одобренную редакцию, как и карточка. claims здесь nil
	// намеренно: витрина — публичный список, и documentForViewer(nil, …) для
	// уже отфильтрованных ListPublished строк ведёт себя так же, как для
	// анонима, — свою правку в списке автор смотрит через ListByOwner
	// («моё»), не здесь.
	shown := make([]*models.Document, len(documents))
	for i, d := range documents {
		shown[i] = documentForViewer(nil, d)
	}

	writeJSON(w, shown)
}

// Get отдаёт карточку разбора: постороннему — одобренную редакцию (или 404 /
// 410, если её вовсе не за что показывать), автору и модератору поданного —
// как есть.
func (h *DocumentHandler) Get(w http.ResponseWriter, r *http.Request) {
	document, ok := loadDocumentByKey(w, r, h.documentRepo)
	if !ok {
		return
	}

	claims, _ := middleware.GetUserFromContext(r.Context())
	if status, message, ok := documentVisibleTo(claims, document); !ok {
		writeError(w, status, message)
		return
	}

	writeJSON(w, documentForViewer(claims, document))
}

func (h *DocumentHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateDocumentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Некорректное тело запроса")
		return
	}

	claims, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "Требуется вход")
		return
	}

	title, message := validateDocumentTitle(req.Title)
	if message != "" {
		writeError(w, http.StatusBadRequest, message)
		return
	}

	ctx := context.Background()

	// Новый разбор не может ссылаться на вклейку: вклейка заводится ПОСЛЕ
	// разбора (POST /documents/{id}/cuts берёт id разбора из адреса), значит
	// ни одна вклейка ещё не числится за ним. newDocumentCutSentinel (0,
	// реальные id начинаются с 1) даёт ByIDs заведомо пустой результат —
	// та же проверка, что и на сохранении, честно объяснит отказ, а не
	// пропустит вперёд битое тело, которое Update потом всё равно отвергнет.
	const newDocumentCutSentinel = 0
	if _, message, status := validateCutPlaceholders(ctx, h.cuts, newDocumentCutSentinel, req.MarkdownContent); message != "" {
		writeError(w, status, message)
		return
	}

	ownerID := claims.UserID
	document := &models.Document{
		Title:           title,
		MarkdownContent: req.MarkdownContent,
		Slug:            documentSlugBase(title),
		OwnerID:         &ownerID,
		// Снимок подписи: у читателя это его ник, у сотрудника пусто
		// (claims.Nickname пуст не у читателя). Им mayEditDocument отличает
		// читательский разбор от сотруднического, и он же переживает
		// удаление учётной записи.
		AuthorNickname: claims.Nickname,
	}

	if err := h.documentRepo.Create(ctx, document); err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось создать разбор")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(document)
}

func (h *DocumentHandler) Update(w http.ResponseWriter, r *http.Request) {
	var req UpdateDocumentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Некорректное тело запроса")
		return
	}

	document, ok := loadDocumentByKey(w, r, h.documentRepo)
	if !ok {
		return
	}
	ctx := context.Background()

	claims, _ := middleware.GetUserFromContext(r.Context())
	if !mayEditDocument(claims, document) {
		writeError(w, http.StatusForbidden, "Правка чужого разбора недоступна")
		return
	}

	// Проверки ДО присвоения полей: ни заглавие, которого база не примет, ни
	// тело, которое рендер не соберёт, не должны тронуть документ, даже если
	// запись потом не дойдёт до репозитория.
	title, titleMessage := validateDocumentTitle(req.Title)
	if titleMessage != "" {
		writeError(w, http.StatusBadRequest, titleMessage)
		return
	}

	ids, message, status := validateCutPlaceholders(ctx, h.cuts, document.ID, req.MarkdownContent)
	if message != "" {
		writeError(w, status, message)
		return
	}

	document.Title = title
	document.MarkdownContent = req.MarkdownContent

	if err := h.documentRepo.Update(ctx, document); err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось сохранить разбор")
		return
	}

	// Сборка мусора — тем же списком id, что прошёл проверку выше (а не
	// пересчитанным заново из уже сохранённого тела: иначе проверка и уборка
	// разойдутся при первой же последующей правке одной из них), ПЛЮС всё,
	// что называет ещё не тронутая одобренная редакция: document на этом
	// шаге прочитан из базы и несёт её старый PublishedMarkdown.
	if err := h.cuts.DeleteUnreferenced(ctx, document.ID, documentCutKeepSet(document, ids)); err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось убрать неиспользуемые вклейки")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(document)
}

// uniqueIDs убирает повторы, сохраняя порядок первого появления: тело может
// называть одну вклейку несколько раз (тег скопирован по ошибке), а
// ByIDs/DeleteUnreferenced ждут список БЕЗ повторов.
func uniqueIDs(ids []int64) []int64 {
	seen := make(map[int64]bool, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// documentCutKeepSet — id вклеек, которые обязаны пережить сохранение
// черновика: те, что называет новое тело, ПЛЮС те, что называет одобренная
// редакция.
//
// Без второй половины автор, убравший тег из черновика, молча ломает то, что
// стоит на людях: строка document_cuts исчезает, и опубликованный разбор
// печатает «Вклейка не найдена» на месте куска корпуса. Сохранение при этом
// проходит успешно — дефект виден только читателю.
func documentCutKeepSet(d *models.Document, draftIDs []int64) []int64 {
	return uniqueIDs(append(append([]int64{}, draftIDs...),
		cutPlaceholderIDs(d.PublishedMarkdown)...))
}

// validateCutPlaceholders проверяет тело разбора перед сохранением и
// отклоняет ДВА разных случая разными сообщениями — автор обязан понять
// причину, а не гадать:
//   - тег стоит не отдельным абзацем (cutPlaceholderBlocks и cutAnyTagRe
//     насчитывают разное число совпадений на одном теле — расхождение и есть
//     признак; рендер такой тег проведёт как встроенный HTML и порвёт
//     вёрстку, а это не лечится на чтении, только отказом здесь). Абзац
//     задаёт пустая строка вокруг тега, а не просто перевод строки — тег на
//     своей строке, но без пустой строки до или после, блоком не считается;
//   - тег называет вклейку, которой нет или которая принадлежит другому
//     разбору (ByIDs сверяет document_id сама).
//
// message == "" значит «проверка пройдена»; иначе status — уже готовый код
// ответа (400 для обеих прикладных причин, 500 для сбоя хранилища). ids —
// уникальные id вклеек тела, прошедшие проверку; сборка мусора обязана
// получить ИМЕННО их, а не пересчитывать заново.
func validateCutPlaceholders(ctx context.Context, cuts DocumentCutStore, documentID int64, body string) (ids []int64, message string, status int) {
	blockCount := len(cutPlaceholderBlocks(body))
	anyCount := len(cutAnyTagRe.FindAllString(body, -1))
	if blockCount != anyCount {
		return nil, "Тег вклейки <cut id=\"…\"> должен стоять отдельной строкой, без другого текста рядом", http.StatusBadRequest
	}

	ids = uniqueIDs(cutPlaceholderIDs(body))
	known, err := cuts.ByIDs(ctx, documentID, ids)
	if err != nil {
		return nil, "Не удалось проверить вклейки разбора", http.StatusInternalServerError
	}
	// ByIDs сверяет document_id, поэтому чужая вклейка сюда не попадёт и счёт
	// не сойдётся — это та же проверка, что и на несуществующую.
	if len(known) != len(ids) {
		return nil, "Разбор ссылается на вклейку, которой нет", http.StatusBadRequest
	}
	return ids, "", 0
}

// Delete снимает разбор. Маршрут сегодня стоит только за сотрудническим
// подроутером (переезд на reader — задача 5), но проверка прав нужна уже
// сейчас: mayEditDocument различает по нику-снимку, а не по владельцу, и без
// неё чужой читательский разбор снимался бы редактором в обход рычага
// снятия с публикации.
func (h *DocumentHandler) Delete(w http.ResponseWriter, r *http.Request) {
	document, ok := loadDocumentByKey(w, r, h.documentRepo)
	if !ok {
		return
	}
	ctx := context.Background()

	claims, _ := middleware.GetUserFromContext(r.Context())
	if !mayEditDocument(claims, document) {
		writeError(w, http.StatusForbidden, "Удаление чужого разбора недоступно")
		return
	}

	if err := h.documentRepo.Delete(ctx, document.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось удалить разбор")
		return
	}

	// Задача 10: удаление отдаётся краулеру тот же час, что и снятие с
	// публикации (тот же кэш, тот же довод — см.
	// DocumentReviewHandler.Unpublish и ServingCache.DropCrawler).
	h.cache.DropCrawler()

	w.WriteHeader(http.StatusNoContent)
}

// View собирает HTML разбора для чтения. Постороннему — из ОДОБРЕННОЙ
// редакции (shown), а не из document.MarkdownContent: правка, ждущая
// одобрения, не должна долетать до чтения ни через рендер, ни через любое
// другое поле ответа.
func (h *DocumentHandler) View(w http.ResponseWriter, r *http.Request) {
	document, ok := loadDocumentByKey(w, r, h.documentRepo)
	if !ok {
		return
	}
	ctx := context.Background()

	claims, _ := middleware.GetUserFromContext(r.Context())
	if status, message, ok := documentVisibleTo(claims, document); !ok {
		writeError(w, status, message)
		return
	}
	shown := documentForViewer(claims, document)

	// Сборка живёт в assembleDocumentFor (document_render.go): с задачи 6 её
	// зовёт ещё и страница краулера, а две копии сборки разошлись бы молча —
	// и та из них, что обходит assembleDocument, обошла бы вместе с ним
	// недоверенный рендерер авторского текста.
	htmlContent, _, err := assembleDocumentFor(ctx, h.renderer, h.cuts, h.works, shown)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось собрать разбор")
		return
	}

	// Полоса состояния на странице просмотра (M7). Автор до сих пор видел
	// здесь свой черновик и ничем не отличал его от публичного вида — ровно
	// то различие, ради которого затеяна вся ветка, ему и не показывали.
	//
	// has_unpublished_changes — ШЕСТОЕ состояние, которого не было в
	// перечне: разбор на людях, автор сохранил правку, но на проверку её не
	// отправил. review_status при этом остаётся «одобрено», и полоса
	// печатала «Разбор на людях», хотя на людях прежний текст. Сличать есть
	// чем: у того, кто вправе видеть черновик, обе редакции на руках.
	//
	// Признак считается по ИСХОДНОЙ строке и только для видящего черновик:
	// у постороннего documentForViewer уже подменил Title/MarkdownContent
	// опубликованной редакцией и погасил Published* — сравнение shown с
	// самим собой дало бы «есть правка» на каждом опубликованном разборе.
	unpublishedChanges := false
	if mayReadDraft(claims, document) {
		unpublishedChanges = document.PublishedAt != nil &&
			(document.Title != document.PublishedTitle ||
				document.MarkdownContent != document.PublishedMarkdown)
	}

	response := map[string]interface{}{
		"id": shown.ID,
		// Slug — вторая половина адреса разбора (вместе с author_nickname
		// выше выбирает форму /documents/{слаг} или /documents/{ник}/{слаг}).
		// Без него фронтовая documentEditPath не может построить адрес
		// правки, и «Править» с самой страницы разбора уводит на
		// .../undefined/edit — см. находку разбора ветки.
		"slug":            shown.Slug,
		"title":           shown.Title,
		"html_content":    htmlContent,
		"owner_id":        shown.OwnerID,
		"author_nickname": shown.AuthorNickname,
		"published_at":    shown.PublishedAt,
		"created_at":      shown.CreatedAt,
		"updated_at":      shown.UpdatedAt,
		// Поля состояния. Гасится постороннему ТОЛЬКО review_status — тем же
		// documentForViewer, что гасит его в карточке. was_published не
		// гасится ни здесь, ни там: по нему читатель отличает снятое (410) от
		// небывшего (404), то есть это и так публичное сведение, а не признак
		// черновика. Утечки в этом нет, но описывать одним словом оба поля
		// нельзя — на этом уже разъехались комментарий и код.
		"review_status":           shown.ReviewStatus,
		"was_published":           shown.WasPublished,
		"has_unpublished_changes": unpublishedChanges,
	}

	writeJSON(w, response)
}
