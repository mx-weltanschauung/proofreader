package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"proofreader/internal/models"
)

type fakeShelfEditions struct {
	editions  []*models.Edition
	summaries []*models.VolumeSummary
	err       error
	calls     int
}

func (f *fakeShelfEditions) List(ctx context.Context) ([]*models.Edition, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.editions, nil
}

func (f *fakeShelfEditions) ListAllWorkSummaries(
	ctx context.Context,
) ([]*models.VolumeSummary, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.summaries, nil
}

type fakeShelfWorks struct {
	works []models.ShelfWork
	err   error
}

func (f *fakeShelfWorks) ListWithoutEdition(ctx context.Context) ([]models.ShelfWork, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.works, nil
}

func volumeIn(editionID int64, id int64, title string) *models.VolumeSummary {
	s := &models.VolumeSummary{}
	s.ID = id
	s.Title = title
	s.EditionID = &editionID
	return s
}

func getShelf(t *testing.T, h *ShelfHandler) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/shelf", nil)
	rec := httptest.NewRecorder()
	h.Get(rec, req)
	return rec
}

// Главная берёт сводки всех собраний одним вызовом и раскладывает их по
// полкам сама. Порядок собраний — тот, что отдал List; порядок томов внутри
// полки — тот, что отдал запрос сводок: он и есть порядок издания.
func TestShelfHandlerGroupsVolumesByEdition(t *testing.T) {
	editions := &fakeShelfEditions{
		editions: []*models.Edition{
			{ID: 7, Title: "Маркс и Энгельс", Slug: "mae"},
			{ID: 3, Title: "Плеханов", Slug: "plekhanov"},
		},
		summaries: []*models.VolumeSummary{
			volumeIn(7, 71, "том 1"),
			volumeIn(7, 72, "том 2"),
			volumeIn(3, 31, "том 1"),
		},
	}
	works := &fakeShelfWorks{works: []models.ShelfWork{{ID: 100, Title: "Сама по себе"}}}

	rec := getShelf(t, NewShelfHandler(editions, works))
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, ожидался 200; тело: %s", rec.Code, rec.Body.String())
	}

	var got models.Shelf
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("разбор ответа: %v; тело: %s", err, rec.Body.String())
	}

	// Один вызов за сводками на всю страницу — ради этого эндпоинт и заведён.
	if editions.calls != 1 {
		t.Errorf("запросов сводок %d, ожидался один", editions.calls)
	}
	if len(got.Editions) != 2 {
		t.Fatalf("полок %d, ожидалось 2: %+v", len(got.Editions), got.Editions)
	}
	if got.Editions[0].Edition.ID != 7 || got.Editions[1].Edition.ID != 3 {
		t.Errorf("порядок собраний = %d, %d; ожидался 7, 3",
			got.Editions[0].Edition.ID, got.Editions[1].Edition.ID)
	}
	if len(got.Editions[0].Volumes) != 2 {
		t.Fatalf("томов на первой полке %d, ожидалось 2", len(got.Editions[0].Volumes))
	}
	if got.Editions[0].Volumes[0].ID != 71 || got.Editions[0].Volumes[1].ID != 72 {
		t.Errorf("порядок томов = %d, %d; ожидался 71, 72",
			got.Editions[0].Volumes[0].ID, got.Editions[0].Volumes[1].ID)
	}
	if len(got.Editions[1].Volumes) != 1 || got.Editions[1].Volumes[0].ID != 31 {
		t.Errorf("вторая полка = %+v, ожидался один том 31", got.Editions[1].Volumes)
	}
	if len(got.LooseWorks) != 1 || got.LooseWorks[0].ID != 100 {
		t.Errorf("работы вне собраний = %+v, ожидалась одна с id 100", got.LooseWorks)
	}
}

// Собрание без томов не пропадает: главная рисует по нему «Пока ни одного
// тома», и без пустой полки администратор не увидел бы, что оно заведено.
func TestShelfHandlerKeepsEmptyEdition(t *testing.T) {
	editions := &fakeShelfEditions{
		editions:  []*models.Edition{{ID: 5, Title: "Пустое", Slug: "empty"}},
		summaries: nil,
	}

	rec := getShelf(t, NewShelfHandler(editions, &fakeShelfWorks{}))
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, ожидался 200", rec.Code)
	}

	var got models.Shelf
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("разбор ответа: %v", err)
	}
	if len(got.Editions) != 1 {
		t.Fatalf("полок %d, ожидалась одна", len(got.Editions))
	}
	if got.Editions[0].Volumes == nil {
		t.Errorf("Volumes = null, ожидался пустой список")
	}
	if len(got.Editions[0].Volumes) != 0 {
		t.Errorf("Volumes = %+v, ожидался пустой список", got.Editions[0].Volumes)
	}
}

// Пустые списки уходят как [], а не null: клиент зовёт .map по ним сразу,
// без проверки. Это тот самый сквозной дефект, из-за которого списочные
// маршруты в этом проекте приходится проверять поимённо.
func TestShelfHandlerEmptyListsAreArrays(t *testing.T) {
	rec := getShelf(t, NewShelfHandler(&fakeShelfEditions{}, &fakeShelfWorks{}))
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, ожидался 200", rec.Code)
	}
	if body := rec.Body.String(); body != `{"editions":[],"loose_works":[],"journals":[]}`+"\n" {
		t.Errorf("тело = %q, ожидались пустые списки", body)
	}
}

func TestShelfHandlerFailsOnStoreError(t *testing.T) {
	cases := map[string]*ShelfHandler{
		"собрания": NewShelfHandler(
			&fakeShelfEditions{err: errors.New("нет связи")}, &fakeShelfWorks{},
		),
		"работы": NewShelfHandler(
			&fakeShelfEditions{}, &fakeShelfWorks{err: errors.New("нет связи")},
		),
	}
	for name, h := range cases {
		t.Run(name, func(t *testing.T) {
			if rec := getShelf(t, h); rec.Code != http.StatusInternalServerError {
				t.Errorf("код %d, ожидался 500", rec.Code)
			}
		})
	}
}

// Сводка тома, чьё собрание List не вернул, на полку не попадает — иначе она
// молча пропала бы из ответа или, хуже, завела бы полку без заголовка.
func TestShelfHandlerDropsVolumeOfUnknownEdition(t *testing.T) {
	editions := &fakeShelfEditions{
		editions:  []*models.Edition{{ID: 1, Title: "Известное", Slug: "known"}},
		summaries: []*models.VolumeSummary{volumeIn(99, 1, "чужой том")},
	}

	rec := getShelf(t, NewShelfHandler(editions, &fakeShelfWorks{}))
	var got models.Shelf
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("разбор ответа: %v", err)
	}
	if len(got.Editions) != 1 || len(got.Editions[0].Volumes) != 0 {
		t.Errorf("полки = %+v, ожидалась одна пустая", got.Editions)
	}
}

type fakeShelfJournals struct{ list []models.JournalSummary }

func (f fakeShelfJournals) List(context.Context) ([]models.JournalSummary, error) { return f.list, nil }

func TestShelfSkipsJournalsWithoutIssues(t *testing.T) {
	h := NewShelfHandler(&fakeShelfEditions{}, &fakeShelfWorks{}).WithJournals(fakeShelfJournals{list: []models.JournalSummary{
		{Journal: models.Journal{ID: 1, Slug: "pzm", Title: "Под знаменем марксизма"}, IssuesTotal: 104},
		{Journal: models.Journal{ID: 2, Slug: "empty", Title: "Пустой"}, IssuesTotal: 0},
	}})
	var shelf models.Shelf
	if err := json.Unmarshal(getShelf(t, h).Body.Bytes(), &shelf); err != nil {
		t.Fatal(err)
	}
	if len(shelf.Journals) != 1 || shelf.Journals[0].Slug != "pzm" {
		t.Fatalf("журналы полки: %+v", shelf.Journals)
	}
}

func TestShelfJournalsIsEmptyArrayWithoutStore(t *testing.T) {
	rec := getShelf(t, NewShelfHandler(&fakeShelfEditions{}, &fakeShelfWorks{}))
	if !strings.Contains(rec.Body.String(), `"journals":[]`) {
		t.Fatalf("journals не пустой массив: %s", rec.Body.String())
	}
}
