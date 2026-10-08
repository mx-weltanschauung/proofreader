// Package api: the reference stream behind a subject-index concept — the
// entry list (GET .../fragments), a page's expanded view (GET
// .../references/{refId}/pages/{pageId}), and cut editing (PUT
// .../references/{refId}/cuts).
//
// Отделено от index_handler.go: там остались старые ручки указателя (импорт,
// чтение понятия, список понятий, обратные ссылки страницы), здесь — три
// ручки потока вырезок и их собственные помощники, ничем со старыми не
// связанные.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"unicode/utf8"

	"github.com/gorilla/mux"

	"proofreader/internal/models"
	"proofreader/internal/seo"
)

// cutPartResponse is one page's share of a cut, rendered.
type cutPartResponse struct {
	PageID      int64             `json:"page_id"`
	PageNumber  int               `json:"page_number"`
	PrintedPage int               `json:"printed_page"`
	PageStatus  models.PageStatus `json:"page_status"`
	HTML        string            `json:"html"`
}

// cutBounds is the byte-offset span of a real cut in its anchor pages.
//
// Отдельный тип, а не поля прямо в cutResponse: так виднее, что все четыре
// числа приходят или уходят вместе. Страницы — идентификаторы БД, не номера:
// это то же представление, что хранит models.IndexFragment, и его же ждёт
// обратно PutCuts после перевода фронтом номеров страниц в те же offset'ы.
type cutBounds struct {
	StartPageID int64 `json:"start_page_id"`
	StartOffset int   `json:"start_offset"`
	EndPageID   int64 `json:"end_page_id"`
	EndOffset   int   `json:"end_offset"`
}

// cutResponse is one cut of an address.
//
// ID и Bounds пусты вместе у синтетической вырезки — той, что подставляется
// вместо ненарезанного или отвязавшегося адреса. У неё нет настоящих границ,
// и подставить вместо них нули значило бы соврать; непустой ID обязан
// сопровождаться непустым Bounds, и наоборот.
type cutResponse struct {
	ID     *int64     `json:"id"`
	Bounds *cutBounds `json:"bounds"`
	Status string     `json:"status"`

	// HeadQuote — головная цитата-якорь вырезки, как она хранится
	// (models.IndexFragment.HeadQuote). Из неё клиент строит внешний адрес
	// места: /works/{w}/pages/{n}?quote=…
	//
	// Цитата — срез markdown, а ?quote= ищется в отрисованном тексте:
	// вырезка, начавшаяся внутри разметки, промахнётся и покажет читателю
	// честное «не нашлась», приведя его при этом на нужную полосу. Замер по
	// корпусу: чисто примерно шесть окон в 120 Б из десяти.
	//
	// У синтетической вырезки пусто — якоря у неё нет, как нет границ и id.
	HeadQuote string `json:"head_quote"`

	Parts []cutPartResponse `json:"parts"`
}

// entryPageResponse is one page of an address, cut or not.
//
// Список страниц адреса нужен клиенту, чтобы показать те из них, которых не
// коснулась ни одна вырезка: без page_id такую страницу нечем даже назвать —
// разворот (ExpandPage) адресуется по идентификатору, а взять его из вырезок
// нельзя ровно потому, что вырезки на этой странице и нет.
type entryPageResponse struct {
	PageID      int64             `json:"page_id"`
	PageNumber  int               `json:"page_number"`
	PrintedPage int               `json:"printed_page"`
	PageStatus  models.PageStatus `json:"page_status"`
}

// entryPages projects an address's pages into the response.
//
// Пустой срез, а не nil: пустой список обязан приезжать как [], иначе клиент
// получит null там, где ждёт массив. Сегодня до пустого не доходит — запись
// без единой страницы Fragments пропускает целиком, — но выдача не должна
// зависеть от этого совпадения.
func entryPages(pages []*models.Page, pageOffset int) []entryPageResponse {
	out := make([]entryPageResponse, 0, len(pages))
	for _, p := range pages {
		out = append(out, entryPageResponse{
			PageID:      p.ID,
			PageNumber:  p.PageNumber,
			PrintedPage: p.PageNumber + pageOffset,
			PageStatus:  p.Status,
		})
	}
	return out
}

// entryRubricPath — путь адреса для ответа, всегда непустой срез (возможно,
// нулевой длины): тем же приёмом и по той же причине, что entryPages.
func entryRubricPath(ref *models.IndexReference) []string {
	path := rubricPathOf(ref)
	out := make([]string, 0, len(path))
	return append(out, path...)
}

// entryResponse is one stream entry: a single index address with its cuts.
type entryResponse struct {
	ReferenceID  int64   `json:"reference_id"`
	VolumeNumber int     `json:"volume_number"`
	VolumePart   *string `json:"volume_part,omitempty"`
	PrintedStart int     `json:"printed_start"`
	PrintedEnd   int     `json:"printed_end"`
	WorkID       int64   `json:"work_id"`
	WorkTitle    string  `json:"work_title"`
	WorkSlug     string  `json:"work_slug,omitempty"`
	ChapterTitle string  `json:"chapter_title"`
	// ChapterID и ChapterSlug — адрес той же главы, что ChapterTitle: шапка
	// записи делает из названия ссылку. nil — страница вне всех глав.
	ChapterID   *int64 `json:"chapter_id"`
	ChapterSlug string `json:"chapter_slug,omitempty"`
	Rubric      string `json:"rubric"`
	// RubricPath — путь подрубрик от корня к листу. Всегда массив, никогда
	// null: по нему поток рисует заголовки уровней, и null уронил бы разбор.
	// Плоский Rubric рядом остаётся листом — его читают места, которых эта
	// работа не трогает.
	RubricPath  []string `json:"rubric_path"`
	IsUncertain bool     `json:"is_uncertain"`
	State       string   `json:"state"`
	// Pages — ВСЕ страницы адреса, в порядке чтения. Cuts покрывает только те
	// из них, куда попала вырезка; разница между списками и есть места, где
	// адрес указателя шире выделенного.
	Pages []entryPageResponse `json:"pages"`
	Cuts  []cutResponse       `json:"cuts"`
	// StaleCuts holds cuts that lost their anchor on re-anchoring, kept out
	// of Cuts so the reader never sees one rendered as text.
	//
	// Список существует, чтобы клиент не забыл эти вырезки при следующем
	// сохранении: PutCuts заменяет набор адреса целиком, и вырезка, не
	// попавшая ни в cuts, ни в stale_cuts, была бы стёрта первым же PUT после
	// правки другой страницы того же адреса. Здесь только id и границы —
	// сервер не пытается отрендерить текст, которого не может найти.
	StaleCuts []cutResponse `json:"stale_cuts"`
}

// entriesResponse is the paginated body of GET /api/concepts/{slug}/fragments.
type entriesResponse struct {
	Total   int             `json:"total"`
	Entries []entryResponse `json:"entries"`
}

// Состояния записи потока: есть вырезки; вырезок нет; вырезки отвязались.
const (
	entryStateFragment  = "fragment"
	entryStateWholePage = "whole_page"
	entryStateStale     = "stale"
)

const (
	fragmentsDefaultLimit = 20
	fragmentsMaxLimit     = 50
)

// Fragments returns the reference stream behind a concept's index addresses:
// one entry per address, each with the cuts of its own subrubric.
//
// Единица потока — адрес, а не страница: подрубрика («определение», «как
// субстанция стоимости») — ось чтения, и слияние двух адресов, попавших на
// одни страницы, потеряло бы, какое место страницы к какой подрубрике
// относится.
//
// Query-параметр reference_id (задача 13a) сужает поток до одной записи —
// той же ручкой, что и общий поток, поэтому total и пагинация считаются
// единообразно. Побочная польза сужения — ссылка на один адрес.
func (h *IndexHandler) Fragments(w http.ResponseWriter, r *http.Request) {
	slug := mux.Vars(r)["slug"]
	ctx := context.Background()

	// Отсутствующий slug приходит из репозитория ошибкой — как в GetConcept.
	c, err := h.index.GetConceptBySlug(ctx, slug)
	if err != nil || c == nil {
		http.Error(w, "Concept not found", http.StatusNotFound)
		return
	}

	resp := entriesResponse{Entries: []entryResponse{}}

	// Одно понятие может быть раскрыто в двух собраниях, и адреса каждой
	// статьи разрешаются по СВОЕЙ карте томов — не по работе-указателю,
	// которой у внешнего источника может не быть вовсе, и не по общей карте:
	// два издания нумеруют тома независимо, и слияние подменило бы том.
	refs, locs, err := conceptAddresses(ctx, h.index, c)
	if err != nil {
		http.Error(w, "Failed to resolve volume map", http.StatusInternalServerError)
		return
	}

	query := r.URL.Query()
	entries := collectEntries(refs, locs, parseFragmentFilter(query), parseEntryOrder(query))
	resp.Total = len(entries)

	limit, offset := parseFragmentPaging(query)
	if offset >= len(entries) {
		writeFragments(w, resp)
		return
	}
	end := offset + limit
	if end > len(entries) {
		end = len(entries)
	}
	entries = entries[offset:end]

	ctxs, err := h.entryContexts(ctx, entries)
	if err != nil {
		http.Error(w, "Failed to retrieve fragment pages", http.StatusInternalServerError)
		return
	}

	refIDs := make([]int64, 0, len(entries))
	for _, e := range entries {
		refIDs = append(refIDs, e.Reference.ID)
	}
	cutsByRef, err := h.fragments.ByReferences(ctx, refIDs)
	if err != nil {
		http.Error(w, "Failed to retrieve fragments", http.StatusInternalServerError)
		return
	}

	for _, e := range entries {
		wc := ctxs[e.WorkID]
		pages := wc.ordered(e.PageNumbers)
		if len(pages) == 0 {
			// Страницы адреса исчезли между разрешением и выборкой тел. Слот
			// в total остаётся: на нём держится пагинация.
			continue
		}

		state, cuts, staleCuts := h.buildCuts(e.Reference.ID, cutsByRef[e.Reference.ID], pages, wc.offset)
		entry := entryResponse{
			ReferenceID:  e.Reference.ID,
			VolumeNumber: e.Reference.VolumeNumber,
			VolumePart:   e.Reference.VolumePart,
			PrintedStart: e.PrintedStart,
			PrintedEnd:   e.PrintedEnd,
			WorkID:       e.WorkID,
			WorkTitle:    wc.title,
			WorkSlug:     wc.slug,
			Rubric:       e.Reference.Rubric,
			RubricPath:   entryRubricPath(e.Reference),
			IsUncertain:  e.Reference.IsUncertain,
			State:        state,
			Pages:        entryPages(pages, wc.offset),
			Cuts:         cuts,
			StaleCuts:    staleCuts,
		}
		if ch := deepestChapterOf(wc.chapters, e.PageNumbers[0]); ch != nil {
			id := ch.ID
			entry.ChapterTitle, entry.ChapterID, entry.ChapterSlug = ch.Title, &id, ch.Slug
		}
		resp.Entries = append(resp.Entries, entry)
	}

	writeFragments(w, resp)
}

// writeFragments encodes a fragments response as JSON.
func writeFragments(w http.ResponseWriter, resp entriesResponse) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// volumeMapper — карта томов издания (IndexStore, ConceptIndex).
type volumeMapper interface {
	VolumeMap(ctx context.Context, editionID int64) ([]models.VolumeLocation, error)
}

// conceptAddresses собирает адреса ВСЕХ статей понятия вместе с местом
// каждого адреса, найденным СТРОГО по карте издания его собственной статьи.
//
// Карты изданий сюда нельзя сливать в один общий словарь по номеру тома:
// каждое собрание нумерует тома заново с единицы, и том 23 есть и у
// Маркса, и у Ленина — общий словарь заставил бы адрес одной статьи
// разрешаться по чужому изданию молча, без ошибки и без признака. Поэтому
// результат — не карта томов, а прямое соответствие «id адреса → его
// местоположение», уже разрешённое против правильного издания; адрес, для
// которого своя карта не нашла том, в это соответствие просто не попадает
// (тот же исход, что и раньше — «незагруженный том», см.
// TestCollectEntriesSkipsUnloadedVolumes), но остаётся в списке refs, чтобы
// total считал его.
func conceptAddresses(ctx context.Context, index volumeMapper, c *models.IndexConcept) ([]*models.IndexReference, map[int64]models.VolumeLocation, error) {
	var refs []*models.IndexReference
	locs := map[int64]models.VolumeLocation{}
	maps := map[int64]map[volumeKey]models.VolumeLocation{}

	for _, a := range c.Articles {
		vols, ok := maps[a.EditionID]
		if !ok {
			raw, err := index.VolumeMap(ctx, a.EditionID)
			if err != nil {
				return nil, nil, err
			}
			vols = buildVolumeMap(raw)
			maps[a.EditionID] = vols
		}
		for _, ref := range a.References {
			if loc, found := vols[keyOf(ref.VolumeNumber, ref.VolumePart)]; found {
				locs[ref.ID] = loc
			}
			refs = append(refs, ref)
		}
	}
	return refs, locs, nil
}

// workContext holds everything needed to render a work's entries.
// Берётся по одному запросу на работу в порции, а не на страницу.
type workContext struct {
	title    string
	slug     string
	offset   int
	pages    map[int]*models.Page
	chapters []*models.Chapter
}

// ordered returns the work's pages for the given numbers, in the order given,
// skipping the ones that are not there.
func (wc workContext) ordered(numbers []int) []*models.Page {
	out := make([]*models.Page, 0, len(numbers))
	for _, n := range numbers {
		if p, ok := wc.pages[n]; ok {
			out = append(out, p)
		}
	}
	return out
}

// entryContexts loads bodies, titles and chapter trees for the works behind a
// slice of entries.
func (h *IndexHandler) entryContexts(ctx context.Context, entries []entrySlot) (map[int64]workContext, error) {
	numbers := map[int64]map[int]struct{}{}
	order := []int64{}
	for _, e := range entries {
		if _, seen := numbers[e.WorkID]; !seen {
			order = append(order, e.WorkID)
			numbers[e.WorkID] = map[int]struct{}{}
		}
		for _, n := range e.PageNumbers {
			// Два адреса на одну страницу — обычное дело; спрашивать её тело
			// дважды незачем.
			numbers[e.WorkID][n] = struct{}{}
		}
	}

	out := make(map[int64]workContext, len(order))
	for _, workID := range order {
		wanted := make([]int, 0, len(numbers[workID]))
		for n := range numbers[workID] {
			wanted = append(wanted, n)
		}
		pages, err := h.pages.GetPagesByNumbers(ctx, workID, wanted)
		if err != nil {
			return nil, err
		}
		byNumber := make(map[int]*models.Page, len(pages))
		for _, p := range pages {
			byNumber[p.PageNumber] = p
		}

		// Название работы и дерево глав — украшение шапки: их отсутствие не
		// повод ронять поток. Недоступность базы уже обнаружилась бы выше,
		// на работе самого понятия.
		title, workSlug, offset := "", "", 0
		if wk, err := h.works.GetByID(ctx, workID); err == nil && wk != nil {
			title, workSlug, offset = wk.Title, wk.Slug, wk.PageOffset
		}
		chapters, _ := h.chapters.ListByWorkHierarchical(ctx, workID)

		out[workID] = workContext{title: title, slug: workSlug, offset: offset, pages: byNumber, chapters: chapters}
	}
	return out, nil
}

// buildCuts renders an address's cuts, falling back to whole pages when there
// are none or they came unmoored. Возвращает отдельно живые (cuts, идут в
// текст) и отвязавшиеся (staleCuts, идут в stale_cuts — id и границы без
// текста, см. entryResponse.StaleCuts) — так вызывающий не обязан заново
// перебирать fragments, чтобы собрать оба списка.
//
// refID — адрес, чьи вырезки рендерятся: из него собирается область имён
// сносок (entryNoteScope), и поток, и ответ правки границ обязаны звать с
// одним и тем же, иначе замена записи на месте сменит её якоря.
func (h *IndexHandler) buildCuts(refID int64, fragments []*models.IndexFragment, pages []*models.Page, pageOffset int) (string, []cutResponse, []cutResponse) {
	scope := entryNoteScope(refID)
	live := make([]*models.IndexFragment, 0, len(fragments))
	staleFragments := make([]*models.IndexFragment, 0, len(fragments))
	for _, f := range fragments {
		if f.Status == models.FragmentStatusStale {
			staleFragments = append(staleFragments, f)
			continue
		}
		live = append(live, f)
	}

	staleCuts := make([]cutResponse, 0, len(staleFragments))
	for _, f := range staleFragments {
		id := f.ID
		bounds := cutBounds{StartPageID: f.StartPageID, StartOffset: f.StartOffset, EndPageID: f.EndPageID, EndOffset: f.EndOffset}
		// HeadQuote здесь намеренно не заполняется, хотя у фрагмента она
		// есть: stale значит, что якорь разошёлся с текущим текстом полосы
		// при переякоривании, и цитата могла устареть вместе с границами —
		// внешний адрес по ней вёл бы читателя наугад (тикет 15).
		staleCuts = append(staleCuts, cutResponse{ID: &id, Bounds: &bounds, Status: f.Status, Parts: []cutPartResponse{}})
	}

	if len(live) == 0 {
		// Показывать отвязавшуюся вырезку нечем: её границы могли уехать куда
		// угодно. Честнее полная страница с пометкой.
		state := entryStateWholePage
		if len(staleFragments) > 0 {
			state = entryStateStale
		}
		parts := make([]cutPartResponse, 0, len(pages))
		for _, p := range pages {
			parts = append(parts, h.renderPart(scope, p, 0, len(p.ContentMarkdown), pageOffset))
		}
		return state, []cutResponse{{ID: nil, Bounds: nil, Status: state, Parts: parts}}, staleCuts
	}

	byID := make(map[int64]*models.Page, len(pages))
	for _, p := range pages {
		byID[p.ID] = p
	}

	cuts := make([]cutResponse, 0, len(live))
	for _, f := range live {
		ranges := cutRanges(f, pages)
		// cutRanges приходит пустым, если якорные страницы вырезки не попали
		// в текущий диапазон адреса (переякоривание отстало от переимпорта) —
		// пустой список частей должен остаться списком, а не стать null.
		parts := make([]cutPartResponse, 0, len(ranges))
		for _, rg := range ranges {
			p, ok := byID[rg.PageID]
			if !ok {
				continue
			}
			parts = append(parts, h.renderPart(scope, p, rg.Start, rg.End, pageOffset))
		}
		id := f.ID
		bounds := cutBounds{StartPageID: f.StartPageID, StartOffset: f.StartOffset, EndPageID: f.EndPageID, EndOffset: f.EndOffset}
		cuts = append(cuts, cutResponse{ID: &id, Bounds: &bounds, Status: f.Status, HeadQuote: f.HeadQuote, Parts: parts})
	}
	return entryStateFragment, cuts, staleCuts
}

// entryNoteScope и expandNoteScope — области имён сносок записи потока
// понятия и её раскрытой полосы. На одной странице понятия стоят десятки
// записей, часто с одних и тех же полос (у двух подрубрик — общий адрес), и
// голые id вида fn:s1 повторялись: превью сноски, которое ищет тело через
// document.getElementById, показывало тело из чужой записи. Раскрытая полоса
// стоит в DOM рядом с вырезкой своей же записи, поэтому у неё другая буква.
// Обе формы проходят pagePrefixRe пакета markdown: буква, цифры, дефис.
func entryNoteScope(refID int64) string  { return fmt.Sprintf("c%d-", refID) }
func expandNoteScope(refID int64) string { return fmt.Sprintf("e%d-", refID) }

// renderPart renders one slice of a page, pulling in the footnote bodies the
// slice references but does not contain.
func (h *IndexHandler) renderPart(scope string, p *models.Page, start, end, pageOffset int) cutPartResponse {
	slice := withFootnoteDefs(sliceCut(p.ContentMarkdown, start, end), p.ContentMarkdown)
	return cutPartResponse{
		PageID:      p.ID,
		PageNumber:  p.PageNumber,
		PrintedPage: p.PageNumber + pageOffset,
		PageStatus:  p.Status,
		HTML:        h.renderer.RenderWithNotes(scope, p.PageNumber, slice),
	}
}

// parseFragmentFilter reads the stream filters out of the query string.
//
// Часть тома учитывается только вместе с номером: «часть II» без тома
// ничего не означает. reference_id (задача 13a) сужает поток до одного
// адреса — им пользуется клиент, чтобы перезапросить ровно одну запись после
// сохранения правки границ, не трогая офсет и total остального потока.
// Неразбираемое значение любого из фильтров игнорируется, а не даёт 400:
// фильтр — сужение, а не команда, тем же правилом, что и мусор в volume.
func parseFragmentFilter(q url.Values) fragmentFilter {
	f := fragmentFilter{Rubric: q.Get("rubric")}
	if v := q.Get("volume"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			f.Volume, f.HasVolume = n, true
			f.VolumePart = q.Get("volume_part")
		}
	}
	if v := q.Get("reference_id"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			f.ReferenceID, f.HasReferenceID = n, true
		}
	}
	if v := q.Get("rubric_path"); v != "" {
		// HasRubricPath ставится только при НЕПУСТОМ пути: пустой префикс
		// накрывает всё, и фильтр молча перестал бы сужать.
		if path := parseRubricPath(v); len(path) > 0 {
			f.RubricPath, f.HasRubricPath = path, true
		}
	}
	return f
}

// parseRubricPath разбирает значение ?rubric_path= — звенья пути, каждое
// закодированное encodeURIComponent, склеенные двоеточием.
//
// Двоеточие разделителем безопасно ровно потому, что encodeURIComponent
// кодирует его всегда (%3A), а дефис — никогда (тот в списке символов,
// которые encodeURIComponent оставляет как есть, вместе с `_ . ! ~ * ' ( )`).
// Тот же довод и тот же выбор, что у rubricAnchor в ConceptArticleBlock.tsx;
// куплен он тремя подрубриками корпуса, несущими двоеточие в заголовке.
//
// Значение приезжает закодированным ДВАЖДЫ: encodeURIComponent на звене плюс
// кодирование всего параметра axios/URLSearchParams. Первое раскодирование
// делает r.URL.Query(), второе — PathUnescape здесь. Это не лишний слой, а
// то, на чём держится разделитель.
//
// PathUnescape, а не QueryUnescape: последний превращает «+» в пробел, а
// encodeURIComponent кодирует «+» как %2B — голый плюс в звене означал бы
// настоящий плюс заголовка, и терять его нельзя.
//
// Звено, которое не раскодировалось или пусто, роняет ВЕСЬ путь, а не
// пропускается: путь с выпавшим звеном — другой путь, и сузить им поток
// значило бы показать читателю не то, что он просил.
//
// Проверка utf8.ValidString — ради согласия с фронтовой половиной контракта:
// decodeURIComponent на бите вроде %FF роняет URIError, и decodeRubricPath
// отдаёт [] — путь фронта пуст, сужения нет. url.PathUnescape("%FF") же
// возвращает "\xff" молча, без ошибки; без этой проверки сервер принял бы
// путь, который фронт считает несуществующим, и поток разошёлся бы с
// панелью статьи — при "?rubric_path=%FF" панель показала бы все адреса, а
// поток встал бы пустым.
//
// Сам разбор — seo.ParseRubricPath: тот же параметр принимает .md понятия
// (internal/seo/render_concept_llm.go), и два разборщика разошлись бы.
func parseRubricPath(raw string) []string {
	return seo.ParseRubricPath(raw)
}

// parseEntryOrder reads the reading order out of the query string.
//
// Дефолт — по подрубрикам: так устроена печатная статья. Нераспознанное
// значение молча даёт дефолт, а не 400 — порядок показа не то, из-за чего
// стоит отказывать в ответе (то же правило, что у фильтров выше).
func parseEntryOrder(q url.Values) entryOrder {
	if q.Get("order") == "page" {
		return orderByPage
	}
	return orderByRubric
}

// parseFragmentPaging reads limit/offset out of the query string, defaulting
// and capping limit so a caller can't ask for an unbounded page.
func parseFragmentPaging(q url.Values) (limit, offset int) {
	limit = fragmentsDefaultLimit
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > fragmentsMaxLimit {
		limit = fragmentsMaxLimit
	}
	if v := q.Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	return limit, offset
}

// putCutRequest is one cut in a PUT body. Страницы — номера работы, не
// печатные: клиент уже видел их в потоке, а обратный перевод печатного
// номера — дело сервера.
type putCutRequest struct {
	// ID references an existing cut of this same address instead of
	// describing it by bounds; when set, StartPage/StartOffset/EndPage/
	// EndOffset/Status below are ignored and the existing cut is kept as is.
	//
	// Набор всё равно заменяется целиком, но нетронутую вырезку не нужно
	// пересказывать границами — сервер и так знает её по id, а цитаты, хеши
	// и статус трогать незачем. Это единственный способ пронести через
	// сохранение вырезку с исчезнувшими частями (parts: [] в потоке): у неё
	// на клиенте нет номеров страниц, чтобы описать границы, а id есть
	// всегда.
	ID          *int64 `json:"id,omitempty"`
	StartPage   int    `json:"start_page"`
	StartOffset int    `json:"start_offset"`
	EndPage     int    `json:"end_page"`
	EndOffset   int    `json:"end_offset"`
	// Status пуст у нарезчика, шлющего один статус на всю пачку — тогда
	// действует putCutsRequest.Status. Редактор границ пересылает набор
	// целиком и обязан проставлять его на каждую вырезку отдельно: общий
	// статус пометил бы уже машинную вырезку соседнего места как
	// подтверждённую человеком только потому, что тот поправил другую.
	Status string `json:"status,omitempty"`
}

type putCutsRequest struct {
	Status string `json:"status"`
	// Cuts is a pointer so an absent key can be told apart from an explicit
	// empty list.
	//
	// Пустой список — законный ответ «раскрытия здесь нет», он стирает
	// прежний набор; отсутствие ключа — недописанный или битый запрос
	// клиента, и раньше оба случая вели себя одинаково, молча стирая набор
	// вырезок адреса. Nil отличает второе от первого и уходит в 400 ниже.
	Cuts *[]putCutRequest `json:"cuts"`
}

// PutCuts replaces the whole set of cuts of one index address.
//
// Замена целиком, а не досыл: нарезчик гоняется повторно, а правка человека
// приходит полным набором — так повторный прогон идемпотентен. Цитаты, хеши
// и порядок снимает сервер: единственный источник правды о тексте страницы —
// он, и клиент не должен уметь соврать о том, что вырезал. Нетронутую вырезку
// можно не пересказывать вовсе — прислать её id (putCutRequest.ID), и сервер
// перенесёт её как есть; id, не принадлежащий этому адресу, — 404, а не 400:
// запрос синтаксически исправен, просто ссылается в никуда.
func (h *IndexHandler) PutCuts(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	refID, err := strconv.ParseInt(vars["refId"], 10, 64)
	if err != nil {
		http.Error(w, "Invalid reference ID", http.StatusBadRequest)
		return
	}

	var req putCutsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.Status != models.FragmentStatusMachine && req.Status != models.FragmentStatusConfirmed {
		http.Error(w, "Status must be machine or confirmed", http.StatusBadRequest)
		return
	}
	if req.Cuts == nil {
		http.Error(w, "cuts is required", http.StatusBadRequest)
		return
	}

	ctx := context.Background()
	c, err := h.index.GetConceptBySlug(ctx, vars["slug"])
	if err != nil || c == nil {
		http.Error(w, "Concept not found", http.StatusNotFound)
		return
	}

	// Адреса и их местоположения — по всем статьям понятия, каждый строго
	// против издания СВОЕЙ статьи (см. conceptAddresses): работы-указателя у
	// внешнего источника может не быть вовсе, а слияние карт разных изданий
	// в одну подменило бы том чужим.
	refs, locs, err := conceptAddresses(ctx, h.index, c)
	if err != nil {
		http.Error(w, "Failed to resolve volume map", http.StatusInternalServerError)
		return
	}

	var ref *models.IndexReference
	for _, candidate := range refs {
		if candidate.ID == refID {
			ref = candidate
			break
		}
	}
	if ref == nil {
		http.Error(w, "Reference not found", http.StatusNotFound)
		return
	}

	loc, ok := locs[ref.ID]
	if !ok {
		http.Error(w, "Volume of this address is not loaded", http.StatusNotFound)
		return
	}

	allowed := referencePages(ref, loc)
	pages, err := h.pages.GetPagesByNumbers(ctx, loc.WorkID, allowed)
	if err != nil {
		http.Error(w, "Failed to retrieve pages", http.StatusInternalServerError)
		return
	}
	byNumber := make(map[int]*models.Page, len(pages))
	for _, p := range pages {
		byNumber[p.PageNumber] = p
	}

	// Существующие вырезки адреса — только чтобы разрешить ссылки по id
	// (putCutRequest.ID); сам набор всё равно заменяется целиком ниже.
	existingByRef, err := h.fragments.ByReferences(ctx, []int64{refID})
	if err != nil {
		http.Error(w, "Failed to retrieve existing cuts", http.StatusInternalServerError)
		return
	}
	existingByID := make(map[int64]*models.IndexFragment, len(existingByRef[refID]))
	for _, f := range existingByRef[refID] {
		existingByID[f.ID] = f
	}

	fragments := make([]*models.IndexFragment, 0, len(*req.Cuts))
	for _, cut := range *req.Cuts {
		if cut.ID != nil {
			// Ссылка на нетронутую вырезку: остальные поля запроса для неё
			// не читаются вовсе — границы, цитаты, хеши и статус берутся из
			// уже сохранённой записи. Id не этого адреса — 404, а не 400:
			// запрос корректен по форме, просто указывает в никуда.
			existing, ok := existingByID[*cut.ID]
			if !ok {
				http.Error(w, fmt.Sprintf("Cut %d not found for this address", *cut.ID), http.StatusNotFound)
				return
			}
			fragments = append(fragments, existing)
			continue
		}
		// Пустой per-cut статус наследует статус уровня запроса — так старый
		// нарезчик, шлющий один статус на пачку, продолжает работать
		// неизменным.
		status := cut.Status
		if status == "" {
			status = req.Status
		}
		if status != models.FragmentStatusMachine && status != models.FragmentStatusConfirmed {
			http.Error(w, "Cut status must be machine or confirmed", http.StatusBadRequest)
			return
		}
		f, msg := buildFragment(refID, cut, byNumber, status)
		if msg != "" {
			http.Error(w, msg, http.StatusBadRequest)
			return
		}
		fragments = append(fragments, f)
	}

	// Порядок отображения — порядок на странице, а не порядок в запросе:
	// сохранение мешает нетронутые вырезки (пришли по id, в порядке,
	// в котором их выдал редактор) с только что переописанными, и без
	// пересортировки order_number унаследовал бы этот случайный порядок.
	pageNumberByID := make(map[int64]int, len(pages))
	for _, p := range pages {
		pageNumberByID[p.ID] = p.PageNumber
	}
	sortFragmentsByPosition(fragments, pageNumberByID)

	if err := h.fragments.ReplaceForReference(ctx, refID, fragments); err != nil {
		http.Error(w, "Failed to store cuts", http.StatusInternalServerError)
		return
	}

	// Ответ — вырезки в форме потока, чтобы UI перерисовался без второго
	// запроса.
	ordered := make([]*models.Page, 0, len(allowed))
	for _, n := range allowed {
		if p, ok := byNumber[n]; ok {
			ordered = append(ordered, p)
		}
	}
	state, cuts, staleCuts := h.buildCuts(refID, fragments, ordered, loc.PageOffset)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(struct {
		State     string        `json:"state"`
		Cuts      []cutResponse `json:"cuts"`
		StaleCuts []cutResponse `json:"stale_cuts"`
	}{State: state, Cuts: cuts, StaleCuts: staleCuts})
}

// sortFragmentsByPosition orders fragments by page position then start
// offset, in place. Смещения на странице не в pageNumberByID (якорная
// страница выпала из текущего диапазона адреса) уходят в конец — не пропадать
// же вырезке из набора, но и вклиниваться в середину ей нечем.
func sortFragmentsByPosition(fragments []*models.IndexFragment, pageNumberByID map[int64]int) {
	sort.SliceStable(fragments, func(i, j int) bool {
		pi, oki := pageNumberByID[fragments[i].StartPageID]
		if !oki {
			pi = math.MaxInt32
		}
		pj, okj := pageNumberByID[fragments[j].StartPageID]
		if !okj {
			pj = math.MaxInt32
		}
		if pi != pj {
			return pi < pj
		}
		return fragments[i].StartOffset < fragments[j].StartOffset
	})
}

// buildFragment validates one requested cut against the address's pages and
// fills in the anchor data. Возвращает текст ошибки вместо error: он уходит
// прямо в 400, и оборачивать его не во что.
func buildFragment(refID int64, cut putCutRequest, byNumber map[int]*models.Page, status string) (*models.IndexFragment, string) {
	startPage, ok := byNumber[cut.StartPage]
	if !ok {
		return nil, fmt.Sprintf("Page %d is not among this address's pages", cut.StartPage)
	}
	endPage, ok := byNumber[cut.EndPage]
	if !ok {
		return nil, fmt.Sprintf("Page %d is not among this address's pages", cut.EndPage)
	}
	if cut.EndPage < cut.StartPage {
		return nil, "Cut ends before it starts"
	}
	if cut.StartOffset < 0 || cut.StartOffset > len(startPage.ContentMarkdown) {
		return nil, fmt.Sprintf("Start offset %d is outside page %d", cut.StartOffset, cut.StartPage)
	}
	if cut.EndOffset < 0 || cut.EndOffset > len(endPage.ContentMarkdown) {
		return nil, fmt.Sprintf("End offset %d is outside page %d", cut.EndOffset, cut.EndPage)
	}
	// Пустой срез и перевёрнутый — разные причины отказа: сообщение уходит
	// в UI дословно, и «вырезка пуста» для (8,2) было бы неправдой — границы
	// просто переставлены местами.
	if cut.StartPage == cut.EndPage {
		if cut.EndOffset < cut.StartOffset {
			return nil, "Cut end offset is before its start offset"
		}
		if cut.EndOffset == cut.StartOffset {
			return nil, "Cut is empty"
		}
	}
	// Смещение посреди руны дало бы срез, который потом никогда не найдётся
	// в тексте: цитата-якорь начиналась бы с половины буквы.
	if !offsetOnRuneBoundary(startPage.ContentMarkdown, cut.StartOffset) ||
		!offsetOnRuneBoundary(endPage.ContentMarkdown, cut.EndOffset) {
		return nil, "Offsets must fall on character boundaries"
	}

	headEnd := len(startPage.ContentMarkdown)
	if cut.StartPage == cut.EndPage {
		headEnd = cut.EndOffset
	}
	tailStart := 0
	if cut.StartPage == cut.EndPage {
		tailStart = cut.StartOffset
	}

	return &models.IndexFragment{
		ReferenceID: refID,
		StartPageID: startPage.ID,
		StartOffset: cut.StartOffset,
		EndPageID:   endPage.ID,
		EndOffset:   cut.EndOffset,
		HeadQuote:   quoteOf(startPage.ContentMarkdown, cut.StartOffset, headEnd, true),
		TailQuote:   quoteOf(endPage.ContentMarkdown, tailStart, cut.EndOffset, false),
		StartHash:   pageHash(startPage.ContentMarkdown),
		EndHash:     pageHash(endPage.ContentMarkdown),
		Status:      status,
	}, ""
}

// offsetOnRuneBoundary reports whether an offset can start a rune (the end of
// the string counts).
func offsetOnRuneBoundary(text string, offset int) bool {
	if offset == len(text) {
		return true
	}
	return utf8.RuneStart(text[offset])
}

// expandChunkResponse is one piece of an expanded page.
type expandChunkResponse struct {
	HTML   string `json:"html"`
	Inside bool   `json:"inside"`
}

type expandPageResponse struct {
	PageID      int64                 `json:"page_id"`
	PageNumber  int                   `json:"page_number"`
	PrintedPage int                   `json:"printed_page"`
	PageStatus  models.PageStatus     `json:"page_status"`
	Markdown    string                `json:"markdown"`
	Chunks      []expandChunkResponse `json:"chunks"`
}

// ExpandPage returns one whole page of an address, split along that address's
// cuts.
//
// Разворот отдельным вызовом, а не полем потока: полный текст всех страниц
// сделал бы порцию потока в разы тяжелее ради того, что читатель открывает
// изредка. Markdown отдаётся вместе с чанками — он же исходник для режима
// правки границ.
func (h *IndexHandler) ExpandPage(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	refID, err := strconv.ParseInt(vars["refId"], 10, 64)
	if err != nil {
		http.Error(w, "Invalid reference ID", http.StatusBadRequest)
		return
	}
	pageID, err := strconv.ParseInt(vars["pageId"], 10, 64)
	if err != nil {
		http.Error(w, "Invalid page ID", http.StatusBadRequest)
		return
	}

	ctx := context.Background()
	c, err := h.index.GetConceptBySlug(ctx, vars["slug"])
	if err != nil || c == nil {
		http.Error(w, "Concept not found", http.StatusNotFound)
		return
	}

	// Адреса и их местоположения — по всем статьям понятия, каждый строго
	// против издания СВОЕЙ статьи (см. conceptAddresses): работы-указателя у
	// внешнего источника может не быть вовсе, а слияние карт разных изданий
	// в одну подменило бы том чужим.
	refs, locs, err := conceptAddresses(ctx, h.index, c)
	if err != nil {
		http.Error(w, "Failed to resolve volume map", http.StatusInternalServerError)
		return
	}

	var ref *models.IndexReference
	for _, candidate := range refs {
		if candidate.ID == refID {
			ref = candidate
			break
		}
	}
	if ref == nil {
		http.Error(w, "Reference not found", http.StatusNotFound)
		return
	}

	loc, ok := locs[ref.ID]
	if !ok {
		http.Error(w, "Volume of this address is not loaded", http.StatusNotFound)
		return
	}

	page, err := h.pages.GetByID(ctx, pageID)
	if err != nil || page == nil {
		http.Error(w, "Page not found", http.StatusNotFound)
		return
	}
	// Страница обязана принадлежать работе адреса и стоять внутри него:
	// разворачивать что попало из-под чужого адреса незачем.
	//
	// referencePages идёт по печатным номерам подряд (PageStart..end) и
	// сдвигает их на постоянный page_offset, отсекая только неполные концы
	// диапазона (page < 1 или page > MaxPage) — эти условия монотонны по
	// printed, поэтому обрезаться может только начало и/или конец, а не
	// середина. Результат всегда смежный отрезок рабочих номеров, и
	// принадлежность проверяется по его границам без прохода по срезу.
	allowed := referencePages(ref, loc)
	if page.WorkID != loc.WorkID || len(allowed) == 0 ||
		page.PageNumber < allowed[0] || page.PageNumber > allowed[len(allowed)-1] {
		http.Error(w, "Page does not belong to this address", http.StatusNotFound)
		return
	}

	cutsByRef, err := h.fragments.ByReferences(ctx, []int64{refID})
	if err != nil {
		http.Error(w, "Failed to retrieve fragments", http.StatusInternalServerError)
		return
	}

	var spans [][2]int
	for _, f := range cutsByRef[refID] {
		if f.Status == models.FragmentStatusStale {
			continue
		}
		for _, rg := range cutRanges(f, []*models.Page{page}) {
			spans = append(spans, [2]int{rg.Start, rg.End})
		}
	}

	resp := expandPageResponse{
		PageID:      page.ID,
		PageNumber:  page.PageNumber,
		PrintedPage: page.PageNumber + loc.PageOffset,
		PageStatus:  page.Status,
		Markdown:    page.ContentMarkdown,
		Chunks:      []expandChunkResponse{},
	}
	for _, chunk := range pageChunks(page.ContentMarkdown, spans) {
		resp.Chunks = append(resp.Chunks, expandChunkResponse{
			// Сноски дотягиваются к каждому куску по той же причине, что и в
			// потоке: ссылка без определения повисает.
			HTML: h.renderer.RenderWithNotes(expandNoteScope(refID), page.PageNumber,
				withFootnoteDefs(chunk.Text, page.ContentMarkdown)),
			Inside: chunk.Inside,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
