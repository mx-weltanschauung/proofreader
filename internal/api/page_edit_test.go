package api

import (
	"context"
	"testing"

	"proofreader/internal/models"
)

// fakeDocumentCutStore — подставное хранилище вклеек, той же формы, что
// fakeFragmentStore (internal/api/fragment_anchor_test.go): карты вместо
// среза, записывающие, что реально сделал reanchorDocumentCuts.
//
// ByPage на пустом наборе обязан отдавать nil, nil — так же, как настоящий
// *repository.DocumentCutRepository (scanCuts возвращает nil-срез, не
// ошибку, когда строк нет); нулевой fakeDocumentCutStore{} с не
// инициализированными картами уже это умеет: чтение из nil-карты не паникует.
type fakeDocumentCutStore struct {
	byPage   map[int64][]*models.DocumentCut
	anchors  map[int64][4]any
	statuses map[int64]string

	// cuts — сырой список всех заведённых вклеек (не только индекс по
	// странице), нужен ByDocument/ByIDs/Create/DeleteUnreferenced — задачам,
	// которых не было у этого фейка до задачи 9 (переякоривание их не звало).
	cuts        []*models.DocumentCut
	nextID      int64
	created     []*models.DocumentCut
	prunedKeep  []int64
	prunedCalls int
	byIDsErr    error
	createErr   error

	// pagesByCutID — необязательная подмена PagesOfCut по id вклейки: без
	// неё (nil-карта, как у нулевого fakeDocumentCutStore{} и у store,
	// заведённого newFakeDocumentCutStore без последующей правки поля) метод
	// ведёт себя как раньше — отдаёт nil, nil для любой вклейки. Нужна
	// тестам, которым мало проверить СПИСОК уцелевших id после сборки мусора
	// (keptAfterPrune) — а важно, что уцелевшая вклейка реально показывает
	// читателю кусок корпуса, а не просто числится в базе.
	pagesByCutID map[int64][]*models.Page

	// pagesOfCutCalls — сколько раз звали PagesOfCut. Нужен ровно одному
	// тесту (seo_document_source_test.go): карточка превью обязана НЕ
	// собирать тело, а самая дорогая часть сборки — чтение полос каждой
	// вклейки, и её отсутствие и есть разница между карточкой и страницей.
	// Проверять «дёшево ли» иначе нечем: и карточка, и страница возвращают
	// правдоподобный результат, только одна из них платит за него рендером
	// главы.
	pagesOfCutCalls int
}

// newFakeDocumentCutStore заводит карты и индексирует переданные вклейки по
// обеим страницам, за которые они держатся (голова и хвост могут лежать на
// разных полосах — ByPage настоящего репозитория ищет по обеим).
func newFakeDocumentCutStore(cuts ...*models.DocumentCut) *fakeDocumentCutStore {
	store := &fakeDocumentCutStore{
		byPage:   map[int64][]*models.DocumentCut{},
		anchors:  map[int64][4]any{},
		statuses: map[int64]string{},
		cuts:     append([]*models.DocumentCut{}, cuts...),
		nextID:   1,
	}
	for _, c := range cuts {
		if c.StartPageID != 0 {
			store.byPage[c.StartPageID] = append(store.byPage[c.StartPageID], c)
		}
		if c.EndPageID != 0 && c.EndPageID != c.StartPageID {
			store.byPage[c.EndPageID] = append(store.byPage[c.EndPageID], c)
		}
		if c.ID >= store.nextID {
			store.nextID = c.ID + 1
		}
	}
	return store
}

// ByDocument отдаёт все вклейки данного разбора — тем же условием, что и
// настоящий репозиторий (document_id = $1), без обращения к byPage.
func (s *fakeDocumentCutStore) ByDocument(ctx context.Context, documentID int64) ([]*models.DocumentCut, error) {
	var out []*models.DocumentCut
	for _, c := range s.cuts {
		if c.DocumentID == documentID {
			out = append(out, c)
		}
	}
	return out, nil
}

// ByIDs повторяет контракт настоящего репозитория: отдаёт вклейку, только
// если она И названа в ids, И числится за documentID (document_id = $1 AND id
// = ANY($2)) — чужая или несуществующая просто не попадает в результат, без
// ошибки.
func (s *fakeDocumentCutStore) ByIDs(ctx context.Context, documentID int64, ids []int64) ([]*models.DocumentCut, error) {
	if s.byIDsErr != nil {
		return nil, s.byIDsErr
	}
	want := make(map[int64]bool, len(ids))
	for _, id := range ids {
		want[id] = true
	}
	var out []*models.DocumentCut
	for _, c := range s.cuts {
		if c.DocumentID == documentID && want[c.ID] {
			out = append(out, c)
		}
	}
	return out, nil
}

func (s *fakeDocumentCutStore) ByPage(ctx context.Context, pageID int64) ([]*models.DocumentCut, error) {
	return s.byPage[pageID], nil
}

// Create присваивает id (как это делает RETURNING id настоящего
// репозитория) и запоминает сохранённую вклейку — lastCreated() читает её
// напрямую, без похода в базу.
func (s *fakeDocumentCutStore) Create(ctx context.Context, cut *models.DocumentCut) error {
	if s.createErr != nil {
		return s.createErr
	}
	cut.ID = s.nextID
	s.nextID++
	s.cuts = append(s.cuts, cut)
	s.created = append(s.created, cut)
	return nil
}

// DeleteUnreferenced запоминает переданный keep и реально убирает из cuts
// всё, чего в нём нет — keptAfterPrune() проверяет результат уборки, а не
// только сам факт вызова.
func (s *fakeDocumentCutStore) DeleteUnreferenced(ctx context.Context, documentID int64, keep []int64) error {
	s.prunedCalls++
	s.prunedKeep = append([]int64{}, keep...)
	want := make(map[int64]bool, len(keep))
	for _, id := range keep {
		want[id] = true
	}
	var kept []*models.DocumentCut
	for _, c := range s.cuts {
		if c.DocumentID != documentID || want[c.ID] {
			kept = append(kept, c)
		}
	}
	s.cuts = kept
	return nil
}

// lastCreated отдаёт последнюю вклейку, сохранённую через Create — ошибка
// вызвать её без предшествующего Create даст панику nil-разыменования, что и
// нужно: тест обязан был создать вклейку до проверки.
func (s *fakeDocumentCutStore) lastCreated() *models.DocumentCut {
	return s.created[len(s.created)-1]
}

// keptAfterPrune отдаёт id вклеек, оставшихся после последнего
// DeleteUnreferenced — то, что реально осталось в cuts, а не то, что было
// передано в keep (список mock мог принять и лишний id, который в базе уже
// не значился).
func (s *fakeDocumentCutStore) keptAfterPrune() []int64 {
	ids := make([]int64, 0, len(s.cuts))
	for _, c := range s.cuts {
		ids = append(ids, c.ID)
	}
	return ids
}

func (s *fakeDocumentCutStore) UpdateAnchor(ctx context.Context, id int64, start, end int, startHash, endHash string) error {
	if s.anchors == nil {
		s.anchors = map[int64][4]any{}
	}
	s.anchors[id] = [4]any{start, end, startHash, endHash}
	return nil
}

func (s *fakeDocumentCutStore) SetStatus(ctx context.Context, id int64, status string) error {
	if s.statuses == nil {
		s.statuses = map[int64]string{}
	}
	s.statuses[id] = status
	return nil
}

func (s *fakeDocumentCutStore) PagesOfCut(ctx context.Context, cut *models.DocumentCut) ([]*models.Page, error) {
	s.pagesOfCutCalls++
	if s.pagesByCutID != nil {
		return s.pagesByCutID[cut.ID], nil
	}
	return nil, nil
}

// moved сообщает, записал ли reanchorDocumentCuts новые смещения вклейки id.
func (s *fakeDocumentCutStore) moved(id int64) bool {
	_, ok := s.anchors[id]
	return ok
}

// movedTo отдаёт смещения, записанные UpdateAnchor для вклейки id.
func (s *fakeDocumentCutStore) movedTo(id int64) (int, int, bool) {
	v, ok := s.anchors[id]
	if !ok {
		return 0, 0, false
	}
	return v[0].(int), v[1].(int), true
}

// status отдаёт статус, записанный SetStatus для вклейки id (пусто — если
// SetStatus не звали вовсе).
func (s *fakeDocumentCutStore) status(id int64) string {
	return s.statuses[id]
}

func TestApplyPageEditWritesVersionThenPage(t *testing.T) {
	page := &models.Page{ID: 42, WorkID: 7, PageNumber: 3,
		ContentMarkdown: "было", Status: models.PageStatusMachineProofread}
	// fakePageStore — склад на замыканиях, а не на срезе: посмотри его
	// объявление в internal/api/stores_test.go, там поля вида getByIDFn.
	pages := &fakePageStore{
		getByIDFn: func(ctx context.Context, id int64) (*models.Page, error) {
			return page, nil
		},
	}
	versions := &fakePageVersions{}
	fragments := &fakeFragmentStore{}
	cuts := &fakeDocumentCutStore{}

	err := applyPageEdit(context.Background(), pages, versions, fragments, cuts,
		page, "стало", models.PageStatusMachineProofread, 5, "принято предложение #1")
	if err != nil {
		t.Fatalf("applyPageEdit: %v", err)
	}

	if len(pages.savedVersions) != 1 {
		t.Fatalf("ожидалась одна версия, создано %d", len(pages.savedVersions))
	}
	// В версию едет ПРЕЖНИЙ текст: это снимок до правки, а не после.
	if pages.savedVersions[0].ContentMarkdown != "было" {
		t.Fatalf("в версию попал текст %q, ожидалось %q",
			pages.savedVersions[0].ContentMarkdown, "было")
	}
	if pages.savedVersions[0].UserID != 5 {
		t.Fatalf("автор версии %d, ожидался 5", pages.savedVersions[0].UserID)
	}
	if page.ContentMarkdown != "стало" {
		t.Fatalf("текст полосы %q, ожидалось %q", page.ContentMarkdown, "стало")
	}
	if !fragments.reanchoredPages[42] {
		t.Fatal("вырезки не переякорены: смещения остались привязаны к прежнему тексту")
	}
}

// Правка полосы обязана двигать ОБЕ машины, держащиеся за неё: вырезку
// понятия и вклейку разбора. Забытая вторая не падает — она молча оставляет
// вклейку на прежних смещениях, и читатель видит на её месте чужой кусок.
func TestApplyPageEditReanchorsBothMachines(t *testing.T) {
	page := &models.Page{ID: 7, WorkID: 1, PageNumber: 3,
		ContentMarkdown: "начало. Ленин писал так. конец."}

	// newFakeFragmentStore() не принимает вырезки в конструкторе (в отличие
	// от того, что писал бриф) — они заводятся через byPage, как в
	// fragment_anchor_test.go.
	fragments := newFakeFragmentStore()
	fragments.byPage[7] = []*models.IndexFragment{{
		ID: 1, StartPageID: 7, StartOffset: 8, EndPageID: 7, EndOffset: 32,
		HeadQuote: "Ленин", TailQuote: "так.", Status: models.FragmentStatusMachine,
	}}

	cuts := newFakeDocumentCutStore(&models.DocumentCut{
		ID: 2, DocumentID: 5,
		Anchor: models.Anchor{
			StartPageID: 7, StartOffset: 8, EndPageID: 7, EndOffset: 32,
			HeadQuote: "Ленин", TailQuote: "так.",
		},
		Status: models.CutStatusOK,
	})

	pages := &fakePageStore{
		getByIDFn: func(ctx context.Context, id int64) (*models.Page, error) {
			return page, nil
		},
	}
	versions := &fakePageVersions{}

	const edited = "ВСТАВКА. начало. Ленин писал так. конец."
	if err := applyPageEdit(context.Background(), pages, versions,
		fragments, cuts, page, edited, models.PageStatusProofread, 1, "правка"); err != nil {
		t.Fatalf("правка не прошла: %v", err)
	}

	// Наблюдатель fakeFragmentStore — карта anchors, а не выдуманный
	// метод moved(): fragments.anchors[1] появляется ровно тогда, когда
	// UpdateAnchor реально записал новые смещения.
	if _, ok := fragments.anchors[1]; !ok {
		t.Fatal("вырезка понятия не переякорена")
	}
	start, end, ok := cuts.movedTo(2)
	if !ok {
		t.Fatal("вклейка разбора не переякорена — правка полосы оставит её на чужом тексте")
	}
	if got := edited[start:end]; got != "Ленин писал так." {
		t.Fatalf("вклейка переякорена на %q", got)
	}
}

// Промах якоря уводит вклейку в её СОБСТВЕННЫЙ словарь состояний: ok|stale,
// а не machine|confirmed|stale вырезки — подтверждать во вклейке нечего.
func TestApplyPageEditMarksCutStaleWithItsOwnVocabulary(t *testing.T) {
	page := &models.Page{ID: 7, ContentMarkdown: "Ленин писал так."}
	cuts := newFakeDocumentCutStore(&models.DocumentCut{
		ID: 2, Anchor: models.Anchor{
			StartPageID: 7, StartOffset: 0, EndPageID: 7, EndOffset: 5,
			HeadQuote: "Ленин", TailQuote: "Ленин",
		},
		Status: models.CutStatusOK,
	})
	pages := &fakePageStore{
		getByIDFn: func(ctx context.Context, id int64) (*models.Page, error) {
			return page, nil
		},
	}
	versions := &fakePageVersions{}

	if err := applyPageEdit(context.Background(), pages, versions,
		newFakeFragmentStore(), cuts, page, "этих слов здесь нет",
		models.PageStatusProofread, 1, ""); err != nil {
		t.Fatalf("правка не прошла: %v", err)
	}
	if got := cuts.status(2); got != models.CutStatusStale {
		t.Fatalf("состояние вклейки %q, ожидалось %q", got, models.CutStatusStale)
	}
}
