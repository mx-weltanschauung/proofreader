package seo

import (
	"context"
	"errors"
	"fmt"
	"proofreader/internal/site"
	"strings"
	"testing"
	"time"

	"proofreader/internal/models"
)

type fakeWorks struct {
	byID map[int64]*models.Work
	// children — служебные передние листы тома (ListChildren), loose — тома
	// вне изданий (ListWithoutEdition). Нужны тексту для нейросетей.
	children map[int64][]*models.Work
	loose    []models.ShelfWork
	// err, если задан, возвращается вместо чтения из byID — так тесты
	// воспроизводят ровно то, что отдают настоящие репозитории:
	// "... not found" на pgx.ErrNoRows и "failed to ..." на прочих бедах.
	err error
}

func (f *fakeWorks) ListChildren(ctx context.Context, parentID int64) ([]*models.Work, error) {
	return f.children[parentID], nil
}

func (f *fakeWorks) ListWithoutEdition(ctx context.Context) ([]models.ShelfWork, error) {
	return f.loose, nil
}

func (f *fakeWorks) GetByID(ctx context.Context, id int64) (*models.Work, error) {
	if f.err != nil {
		return nil, f.err
	}
	w, ok := f.byID[id]
	if !ok {
		// Именно так отвечает *repository.WorkRepository на отсутствующий
		// том: голая fmt.Errorf("work not found") на pgx.ErrNoRows, без
		// ErrNotFound в цепочке. Фикстура прежде отдавала nil, nil — форму,
		// которой в жизни нет, — и тем прятала 500 на несуществующем томе.
		return nil, fmt.Errorf("work not found")
	}
	return w, nil
}

// coverKey — «полоса такая-то такой-то работы» для fakeChapters.covering.
type coverKey struct {
	workID     int64
	pageNumber int
}

type fakeChapters struct {
	trees map[int64][]*models.Chapter
	byID  map[int64]*models.Chapter
	// covering — готовый ответ FindByPage. Карта, а не поиск по byID
	// диапазонами: правило выбора (самая узкая из накрывающих глав)
	// принадлежит SQL-запросу, и, повторённое в фикстуре, оно проверяло бы
	// фикстуру. Правило сторожит TestFindByPagePrefersNarrowestChapter
	// (internal/repository, одноразовая БД).
	covering map[coverKey]*models.Chapter
}

func (f *fakeChapters) GetByID(ctx context.Context, id int64) (*models.Chapter, error) {
	ch, ok := f.byID[id]
	if !ok {
		// Как *repository.ChapterRepository — см. тот же довод у fakeWorks.
		return nil, fmt.Errorf("chapter not found")
	}
	return ch, nil
}

func (f *fakeChapters) ListByWorkHierarchical(
	ctx context.Context, workID int64,
) ([]*models.Chapter, error) {
	return f.trees[workID], nil
}

func (f *fakeChapters) FindByPage(
	ctx context.Context, workID int64, pageNumber int,
) (*models.Chapter, error) {
	return f.covering[coverKey{workID, pageNumber}], nil
}

type fakeEditions struct {
	byID  map[int64]*models.Edition
	works map[int64][]*models.Work
	all   []*models.Edition
	// err, если задан, возвращается из GetByID вместо чтения byID — тем же
	// приёмом, что и fakeWorks.err, воспроизводит и «not found», и поломку
	// базы для необязательной выборки издания в Work().
	err error
}

func (f *fakeEditions) GetByID(ctx context.Context, id int64) (*models.Edition, error) {
	if f.err != nil {
		return nil, f.err
	}
	ed, ok := f.byID[id]
	if !ok {
		// Как *repository.EditionRepository — см. тот же довод у fakeWorks.
		return nil, fmt.Errorf("edition not found")
	}
	return ed, nil
}
func (f *fakeEditions) List(ctx context.Context) ([]*models.Edition, error) { return f.all, nil }
func (f *fakeEditions) ListWorks(ctx context.Context, id int64) ([]*models.Work, error) {
	return f.works[id], nil
}

func volumeSource() *Source {
	editionID := int64(7)
	vol := 42
	return &Source{
		BaseURL: "https://lib.example.org",
		Works: &fakeWorks{byID: map[int64]*models.Work{
			1: {
				ID: 1, Title: "Полное собрание сочинений. Том 42",
				Author: "В. И. Ленин", EditionID: &editionID, VolumeNumber: &vol,
				Role: models.WorkRoleVolume, UpdatedAt: time.Unix(1_700_000_000, 0),
				// Слаг несёт настоящий репозиторий (задача 4) — фикстура без
				// него проверяла бы то, чего в жизни не бывает.
				Slug: "lenin-t42",
			},
		}},
		Chapters: &fakeChapters{trees: map[int64][]*models.Chapter{
			1: {
				{ID: 10, WorkID: 1, Title: "Государство и революция", StartPage: 5, EndPage: 120, Slug: "gosudarstvo-i-revolyuciya"},
				{ID: 11, WorkID: 1, Title: `"Левые" о войне`, StartPage: 121, EndPage: 130},
			},
		}},
		Editions: &fakeEditions{
			byID: map[int64]*models.Edition{
				// URLSlug заполнен — в базе он есть у всех пяти собраний
				// корпуса (тот же "lenin", что и на живом /editions/4-lenin).
				7: {ID: 7, Title: "В. И. Ленин. Полное собрание сочинений", URLSlug: "lenin"},
			},
			// Непустая карта нужна тесту издания: он кладёт в неё тома.
			works: map[int64][]*models.Work{},
		},
	}
}

// Карточка тома — метаданные и дерево глав ссылками. Текста страниц в ней
// нет: том целиком это мегабайты, а рядом есть адреса глав.
func TestWorkDocIsCardWithChapterLinks(t *testing.T) {
	doc, err := volumeSource().Work(context.Background(), 1)
	if err != nil {
		t.Fatalf("Work: %v", err)
	}

	if doc.Canonical != "https://lib.example.org/works/1-lenin-t42" {
		t.Errorf("canonical: %q", doc.Canonical)
	}
	// Различающее — название тома — вперёд, автор следом: у этого корпуса
	// 45 томов одного автора, и вкладка/сниппет обрезаются с конца. Строка
	// продублирована буквально во фронтовом тесте useDocumentTitle.test.ts —
	// расхождение форматов там и здесь означало бы, что закладка на вкладке
	// снова не совпадает с тем, что отдаёт краулеру этот же хендлер.
	wantTitle := "Полное собрание сочинений. Том 42 — В. И. Ленин — " + site.Name()
	if doc.Title != wantTitle {
		t.Errorf("заголовок: %q, ожидалось %q", doc.Title, wantTitle)
	}
	if doc.Robots != RobotsIndex {
		t.Errorf("том обязан индексироваться, получено %q", doc.Robots)
	}
	if doc.ImageURL != "https://lib.example.org/og/work/1.png" {
		t.Errorf("ссылка на карточку: %q", doc.ImageURL)
	}
	if !strings.Contains(doc.Body, `href="/works/1-lenin-t42/chapters/10-gosudarstvo-i-revolyuciya"`) {
		t.Errorf("в теле нет ссылки на главу:\n%s", doc.Body)
	}
	if !strings.Contains(doc.Description, "В. И. Ленин") {
		t.Errorf("в описании нет автора: %q", doc.Description)
	}
	if strings.Contains(doc.Body, `"Левые"`) {
		t.Error("название главы попало в тело неэкранированным")
	}
}

// Канон тома обязан нести слаг — задача 8. Карточка превью (og:image) при
// этом остаётся числовой: её адресует только наш HTML, слаг там не нужен.
func TestWorkCanonicalCarriesSlug(t *testing.T) {
	s := volumeSource()
	doc, err := s.Work(context.Background(), 1)
	if err != nil {
		t.Fatalf("рендер тома: %v", err)
	}
	want := "https://lib.example.org/works/1-lenin-t42"
	if doc.Canonical != want {
		t.Errorf("Canonical = %q, ожидалось %q", doc.Canonical, want)
	}
	// Карточка превью остаётся числовой: её адресует только наш HTML.
	if doc.ImageURL != "https://lib.example.org/og/work/1.png" {
		t.Errorf("ImageURL = %q", doc.ImageURL)
	}
}

// Служебные передние листы скрыты из каталога — открывать их краулеру тоже
// незачем: это дубль текста тома под вторым адресом.
func TestWorkFrontMatterIsNotFound(t *testing.T) {
	s := volumeSource()
	parent := int64(1)
	s.Works.(*fakeWorks).byID[2] = &models.Work{
		ID: 2, Title: "Передние листы", Role: models.WorkRoleFrontMatter, ParentWorkID: &parent,
	}

	if _, err := s.Work(context.Background(), 2); err == nil {
		t.Fatal("ожидалась ошибка ErrNotFound для служебной работы")
	}
}

// Три дешёвые мелочи итогового ревью: пустой ключ "author" в JSON-LD — ошибка
// проверки в Яндекс.Вебмастере. У тома без автора (подборка, коллективный
// труд) ключа быть не должно вовсе, а не пустой строки.
func TestWorkWithoutAuthorOmitsJSONLDAuthorKey(t *testing.T) {
	s := volumeSource()
	s.Works.(*fakeWorks).byID[3] = &models.Work{
		ID: 3, Title: "Подборка без автора", Role: models.WorkRoleVolume,
	}

	doc, err := s.Work(context.Background(), 3)
	if err != nil {
		t.Fatalf("Work: %v", err)
	}
	jsonLD, ok := doc.JSONLD.(map[string]any)
	if !ok {
		t.Fatalf("JSONLD не map[string]any: %T", doc.JSONLD)
	}
	if _, present := jsonLD["author"]; present {
		t.Errorf("ключ author не должен печататься для тома без автора: %#v", jsonLD)
	}
}

func TestWorkMissingIsNotFound(t *testing.T) {
	if _, err := volumeSource().Work(context.Background(), 999); err == nil {
		t.Fatal("ожидалась ошибка для отсутствующей работы")
	}
}

func TestEditionDocListsVolumes(t *testing.T) {
	s := volumeSource()
	s.Editions.(*fakeEditions).works[7] = []*models.Work{
		{ID: 1, Title: "Том 42", Role: models.WorkRoleVolume, Slug: "lenin-t42"},
		{ID: 2, Title: "Передние листы", Role: models.WorkRoleFrontMatter},
	}

	doc, err := s.Edition(context.Background(), 7)
	if err != nil {
		t.Fatalf("Edition: %v", err)
	}
	// Канон — со слагом задачи 8: голый номер после задачи 10 стал бы
	// лишним 301 на каждый переход по оглавлению издания.
	if !strings.Contains(doc.Body, `href="/works/1-lenin-t42"`) {
		t.Errorf("в теле издания нет ссылки на том со слагом:\n%s", doc.Body)
	}
	if strings.Contains(doc.Body, `href="/works/2"`) {
		t.Errorf("служебные передние листы попали в список томов издания:\n%s", doc.Body)
	}
	if doc.Canonical != "https://lib.example.org/editions/7-lenin" {
		t.Errorf("canonical издания: %q", doc.Canonical)
	}
	if doc.OGType != "website" {
		t.Errorf("тип издания: %q", doc.OGType)
	}
}

// Выборка издания для карточки тома необязательна: её провал не должен
// ронять карточку, но и не должен теряться молча (прецедент —
// internal/api/work_handler.go, ListChildren). Тест проверяет только
// внешне видимое поведение — карточка цела, строки издания нет; сам факт
// записи в лог не проверяется.
func TestWorkEditionLookupFailureKeepsCardWithoutEditionLine(t *testing.T) {
	s := volumeSource()
	s.Editions.(*fakeEditions).err = fmt.Errorf("failed to get edition: %w", errors.New("db down"))

	doc, err := s.Work(context.Background(), 1)
	if err != nil {
		t.Fatalf("Work: %v", err)
	}
	editionTitle := "В. И. Ленин. Полное собрание сочинений"
	if strings.Contains(doc.Body, Esc(editionTitle)) {
		t.Errorf("строка издания не должна попасть в тело при отказе репозитория:\n%s", doc.Body)
	}
	if strings.Contains(doc.Description, editionTitle) {
		t.Errorf("издание не должно попасть в описание при отказе репозитория: %q", doc.Description)
	}
}

// Настоящий репозиторий на pgx.ErrNoRows отдаёт "work not found" без
// sentinel-обёртки — рендерер обязан узнавать в этом «не найдено» и отдавать
// ErrNotFound, а не пятисотить.
func TestWorkRepositoryNotFoundTextBecomesErrNotFound(t *testing.T) {
	s := volumeSource()
	s.Works.(*fakeWorks).err = fmt.Errorf("work not found")

	_, err := s.Work(context.Background(), 1)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("ожидался ErrNotFound, получено: %v", err)
	}
}

// А поломка базы ("failed to ...") — это не «не найдено»: под ErrNotFound она
// уехала бы краулеру голой 404, спрятав живую аварию за неотличимым «нет
// страницы». Исходная ошибка обязана дойти наружу.
func TestWorkRepositoryFailureIsNotErrNotFound(t *testing.T) {
	s := volumeSource()
	boom := errors.New("connection refused")
	s.Works.(*fakeWorks).err = fmt.Errorf("failed to get work: %w", boom)

	_, err := s.Work(context.Background(), 1)
	if errors.Is(err, ErrNotFound) {
		t.Fatalf("поломка базы не должна маскироваться под ErrNotFound: %v", err)
	}
	if !errors.Is(err, boom) {
		t.Fatalf("исходная ошибка потеряна: %v", err)
	}
}
