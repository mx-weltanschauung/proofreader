package repository

import (
	"context"
	"errors"
	"sync"
	"testing"

	"proofreader/internal/models"
)

// Избранное держится на каскадах (глава, том, собрание) и на замке строки
// собрания — подделкой это не проверить. Без PROOFREADER_TEST_DB_URL тест
// пропускается.
type highlightsFixture struct {
	editions *EditionRepository
	works    *WorkRepository
	chapters *ChapterRepository
	edition  *models.Edition
	volume   *models.Work
	first    *models.Chapter
	second   *models.Chapter
}

func newHighlightsFixture(t *testing.T) highlightsFixture {
	t.Helper()
	ctx := context.Background()
	pool := testPool(t)
	f := highlightsFixture{
		editions: NewEditionRepository(pool),
		works:    NewWorkRepository(pool),
		chapters: NewChapterRepository(pool),
	}
	f.edition = &models.Edition{Title: "проба-избранное", Slug: "proba-izbrannoe"}
	if err := f.editions.Create(ctx, f.edition); err != nil {
		t.Fatalf("create edition: %v", err)
	}
	f.volume = newVolume(t, f.works, "проба-избранное-том")
	number := 23
	f.volume.EditionID = &f.edition.ID
	f.volume.VolumeNumber = &number
	if err := f.works.Update(ctx, f.volume); err != nil {
		t.Fatalf("attach volume: %v", err)
	}
	for i, title := range []string{"КАПИТАЛ. Критика политической экономии", "Немецкая идеология"} {
		c := &models.Chapter{
			WorkID: f.volume.ID, Title: title, Type: "chapter",
			OrderNumber: i + 1, StartPage: i*10 + 1, EndPage: i*10 + 10,
		}
		if err := f.chapters.Create(ctx, c); err != nil {
			t.Fatalf("create chapter: %v", err)
		}
		if i == 0 {
			f.first = c
		} else {
			f.second = c
		}
	}
	return f
}

func TestEditionHighlightsReplaceKeepsOrderAndLabels(t *testing.T) {
	ctx := context.Background()
	f := newHighlightsFixture(t)

	err := f.editions.ReplaceHighlights(ctx, f.edition.ID, []models.HighlightInput{
		{ChapterID: f.second.ID, Label: ""},
		{ChapterID: f.first.ID, Label: "Капитал, т. I"},
	})
	if err != nil {
		t.Fatalf("replace: %v", err)
	}
	got, err := f.editions.ListHighlights(ctx, f.edition.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 2 || got[0].ChapterID != f.second.ID || got[1].ChapterID != f.first.ID {
		t.Fatalf("порядок не тот: %+v", got)
	}
	if got[1].Label != "Капитал, т. I" || got[1].ChapterTitle != f.first.Title {
		t.Fatalf("подпись или заглавие потеряны: %+v", got[1])
	}
	if got[1].WorkID != f.volume.ID || got[1].VolumeNumber == nil || *got[1].VolumeNumber != 23 {
		t.Fatalf("координата тома не та: %+v", got[1])
	}
	if got[1].WorkSlug != "proba-izbrannoe-t23" || got[1].ChapterSlug == "" {
		t.Fatalf("слаги не собраны: work=%q chapter=%q", got[1].WorkSlug, got[1].ChapterSlug)
	}

	// Второй вызов заменяет список целиком, а не дописывает.
	if err := f.editions.ReplaceHighlights(ctx, f.edition.ID, []models.HighlightInput{
		{ChapterID: f.first.ID},
	}); err != nil {
		t.Fatalf("replace again: %v", err)
	}
	got, _ = f.editions.ListHighlights(ctx, f.edition.ID)
	if len(got) != 1 || got[0].ChapterID != f.first.ID {
		t.Fatalf("список не заменён: %+v", got)
	}
}

func TestEditionHighlightsRejectForeignChapter(t *testing.T) {
	ctx := context.Background()
	f := newHighlightsFixture(t)
	stranger := newVolume(t, f.works, "проба-чужой-том")
	foreign := &models.Chapter{WorkID: stranger.ID, Title: "Чужая", Type: "chapter", OrderNumber: 1, StartPage: 1, EndPage: 2}
	if err := f.chapters.Create(ctx, foreign); err != nil {
		t.Fatalf("create foreign chapter: %v", err)
	}
	if err := f.editions.ReplaceHighlights(ctx, f.edition.ID, []models.HighlightInput{
		{ChapterID: f.first.ID}, {ChapterID: foreign.ID},
	}); !errors.Is(err, ErrHighlightForeignChapter) {
		t.Fatalf("чужая глава прошла: %v", err)
	}
	// Отказ ничего не записал — даже свою главу из того же списка.
	got, _ := f.editions.ListHighlights(ctx, f.edition.ID)
	if len(got) != 0 {
		t.Fatalf("частичная запись: %+v", got)
	}
}

func TestEditionHighlightsCascadeOnChapterAndVolume(t *testing.T) {
	ctx := context.Background()
	f := newHighlightsFixture(t)
	if err := f.editions.ReplaceHighlights(ctx, f.edition.ID, []models.HighlightInput{
		{ChapterID: f.first.ID}, {ChapterID: f.second.ID},
	}); err != nil {
		t.Fatalf("replace: %v", err)
	}
	if err := f.chapters.Delete(ctx, f.first.ID); err != nil {
		t.Fatalf("delete chapter: %v", err)
	}
	got, _ := f.editions.ListHighlights(ctx, f.edition.ID)
	if len(got) != 1 || got[0].ChapterID != f.second.ID {
		t.Fatalf("удалённая глава осталась в избранном: %+v", got)
	}
	if err := f.works.Delete(ctx, f.volume.ID); err != nil {
		t.Fatalf("delete volume: %v", err)
	}
	all, err := f.editions.ListAllHighlights(ctx)
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) != 0 {
		t.Fatalf("снятый том оставил избранное: %+v", all)
	}
}

// Два одновременных сохранения одного собрания: без замка строки собрания оба
// удаляли, оба вставляли одну главу, и второй падал на первичном ключе.
func TestEditionHighlightsConcurrentReplaceBothSucceed(t *testing.T) {
	ctx := context.Background()
	f := newHighlightsFixture(t)
	base := [][]models.HighlightInput{
		{{ChapterID: f.first.ID, Label: "А"}, {ChapterID: f.second.ID}},
		{{ChapterID: f.first.ID, Label: "Б"}},
	}
	// Восемь сохранений разом, старт по общему сигналу: двух, стартующих
	// вразнобой, мало — без замка они иногда просто успевают друг за другом.
	const writers = 8
	errs := make([]error, writers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs[i] = f.editions.ReplaceHighlights(ctx, f.edition.ID, base[i%len(base)])
		}(i)
	}
	close(start)
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("сохранение %d упало: %v", i, err)
		}
	}
	got, _ := f.editions.ListHighlights(ctx, f.edition.ID)
	if len(got) != 1 && len(got) != 2 {
		t.Fatalf("итог не похож ни на один из списков: %+v", got)
	}
}

// Том, переехавший в другое собрание, уносит с собой и право на витрину
// старого: иначе карточка старого собрания ссылается в чужое, а каждое
// следующее сохранение её списка падает на «глава не из этого собрания».
func TestEditionHighlightsDropWhenVolumeMovesToAnotherEdition(t *testing.T) {
	ctx := context.Background()
	f := newHighlightsFixture(t)
	other := &models.Edition{Title: "проба-избранное-другое", Slug: "proba-izbrannoe-drugoe"}
	if err := f.editions.Create(ctx, other); err != nil {
		t.Fatalf("create other edition: %v", err)
	}
	// Вторая книга остаётся в старом собрании, чтобы список после переезда не пустел.
	stay := newVolume(t, f.works, "проба-избранное-остаётся")
	number := 24
	stay.EditionID = &f.edition.ID
	stay.VolumeNumber = &number
	if err := f.works.Update(ctx, stay); err != nil {
		t.Fatalf("attach stay: %v", err)
	}
	stayChapter := &models.Chapter{WorkID: stay.ID, Title: "Остающаяся", Type: "chapter", OrderNumber: 1, StartPage: 1, EndPage: 2}
	if err := f.chapters.Create(ctx, stayChapter); err != nil {
		t.Fatalf("create chapter: %v", err)
	}
	if err := f.editions.ReplaceHighlights(ctx, f.edition.ID, []models.HighlightInput{
		{ChapterID: f.first.ID}, {ChapterID: stayChapter.ID},
	}); err != nil {
		t.Fatalf("replace: %v", err)
	}

	f.volume.EditionID = &other.ID
	if err := f.works.Update(ctx, f.volume); err != nil {
		t.Fatalf("move volume: %v", err)
	}

	for name, list := range map[string]func() ([]models.EditionHighlight, error){
		"ListHighlights":    func() ([]models.EditionHighlight, error) { return f.editions.ListHighlights(ctx, f.edition.ID) },
		"ListAllHighlights": func() ([]models.EditionHighlight, error) { return f.editions.ListAllHighlights(ctx) },
	} {
		got, err := list()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, h := range got {
			if h.ChapterID == f.first.ID {
				t.Fatalf("%s отдаёт главу переехавшего тома: %+v", name, h)
			}
		}
	}
	if err := f.editions.ReplaceHighlights(ctx, f.edition.ID, []models.HighlightInput{
		{ChapterID: stayChapter.ID},
	}); err != nil {
		t.Fatalf("сохранение оставшегося упало: %v", err)
	}
}
