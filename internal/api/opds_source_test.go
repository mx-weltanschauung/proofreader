package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"proofreader/internal/models"
	"proofreader/internal/opds"
	"proofreader/internal/repository"
	"proofreader/internal/stats"
)

type opdsFakeEditions struct {
	list      []*models.Edition
	sums      []*models.VolumeSummary
	sumsCalls int // сколько раз собиралась полка — дорогой запрос по всему корпусу
}

func (f *opdsFakeEditions) List(context.Context) ([]*models.Edition, error) { return f.list, nil }
func (f *opdsFakeEditions) GetByID(_ context.Context, id int64) (*models.Edition, error) {
	for _, e := range f.list {
		if e.ID == id {
			return e, nil
		}
	}
	return nil, errors.New("edition not found")
}
func (f *opdsFakeEditions) ListAllWorkSummaries(context.Context) ([]*models.VolumeSummary, error) {
	f.sumsCalls++
	return f.sums, nil
}

type opdsFakeWorks struct {
	byID     map[int64]*models.Work
	loose    []models.ShelfWork
	err      error
	getCalls int // сколько раз читалась строка тома — Tree зовёт GetByID первым
}

// GetByID отвечает так же, как WorkRepository: «work not found» текстом.
func (f *opdsFakeWorks) GetByID(_ context.Context, id int64) (*models.Work, error) {
	f.getCalls++
	if f.err != nil {
		return nil, f.err
	}
	w, ok := f.byID[id]
	if !ok {
		return nil, fmt.Errorf("work not found")
	}
	return w, nil
}
func (f *opdsFakeWorks) ListWithoutEdition(context.Context) ([]models.ShelfWork, error) {
	return f.loose, nil
}

type opdsFakeChapters struct{ tree map[int64][]*models.Chapter }

func (f *opdsFakeChapters) ListByWorkHierarchical(_ context.Context, id int64) ([]*models.Chapter, error) {
	return f.tree[id], nil
}

// opdsFakeCatalog — repository.OPDSRepository: метки и выборки томов. Тома
// без полос и передние листы отсеивает сам запрос, как в SQL
// (volumeWithPages); фейк повторяет это правило по меткам.
type opdsFakeCatalog struct {
	volumes   map[int64]repository.PageStamp
	chapters  map[int64]repository.PageStamp
	works     map[int64]*models.Work
	recent    []*models.Work // уже отсеянные запросом, свежие сверху
	gotLimit  int
	gotOffset int
}

func (f *opdsFakeCatalog) RecentVolumes(_ context.Context, limit, offset int) ([]*models.Work, error) {
	f.gotLimit, f.gotOffset = limit, offset
	if offset >= len(f.recent) {
		return nil, nil
	}
	return f.recent[offset:min(offset+limit, len(f.recent))], nil
}

func (f *opdsFakeCatalog) VolumesByID(_ context.Context, ids []int64) ([]*models.Work, error) {
	var out []*models.Work
	for _, id := range ids {
		w, ok := f.works[id]
		if _, stamped := f.volumes[id]; ok && stamped && isVolume(w) {
			out = append(out, w)
		}
	}
	return out, nil
}

func (f *opdsFakeCatalog) VolumeStamps(_ context.Context, ids []int64) (map[int64]repository.PageStamp, error) {
	out := map[int64]repository.PageStamp{}
	for _, id := range ids {
		if s, ok := f.volumes[id]; ok {
			out[id] = s
		}
	}
	return out, nil
}
func (f *opdsFakeCatalog) ChapterStamps(context.Context, int64) (map[int64]repository.PageStamp, error) {
	return f.chapters, nil
}

type opdsFakeSearch struct {
	res *models.SearchResult
	err error
	got models.SearchQuery
}

func (f *opdsFakeSearch) Terms(context.Context, models.SearchQuery) ([]string, error) {
	return nil, nil
}
func (f *opdsFakeSearch) Search(_ context.Context, q models.SearchQuery) (*models.SearchResult, error) {
	f.got = q
	return f.res, f.err
}
func (f *opdsFakeSearch) SearchPages(context.Context, models.SearchQuery, int64, []int64, int, int) (*models.SearchPagesResult, error) {
	return nil, nil
}

var (
	opdsT0 = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	opdsT1 = time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	opdsT2 = time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)
)

func int64p(v int64) *int64 { return &v }
func intp(v int) *int       { return &v }

// opdsFixture: собрание 7 с томом 252 (смещение страниц 2, полосы 1—400) и
// томом 253 без полос; передние листы 254 тома 252; том 300 вне собраний;
// служебная работа издания 260 (edition_front_matter) в сводках.
func opdsFixture() (*OPDSSource, *opdsFakeSearch, *opdsFakeWorks) {
	pub := time.Date(1946, 1, 1, 0, 0, 0, 0, time.UTC)
	vol := &models.Work{ID: 252, Title: "И. В. Сталин. Сочинения. Том 1", Author: "И. В. Сталин",
		Slug: "stalin-t01", EditionID: int64p(7), VolumeNumber: intp(1), PageOffset: 2,
		PublicationDate: &pub, Role: models.WorkRoleVolume, UpdatedAt: opdsT0}
	noPages := &models.Work{ID: 253, Title: "Том без полос", EditionID: int64p(7), VolumeNumber: intp(2),
		Role: models.WorkRoleVolume, UpdatedAt: opdsT0}
	front := &models.Work{ID: 254, Title: "Передние листы", ParentWorkID: int64p(252),
		Role: models.WorkRoleFrontMatter, UpdatedAt: opdsT0}
	loose := &models.Work{ID: 300, Title: "Отдельная книга", Slug: "otdelnaya", Role: models.WorkRoleVolume, UpdatedAt: opdsT0}
	edFront := &models.Work{ID: 260, Title: "Титул издания", EditionID: int64p(7), Role: "edition_front_matter", UpdatedAt: opdsT0}

	parentID := int64(10)
	chapters := []*models.Chapter{
		{ID: 10, Title: "1901–1907", Slug: "1901-1907", StartPage: 1, EndPage: 370, UpdatedAt: opdsT0,
			Children: []*models.Chapter{
				{ID: 11, ParentID: &parentID, Title: "Письмо из Кутаиса", Slug: "pismo-iz-kutaisa",
					StartPage: 54, EndPage: 56, UpdatedAt: opdsT2},
			}},
		{ID: 40, Title: "Примечания", StartPage: 371, EndPage: 400, IsApparatus: true, UpdatedAt: opdsT0},
		// Диапазон за последней полосой тома: метка есть, полос ноль.
		{ID: 50, Title: "За краем", StartPage: 500, EndPage: 501, UpdatedAt: opdsT0},
	}
	search := &opdsFakeSearch{}
	sh := NewSearchHandler(search)
	sh.wait = time.Millisecond
	works := &opdsFakeWorks{
		byID:  map[int64]*models.Work{252: vol, 253: noPages, 254: front, 300: loose, 260: edFront},
		loose: []models.ShelfWork{{ID: 300, Title: "Отдельная книга"}},
	}
	src := NewOPDSSource(
		&opdsFakeEditions{
			list: []*models.Edition{{ID: 7, Title: "Сочинения в 13 томах"}},
			sums: []*models.VolumeSummary{{Work: *edFront}, {Work: *vol}, {Work: *noPages}},
		},
		works,
		&opdsFakeChapters{tree: map[int64][]*models.Chapter{252: chapters}},
		&opdsFakeCatalog{
			volumes: map[int64]repository.PageStamp{
				252: {Updated: opdsT1, First: 1, Last: 400, Pages: 400},
				254: {Updated: opdsT1, First: 1, Last: 8, Pages: 8},
				260: {Updated: opdsT1, First: 1, Last: 4, Pages: 4},
				300: {Updated: opdsT0, First: 1, Last: 10, Pages: 10},
			},
			chapters: map[int64]repository.PageStamp{
				10: {Updated: opdsT1, Pages: 370},
				11: {Updated: opdsT0, Pages: 3},
				40: {Updated: opdsT1, Pages: 30},
				50: {},
			},
			works:  works.byID,
			recent: []*models.Work{loose, vol},
		},
		sh,
	)
	return src, search, works
}

func TestOPDSTreeMapsVolumeAndChapters(t *testing.T) {
	src, _, _ := opdsFixture()
	tree, err := src.Tree(context.Background(), 252)
	if err != nil {
		t.Fatal(err)
	}
	v := tree.Volume
	if v.PageFrom != 3 || v.PageTo != 402 {
		t.Errorf("печатные страницы тома со смещением: %d—%d", v.PageFrom, v.PageTo)
	}
	if v.EditionTitle != "Сочинения в 13 томах" || v.VolumeLabel != "Том 1" || v.Year != 1946 || v.EditionID != 7 {
		t.Errorf("том: %+v", v)
	}
	if v.HTMLPath != "/works/252-stalin-t01" || !v.Updated.Equal(opdsT1) {
		t.Errorf("адрес и метка тома: %q %v", v.HTMLPath, v.Updated)
	}
	years := tree.Nodes[0]
	if years.PageFrom != 3 || years.PageTo != 372 || !years.HasPages {
		t.Errorf("группа: %+v", years)
	}
	// Метка главы — та же формула, что CacheKey выгрузки: здесь побеждает
	// chapters.updated_at (T2) над полосами (T0) и томом (T0).
	letter := years.Children[0]
	if !letter.Updated.Equal(opdsT2) || letter.HTMLPath != "/works/252-stalin-t01/chapters/11-pismo-iz-kutaisa" {
		t.Errorf("лист: %+v", letter)
	}
	if !tree.Nodes[1].IsApparatus {
		t.Error("признак аппарата должен доехать до пакета opds")
	}
	if tree.Nodes[2].HasPages {
		t.Error("глава без полос в диапазоне: HasPages обязан быть false — её выгрузка ответит 404")
	}
}

func TestOPDSTreeHidesWhatCatalogDoesNotShow(t *testing.T) {
	src, _, works := opdsFixture()
	for id, why := range map[int64]string{253: "том без полос", 254: "передние листы", 999: "нет такого"} {
		if _, err := src.Tree(context.Background(), id); !errors.Is(err, opds.ErrNotFound) {
			t.Errorf("%s (%d): %v, ждали opds.ErrNotFound", why, id, err)
		}
	}
	works.err = errors.New("соединение оборвано")
	if _, err := src.Tree(context.Background(), 252); err == nil || errors.Is(err, opds.ErrNotFound) {
		t.Errorf("сбой базы не должен становиться 404: %v", err)
	}
}

func TestOPDSShelf(t *testing.T) {
	src, _, _ := opdsFixture()
	shelf, err := src.Shelf(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(shelf.Editions) != 1 || len(shelf.Editions[0].Volumes) != 1 || shelf.Editions[0].Volumes[0].ID != 252 {
		t.Fatalf("собрание: %+v (том без полос и служебная работа издания выпадают)", shelf.Editions)
	}
	if len(shelf.Loose) != 1 || shelf.Loose[0].ID != 300 || shelf.Loose[0].HTMLPath != "/works/300-otdelnaya" {
		t.Fatalf("вне собраний: %+v", shelf.Loose)
	}
}

// Новые поступления берутся запросом, который сам отсеивает тома без полос
// до LIMIT (OPDSRepository.RecentVolumes): окно и смещение доезжают до него
// как есть, иначе разбивка ленты разъедется с запросом.
func TestOPDSRecentAsksCatalogForVolumesWithPages(t *testing.T) {
	src, _, _ := opdsFixture()
	if _, err := src.Recent(context.Background(), 31, 30); err != nil {
		t.Fatal(err)
	}
	cat := src.catalog.(*opdsFakeCatalog)
	if cat.gotLimit != 31 || cat.gotOffset != 30 {
		t.Errorf("окно запроса: limit %d, offset %d", cat.gotLimit, cat.gotOffset)
	}
	vs, err := src.Recent(context.Background(), 31, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 2 || vs[0].ID != 300 || vs[1].ID != 252 || vs[1].EditionTitle != "Сочинения в 13 томах" {
		t.Fatalf("новые: %+v", vs)
	}
}

// На страницах поиска после первой главы не показываются — и не строятся:
// деревья томов (пять запросов на том) и полка по всему корпусу шли бы мимо
// слота поиска на каждой странице впустую. Тома выдачи — в порядке поиска.
func TestOPDSSearchWithoutChaptersSkipsTreesAndShelf(t *testing.T) {
	src, search, works := opdsFixture()
	search.res = &models.SearchResult{
		Terms:    []string{"письм"},
		Chapters: []models.SearchChapter{{ID: 11, WorkID: 252}},
		Volumes:  []models.SearchVolume{{WorkID: 300, TextHits: 2}, {WorkID: 252, TextHits: 21}},
	}
	res, err := src.Search(opdsSearchRequest(""), "письмо", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Chapters) != 0 {
		t.Errorf("главы на странице без глав: %+v", res.Chapters)
	}
	if works.getCalls != 0 {
		t.Errorf("строилось дерево тома: GetByID звался %d раз", works.getCalls)
	}
	if n := src.editions.(*opdsFakeEditions).sumsCalls; n != 0 {
		t.Errorf("собиралась полка по всему корпусу: %d раз", n)
	}
	if len(res.Volumes) != 2 || res.Volumes[0].Volume.ID != 300 || res.Volumes[1].Volume.ID != 252 {
		t.Fatalf("тома не в порядке выдачи: %+v", res.Volumes)
	}
	if v := res.Volumes[1].Volume; v.EditionTitle != "Сочинения в 13 томах" || v.PageFrom != 3 || !v.Updated.Equal(opdsT1) {
		t.Errorf("том выдачи: %+v", v)
	}
}

func opdsSearchRequest(ua string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/opds/search?q=x", nil)
	r.Header.Set("User-Agent", ua)
	return r
}

func TestOPDSSearchMapsHitsAndRecordsStats(t *testing.T) {
	src, search, _ := opdsFixture()
	rec := &captureRecorder{}
	src.search.WithRecorder(rec, false)
	search.res = &models.SearchResult{
		Terms: []string{"письм"},
		Chapters: []models.SearchChapter{
			{ID: 11, WorkID: 252},                    // лист тома
			{ID: 40, WorkID: 252, IsApparatus: true}, // аппарат — выпадает
			{ID: 5, WorkID: 254},                     // передние листы — выпадает
		},
		Volumes: []models.SearchVolume{
			{WorkID: 252, TextHits: 21},
			{WorkID: 300, TextHits: 0, ApparatusHits: 3}, // только в аппарате — не том выдачи
			{WorkID: 254, TextHits: 5},                   // передние листы
		},
		TotalHits: 26,
	}
	res, err := src.Search(opdsSearchRequest("KOReader/2024.11"), "  Письмо  ", true)
	if err != nil {
		t.Fatal(err)
	}
	// Нормализация та же, что у сайта (NormalizeSearchText — пробелы, без
	// регистра); нижний регистр — только у текста в статистике (CleanQuery).
	if search.got.Text != "Письмо" || len(search.got.EditionIDs)+len(search.got.WorkIDs) != 0 {
		t.Errorf("запрос до хранилища: %+v", search.got)
	}
	if len(res.Chapters) != 1 || res.Chapters[0].ChapterID != 11 {
		t.Errorf("главы: %+v", res.Chapters)
	}
	if len(res.Volumes) != 1 || res.Volumes[0].Volume.ID != 252 || res.Volumes[0].TextHits != 21 {
		t.Errorf("тома: %+v", res.Volumes)
	}
	ev := rec.all()
	if len(ev) != 1 || ev[0].Row.Channel != stats.ChannelSearch || ev[0].Row.Agent != "opds" ||
		ev[0].Row.Query != "письмо" || ev[0].Row.Hits == nil {
		t.Fatalf("счёт поиска: %+v", ev)
	}

	// Бот в «Что ищут» не пишется вовсе.
	if _, err := src.Search(opdsSearchRequest("Googlebot/2.1"), "письмо", true); err != nil {
		t.Fatal(err)
	}
	if len(rec.all()) != 1 {
		t.Error("поиск бота записан в статистику")
	}
}

func TestOPDSSearchRefusals(t *testing.T) {
	src, search, _ := opdsFixture()
	var re *opds.RequestError

	_, err := src.Search(opdsSearchRequest(""), "ы", true)
	if !errors.As(err, &re) || re.Status != http.StatusBadRequest || re.Message != models.SearchTooShortMessage {
		t.Errorf("короткий запрос: %v", err)
	}

	search.err = repository.ErrSearchTimeout
	if _, err = src.Search(opdsSearchRequest(""), "письмо", true); !errors.As(err, &re) ||
		re.Status != http.StatusServiceUnavailable || re.RetryAfter != 0 {
		t.Errorf("таймаут: %v", err)
	}

	search.err = nil
	search.res = &models.SearchResult{Terms: []string{"письм"}}
	for i := 0; i < cap(src.search.slots); i++ {
		src.search.slots <- struct{}{}
	}
	if _, err = src.Search(opdsSearchRequest(""), "письмо", true); !errors.As(err, &re) ||
		re.Status != http.StatusServiceUnavailable || re.RetryAfter != searchRetryAfterSeconds {
		t.Errorf("занятые слоты: %v", err)
	}
}
