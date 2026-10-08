// Package api tests: the concept reference stream — Fragments, ExpandPage,
// PutCuts.
//
// Отделено от index_handler_test.go по тому же шву, что и concept_stream.go:
// тесты потоковых ручек здесь, общие фикстуры (fakeIndexStore, fakeWorkGetter,
// pageStub, fakeChapterTrees) остались в index_handler_test.go — пакет один,
// всё видно без дублирования.
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"proofreader/internal/models"
	"proofreader/pkg/markdown"
)

func fragmentsRequest(t *testing.T, h *IndexHandler, query string) (*httptest.ResponseRecorder, fragmentsBody) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/concepts/abstraktnyj-trud/fragments"+query, nil)
	req = mux.SetURLVars(req, map[string]string{"slug": "abstraktnyj-trud"})
	rec := httptest.NewRecorder()
	h.Fragments(rec, req)

	var body fragmentsBody
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("разбор ответа: %v; тело: %s", err, rec.Body.String())
		}
	}
	return rec, body
}

type fragmentsCutBody struct {
	ID     *int64 `json:"id"`
	Bounds *struct {
		StartPageID int64 `json:"start_page_id"`
		StartOffset int   `json:"start_offset"`
		EndPageID   int64 `json:"end_page_id"`
		EndOffset   int   `json:"end_offset"`
	} `json:"bounds"`
	Status    string `json:"status"`
	HeadQuote string `json:"head_quote"`
	Parts     []struct {
		PageID      int64  `json:"page_id"`
		PageNumber  int    `json:"page_number"`
		PrintedPage int    `json:"printed_page"`
		PageStatus  string `json:"page_status"`
		HTML        string `json:"html"`
	} `json:"parts"`
}

type fragmentsBody struct {
	Total   int `json:"total"`
	Entries []struct {
		ReferenceID  int64    `json:"reference_id"`
		VolumeNumber int      `json:"volume_number"`
		PrintedStart int      `json:"printed_start"`
		PrintedEnd   int      `json:"printed_end"`
		WorkID       int64    `json:"work_id"`
		WorkTitle    string   `json:"work_title"`
		WorkSlug     string   `json:"work_slug"`
		ChapterTitle string   `json:"chapter_title"`
		ChapterID    *int64   `json:"chapter_id"`
		ChapterSlug  string   `json:"chapter_slug"`
		Rubric       string   `json:"rubric"`
		RubricPath   []string `json:"rubric_path"`
		State        string   `json:"state"`
		Pages        []struct {
			PageID      int64  `json:"page_id"`
			PageNumber  int    `json:"page_number"`
			PrintedPage int    `json:"printed_page"`
			PageStatus  string `json:"page_status"`
		} `json:"pages"`
		Cuts      []fragmentsCutBody `json:"cuts"`
		StaleCuts []fragmentsCutBody `json:"stale_cuts"`
	} `json:"entries"`
}

// conceptWithTwoRubrics воспроизводит живой случай «Абстрактного труда»: одна
// пара страниц в двух подрубриках, том 12 загружен.
//
// References живёт только на статье — conceptAddresses читает адреса
// исключительно через Articles.
func conceptWithTwoRubrics() *models.IndexConcept {
	refs := []*models.IndexReference{
		{ID: 473, VolumeNumber: 12, PageStart: 730, PageEnd: 731, Rubric: "определение", OrderNumber: 1},
		{ID: 541, VolumeNumber: 12, PageStart: 730, PageEnd: 731, Rubric: "и конкретный (полезный) труд", OrderNumber: 69},
	}
	return &models.IndexConcept{
		ID: 8, Title: "Абстрактный труд", Slug: "abstraktnyj-trud",
		SortKey: "абстрактный труд",
		Articles: []*models.IndexArticle{
			{ID: 1, ConceptID: 8, EditionID: 3, Kind: models.IndexConceptKindArticle, References: refs},
		},
	}
}

// conceptWithNestedRubrics воспроизводит живой случай статьи 2698
// («КПСС — съезды»): один адрес висит прямо на съезде, другой — на аспекте
// внутри него. Оба тома разрешаются одной картой fragmentsHandler (том 12).
func conceptWithNestedRubrics() *models.IndexConcept {
	refs := []*models.IndexReference{
		{ID: 473, VolumeNumber: 12, PageStart: 730, PageEnd: 730,
			Rubric: "II съезд РСДРП", RubricPath: []string{"II съезд РСДРП"}, OrderNumber: 1},
		{ID: 541, VolumeNumber: 12, PageStart: 731, PageEnd: 731,
			Rubric:     "значение съезда",
			RubricPath: []string{"II съезд РСДРП", "значение съезда"}, OrderNumber: 2},
	}
	return &models.IndexConcept{
		ID: 8, Title: "КПСС — съезды", Slug: "abstraktnyj-trud",
		SortKey: "кпсс — съезды",
		Articles: []*models.IndexArticle{
			{ID: 1, ConceptID: 8, EditionID: 3, Kind: models.IndexConceptKindArticle, References: refs},
		},
	}
}

// conceptWithLaterRubricOnEarlierPage переиспользует conceptWithTwoRubrics,
// но переносит адрес второй подрубрики на более раннюю печатную страницу:
// по страницам он шёл бы первым, по подрубрикам — вторым. Общая фикстура для
// трёх тестов параметра order.
func conceptWithLaterRubricOnEarlierPage() (*models.IndexConcept, *pageStub) {
	concept := conceptWithTwoRubrics()
	concept.Articles[0].References[1] = &models.IndexReference{
		ID: 541, VolumeNumber: 12, PageStart: 700, PageEnd: 700,
		Rubric: "и конкретный (полезный) труд", OrderNumber: 69,
	}
	pages := &pageStub{pages: []*models.Page{
		{ID: 9100, WorkID: 14, PageNumber: 700, ContentMarkdown: "раньше"},
		{ID: 9143, WorkID: 14, PageNumber: 730, ContentMarkdown: "позже"},
		{ID: 9144, WorkID: 14, PageNumber: 731, ContentMarkdown: "и дальше"},
	}}
	return concept, pages
}

func fragmentsHandler(concept *models.IndexConcept, pages *pageStub, tree []*models.Chapter, frags *fakeFragmentStore) *IndexHandler {
	edID := int64(3)
	store := &fakeIndexStore{
		concept: concept,
		volumes: []models.VolumeLocation{{VolumeNumber: 12, WorkID: 14, PageOffset: 0, MaxPage: 800}},
	}
	work := &models.Work{ID: 14, Title: "Экономические рукописи 1857—1859 годов", Slug: "mae-t46-1", EditionID: &edID}
	if frags == nil {
		frags = newFakeFragmentStore()
	}
	return NewIndexHandler(store, &fakeWorkGetter{work: work}, &fakeEditionStore{}, pages, &fakeChapterTrees{tree: tree}, frags, markdown.NewRenderer())
}

func TestFragmentsUncutAddressShowsWholePage(t *testing.T) {
	pages := &pageStub{pages: []*models.Page{
		{ID: 9143, WorkID: 14, PageNumber: 730, ContentMarkdown: "Огромным достижением **Адама Смита**", Status: models.PageStatusNotProofread},
		{ID: 9144, WorkID: 14, PageNumber: 731, ContentMarkdown: "продолжение", Status: models.PageStatusProofread},
	}}
	tree := []*models.Chapter{{ID: 1, Title: "Введение", Slug: "vvedenie", StartPage: 725, EndPage: 740}}
	h := fragmentsHandler(conceptWithTwoRubrics(), pages, tree, nil)

	rec, body := fragmentsRequest(t, h, "")

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d; тело: %s", rec.Code, rec.Body.String())
	}
	// Два адреса из разных подрубрик — две записи, а не одна склеенная.
	if body.Total != 2 || len(body.Entries) != 2 {
		t.Fatalf("total=%d, записей %d", body.Total, len(body.Entries))
	}
	first := body.Entries[0]
	if first.State != "whole_page" {
		t.Fatalf("состояние %q, ожидалось whole_page", first.State)
	}
	if first.Rubric != "определение" || first.PrintedStart != 730 || first.PrintedEnd != 731 {
		t.Fatalf("шапка записи: %+v", first)
	}
	if first.WorkTitle == "" || first.ChapterTitle != "Введение" {
		t.Fatalf("контекст: %q / %q", first.WorkTitle, first.ChapterTitle)
	}
	// Название главы в шапке — ссылка: без id главы и слагов её не собрать.
	if first.ChapterID == nil || *first.ChapterID != 1 || first.ChapterSlug != "vvedenie" || first.WorkSlug != "mae-t46-1" {
		t.Fatalf("адрес главы: id=%v slug=%q work_slug=%q", first.ChapterID, first.ChapterSlug, first.WorkSlug)
	}
	// Синтетическая вырезка: одна, без id, во все страницы адреса. Границ у
	// неё не существует — bounds обязан быть nil, а не выдуманными нулями.
	if len(first.Cuts) != 1 || first.Cuts[0].ID != nil || first.Cuts[0].Bounds != nil || len(first.Cuts[0].Parts) != 2 {
		t.Fatalf("вырезки: %+v", first.Cuts)
	}
	if !strings.Contains(first.Cuts[0].Parts[0].HTML, "<strong>Адама Смита</strong>") {
		t.Fatalf("HTML не отрендерен: %q", first.Cuts[0].Parts[0].HTML)
	}
	if first.Cuts[0].Parts[0].PageStatus != "не_вычитана" {
		t.Fatalf("статус страницы %q", first.Cuts[0].Parts[0].PageStatus)
	}
}

func TestFragmentsCutAddressShowsOnlyTheCut(t *testing.T) {
	body730 := "Первый абзац страницы.\n\nВот здесь понятие раскрывается.\n\nХвост страницы."
	pages := &pageStub{pages: []*models.Page{
		{ID: 9143, WorkID: 14, PageNumber: 730, ContentMarkdown: body730, Status: models.PageStatusProofread},
		{ID: 9144, WorkID: 14, PageNumber: 731, ContentMarkdown: "продолжение", Status: models.PageStatusProofread},
	}}
	start := strings.Index(body730, "Вот здесь")
	end := start + len("Вот здесь понятие раскрывается.")

	frags := newFakeFragmentStore()
	frags.byRefs[473] = []*models.IndexFragment{{
		ID: 91, ReferenceID: 473, OrderNumber: 1,
		StartPageID: 9143, StartOffset: start, EndPageID: 9143, EndOffset: end,
		Status: models.FragmentStatusMachine,
	}}
	h := fragmentsHandler(conceptWithTwoRubrics(), pages, nil, frags)

	_, got := fragmentsRequest(t, h, "")

	cut := got.Entries[0]
	if cut.State != "fragment" {
		t.Fatalf("состояние %q", cut.State)
	}
	if len(cut.Cuts) != 1 || len(cut.Cuts[0].Parts) != 1 {
		t.Fatalf("вырезки: %+v", cut.Cuts)
	}
	// Настоящая вырезка (есть id) обязана отдавать bounds — те же четыре
	// числа, что лежат в фикстуре хранилища.
	bounds := cut.Cuts[0].Bounds
	if cut.Cuts[0].ID == nil || bounds == nil {
		t.Fatalf("bounds пусты у настоящей вырезки: %+v", cut.Cuts[0])
	}
	if bounds.StartPageID != 9143 || bounds.StartOffset != start ||
		bounds.EndPageID != 9143 || bounds.EndOffset != end {
		t.Fatalf("bounds: %+v, ожидалось start=%d end=%d на странице 9143", bounds, start, end)
	}
	html := cut.Cuts[0].Parts[0].HTML
	if !strings.Contains(html, "понятие раскрывается") {
		t.Fatalf("вырезка не показана: %q", html)
	}
	if strings.Contains(html, "Хвост страницы") || strings.Contains(html, "Первый абзац") {
		t.Fatalf("показана вся страница вместо вырезки: %q", html)
	}
	// Соседний адрес не нарезан — он остаётся полной страницей.
	if got.Entries[1].State != "whole_page" {
		t.Fatalf("второй адрес: %q", got.Entries[1].State)
	}
}

func TestFragmentsLiveCutWithVanishedAnchorsHasEmptyPartsNotNull(t *testing.T) {
	pages := &pageStub{pages: []*models.Page{
		{ID: 9143, WorkID: 14, PageNumber: 730, ContentMarkdown: "текст страницы"},
		{ID: 9144, WorkID: 14, PageNumber: 731, ContentMarkdown: "продолжение"},
	}}
	frags := newFakeFragmentStore()
	// Живая (не stale) вырезка, чьи якорные страницы не входят в текущий набор
	// адреса: адрес переимпортировали с другим печатным диапазоном, а
	// переякоривание ещё не успело пометить вырезку stale. cutRanges честно
	// возвращает nil — пустой список частей должен остаться списком [], а не
	// стать null в JSON.
	frags.byRefs[473] = []*models.IndexFragment{{
		ID: 91, ReferenceID: 473, StartPageID: 5001, StartOffset: 0,
		EndPageID: 5002, EndOffset: 5, Status: models.FragmentStatusMachine,
	}}
	h := fragmentsHandler(conceptWithTwoRubrics(), pages, nil, frags)

	rec, got := fragmentsRequest(t, h, "")

	if !strings.Contains(rec.Body.String(), `"parts":[]`) {
		t.Fatalf("пустой список частей ушёл как null, а не []: %s", rec.Body.String())
	}
	// Вырезка живая (не stale), просто ей нечего показать — запись остаётся
	// в состоянии fragment, а не откатывается на whole_page/stale.
	first := got.Entries[0]
	if first.State != "fragment" {
		t.Fatalf("состояние %q, ожидалось fragment", first.State)
	}
	if len(first.Cuts) != 1 || first.Cuts[0].ID == nil || first.Cuts[0].Bounds == nil || len(first.Cuts[0].Parts) != 0 {
		t.Fatalf("вырезки: %+v", first.Cuts)
	}
}

func TestFragmentsStaleCutFallsBackToWholePage(t *testing.T) {
	pages := &pageStub{pages: []*models.Page{
		{ID: 9143, WorkID: 14, PageNumber: 730, ContentMarkdown: "текст страницы"},
		{ID: 9144, WorkID: 14, PageNumber: 731, ContentMarkdown: "продолжение"},
	}}
	frags := newFakeFragmentStore()
	frags.byRefs[473] = []*models.IndexFragment{{
		ID: 91, ReferenceID: 473, StartPageID: 9143, StartOffset: 0,
		EndPageID: 9143, EndOffset: 5, Status: models.FragmentStatusStale,
	}}
	h := fragmentsHandler(conceptWithTwoRubrics(), pages, nil, frags)

	_, body := fragmentsRequest(t, h, "")

	// Показывать отвязавшуюся вырезку нечем: её границы могли уехать куда
	// угодно. Честнее полная страница с пометкой.
	if body.Entries[0].State != "stale" {
		t.Fatalf("состояние %q, ожидалось stale", body.Entries[0].State)
	}
	if len(body.Entries[0].Cuts[0].Parts) != 2 {
		t.Fatalf("частей %d, ожидались обе страницы адреса", len(body.Entries[0].Cuts[0].Parts))
	}
	if body.Entries[0].Cuts[0].ID != nil || body.Entries[0].Cuts[0].Bounds != nil {
		t.Fatalf("подстановка вместо отвязавшейся вырезки не синтетическая: %+v", body.Entries[0].Cuts[0])
	}
	// Отвязавшаяся вырезка не пропадает бесследно — её границы уходят
	// отдельным полем, чтобы правка живой вырезки на другой странице могла
	// перенести её по id, не потеряв.
	if len(body.Entries[0].StaleCuts) != 1 {
		t.Fatalf("stale_cuts: %+v, ожидалась одна запись", body.Entries[0].StaleCuts)
	}
	sc := body.Entries[0].StaleCuts[0]
	if sc.ID == nil || *sc.ID != 91 || sc.Bounds == nil ||
		sc.Bounds.StartPageID != 9143 || sc.Bounds.StartOffset != 0 ||
		sc.Bounds.EndPageID != 9143 || sc.Bounds.EndOffset != 5 {
		t.Fatalf("stale_cuts[0]: %+v", sc)
	}
}

// TestFragmentsMixedLiveAndStaleCutsExposesStaleSeparately: адрес с двумя
// вырезками, одна из которых отвязалась (правка соседней страницы), а
// другая жива. Находка финального разбора: buildCuts раньше выбрасывал
// stale-вырезку без следа, если рядом оставались живые, — набор на запись
// молча терял её. Теперь она уходит в stale_cuts (не в cuts, чтобы реестр
// не отрендерил её как текст), а живая продолжает показываться как обычно.
func TestFragmentsMixedLiveAndStaleCutsExposesStaleSeparately(t *testing.T) {
	pages := &pageStub{pages: []*models.Page{
		{ID: 9143, WorkID: 14, PageNumber: 730, ContentMarkdown: "текст страницы 730"},
		{ID: 9144, WorkID: 14, PageNumber: 731, ContentMarkdown: "текст страницы 731"},
	}}
	frags := newFakeFragmentStore()
	frags.byRefs[473] = []*models.IndexFragment{
		{ID: 91, ReferenceID: 473, StartPageID: 9143, StartOffset: 0, EndPageID: 9143, EndOffset: 5,
			Status: models.FragmentStatusStale},
		{ID: 92, ReferenceID: 473, StartPageID: 9144, StartOffset: 0, EndPageID: 9144, EndOffset: 5,
			Status: models.FragmentStatusMachine},
	}
	h := fragmentsHandler(conceptWithTwoRubrics(), pages, nil, frags)

	_, body := fragmentsRequest(t, h, "")

	first := body.Entries[0]
	// Живая вырезка осталась записью в состоянии fragment — рядом лежащая
	// stale не откатывает её на whole_page/stale.
	if first.State != "fragment" {
		t.Fatalf("состояние %q, ожидалось fragment", first.State)
	}
	if len(first.Cuts) != 1 || first.Cuts[0].ID == nil || *first.Cuts[0].ID != 92 {
		t.Fatalf("живые вырезки: %+v", first.Cuts)
	}
	if len(first.StaleCuts) != 1 || first.StaleCuts[0].ID == nil || *first.StaleCuts[0].ID != 91 {
		t.Fatalf("отвязавшиеся вырезки: %+v", first.StaleCuts)
	}
	// Отвязавшаяся вырезка не рендерится как текст: частей у неё нет.
	if len(first.StaleCuts[0].Parts) != 0 {
		t.Fatalf("stale-вырезка отрендерена как текст: %+v", first.StaleCuts[0])
	}
}

func TestFragmentsCutAcrossPagesGivesTwoParts(t *testing.T) {
	pages := &pageStub{pages: []*models.Page{
		{ID: 9143, WorkID: 14, PageNumber: 730, ContentMarkdown: "начало мысли на 730-й"},
		{ID: 9144, WorkID: 14, PageNumber: 731, ContentMarkdown: "и её конец на 731-й"},
	}}
	frags := newFakeFragmentStore()
	frags.byRefs[473] = []*models.IndexFragment{{
		ID: 91, ReferenceID: 473,
		StartPageID: 9143, StartOffset: 7,
		EndPageID: 9144, EndOffset: len("и её конец"),
		Status: models.FragmentStatusConfirmed,
	}}
	h := fragmentsHandler(conceptWithTwoRubrics(), pages, nil, frags)

	_, body := fragmentsRequest(t, h, "")

	parts := body.Entries[0].Cuts[0].Parts
	if len(parts) != 2 {
		t.Fatalf("частей %d, ожидалось 2", len(parts))
	}
	if parts[0].PrintedPage != 730 || parts[1].PrintedPage != 731 {
		t.Fatalf("печатные номера частей: %d, %d", parts[0].PrintedPage, parts[1].PrintedPage)
	}
	if !strings.Contains(parts[1].HTML, "и её конец") || strings.Contains(parts[1].HTML, "731-й") {
		t.Fatalf("вторая часть: %q", parts[1].HTML)
	}
}

func TestFragmentsPaginatesByEntries(t *testing.T) {
	pages := &pageStub{pages: []*models.Page{
		{ID: 9143, WorkID: 14, PageNumber: 730, ContentMarkdown: "первая"},
		{ID: 9144, WorkID: 14, PageNumber: 731, ContentMarkdown: "вторая"},
	}}
	h := fragmentsHandler(conceptWithTwoRubrics(), pages, nil, nil)

	_, body := fragmentsRequest(t, h, "?limit=1&offset=1")

	if body.Total != 2 {
		t.Fatalf("total=%d — счёт до пагинации", body.Total)
	}
	if len(body.Entries) != 1 || body.Entries[0].Rubric != "и конкретный (полезный) труд" {
		t.Fatalf("порция: %+v", body.Entries)
	}
}

func TestFragmentsOffsetBeyondEndIsEmptyList(t *testing.T) {
	pages := &pageStub{pages: []*models.Page{{ID: 9143, WorkID: 14, PageNumber: 730}}}
	h := fragmentsHandler(conceptWithTwoRubrics(), pages, nil, nil)

	rec, body := fragmentsRequest(t, h, "?offset=99")

	if rec.Code != http.StatusOK || body.Total != 2 {
		t.Fatalf("код %d, total %d", rec.Code, body.Total)
	}
	// Именно [], не null: клиент ждёт список.
	if !strings.Contains(rec.Body.String(), `"entries":[]`) {
		t.Fatalf("пустая порция отдана как %s", rec.Body.String())
	}
}

// Значение ?rubric_path= — звенья encodeURIComponent, склеенные двоеточием.
// Двоеточие внутри звена приезжает как %3A и обязано остаться внутри звена,
// а не развалить путь надвое: в корпусе три подрубрики несут двоеточие в
// заголовке. Вход снят прогоном живой пары axios -> net/url, а не сочинён.
func TestParseFragmentFilterReadsRubricPath(t *testing.T) {
	q, err := url.ParseQuery("rubric_path=a%253Ab:%D0%B7%D0%BD%D0%B0%D1%87%D0%B5%D0%BD%D0%B8%D0%B5%2520%D1%81%D1%8A%D0%B5%D0%B7%D0%B4%D0%B0")
	if err != nil {
		t.Fatalf("строка запроса не разобралась: %v", err)
	}

	f := parseFragmentFilter(q)
	if !f.HasRubricPath {
		t.Fatal("путь не разобран")
	}
	want := []string{"a:b", "значение съезда"}
	if len(f.RubricPath) != 2 || f.RubricPath[0] != want[0] || f.RubricPath[1] != want[1] {
		t.Fatalf("путь %q, ожидался %q", f.RubricPath, want)
	}
}

// Мусор — не команда, а непонятое сужение: фильтр не применяется, 400 не
// отдаётся. То же правило, что уже записано для volume.
func TestParseFragmentFilterIgnoresBrokenRubricPath(t *testing.T) {
	for _, raw := range []string{"%zz", "a::b", "", "%FF"} {
		q := url.Values{"rubric_path": []string{raw}}
		if f := parseFragmentFilter(q); f.HasRubricPath {
			t.Fatalf("значение %q принято как путь: %q", raw, f.RubricPath)
		}
	}
}

// Поле rubric_path обязано доезжать до клиента: без него поток не нарисует
// заголовок съезда. Проверяется РАЗБОРОМ ТЕЛА ОТВЕТА, а не полем структуры
// Go: тест, читающий структуру, сторожит её форму, а не проводку.
func TestFragmentsEntryCarriesRubricPath(t *testing.T) {
	pages := &pageStub{pages: []*models.Page{
		{ID: 9143, WorkID: 14, PageNumber: 730, ContentMarkdown: "свой адрес съезда"},
		{ID: 9144, WorkID: 14, PageNumber: 731, ContentMarkdown: "аспект внутри съезда"},
	}}
	h := fragmentsHandler(conceptWithNestedRubrics(), pages, nil, nil)

	rec, body := fragmentsRequest(t, h, "")

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d; тело: %s", rec.Code, rec.Body.String())
	}
	if len(body.Entries) != 2 {
		t.Fatalf("записей %d, ожидалось 2", len(body.Entries))
	}
	if got := body.Entries[0].RubricPath; len(got) != 1 || got[0] != "II съезд РСДРП" {
		t.Fatalf("путь своего адреса съезда: %q", got)
	}
	deep := body.Entries[1].RubricPath
	if len(deep) != 2 || deep[0] != "II съезд РСДРП" || deep[1] != "значение съезда" {
		t.Fatalf("путь аспекта: %q", deep)
	}
	// Плоское поле остаётся рядом и несёт ЛИСТ — его читают места, которых
	// эта работа не трогает.
	if body.Entries[1].Rubric != "значение съезда" {
		t.Fatalf("rubric %q, ожидался лист", body.Entries[1].Rubric)
	}
}

// Адрес без подрубрики отдаёт ПУСТОЙ МАССИВ, а не null: клиент ждёт массив,
// и null уронил бы разбор заголовков. Проверяется по СЫРОМУ телу — после
// json.Unmarshal и null, и [] дают одинаковый пустой срез, то есть разбор в
// структуру этот дефект увидеть не способен в принципе.
func TestFragmentsRubricPathIsArrayNeverNull(t *testing.T) {
	pages := &pageStub{pages: []*models.Page{
		{ID: 9143, WorkID: 14, PageNumber: 730, ContentMarkdown: "без подрубрики"},
	}}
	concept := conceptWithNestedRubrics()
	concept.Articles[0].References = []*models.IndexReference{
		{ID: 473, VolumeNumber: 12, PageStart: 730, PageEnd: 730, Rubric: "", OrderNumber: 1},
	}
	h := fragmentsHandler(concept, pages, nil, nil)

	rec, _ := fragmentsRequest(t, h, "")

	raw := rec.Body.String()
	if !strings.Contains(raw, `"rubric_path":[]`) {
		t.Fatalf("пустой путь не приехал массивом; тело: %s", raw)
	}
	if strings.Contains(raw, `"rubric_path":null`) {
		t.Fatalf("путь приехал как null; тело: %s", raw)
	}
}

func TestFragmentsFiltersByRubric(t *testing.T) {
	concept := conceptWithTwoRubrics()
	concept.Articles[0].References = append(concept.Articles[0].References,
		&models.IndexReference{ID: 600, VolumeNumber: 12, PageStart: 745, PageEnd: 745, Rubric: "его мера", OrderNumber: 70})
	pages := &pageStub{pages: []*models.Page{
		{ID: 9143, WorkID: 14, PageNumber: 730},
		{ID: 9144, WorkID: 14, PageNumber: 731},
		{ID: 9158, WorkID: 14, PageNumber: 745},
	}}
	h := fragmentsHandler(concept, pages, nil, nil)

	_, body := fragmentsRequest(t, h, "?rubric="+url.QueryEscape("его мера"))

	if body.Total != 1 || len(body.Entries) != 1 || body.Entries[0].PrintedStart != 745 {
		t.Fatalf("фильтр по подрубрике: total=%d, %+v", body.Total, body.Entries)
	}
}

// TestFragmentsOrdersByRubricByDefault: подрубрика — ось чтения статьи, и
// поток без параметров обязан идти по ней, а не по страницам.
func TestFragmentsOrdersByRubricByDefault(t *testing.T) {
	concept, pages := conceptWithLaterRubricOnEarlierPage()
	h := fragmentsHandler(concept, pages, nil, nil)

	_, body := fragmentsRequest(t, h, "")

	if len(body.Entries) != 2 {
		t.Fatalf("записей %d, ожидалось 2", len(body.Entries))
	}
	if body.Entries[0].Rubric != "определение" || body.Entries[1].Rubric != "и конкретный (полезный) труд" {
		t.Fatalf("порядок подрубрик: %q, %q", body.Entries[0].Rubric, body.Entries[1].Rubric)
	}
}

func TestFragmentsOrderPageKeepsPageOrder(t *testing.T) {
	concept, pages := conceptWithLaterRubricOnEarlierPage()
	h := fragmentsHandler(concept, pages, nil, nil)

	_, body := fragmentsRequest(t, h, "?order=page")

	if len(body.Entries) != 2 || body.Entries[0].PrintedStart != 700 {
		t.Fatalf("порядок по страницам: %+v", body.Entries)
	}
}

// TestFragmentsJunkOrderFallsBackToDefault: порядок показа — не команда, и
// мусор в нём не повод отказывать в ответе. То же правило, что у фильтров.
func TestFragmentsJunkOrderFallsBackToDefault(t *testing.T) {
	concept, pages := conceptWithLaterRubricOnEarlierPage()
	h := fragmentsHandler(concept, pages, nil, nil)

	rec, body := fragmentsRequest(t, h, "?order=по-настроению")

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d", rec.Code)
	}
	if len(body.Entries) != 2 || body.Entries[0].Rubric != "определение" {
		t.Fatalf("мусорный порядок дал %+v", body.Entries)
	}
}

// TestFragmentsFiltersByReferenceID: задача 13a — сужение до одного адреса
// нужно клиенту, чтобы перезапросить ровно исправленную запись после
// сохранения границ, не трогая офсет и total остального потока.
func TestFragmentsFiltersByReferenceID(t *testing.T) {
	pages := &pageStub{pages: []*models.Page{
		{ID: 9143, WorkID: 14, PageNumber: 730},
		{ID: 9144, WorkID: 14, PageNumber: 731},
	}}
	h := fragmentsHandler(conceptWithTwoRubrics(), pages, nil, nil)

	rec, body := fragmentsRequest(t, h, "?reference_id=541")

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d; тело: %s", rec.Code, rec.Body.String())
	}
	if body.Total != 1 || len(body.Entries) != 1 {
		t.Fatalf("total=%d, записей %d, ожидалась ровно одна", body.Total, len(body.Entries))
	}
	if body.Entries[0].ReferenceID != 541 {
		t.Fatalf("reference_id %d, ожидался 541", body.Entries[0].ReferenceID)
	}
}

// TestFragmentsReferenceIDOfAnotherConceptIsEmpty: адрес и понятие
// проверяются раздельно — id, не принадлежащий этому понятию (например,
// адрес другой статьи или вовсе мусор), даёт пустой поток, а не ошибку.
func TestFragmentsReferenceIDOfAnotherConceptIsEmpty(t *testing.T) {
	pages := &pageStub{pages: []*models.Page{
		{ID: 9143, WorkID: 14, PageNumber: 730},
		{ID: 9144, WorkID: 14, PageNumber: 731},
	}}
	h := fragmentsHandler(conceptWithTwoRubrics(), pages, nil, nil)

	rec, body := fragmentsRequest(t, h, "?reference_id=99999")

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d; тело: %s", rec.Code, rec.Body.String())
	}
	if body.Total != 0 || len(body.Entries) != 0 {
		t.Fatalf("total=%d, записей %d, ожидался пустой поток", body.Total, len(body.Entries))
	}
	if !strings.Contains(rec.Body.String(), `"entries":[]`) {
		t.Fatalf("пустой поток отдан как %s", rec.Body.String())
	}
}

// TestFragmentsReferenceIDCombinesWithRubric: сужение по адресу сочетается с
// прочими фильтрами потока — обе проверки обязаны совпасть одновременно.
func TestFragmentsReferenceIDCombinesWithRubric(t *testing.T) {
	pages := &pageStub{pages: []*models.Page{
		{ID: 9143, WorkID: 14, PageNumber: 730},
		{ID: 9144, WorkID: 14, PageNumber: 731},
	}}
	h := fragmentsHandler(conceptWithTwoRubrics(), pages, nil, nil)

	// Адрес 473 существует, но в другой подрубрике — сочетание фильтров
	// обязано дать пусто, а не проигнорировать один из них.
	rec, body := fragmentsRequest(t, h, "?reference_id=473&rubric="+url.QueryEscape("и конкретный (полезный) труд"))
	if rec.Code != http.StatusOK || body.Total != 0 {
		t.Fatalf("несовпавшая подрубрика: код %d, total %d", rec.Code, body.Total)
	}

	_, matched := fragmentsRequest(t, h, "?reference_id=473&rubric="+url.QueryEscape("определение"))
	if matched.Total != 1 || len(matched.Entries) != 1 || matched.Entries[0].ReferenceID != 473 {
		t.Fatalf("совпавшая подрубрика: total=%d, %+v", matched.Total, matched.Entries)
	}
}

func TestFragmentsAddressWithoutPagesDropsButCounts(t *testing.T) {
	// Страницы адреса исчезли между разрешением и выборкой тел: запись
	// выпадает, но слот в total остаётся — на нём держится пагинация
	// (hasMoreSlots на фронте написан ровно про этот случай).
	pages := &pageStub{}
	h := fragmentsHandler(conceptWithTwoRubrics(), pages, nil, nil)

	_, body := fragmentsRequest(t, h, "")

	if body.Total != 2 {
		t.Fatalf("total=%d", body.Total)
	}
	if len(body.Entries) != 0 {
		t.Fatalf("записей %d, ожидалось 0", len(body.Entries))
	}
}

func TestFragmentsCapsLimit(t *testing.T) {
	// Шестьдесят разных адресов — заведомо больше потолка в 50, каждый на
	// своей странице. Фикстура вроде conceptWithTwoRubrics (две записи) не
	// годится: она сама меньше потолка, и ?limit=5000 обрежется длиной среза,
	// а не потолком, — тест прошёл бы даже с полностью удалённым ограничением.
	concept := &models.IndexConcept{
		ID: 8, Title: "Абстрактный труд", Slug: "abstraktnyj-trud",
	}
	var refs []*models.IndexReference
	for i := 0; i < 60; i++ {
		printed := 700 + i
		refs = append(refs, &models.IndexReference{
			ID: int64(1000 + i), VolumeNumber: 12,
			PageStart: printed, PageEnd: printed, Rubric: "определение", OrderNumber: i,
		})
	}
	concept.Articles = []*models.IndexArticle{
		{ID: 1, ConceptID: 8, EditionID: 3, Kind: models.IndexConceptKindArticle, References: refs},
	}
	pages := &pageStub{}
	h := fragmentsHandler(concept, pages, nil, nil)

	_, body := fragmentsRequest(t, h, "?limit=5000")

	// Потолок 50: запрос на пять тысяч записей никто не должен уметь выдать.
	if len(pages.askedFor) != 50 {
		t.Fatalf("в сторадж ушло %d номеров, ожидалось 50 — потолок limit", len(pages.askedFor))
	}
	if body.Total != 60 {
		t.Fatalf("total=%d, ожидалось 60 — счёт до пагинации", body.Total)
	}
}

// TestFragmentsCoverAllArticlesOfConcept — живой случай: одно понятие раскрыто
// в двух собраниях, каждая статья своей карты томов, и НОМЕРА ТОМОВ
// СОВПАДАЮТ: у обоих изданий есть том 23, но он ведёт в разные работы —
// собрания нумеруют тома заново с единицы, и совпадение номера между ними не
// исключение, а рутина (том 23 есть и у Маркса, и у Ленина). Прежний код брал
// издание единственной работы-указателя понятия и терял вторую статью
// целиком; conceptAddresses обязан не только собрать адреса ОБЕИХ статей, но
// и развести их строго по СВОИМ изданиям — общий словарь по номеру тома
// подменил бы адрес одной статьи работой чужого издания молча, без ошибки и
// без признака.
func TestFragmentsCoverAllArticlesOfConcept(t *testing.T) {
	w1, w2 := int64(40), int64(98)
	store := &fakeIndexStore{concept: &models.IndexConcept{
		ID: 8, Slug: "trud",
		Articles: []*models.IndexArticle{
			{ID: 1, EditionID: 1, WorkID: &w1, Kind: "article", References: []*models.IndexReference{
				{ID: 1, VolumeNumber: 23, PageStart: 730, PageEnd: 730, Rubric: "определение", OrderNumber: 1},
			}},
			{ID: 2, EditionID: 4, WorkID: &w2, Kind: "article", References: []*models.IndexReference{
				{ID: 2, VolumeNumber: 23, PageStart: 12, PageEnd: 12, Rubric: "сущность", OrderNumber: 1},
			}},
		},
	}}
	// Карты томов разных изданий: у ОБОИХ есть том 23 (та самая коллизия), но
	// он ведёт в разные работы. Слияние карт с правилом «первое издание
	// выигрывает» подменило бы работу второй статьи работой первой — тест
	// ниже это ловит по work_id каждой записи, а не только по их числу.
	store.volumesByEdition = map[int64][]models.VolumeLocation{
		1: {{VolumeNumber: 23, WorkID: 7, MaxPage: 900}},
		4: {{VolumeNumber: 23, WorkID: 96, MaxPage: 434}},
	}
	// Тела страниц обеих работ — иначе записи отсеются в entryContexts как
	// «страницы не нашлись», и byRubric ниже сверял бы пустоту с пустотой.
	pages := &pageStub{pages: []*models.Page{
		{ID: 501, WorkID: 7, PageNumber: 730, ContentMarkdown: "текст издания 1"},
		{ID: 502, WorkID: 96, PageNumber: 12, ContentMarkdown: "текст издания 4"},
	}}
	h := NewIndexHandler(store, &fakeWorkGetter{}, &fakeEditionStore{}, pages, &fakeChapterTrees{}, newFakeFragmentStore(), markdown.NewRenderer())

	rec, body := fragmentsRequest(t, h, "")

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d; тело: %s", rec.Code, rec.Body.String())
	}
	if body.Total != 2 {
		t.Fatalf("total %d, хотели 2 (по адресу из каждой статьи)", body.Total)
	}

	byRubric := map[string]int64{}
	for _, e := range body.Entries {
		byRubric[e.Rubric] = e.WorkID
	}
	// Обе подрубрики сидят на томе 23, но в разных изданиях — правильный
	// результат называет РАЗНЫЕ работы. Одинаковый work_id здесь и означал бы
	// смешение карт.
	if byRubric["определение"] != 7 {
		t.Fatalf("статья издания 1 (том 23) ушла в работу %d, ожидалась 7 — работа СВОЕГО издания", byRubric["определение"])
	}
	if byRubric["сущность"] != 96 {
		t.Fatalf("статья издания 4 (том 23) ушла в работу %d, ожидалась 96 — работа СВОЕГО издания, а не соседнего", byRubric["сущность"])
	}
}

func TestFragmentsEmptyForRedirectAndUnlinkedWork(t *testing.T) {
	// Отсылка: вид теперь свойство статьи, не понятия, и у отсылочной статьи
	// нет собственных адресов — conceptAddresses не находит что собирать.
	redirect := &models.IndexConcept{
		ID: 7, Title: "Абсолютное и относительное", Slug: "absolyutnoe",
		Articles: []*models.IndexArticle{
			{ID: 2, ConceptID: 7, EditionID: 3, Kind: models.IndexConceptKindRedirect},
		},
	}
	h := fragmentsHandler(redirect, &pageStub{}, nil, nil)
	rec, body := fragmentsRequest(t, h, "")
	if rec.Code != http.StatusOK || body.Total != 0 {
		t.Fatalf("отсылка: код %d, total %d", rec.Code, body.Total)
	}

	// Понятие без единой статьи (осиротевший ввоз) — адреса разрешать нечем;
	// пути «через работу-указателя к изданию» для этого больше не
	// существует — у внешнего источника такой работы может не быть вовсе.
	store := &fakeIndexStore{concept: &models.IndexConcept{ID: 9, Slug: "sirota"}}
	h2 := NewIndexHandler(store, &fakeWorkGetter{}, &fakeEditionStore{}, &pageStub{}, &fakeChapterTrees{}, newFakeFragmentStore(), markdown.NewRenderer())
	rec2, body2 := fragmentsRequest(t, h2, "")
	if rec2.Code != http.StatusOK || body2.Total != 0 {
		t.Fatalf("понятие без статей: код %d, total %d", rec2.Code, body2.Total)
	}
	if !strings.Contains(rec2.Body.String(), `"entries":[]`) {
		t.Fatalf("тело: %s", rec2.Body.String())
	}
}

func TestFragmentsUnknownSlugIs404(t *testing.T) {
	h := NewIndexHandler(&fakeIndexStore{}, &fakeWorkGetter{}, &fakeEditionStore{}, &pageStub{}, &fakeChapterTrees{}, newFakeFragmentStore(), markdown.NewRenderer())
	rec, _ := fragmentsRequest(t, h, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("код %d, ожидался 404", rec.Code)
	}
}

func putCutsRequestFor(t *testing.T, h *IndexHandler, refID string, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut,
		"/api/concepts/abstraktnyj-trud/references/"+refID+"/cuts", strings.NewReader(body))
	req = mux.SetURLVars(req, map[string]string{"slug": "abstraktnyj-trud", "refId": refID})
	rec := httptest.NewRecorder()
	h.PutCuts(rec, req)
	return rec
}

func TestPutCutsStoresQuotesAndHashes(t *testing.T) {
	body730 := "Первый абзац.\n\nВот здесь понятие раскрывается.\n\nХвост."
	pages := &pageStub{pages: []*models.Page{
		{ID: 9143, WorkID: 14, PageNumber: 730, ContentMarkdown: body730},
		{ID: 9144, WorkID: 14, PageNumber: 731, ContentMarkdown: "продолжение"},
	}}
	frags := newFakeFragmentStore()
	h := fragmentsHandler(conceptWithTwoRubrics(), pages, nil, frags)

	start := strings.Index(body730, "Вот здесь")
	end := start + len("Вот здесь понятие раскрывается.")
	rec := putCutsRequestFor(t, h, "473", fmt.Sprintf(
		`{"status":"machine","cuts":[{"start_page":730,"start_offset":%d,"end_page":730,"end_offset":%d}]}`,
		start, end))

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d; тело: %s", rec.Code, rec.Body.String())
	}
	stored := frags.replaced[473]
	if len(stored) != 1 {
		t.Fatalf("записано %d вырезок", len(stored))
	}
	f := stored[0]
	if f.StartPageID != 9143 || f.EndPageID != 9143 {
		t.Fatalf("страницы вырезки: %d, %d", f.StartPageID, f.EndPageID)
	}
	// Цитаты и хеши снимает сервер: клиент не должен уметь соврать о том,
	// что он вырезал.
	if f.HeadQuote == "" || f.TailQuote == "" {
		t.Fatalf("цитаты пусты: %+v", f)
	}
	if !strings.HasPrefix(f.HeadQuote, "Вот здесь") {
		t.Fatalf("головная цитата %q", f.HeadQuote)
	}
	if f.StartHash != pageHash(body730) || f.EndHash != pageHash(body730) {
		t.Fatalf("хеши не сняты со страницы: %+v", f)
	}
	if f.Status != models.FragmentStatusMachine {
		t.Fatalf("статус %q", f.Status)
	}

	// Ответ PUT — та же форма потока: bounds нужны там же, где id, чтобы
	// повторное открытие редактора без перезапроса видело настоящие границы.
	var resp putCutsBody
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("разбор ответа: %v; тело: %s", err, rec.Body.String())
	}
	if len(resp.Cuts) != 1 || resp.Cuts[0].Bounds == nil {
		t.Fatalf("ответ без bounds: %+v", resp.Cuts)
	}
	if resp.Cuts[0].Bounds.StartPageID != 9143 || resp.Cuts[0].Bounds.StartOffset != start ||
		resp.Cuts[0].Bounds.EndPageID != 9143 || resp.Cuts[0].Bounds.EndOffset != end {
		t.Fatalf("bounds ответа: %+v, ожидалось start=%d end=%d", resp.Cuts[0].Bounds, start, end)
	}
}

type putCutsBody struct {
	State string `json:"state"`
	Cuts  []struct {
		ID     *int64 `json:"id"`
		Bounds *struct {
			StartPageID int64 `json:"start_page_id"`
			StartOffset int   `json:"start_offset"`
			EndPageID   int64 `json:"end_page_id"`
			EndOffset   int   `json:"end_offset"`
		} `json:"bounds"`
		Status string `json:"status"`
	} `json:"cuts"`
	StaleCuts []fragmentsCutBody `json:"stale_cuts"`
}

// TestPutCutsPerCutStatusOverridesRequestStatus: набор пересылается целиком,
// и единый статус на весь запрос пометил бы уже машинную вырезку как
// подтверждённую человеком только потому, что рядом правится другая. Вырезка
// со своим status побеждает; вырезка без него наследует статус запроса —
// так нарезчик, шлющий один статус на пачку, продолжает работать неизменным.
func TestPutCutsPerCutStatusOverridesRequestStatus(t *testing.T) {
	body730 := "Первый абзац.\n\nВторой абзац подлиннее."
	body731 := "Третий абзац страницы."
	pages := &pageStub{pages: []*models.Page{
		{ID: 9143, WorkID: 14, PageNumber: 730, ContentMarkdown: body730},
		{ID: 9144, WorkID: 14, PageNumber: 731, ContentMarkdown: body731},
	}}
	frags := newFakeFragmentStore()
	h := fragmentsHandler(conceptWithTwoRubrics(), pages, nil, frags)

	firstEnd := len("Первый абзац.")
	rec := putCutsRequestFor(t, h, "473", fmt.Sprintf(`{"status":"confirmed","cuts":[
		{"start_page":730,"start_offset":0,"end_page":730,"end_offset":%d,"status":"machine"},
		{"start_page":731,"start_offset":0,"end_page":731,"end_offset":%d}
	]}`, firstEnd, len(body731)))

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d; тело: %s", rec.Code, rec.Body.String())
	}
	stored := frags.replaced[473]
	if len(stored) != 2 {
		t.Fatalf("сохранено %d вырезок, ожидалось 2", len(stored))
	}
	if stored[0].Status != models.FragmentStatusMachine {
		t.Fatalf("первая вырезка со своим статусом: %q, ожидался machine", stored[0].Status)
	}
	if stored[1].Status != models.FragmentStatusConfirmed {
		t.Fatalf("вторая вырезка без своего статуса: %q, ожидался confirmed уровня запроса", stored[1].Status)
	}
}

// TestPutCutsRejectsBadPerCutStatus: per-cut статус проверяется так же
// строго, как и статус уровня запроса — stale им не поставить.
func TestPutCutsRejectsBadPerCutStatus(t *testing.T) {
	pages := &pageStub{pages: []*models.Page{{ID: 9143, WorkID: 14, PageNumber: 730, ContentMarkdown: "текст"}}}
	h := fragmentsHandler(conceptWithTwoRubrics(), pages, nil, newFakeFragmentStore())

	rec := putCutsRequestFor(t, h, "473",
		`{"status":"machine","cuts":[{"start_page":730,"start_offset":0,"end_page":730,"end_offset":2,"status":"stale"}]}`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("код %d, ожидался 400; тело %s", rec.Code, rec.Body.String())
	}
}

// TestPutCutsKeepsUntouchedCutByIDAndCreatesNew: набор пересылается целиком,
// но нетронутую вырезку клиент не обязан пересказывать границами — id
// достаточно, и сервер переносит её как есть (границы, цитаты, хеши, статус),
// игнорируя прочие поля запроса для этого элемента. Рядом создаётся новая
// вырезка границами — обе формы уживаются в одном запросе.
func TestPutCutsKeepsUntouchedCutByIDAndCreatesNew(t *testing.T) {
	pages := &pageStub{pages: []*models.Page{
		{ID: 9143, WorkID: 14, PageNumber: 730, ContentMarkdown: "Первый абзац. Второй абзац."},
		{ID: 9144, WorkID: 14, PageNumber: 731, ContentMarkdown: "Третий абзац страницы."},
	}}
	frags := newFakeFragmentStore()
	frags.byRefs[473] = []*models.IndexFragment{{
		ID: 91, ReferenceID: 473,
		StartPageID: 9143, StartOffset: 5, EndPageID: 9143, EndOffset: 12,
		HeadQuote: "прежняя голова", TailQuote: "прежний хвост",
		StartHash: "прежний-хеш-начала", EndHash: "прежний-хеш-конца",
		Status: models.FragmentStatusMachine,
	}}
	h := fragmentsHandler(conceptWithTwoRubrics(), pages, nil, frags)

	// Статус запроса — confirmed, но у вырезки-ссылки его быть не должно:
	// человек её не видел и не трогал.
	rec := putCutsRequestFor(t, h, "473", `{"status":"confirmed","cuts":[
		{"id":91},
		{"start_page":731,"start_offset":0,"end_page":731,"end_offset":13}
	]}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d; тело: %s", rec.Code, rec.Body.String())
	}
	stored := frags.replaced[473]
	if len(stored) != 2 {
		t.Fatalf("сохранено %d вырезок, ожидалось 2", len(stored))
	}
	kept := stored[0]
	if kept.StartOffset != 5 || kept.EndOffset != 12 || kept.HeadQuote != "прежняя голова" ||
		kept.TailQuote != "прежний хвост" || kept.StartHash != "прежний-хеш-начала" ||
		kept.EndHash != "прежний-хеш-конца" || kept.Status != models.FragmentStatusMachine {
		t.Fatalf("вырезка по id изменилась: %+v", kept)
	}
	created := stored[1]
	if created.StartPageID != 9144 || created.EndPageID != 9144 || created.Status != models.FragmentStatusConfirmed {
		t.Fatalf("новая вырезка: %+v", created)
	}
}

// TestPutCutsStoresInPagePositionOrderRegardlessOfRequestOrder: находка
// финального разбора — order_number раньше следовал порядку среза (порядку
// в запросе), а не позиции на странице. Правка страницы 731 пересылает
// нетронутую вырезку страницы 730 по id ПОСЛЕ новой вырезки страницы 731 (так
// её отдал useCutEditor: сначала foreign, потом editable) — сохранённый
// порядок обязан остаться «730 раньше 731», иначе разворот адреса покажет
// вторую страницу перед первой.
func TestPutCutsStoresInPagePositionOrderRegardlessOfRequestOrder(t *testing.T) {
	pages := &pageStub{pages: []*models.Page{
		{ID: 9143, WorkID: 14, PageNumber: 730, ContentMarkdown: "Первый абзац. Второй абзац."},
		{ID: 9144, WorkID: 14, PageNumber: 731, ContentMarkdown: "Третий абзац страницы."},
	}}
	frags := newFakeFragmentStore()
	frags.byRefs[473] = []*models.IndexFragment{{
		ID: 91, ReferenceID: 473,
		StartPageID: 9143, StartOffset: 5, EndPageID: 9143, EndOffset: 12,
		Status: models.FragmentStatusMachine,
	}}
	h := fragmentsHandler(conceptWithTwoRubrics(), pages, nil, frags)

	// Порядок в запросе — страница 731 (новая), затем страница 730 (по id).
	rec := putCutsRequestFor(t, h, "473", `{"status":"confirmed","cuts":[
		{"start_page":731,"start_offset":0,"end_page":731,"end_offset":13},
		{"id":91}
	]}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d; тело: %s", rec.Code, rec.Body.String())
	}
	stored := frags.replaced[473]
	if len(stored) != 2 {
		t.Fatalf("сохранено %d вырезок, ожидалось 2", len(stored))
	}
	if stored[0].StartPageID != 9143 || stored[1].StartPageID != 9144 {
		t.Fatalf("порядок хранения не по позиции на странице: %d, затем %d",
			stored[0].StartPageID, stored[1].StartPageID)
	}
}

// TestPutCutsForeignIDIs404: id, принадлежащий другому адресу того же
// понятия, — не обходной путь стереть его вырезку от имени соседнего адреса.
// 404, а не 400: запрос синтаксически исправен, просто ссылается в никуда.
func TestPutCutsForeignIDIs404(t *testing.T) {
	pages := &pageStub{pages: []*models.Page{{ID: 9143, WorkID: 14, PageNumber: 730, ContentMarkdown: "текст страницы"}}}
	frags := newFakeFragmentStore()
	frags.byRefs[541] = []*models.IndexFragment{{
		ID: 77, ReferenceID: 541,
		StartPageID: 9143, StartOffset: 0, EndPageID: 9143, EndOffset: 5,
		Status: models.FragmentStatusMachine,
	}}
	h := fragmentsHandler(conceptWithTwoRubrics(), pages, nil, frags)

	rec := putCutsRequestFor(t, h, "473", `{"status":"confirmed","cuts":[{"id":77}]}`)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("код %d, ожидался 404; тело %s", rec.Code, rec.Body.String())
	}
}

func TestPutCutsAcceptsConfirmedAndEmptyList(t *testing.T) {
	pages := &pageStub{pages: []*models.Page{{ID: 9143, WorkID: 14, PageNumber: 730, ContentMarkdown: "текст"}}}
	frags := newFakeFragmentStore()
	h := fragmentsHandler(conceptWithTwoRubrics(), pages, nil, frags)

	// end_offset:4 — граница после «те»: кириллица занимает по 2 байта на
	// букву, а нечётный офсет здесь пришёлся бы на середину буквы.
	rec := putCutsRequestFor(t, h, "473",
		`{"status":"confirmed","cuts":[{"start_page":730,"start_offset":0,"end_page":730,"end_offset":4}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("confirmed: код %d, тело %s", rec.Code, rec.Body.String())
	}
	if frags.replaced[473][0].Status != models.FragmentStatusConfirmed {
		t.Fatalf("статус %q", frags.replaced[473][0].Status)
	}

	// Пустой список — законное «нарезке не поддался», а не ошибка.
	rec = putCutsRequestFor(t, h, "473", `{"status":"machine","cuts":[]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("пустой список: код %d", rec.Code)
	}
	if len(frags.replaced[473]) != 0 {
		t.Fatalf("после стирания осталось %d", len(frags.replaced[473]))
	}
}

// TestPutCutsMissingCutsKeyIs400: находка финального разбора — отсутствие
// ключа cuts раньше вело себя как cuts:[] и молча стирало набор. «Стереть» и
// «прислал не то» должны различаться: явный пустой список стирает, а
// отсутствие ключа — 400, без единого обращения к хранилищу.
func TestPutCutsMissingCutsKeyIs400(t *testing.T) {
	pages := &pageStub{pages: []*models.Page{{ID: 9143, WorkID: 14, PageNumber: 730, ContentMarkdown: "текст"}}}
	frags := newFakeFragmentStore()
	frags.byRefs[473] = []*models.IndexFragment{{
		ID: 91, ReferenceID: 473, StartPageID: 9143, StartOffset: 0, EndPageID: 9143, EndOffset: 2,
		Status: models.FragmentStatusMachine,
	}}
	h := fragmentsHandler(conceptWithTwoRubrics(), pages, nil, frags)

	rec := putCutsRequestFor(t, h, "473", `{"status":"machine"}`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("код %d, ожидался 400; тело %s", rec.Code, rec.Body.String())
	}
	if _, replaced := frags.replaced[473]; replaced {
		t.Fatal("набор переписан при отсутствующем ключе cuts")
	}
}

func TestPutCutsRejectsBadBoundaries(t *testing.T) {
	body := "абвгдежзий"
	pages := &pageStub{pages: []*models.Page{
		{ID: 9143, WorkID: 14, PageNumber: 730, ContentMarkdown: body},
		{ID: 9144, WorkID: 14, PageNumber: 731, ContentMarkdown: "продолжение"},
	}}
	h := fragmentsHandler(conceptWithTwoRubrics(), pages, nil, newFakeFragmentStore())

	cases := []struct{ name, payload string }{
		{"конец за длиной", `{"status":"machine","cuts":[{"start_page":730,"start_offset":0,"end_page":730,"end_offset":9999}]}`},
		{"перевёрнутый диапазон", `{"status":"machine","cuts":[{"start_page":730,"start_offset":8,"end_page":730,"end_offset":2}]}`},
		{"страница не из адреса", `{"status":"machine","cuts":[{"start_page":999,"start_offset":0,"end_page":999,"end_offset":2}]}`},
		{"конец раньше начала по страницам", `{"status":"machine","cuts":[{"start_page":731,"start_offset":0,"end_page":730,"end_offset":2}]}`},
		{"отрицательное смещение", `{"status":"machine","cuts":[{"start_page":730,"start_offset":-1,"end_page":730,"end_offset":2}]}`},
		{"неизвестный статус", `{"status":"выдумка","cuts":[]}`},
	}
	for _, c := range cases {
		rec := putCutsRequestFor(t, h, "473", c.payload)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: код %d, ожидался 400; тело %s", c.name, rec.Code, rec.Body.String())
		}
	}
}

func TestPutCutsRejectsOffsetInsideRune(t *testing.T) {
	// «абв» — по два байта на букву; смещение 1 стоит посреди буквы.
	// Такой срез дал бы небайтовый мусор, который потом никогда не найдётся.
	pages := &pageStub{pages: []*models.Page{{ID: 9143, WorkID: 14, PageNumber: 730, ContentMarkdown: "абв"}}}
	h := fragmentsHandler(conceptWithTwoRubrics(), pages, nil, newFakeFragmentStore())

	rec := putCutsRequestFor(t, h, "473",
		`{"status":"machine","cuts":[{"start_page":730,"start_offset":1,"end_page":730,"end_offset":4}]}`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("код %d, ожидался 400", rec.Code)
	}
}

func TestPutCutsRejectsForeignReference(t *testing.T) {
	pages := &pageStub{pages: []*models.Page{{ID: 9143, WorkID: 14, PageNumber: 730, ContentMarkdown: "текст"}}}
	h := fragmentsHandler(conceptWithTwoRubrics(), pages, nil, newFakeFragmentStore())

	rec := putCutsRequestFor(t, h, "99999", `{"status":"machine","cuts":[]}`)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("код %d, ожидался 404", rec.Code)
	}
}

func TestPutCutsAcrossPagesIsAccepted(t *testing.T) {
	tail := "и её конец"
	pages := &pageStub{pages: []*models.Page{
		{ID: 9143, WorkID: 14, PageNumber: 730, ContentMarkdown: "начало мысли"},
		{ID: 9144, WorkID: 14, PageNumber: 731, ContentMarkdown: tail},
	}}
	frags := newFakeFragmentStore()
	h := fragmentsHandler(conceptWithTwoRubrics(), pages, nil, frags)

	// end_offset — конец страницы 731 в БАЙТАХ (len(tail)), а не в рунах:
	// кириллица занимает по 2 байта на букву, и счёт «символами» (10)
	// обрезал бы «конец» до одной буквы «к».
	rec := putCutsRequestFor(t, h, "473", fmt.Sprintf(
		`{"status":"machine","cuts":[{"start_page":730,"start_offset":0,"end_page":731,"end_offset":%d}]}`,
		len(tail)))

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d; тело: %s", rec.Code, rec.Body.String())
	}
	f := frags.replaced[473][0]
	// Хвостовая цитата берётся с ПОСЛЕДНЕЙ страницы, головная — с первой.
	if f.StartPageID != 9143 || f.EndPageID != 9144 {
		t.Fatalf("страницы: %d, %d", f.StartPageID, f.EndPageID)
	}
	if !strings.HasPrefix(f.HeadQuote, "начало") || !strings.HasSuffix(f.TailQuote, "конец") {
		t.Fatalf("цитаты: %q / %q", f.HeadQuote, f.TailQuote)
	}
	if f.StartHash != pageHash("начало мысли") || f.EndHash != pageHash("и её конец") {
		t.Fatalf("хеши сняты не с тех страниц: %+v", f)
	}
}

// Работа концепта не найдена и работа без собрания — разные ветки кода, но
// одна и та же причина отказа с точки зрения клиента: адрес нерешаем, а не
// прислан неправильно. Оба случая — 404, как и «том адреса не загружен»
// ниже, и как у ExpandPage в этом же файле: 400 остаётся строго за битым
// запросом (JSON, смещения не на границе руны, недопустимый статус).
// Прежде «без собрания» отвечал 400 — тот выбор был точечным, до появления
// ExpandPage рядом; расхождение двух ручек на одном и том же состоянии
// данных обманывало бы клиента, ветвящегося по коду ответа.

// TestPutCutsUnknownReferenceIs404 бьёт по ветке «адрес не найден среди
// собранных conceptAddresses»: у понятия нет ни одной статьи, значит и
// адресов нет вовсе — работы-указателя, через которую раньше искалось
// издание, здесь больше не спрашивают.
func TestPutCutsUnknownReferenceIs404(t *testing.T) {
	store := &fakeIndexStore{concept: &models.IndexConcept{ID: 8, Slug: "abstraktnyj-trud"}}
	h := NewIndexHandler(store, &fakeWorkGetter{}, &fakeEditionStore{}, &pageStub{}, &fakeChapterTrees{}, newFakeFragmentStore(), markdown.NewRenderer())

	rec := putCutsRequestFor(t, h, "473", `{"status":"machine","cuts":[]}`)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("код %d, ожидался 404; тело %s", rec.Code, rec.Body.String())
	}
	// Тело, не только код: сосед ниже (TestPutCutsUnloadedVolumeIs404) тоже
	// даёт 404, но по другой причине, и при случайной перестановке веток
	// проверка одного кода подмену не заметит.
	if !strings.Contains(rec.Body.String(), "Reference not found") {
		t.Fatalf("тело: %s", rec.Body.String())
	}
}

// TestPutCutsUnloadedVolumeIs404 бьёт по соседней ветке: адрес у статьи есть
// (том 12, ссылка 473), но фейковое хранилище не отдаёт под издание статьи
// ни одной карты томов — VolumeMap() возвращает пусто, и запрошенный том не
// резолвится.
func TestPutCutsUnloadedVolumeIs404(t *testing.T) {
	store := &fakeIndexStore{concept: conceptWithTwoRubrics()}
	h := NewIndexHandler(store, &fakeWorkGetter{}, &fakeEditionStore{}, &pageStub{}, &fakeChapterTrees{}, newFakeFragmentStore(), markdown.NewRenderer())

	rec := putCutsRequestFor(t, h, "473", `{"status":"machine","cuts":[]}`)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("код %d, ожидался 404; тело %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Volume of this address is not loaded") {
		t.Fatalf("тело: %s", rec.Body.String())
	}
}

type expandBody struct {
	PageID      int64  `json:"page_id"`
	PageNumber  int    `json:"page_number"`
	PrintedPage int    `json:"printed_page"`
	PageStatus  string `json:"page_status"`
	Markdown    string `json:"markdown"`
	Chunks      []struct {
		HTML   string `json:"html"`
		Inside bool   `json:"inside"`
	} `json:"chunks"`
}

func expandRequest(t *testing.T, h *IndexHandler, refID, pageID string) (*httptest.ResponseRecorder, expandBody) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet,
		"/api/concepts/abstraktnyj-trud/references/"+refID+"/pages/"+pageID, nil)
	req = mux.SetURLVars(req, map[string]string{
		"slug": "abstraktnyj-trud", "refId": refID, "pageId": pageID,
	})
	rec := httptest.NewRecorder()
	h.ExpandPage(rec, req)

	var body expandBody
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("разбор: %v; тело: %s", err, rec.Body.String())
		}
	}
	return rec, body
}

func TestExpandPageMarksCutChunks(t *testing.T) {
	body730 := "Первый абзац.\n\nВот вырезка.\n\nХвост."
	pages := &pageStub{pages: []*models.Page{
		{ID: 9143, WorkID: 14, PageNumber: 730, ContentMarkdown: body730, Status: models.PageStatusProofread},
	}}
	start := strings.Index(body730, "Вот вырезка.")
	frags := newFakeFragmentStore()
	frags.byRefs[473] = []*models.IndexFragment{{
		ID: 91, ReferenceID: 473, StartPageID: 9143, StartOffset: start,
		EndPageID: 9143, EndOffset: start + len("Вот вырезка."),
		Status: models.FragmentStatusMachine,
	}}
	h := fragmentsHandler(conceptWithTwoRubrics(), pages, nil, frags)

	rec, got := expandRequest(t, h, "473", "9143")

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d; тело %s", rec.Code, rec.Body.String())
	}
	if got.Markdown != body730 {
		t.Fatalf("исходник не отдан целиком: %q", got.Markdown)
	}
	if len(got.Chunks) != 3 {
		t.Fatalf("кусков %d: %+v", len(got.Chunks), got.Chunks)
	}
	if got.Chunks[0].Inside || !got.Chunks[1].Inside || got.Chunks[2].Inside {
		t.Fatalf("флаги кусков: %+v", got.Chunks)
	}
	if !strings.Contains(got.Chunks[1].HTML, "Вот вырезка") {
		t.Fatalf("средний кусок: %q", got.Chunks[1].HTML)
	}
	if got.PrintedPage != 730 || got.PageStatus != "вычитана" {
		t.Fatalf("шапка: %d / %q", got.PrintedPage, got.PageStatus)
	}
}

func TestExpandPageWithoutCutsIsOneChunk(t *testing.T) {
	pages := &pageStub{pages: []*models.Page{
		{ID: 9143, WorkID: 14, PageNumber: 730, ContentMarkdown: "просто страница"},
	}}
	h := fragmentsHandler(conceptWithTwoRubrics(), pages, nil, nil)

	_, got := expandRequest(t, h, "473", "9143")

	if len(got.Chunks) != 1 || got.Chunks[0].Inside {
		t.Fatalf("куски: %+v", got.Chunks)
	}
}

func TestExpandPageIgnoresOtherAddressCuts(t *testing.T) {
	// Подсвечиваются вырезки ИМЕННО этого адреса: у соседней подрубрики на
	// той же странице своё место, и мешать их значит врать читателю.
	pages := &pageStub{pages: []*models.Page{
		{ID: 9143, WorkID: 14, PageNumber: 730, ContentMarkdown: "аааааааааа"},
	}}
	frags := newFakeFragmentStore()
	frags.byRefs[541] = []*models.IndexFragment{{
		ID: 92, ReferenceID: 541, StartPageID: 9143, StartOffset: 0,
		EndPageID: 9143, EndOffset: 5, Status: models.FragmentStatusMachine,
	}}
	h := fragmentsHandler(conceptWithTwoRubrics(), pages, nil, frags)

	_, got := expandRequest(t, h, "473", "9143")

	if len(got.Chunks) != 1 || got.Chunks[0].Inside {
		t.Fatalf("подсвечены чужие вырезки: %+v", got.Chunks)
	}
}

func TestExpandPageRejectsForeignPageAndReference(t *testing.T) {
	pages := &pageStub{pages: []*models.Page{
		{ID: 9143, WorkID: 14, PageNumber: 730, ContentMarkdown: "текст"},
		{ID: 7000, WorkID: 99, PageNumber: 12, ContentMarkdown: "чужая работа"},
	}}
	h := fragmentsHandler(conceptWithTwoRubrics(), pages, nil, nil)

	if rec, _ := expandRequest(t, h, "99999", "9143"); rec.Code != http.StatusNotFound {
		t.Fatalf("чужой адрес: код %d", rec.Code)
	}
	if rec, _ := expandRequest(t, h, "473", "7000"); rec.Code != http.StatusNotFound {
		t.Fatalf("страница чужой работы: код %d", rec.Code)
	}
	if rec, _ := expandRequest(t, h, "473", "424242"); rec.Code != http.StatusNotFound {
		t.Fatalf("несуществующая страница: код %d", rec.Code)
	}
}

func TestExpandPageRejectsPageOutsideAddress(t *testing.T) {
	// Страница той же работы, но вне диапазона адреса: разворачивать её из-под
	// этого адреса незачем.
	pages := &pageStub{pages: []*models.Page{
		{ID: 9143, WorkID: 14, PageNumber: 730, ContentMarkdown: "текст"},
		{ID: 9200, WorkID: 14, PageNumber: 780, ContentMarkdown: "далеко"},
	}}
	h := fragmentsHandler(conceptWithTwoRubrics(), pages, nil, nil)

	rec, _ := expandRequest(t, h, "473", "9200")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("код %d, ожидался 404", rec.Code)
	}
}

// Адрес шире своих вырезок: указатель называет 730—731, вырезка лежит на
// 730-й. 731-я обязана быть названа в записи — иначе клиенту нечем её
// открыть: разворот адресуется по page_id, а взять его больше неоткуда.
func TestFragmentsEntryListsEveryPageOfTheAddress(t *testing.T) {
	body730 := "Первый абзац страницы.\n\nВот здесь понятие раскрывается."
	pages := &pageStub{pages: []*models.Page{
		{ID: 9143, WorkID: 14, PageNumber: 730, ContentMarkdown: body730, Status: models.PageStatusNotProofread},
		{ID: 9144, WorkID: 14, PageNumber: 731, ContentMarkdown: "продолжение", Status: models.PageStatusProofread},
	}}
	start := strings.Index(body730, "Вот здесь")

	frags := newFakeFragmentStore()
	frags.byRefs[473] = []*models.IndexFragment{{
		ID: 91, ReferenceID: 473, OrderNumber: 1,
		StartPageID: 9143, StartOffset: start, EndPageID: 9143, EndOffset: len(body730),
		Status: models.FragmentStatusMachine,
	}}
	h := fragmentsHandler(conceptWithTwoRubrics(), pages, nil, frags)

	_, got := fragmentsRequest(t, h, "")

	entry := got.Entries[0]
	if len(entry.Pages) != 2 {
		t.Fatalf("страницы адреса: %+v", entry.Pages)
	}
	if entry.Pages[0].PageID != 9143 || entry.Pages[0].PageNumber != 730 {
		t.Fatalf("первая страница адреса: %+v", entry.Pages[0])
	}
	if entry.Pages[1].PageID != 9144 || entry.Pages[1].PageNumber != 731 ||
		entry.Pages[1].PrintedPage != 731 || entry.Pages[1].PageStatus != "вычитана" {
		t.Fatalf("вторая страница адреса: %+v", entry.Pages[1])
	}
	// Список страниц не подменяет вырезки: нарезана по-прежнему одна.
	if len(entry.Cuts) != 1 || len(entry.Cuts[0].Parts) != 1 {
		t.Fatalf("вырезки: %+v", entry.Cuts)
	}
}

// Страница, которой нет в базе, не должна попадать в список: заглушка на неё
// вела бы в разворот, отвечающий 404.
func TestFragmentsAddressPagesSkipPageMissingFromDatabase(t *testing.T) {
	pages := &pageStub{pages: []*models.Page{
		{ID: 9143, WorkID: 14, PageNumber: 730, ContentMarkdown: "текст", Status: models.PageStatusNotProofread},
	}}
	h := fragmentsHandler(conceptWithTwoRubrics(), pages, nil, nil)

	_, got := fragmentsRequest(t, h, "")

	entry := got.Entries[0]
	if len(entry.Pages) != 1 || entry.Pages[0].PageNumber != 730 {
		t.Fatalf("страницы адреса: %+v", entry.Pages)
	}
}

// Колонцифры внутри одной записи обязаны считаться одним смещением: список
// страниц адреса и части вырезки описывают одни и те же страницы, и
// разъехавшиеся номера показали бы читателю две разные нумерации.
func TestFragmentsAddressPagesPrintedNumbersMatchCutParts(t *testing.T) {
	pages := &pageStub{pages: []*models.Page{
		{ID: 9143, WorkID: 14, PageNumber: 730, ContentMarkdown: "текст", Status: models.PageStatusNotProofread},
		{ID: 9144, WorkID: 14, PageNumber: 731, ContentMarkdown: "продолжение", Status: models.PageStatusNotProofread},
	}}
	edID := int64(3)
	store := &fakeIndexStore{
		concept: conceptWithTwoRubrics(),
		volumes: []models.VolumeLocation{{VolumeNumber: 12, WorkID: 14, PageOffset: 0, MaxPage: 800}},
	}
	work := &models.Work{ID: 14, Title: "Экономические рукописи", EditionID: &edID, PageOffset: 4}
	h := NewIndexHandler(store, &fakeWorkGetter{work: work}, &fakeEditionStore{}, pages,
		&fakeChapterTrees{}, newFakeFragmentStore(), markdown.NewRenderer())

	_, got := fragmentsRequest(t, h, "")

	entry := got.Entries[0]
	printedByPage := map[int64]int{}
	for _, p := range entry.Pages {
		printedByPage[p.PageID] = p.PrintedPage
	}
	for _, part := range entry.Cuts[0].Parts {
		if printedByPage[part.PageID] != part.PrintedPage {
			t.Fatalf("страница %d: в pages колонцифра %d, в parts %d",
				part.PageID, printedByPage[part.PageID], part.PrintedPage)
		}
	}
	if printedByPage[9143] != 734 {
		t.Fatalf("колонцифра 730-й страницы при offset=4: %d", printedByPage[9143])
	}
}

// Головная цитата вырезки уходит клиенту: из неё он строит внешний адрес
// места (?quote=). Смещения, хэш и статус остаются внутри — наружу идёт
// только то, что резолвится без строки в нашей БД.
func TestFragmentsCarryHeadQuote(t *testing.T) {
	body730 := "Первый абзац страницы.\n\nВот здесь понятие раскрывается.\n\nХвост страницы."
	pages := &pageStub{pages: []*models.Page{
		{ID: 9143, WorkID: 14, PageNumber: 730, ContentMarkdown: body730, Status: models.PageStatusProofread},
		{ID: 9144, WorkID: 14, PageNumber: 731, ContentMarkdown: "продолжение", Status: models.PageStatusProofread},
	}}
	start := strings.Index(body730, "Вот здесь")
	end := start + len("Вот здесь понятие раскрывается.")

	frags := newFakeFragmentStore()
	frags.byRefs[473] = []*models.IndexFragment{{
		ID: 91, ReferenceID: 473, OrderNumber: 1,
		StartPageID: 9143, StartOffset: start, EndPageID: 9143, EndOffset: end,
		HeadQuote: "Вот здесь понятие",
		Status:    models.FragmentStatusMachine,
	}}
	// Вторая подрубрика (ссылка 541) остаётся без вырезок — её адрес придёт
	// синтетической вырезкой на всю страницу.
	h := fragmentsHandler(conceptWithTwoRubrics(), pages, nil, frags)

	_, got := fragmentsRequest(t, h, "")

	cut := got.Entries[0].Cuts[0]
	if cut.HeadQuote == "" {
		t.Error("вырезка без головной цитаты: внешнего адреса из неё не собрать")
	}

	// Синтетическая вырезка (ненарезанный адрес) якоря не имеет, и цитаты у
	// неё быть не должно.
	synthetic := got.Entries[1].Cuts[0]
	if synthetic.ID == nil && synthetic.HeadQuote != "" {
		t.Error("синтетическая вырезка несёт цитату, которой у неё нет")
	}

	// Отвязавшаяся (stale) вырезка тоже без цитаты наружу: тикет 15 —
	// «у отвязавшейся вырезки внешней ссылки просто нет», якорь у неё не
	// подтверждён переякориванием, и граница вместе с цитатой могла уехать
	// от текущего текста полосы. Фрагмент хранит HeadQuote, но в ответ он
	// не должен попасть.
	staleFrags := newFakeFragmentStore()
	staleFrags.byRefs[473] = []*models.IndexFragment{{
		ID: 92, ReferenceID: 473, StartPageID: 9143, StartOffset: start, EndPageID: 9143, EndOffset: end,
		HeadQuote: "Вот здесь понятие",
		Status:    models.FragmentStatusStale,
	}}
	hStale := fragmentsHandler(conceptWithTwoRubrics(), pages, nil, staleFrags)

	_, gotStale := fragmentsRequest(t, hStale, "")

	staleCut := gotStale.Entries[0].StaleCuts[0]
	if staleCut.HeadQuote != "" {
		t.Error("отвязавшаяся вырезка несёт цитату: якорь не подтверждён переякориванием")
	}
}
