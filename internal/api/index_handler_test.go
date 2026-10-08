package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"proofreader/internal/models"
	"proofreader/internal/repository"
	"proofreader/pkg/markdown"
)

type fakeIndexStore struct {
	replaced        []*models.IndexConcept
	replacedIn      int64
	replaceWarnings []string
	replaceErr      error
	concept         *models.IndexConcept
	list            []*models.IndexConcept
	volumes         []models.VolumeLocation
	// volumesByEdition, when set, makes VolumeMap distinguish editions — a
	// statья's references must resolve against ITS OWN edition's volumes, and
	// a fake that hands every caller the same slice (via `volumes` alone)
	// can't tell an implementation that does this apart from one that
	// resolves every article against one shared map. Falls back to `volumes`
	// for any editionID not present, so existing single-edition fixtures
	// keep working unchanged.
	volumesByEdition map[int64][]models.VolumeLocation
	backlinks        []models.ConceptBacklink
	taken            map[string]bool
	links            []repository.LinkWithTarget
	incoming         []repository.IncomingLink
}

func (f *fakeIndexStore) ReplaceForEdition(ctx context.Context, editionID int64, cs []*models.IndexConcept) ([]string, error) {
	f.replacedIn, f.replaced = editionID, cs
	return f.replaceWarnings, f.replaceErr
}
func (f *fakeIndexStore) GetConceptBySlug(ctx context.Context, slug string) (*models.IndexConcept, error) {
	return f.concept, nil
}
func (f *fakeIndexStore) ListConcepts(ctx context.Context, q, letter string, limit, offset int) ([]*models.IndexConcept, error) {
	return f.list, nil
}
func (f *fakeIndexStore) VolumeMap(ctx context.Context, editionID int64) ([]models.VolumeLocation, error) {
	if vols, ok := f.volumesByEdition[editionID]; ok {
		return vols, nil
	}
	return f.volumes, nil
}
func (f *fakeIndexStore) Backlinks(ctx context.Context, editionID int64, vol int, part *string, page int) ([]models.ConceptBacklink, error) {
	return f.backlinks, nil
}
func (f *fakeIndexStore) TakenSlugs(ctx context.Context) (map[string]bool, error) {
	return f.taken, nil
}
func (f *fakeIndexStore) ArticleLinks(ctx context.Context, articleID int64) ([]repository.LinkWithTarget, error) {
	return f.links, nil
}
func (f *fakeIndexStore) IncomingLinks(ctx context.Context, conceptID int64) ([]repository.IncomingLink, error) {
	return f.incoming, nil
}

type fakeWorkGetter struct{ work *models.Work }

func (f *fakeWorkGetter) List(ctx context.Context, limit, offset int, s *models.WorkStatus, o *int64) ([]*models.Work, error) {
	return nil, nil
}
func (f *fakeWorkGetter) GetByID(ctx context.Context, id int64) (*models.Work, error) {
	return f.work, nil
}

type fakePageGetter struct{ page *models.Page }

func (f *fakePageGetter) Create(ctx context.Context, p *models.Page) error { return nil }
func (f *fakePageGetter) GetByID(ctx context.Context, id int64) (*models.Page, error) {
	return f.page, nil
}
func (f *fakePageGetter) GetByWorkAndPageNumber(ctx context.Context, w int64, n int) (*models.Page, error) {
	return f.page, nil
}
func (f *fakePageGetter) GetPageRange(ctx context.Context, w int64, s, e int) ([]*models.Page, error) {
	return nil, nil
}
func (f *fakePageGetter) GetPagesByNumbers(ctx context.Context, w int64, numbers []int) ([]*models.Page, error) {
	return nil, nil
}
func (f *fakePageGetter) ListByWork(ctx context.Context, w int64) ([]*models.Page, error) {
	return nil, nil
}
func (f *fakePageGetter) ListPageMap(ctx context.Context, w int64) ([]models.PageMapEntry, error) {
	return nil, nil
}
func (f *fakePageGetter) MaxPageNumber(ctx context.Context, w int64) (int, error) { return 0, nil }

// Заглушка только на чтение: указатель полос не правит. Отказ, а не тихий
// успех, — иначе тест, забредший сюда на запись, остался бы зелёным.
func (f *fakePageGetter) SaveEdit(ctx context.Context, p *models.Page, v *models.PageVersion) error {
	return fmt.Errorf("fakePageGetter: правка полосы не поддерживается")
}

func TestImportAssignsUniqueSlugs(t *testing.T) {
	store := &fakeIndexStore{taken: map[string]bool{"trud": true}}
	editions := &fakeEditionStore{editions: []*models.Edition{{ID: 9}}}
	h := NewIndexHandler(store, &fakeWorkGetter{}, editions, &fakePageGetter{}, &fakeChapterTrees{}, newFakeFragmentStore(), markdown.NewRenderer())

	body := bytes.NewBufferString(`{"concepts":[
		{"title":"Труд","slug":"trud","sort_key":"труд","kind":"article"},
		{"title":"Труд","slug":"trud","sort_key":"труд","kind":"article"}
	]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/editions/9/index/import", body)
	req = mux.SetURLVars(req, map[string]string{"id": "9"})
	rec := httptest.NewRecorder()
	h.Import(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, ожидался 200; тело: %s", rec.Code, rec.Body.String())
	}
	if store.replacedIn != 9 {
		t.Fatalf("импорт ушёл в издание %d, ожидалось 9", store.replacedIn)
	}
	if len(store.replaced) != 2 {
		t.Fatalf("в стор пришло %d понятий, ожидалось 2", len(store.replaced))
	}
	// «trud» занят чужой статьёй, поэтому оба понятия получают суффиксы по порядку.
	if store.replaced[0].Slug != "trud-2" || store.replaced[1].Slug != "trud-3" {
		t.Fatalf("слаги: %q, %q", store.replaced[0].Slug, store.replaced[1].Slug)
	}
}

func TestImportUnknownEditionIsNotFound(t *testing.T) {
	// Издание — теперь единственная сущность, на которую опирается ввоз;
	// отсутствующее издание обязано отвечать 404, а не проваливаться дальше
	// с паникой на nil.
	store := &fakeIndexStore{}
	editions := &fakeEditionStore{}
	h := NewIndexHandler(store, &fakeWorkGetter{}, editions, &fakePageGetter{}, &fakeChapterTrees{}, newFakeFragmentStore(), markdown.NewRenderer())

	body := bytes.NewBufferString(`{"concepts":[]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/editions/9/index/import", body)
	req = mux.SetURLVars(req, map[string]string{"id": "9"})
	rec := httptest.NewRecorder()
	h.Import(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("код %d, ожидался 404", rec.Code)
	}
}

// TestImportExtendsBothDeadlines — ReadTimeout (cmd/server/main.go) покрывает
// чтение всего запроса вместе с телом, а тело ленинского указателя ≈43 МБ.
// Обработчик обязан раздвигать дедлайн ЧТЕНИЯ так же, как уже раздвигает
// дедлайн ЗАПИСИ (см. TestIndexImportExtendsWriteDeadline ниже, тот же
// deadlineRecorder) — под httptest дотянуться до соединения не до чего,
// поэтому проверяется не таймаут по часам, а сам факт вызова контроллера.
func TestImportExtendsBothDeadlines(t *testing.T) {
	handler := NewIndexHandler(&fakeIndexStore{}, &fakeWorkGetter{},
		&fakeEditionStore{}, &fakePageGetter{}, &fakeChapterTrees{},
		newFakeFragmentStore(), markdown.NewRenderer())
	body := strings.NewReader(`{"concepts":[]}`)
	req := mux.SetURLVars(httptest.NewRequest(http.MethodPost, "/", body),
		map[string]string{"id": "4"})
	rec := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}

	handler.Import(rec, req)

	if len(rec.deadlines) != 1 {
		t.Errorf("дедлайн записи раздвинут %d раз, ожидался 1", len(rec.deadlines))
	}
	if len(rec.readDeadlines) != 1 {
		t.Errorf("дедлайн ЧТЕНИЯ раздвинут %d раз, ожидался 1: ReadTimeout "+
			"покрывает чтение тела, и 43 МБ в 15 секунд упираются в него", len(rec.readDeadlines))
	}
}

// TestImportFailureCarriesTheReasonToTheOperator — находка сквозной рецензии.
// Ветка специально завела громкие отказы ввоза (дубль заголовка понятия,
// дубль подрубрики), называющие виновную строку указателя. Обработчик глотал
// их все в одну фразу «Failed to import index», и на ввозе в 2819 статей это
// разница между «поправить строку разборщика» и «лезть в логи сервера».
// Форма ответа — {"message": …}: frontend/src/utils/apiError.ts читает
// текст ошибки только так, text/plain он подменяет запасной фразой.
func TestImportFailureCarriesTheReasonToTheOperator(t *testing.T) {
	const reason = "два понятия с одним ключом заголовка в одном ввозе: \"Труд\" и \"труд\""
	store := &fakeIndexStore{replaceErr: fmt.Errorf("%s", reason)}
	editions := &fakeEditionStore{editions: []*models.Edition{{ID: 9}}}
	h := NewIndexHandler(store, &fakeWorkGetter{}, editions, &fakePageGetter{}, &fakeChapterTrees{}, newFakeFragmentStore(), markdown.NewRenderer())

	body := bytes.NewBufferString(`{"concepts":[{"title":"Труд","slug":"trud","sort_key":"труд"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/editions/9/index/import", body)
	req = mux.SetURLVars(req, map[string]string{"id": "9"})
	rec := httptest.NewRecorder()
	h.Import(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("код %d, ожидался 500; тело: %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("ответ не JSON с полем message: %v", err)
	}
	if !strings.Contains(got.Message, reason) {
		t.Errorf("причина отказа не доехала до оператора: %q", got.Message)
	}
}

func TestGetConceptResolvesReferences(t *testing.T) {
	store := &fakeIndexStore{
		concept: &models.IndexConcept{
			ID: 1, Title: "Абстрактный труд", Slug: "abstraktnyj-trud",
			Articles: []*models.IndexArticle{{
				ID: 1, EditionID: 1, Kind: models.IndexConceptKindArticle,
				References: []*models.IndexReference{
					{VolumeNumber: 4, PageStart: 85, PageEnd: 85, Rubric: "определение", OrderNumber: 1},
					{VolumeNumber: 31, PageStart: 268, PageEnd: 268, Rubric: "определение", OrderNumber: 2},
				},
			}},
		},
		volumes: []models.VolumeLocation{{VolumeNumber: 4, WorkID: 4, PageOffset: 0, MaxPage: 640}},
	}
	h := NewIndexHandler(store, &fakeWorkGetter{}, &fakeEditionStore{}, &fakePageGetter{}, &fakeChapterTrees{}, newFakeFragmentStore(), markdown.NewRenderer())

	req := httptest.NewRequest(http.MethodGet, "/api/concepts/abstraktnyj-trud", nil)
	req = mux.SetURLVars(req, map[string]string{"slug": "abstraktnyj-trud"})
	rec := httptest.NewRecorder()
	h.GetConcept(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, ожидался 200", rec.Code)
	}
	var got struct {
		Articles []struct {
			References []struct {
				Resolved   bool  `json:"resolved"`
				WorkID     int64 `json:"work_id,omitempty"`
				PageNumber int   `json:"page_number,omitempty"`
			} `json:"references"`
		} `json:"articles"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("ответ не разобран: %v", err)
	}
	if len(got.Articles) != 1 {
		t.Fatalf("статей %d, ожидалась 1", len(got.Articles))
	}
	refs := got.Articles[0].References
	if len(refs) != 2 {
		t.Fatalf("ссылок %d, ожидалось 2", len(refs))
	}
	if !refs[0].Resolved || refs[0].WorkID != 4 || refs[0].PageNumber != 85 {
		t.Fatalf("первая ссылка не разрешена: %+v", refs[0])
	}
	if refs[1].Resolved {
		t.Fatalf("ссылка на незагруженный том 31 не должна быть разрешена")
	}
}

// TestGetConceptReturnsArticlesWithResolvedReferences покрывает форму задачи
// 4: понятие каталога отдаёт СТАТЬИ, каждая со своими адресами, разрешёнными
// против издания самой статьи (a.EditionID), а не через работу понятия.
func TestGetConceptReturnsArticlesWithResolvedReferences(t *testing.T) {
	workID := int64(40)
	store := &fakeIndexStore{
		concept: &models.IndexConcept{
			ID: 8, Slug: "abstraktnyj-trud", Title: "Абстрактный труд",
			Articles: []*models.IndexArticle{{
				ID: 1, EditionID: 1, EditionTitle: "Маркс и Энгельс", WorkID: &workID,
				Title: "Абстрактный труд", Kind: "article",
				References: []*models.IndexReference{
					{ID: 1, VolumeNumber: 12, PageStart: 730, PageEnd: 730, Rubric: "определение"},
				},
			}},
		},
		volumes: []models.VolumeLocation{
			{VolumeNumber: 12, WorkID: 7, WorkSlug: "7-mae-t12", PageOffset: 0, MaxPage: 900},
		},
	}
	h := NewIndexHandler(store, &fakeWorkGetter{}, &fakeEditionStore{}, &fakePageGetter{}, &fakeChapterTrees{}, newFakeFragmentStore(), markdown.NewRenderer())

	req := httptest.NewRequest(http.MethodGet, "/api/concepts/abstraktnyj-trud", nil)
	req = mux.SetURLVars(req, map[string]string{"slug": "abstraktnyj-trud"})
	rec := httptest.NewRecorder()
	h.GetConcept(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, ожидался 200; тело: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Articles []struct {
			EditionTitle string `json:"edition_title"`
			References   []struct {
				Rubric   string `json:"rubric"`
				Resolved bool   `json:"resolved"`
				WorkID   int64  `json:"work_id"`
				PageNum  int    `json:"page_number"`
			} `json:"references"`
		} `json:"articles"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("ответ не разбирается: %v", err)
	}
	if len(body.Articles) != 1 {
		t.Fatalf("статей %d, хотели 1", len(body.Articles))
	}
	if body.Articles[0].EditionTitle != "Маркс и Энгельс" {
		t.Errorf("издание %q", body.Articles[0].EditionTitle)
	}
	refs := body.Articles[0].References
	if len(refs) != 1 || !refs[0].Resolved || refs[0].WorkID != 7 || refs[0].PageNum != 730 {
		t.Errorf("адрес разрешён неверно: %+v", refs)
	}
	if refs[0].Rubric != "определение" {
		t.Errorf("подрубрика %q", refs[0].Rubric)
	}
}

// TestGetConceptResolvesEachArticleAgainstItsOwnEdition — раунд правок 1 по
// задаче 4. Предыдущая версия (TestGetConceptReturnsArticlesWithResolvedReferences
// выше) заводила понятие с ОДНОЙ статьёй и прошла бы одинаково, разрешай
// обработчик её адрес против общей карты или против карты своего издания —
// не отличить. Здесь у понятия ДВЕ статьи РАЗНЫХ изданий, и в картах обоих
// изданий один и тот же номер тома (12) ведёт в РАЗНЫЕ работы — если бы
// обработчик разрешал обе статьи против одной карты (свой номер издания или
// первой попавшейся), совпадение по номеру тома дало бы для одной из статей
// работу чужого издания, и тест это поймает.
func TestGetConceptResolvesEachArticleAgainstItsOwnEdition(t *testing.T) {
	workA := int64(40)
	workB := int64(41)
	store := &fakeIndexStore{
		concept: &models.IndexConcept{
			ID: 8, Slug: "abstraktnyj-trud", Title: "Абстрактный труд",
			Articles: []*models.IndexArticle{
				{
					ID: 1, EditionID: 1, EditionTitle: "Маркс и Энгельс", WorkID: &workA,
					Title: "Абстрактный труд", Kind: "article",
					References: []*models.IndexReference{
						{ID: 1, VolumeNumber: 12, PageStart: 730, PageEnd: 730, Rubric: "определение"},
					},
				},
				{
					ID: 2, EditionID: 2, EditionTitle: "Ленин", WorkID: &workB,
					Title: "Абстрактный труд", Kind: "article",
					References: []*models.IndexReference{
						{ID: 2, VolumeNumber: 12, PageStart: 50, PageEnd: 50, Rubric: "критика"},
					},
				},
			},
		},
		volumesByEdition: map[int64][]models.VolumeLocation{
			1: {{VolumeNumber: 12, WorkID: 7, WorkSlug: "7-mae-t12", PageOffset: 0, MaxPage: 900}},
			2: {{VolumeNumber: 12, WorkID: 20, WorkSlug: "20-lenin-t12", PageOffset: 0, MaxPage: 500}},
		},
	}
	h := NewIndexHandler(store, &fakeWorkGetter{}, &fakeEditionStore{}, &fakePageGetter{}, &fakeChapterTrees{}, newFakeFragmentStore(), markdown.NewRenderer())

	req := httptest.NewRequest(http.MethodGet, "/api/concepts/abstraktnyj-trud", nil)
	req = mux.SetURLVars(req, map[string]string{"slug": "abstraktnyj-trud"})
	rec := httptest.NewRecorder()
	h.GetConcept(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, ожидался 200; тело: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Articles []struct {
			EditionID  int64 `json:"edition_id"`
			References []struct {
				Resolved bool  `json:"resolved"`
				WorkID   int64 `json:"work_id"`
				PageNum  int   `json:"page_number"`
			} `json:"references"`
		} `json:"articles"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("ответ не разбирается: %v", err)
	}
	if len(body.Articles) != 2 {
		t.Fatalf("статей %d, хотели 2", len(body.Articles))
	}

	byEdition := map[int64][]struct {
		Resolved bool  `json:"resolved"`
		WorkID   int64 `json:"work_id"`
		PageNum  int   `json:"page_number"`
	}{}
	for _, a := range body.Articles {
		byEdition[a.EditionID] = a.References
	}

	refs1, ok := byEdition[1]
	if !ok || len(refs1) != 1 || !refs1[0].Resolved || refs1[0].WorkID != 7 || refs1[0].PageNum != 730 {
		t.Errorf("статья издания 1 разрешена неверно: %+v (есть=%v)", refs1, ok)
	}
	refs2, ok := byEdition[2]
	if !ok || len(refs2) != 1 || !refs2[0].Resolved || refs2[0].WorkID != 20 || refs2[0].PageNum != 50 {
		t.Errorf("статья издания 2 разрешена неверно: %+v (есть=%v)", refs2, ok)
	}
}

func TestPageConceptsWithoutVolumeIsEmpty(t *testing.T) {
	store := &fakeIndexStore{backlinks: []models.ConceptBacklink{{ConceptID: 1, Slug: "x", Title: "X"}}}
	h := NewIndexHandler(store, &fakeWorkGetter{work: &models.Work{ID: 9}}, &fakeEditionStore{}, &fakePageGetter{page: &models.Page{ID: 5, WorkID: 9, PageNumber: 85}}, &fakeChapterTrees{}, newFakeFragmentStore(), markdown.NewRenderer())

	req := httptest.NewRequest(http.MethodGet, "/api/works/9/pages/5/concepts", nil)
	req = mux.SetURLVars(req, map[string]string{"workId": "9", "pageId": "5"})
	rec := httptest.NewRecorder()
	h.PageConcepts(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, ожидался 200", rec.Code)
	}
	var got []models.ConceptBacklink
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("ответ не разобран: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("у работы без volume_number обратных ссылок быть не может, получено %d", len(got))
	}
}

func TestListConceptsEmptyIsArray(t *testing.T) {
	// Репозиторий отдаёт nil-слайс на пустой выборке, и encoding/json пишет
	// null. Клиент навигатора вызвал бы .map на null и упал бы — пустота
	// обязана приходить как [].
	store := &fakeIndexStore{list: nil}
	h := NewIndexHandler(store, &fakeWorkGetter{}, &fakeEditionStore{}, &fakePageGetter{}, &fakeChapterTrees{}, newFakeFragmentStore(), markdown.NewRenderer())

	req := httptest.NewRequest(http.MethodGet, "/api/concepts", nil)
	rec := httptest.NewRecorder()
	h.ListConcepts(rec, req)

	if got := strings.TrimSpace(rec.Body.String()); got != "[]" {
		t.Fatalf("тело %q, ожидалось []", got)
	}
}

func TestGetConceptLinksEmptyIsArray(t *testing.T) {
	// Второе место с той же болезнью: у статьи без отсылок и без адресов —
	// а таких большинство — поля links/references уезжали как null.
	store := &fakeIndexStore{
		concept: &models.IndexConcept{
			ID: 1, Title: "Абстрактный труд", Slug: "abstraktnyj-trud",
			Articles: []*models.IndexArticle{{ID: 1, EditionID: 1, Title: "Абстрактный труд"}},
		},
		links: nil,
	}
	h := NewIndexHandler(store, &fakeWorkGetter{}, &fakeEditionStore{}, &fakePageGetter{}, &fakeChapterTrees{}, newFakeFragmentStore(), markdown.NewRenderer())

	req := httptest.NewRequest(http.MethodGet, "/api/concepts/abstraktnyj-trud", nil)
	req = mux.SetURLVars(req, map[string]string{"slug": "abstraktnyj-trud"})
	rec := httptest.NewRecorder()
	h.GetConcept(rec, req)

	var body struct {
		Articles []struct {
			References []json.RawMessage `json:"references"`
			Links      []json.RawMessage `json:"links"`
		} `json:"articles"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("не разобрать ответ: %v; тело: %s", err, rec.Body.String())
	}
	if len(body.Articles) != 1 {
		t.Fatalf("статей %d, ожидалась 1", len(body.Articles))
	}
	if body.Articles[0].Links == nil {
		t.Fatal("links пришли как null, ожидался []")
	}
	if body.Articles[0].References == nil {
		t.Fatal("references пришли как null, ожидался []")
	}
}

type fakeChapterTrees struct{ tree []*models.Chapter }

func (f *fakeChapterTrees) ListByWorkHierarchical(ctx context.Context, workID int64) ([]*models.Chapter, error) {
	return f.tree, nil
}

// pageStub — подставной PageStore, знающий тела страниц одной работы.
type pageStub struct {
	fakePageGetter
	pages     []*models.Page
	askedFor  []int
	askedWork int64
}

func (p *pageStub) GetPagesByNumbers(ctx context.Context, workID int64, numbers []int) ([]*models.Page, error) {
	p.askedWork, p.askedFor = workID, numbers
	var out []*models.Page
	for _, n := range numbers {
		for _, page := range p.pages {
			if page.PageNumber == n {
				out = append(out, page)
			}
		}
	}
	return out, nil
}

func (p *pageStub) GetByID(ctx context.Context, id int64) (*models.Page, error) {
	for _, page := range p.pages {
		if page.ID == id {
			return page, nil
		}
	}
	return nil, fmt.Errorf("page %d not found", id)
}

func TestGetConceptReturnsIncomingLinks(t *testing.T) {
	edID := int64(3)
	store := &fakeIndexStore{
		concept:  &models.IndexConcept{ID: 8, Title: "Абстрактный труд", Slug: "abstraktnyj-trud"},
		incoming: []repository.IncomingLink{{Slug: "trud", Title: "Труд", Kind: models.IndexLinkKindSeeAlso}},
	}
	h := NewIndexHandler(store, &fakeWorkGetter{work: &models.Work{ID: 40, EditionID: &edID}}, &fakeEditionStore{}, &pageStub{}, &fakeChapterTrees{}, newFakeFragmentStore(), markdown.NewRenderer())

	req := httptest.NewRequest(http.MethodGet, "/api/concepts/abstraktnyj-trud", nil)
	req = mux.SetURLVars(req, map[string]string{"slug": "abstraktnyj-trud"})
	rec := httptest.NewRecorder()
	h.GetConcept(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d; тело: %s", rec.Code, rec.Body.String())
	}
	var got struct {
		IncomingLinks []struct {
			Slug  string `json:"slug"`
			Title string `json:"title"`
			Kind  string `json:"kind"`
		} `json:"incoming_links"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if len(got.IncomingLinks) != 1 || got.IncomingLinks[0].Slug != "trud" || got.IncomingLinks[0].Kind != "see_also" {
		t.Fatalf("обратные отсылки: %+v", got.IncomingLinks)
	}
}

func TestGetConceptEmptyIncomingLinksIsList(t *testing.T) {
	edID := int64(3)
	store := &fakeIndexStore{
		concept: &models.IndexConcept{ID: 8, Slug: "abstraktnyj-trud"},
	}
	h := NewIndexHandler(store, &fakeWorkGetter{work: &models.Work{ID: 40, EditionID: &edID}}, &fakeEditionStore{}, &pageStub{}, &fakeChapterTrees{}, newFakeFragmentStore(), markdown.NewRenderer())

	req := httptest.NewRequest(http.MethodGet, "/api/concepts/abstraktnyj-trud", nil)
	req = mux.SetURLVars(req, map[string]string{"slug": "abstraktnyj-trud"})
	rec := httptest.NewRecorder()
	h.GetConcept(rec, req)

	// null сломал бы клиент так же, как в списке понятий.
	if !strings.Contains(rec.Body.String(), `"incoming_links":[]`) {
		t.Fatalf("пустые обратные отсылки отданы как %s", rec.Body.String())
	}
}

// deadlineRecorder — httptest.ResponseRecorder, умеющий то, чего ему не
// хватает: принимать срок записи и срок чтения. Без него http.NewResponseController
// отвечает ErrNotSupported, и проверить снятие дедлайна нечем.
type deadlineRecorder struct {
	*httptest.ResponseRecorder
	deadlines     []time.Time
	readDeadlines []time.Time
}

func (d *deadlineRecorder) SetWriteDeadline(t time.Time) error {
	d.deadlines = append(d.deadlines, t)
	return nil
}

func (d *deadlineRecorder) SetReadDeadline(t time.Time) error {
	d.readDeadlines = append(d.readDeadlines, t)
	return nil
}

// Ввоз ленинского указателя — одно тело на 2819 статей и 199 тыс. адресов.
// Глобальный WriteTimeout (15 с, cmd/server/main.go) оборвал бы ответ на
// середине: транзакция бы закоммитилась, а клиент увидел бы обрыв и не узнал,
// прошёл ввоз или нет.
func TestIndexImportExtendsWriteDeadline(t *testing.T) {
	store := &fakeIndexStore{}
	editions := &fakeEditionStore{editions: []*models.Edition{{ID: 9}}}
	h := NewIndexHandler(store, &fakeWorkGetter{}, editions, &fakePageGetter{},
		&fakeChapterTrees{}, newFakeFragmentStore(), markdown.NewRenderer())

	rec := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	req := httptest.NewRequest(http.MethodPost, "/api/editions/9/index/import",
		bytes.NewBufferString(`{"concepts":[]}`))
	req = mux.SetURLVars(req, map[string]string{"id": "9"})

	h.Import(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, ожидался 200; тело: %s", rec.Code, rec.Body.String())
	}

	if len(rec.deadlines) != 1 {
		t.Fatalf("дедлайн записи не снят: вызовов SetWriteDeadline = %d", len(rec.deadlines))
	}
	if d := time.Until(rec.deadlines[0]); d < time.Minute {
		t.Errorf("дедлайн раздвинут всего на %v — меньше минуты", d)
	}
}
