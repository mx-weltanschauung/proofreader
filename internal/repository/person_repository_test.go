package repository

import (
	"context"
	"errors"
	"testing"

	"proofreader/internal/models"
)

type creditFixture struct {
	persons  *PersonRepository
	chapters *ChapterRepository
	work     *models.Work
	article  *models.Chapter
}

func newCreditFixture(t *testing.T, slug string) creditFixture {
	t.Helper()
	ctx := context.Background()
	pool := testPool(t)
	journals := NewJournalRepository(pool)
	f := creditFixture{persons: NewPersonRepository(pool), chapters: NewChapterRepository(pool)}
	j := newJournal(t, journals, slug)
	_, f.work = newIssue(t, journals, j, 1928, 12, 12)
	kind := "статья"
	f.article = &models.Chapter{WorkID: f.work.ID, Title: "Памяти Иосифа Дидгена", Type: "chapter",
		OrderNumber: 1, StartPage: 5, EndPage: 25, ArticleKind: &kind}
	if err := f.chapters.Create(ctx, f.article); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestPersonSlugCollisionGetsSuffix(t *testing.T) {
	ctx := context.Background()
	repo := NewPersonRepository(testPool(t))
	a := &models.Person{Name: "А. Деборин"}
	b := &models.Person{Name: "А. Деборин"}
	if err := repo.Create(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, b); err != nil {
		t.Fatal(err)
	}
	if a.Slug == "" || b.Slug != a.Slug+"-2" {
		t.Fatalf("слаги %q и %q", a.Slug, b.Slug)
	}
	if a.SortKey != "деборин а" {
		t.Fatalf("ключ %q", a.SortKey)
	}
}

func TestCreditsReplaceAndListByWork(t *testing.T) {
	ctx := context.Background()
	f := newCreditFixture(t, "cr-list")
	p := &models.Person{Name: "Гр. Баммель"}
	if err := f.persons.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	in := []models.CreditInput{
		{Role: models.CreditRoleAuthor, Printed: "Гр. Баммель", PersonID: &p.ID},
		{Role: models.CreditRoleTranslator, Printed: "Н. Н."},
	}
	if err := f.persons.ReplaceCredits(ctx, f.article.ID, in); err != nil {
		t.Fatal(err)
	}
	m, err := f.persons.ListCreditsByWork(ctx, f.work.ID)
	if err != nil {
		t.Fatal(err)
	}
	got := m[f.article.ID]
	if len(got) != 2 || got[0].Position != 1 || got[0].PersonSlug != p.Slug || got[1].PersonID != nil {
		t.Fatalf("подписи: %+v", got)
	}
	// Замена целиком: пустой список снимает подпись.
	if err := f.persons.ReplaceCredits(ctx, f.article.ID, nil); err != nil {
		t.Fatal(err)
	}
	m, _ = f.persons.ListCreditsByWork(ctx, f.work.ID)
	if len(m[f.article.ID]) != 0 {
		t.Fatalf("после снятия: %+v", m[f.article.ID])
	}
	missing := int64(999999)
	err = f.persons.ReplaceCredits(ctx, f.article.ID, []models.CreditInput{{Role: "author", Printed: "X", PersonID: &missing}})
	if !errors.Is(err, ErrPersonNotFound) {
		t.Fatalf("несуществующий человек: %v", err)
	}
}

func TestPersonDetailListsArticles(t *testing.T) {
	ctx := context.Background()
	f := newCreditFixture(t, "cr-detail")
	p := &models.Person{Name: "Гр. Баммель"}
	_ = f.persons.Create(ctx, p)
	_ = f.persons.ReplaceCredits(ctx, f.article.ID, []models.CreditInput{{Role: "author", Printed: "Гр. Баммель", PersonID: &p.ID}})
	d, err := f.persons.Detail(ctx, p.Slug)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Articles) != 1 || d.Articles[0].Title != "Памяти Иосифа Дидгена" || d.Articles[0].Label != "12" ||
		d.Articles[0].Year != 1928 || d.Articles[0].JournalSlug != "cr-detail" || d.Articles[0].ArticleKind != "статья" {
		t.Fatalf("статьи: %+v", d.Articles)
	}
	if _, err := f.persons.Detail(ctx, "net-takogo"); !errors.Is(err, ErrPersonNotFound) {
		t.Fatalf("неизвестный: %v", err)
	}
}

func TestPersonMergeMovesCredits(t *testing.T) {
	ctx := context.Background()
	f := newCreditFixture(t, "cr-merge")
	keep := &models.Person{Name: "В. Позняков"}
	gone := &models.Person{Name: "В. Повняков"}
	_ = f.persons.Create(ctx, keep)
	_ = f.persons.Create(ctx, gone)
	_ = f.persons.ReplaceCredits(ctx, f.article.ID, []models.CreditInput{{Role: "author", Printed: "В. Позняков", PersonID: &gone.ID}})
	if err := f.persons.Merge(ctx, keep.ID, gone.ID); err != nil {
		t.Fatal(err)
	}
	m, _ := f.persons.ListCreditsByWork(ctx, f.work.ID)
	if c := m[f.article.ID]; len(c) != 1 || c[0].PersonID == nil || *c[0].PersonID != keep.ID {
		t.Fatalf("подпись не переехала: %+v", c)
	}
	if _, err := f.persons.Detail(ctx, gone.Slug); !errors.Is(err, ErrPersonNotFound) {
		t.Fatalf("слитый остался: %v", err)
	}
	if err := f.persons.Merge(ctx, keep.ID, keep.ID); !errors.Is(err, ErrMergeSelf) {
		t.Fatalf("слияние с собой: %v", err)
	}
}

func TestPersonSurvivesIssueDeletion(t *testing.T) {
	ctx := context.Background()
	f := newCreditFixture(t, "cr-survive")
	p := &models.Person{Name: "Я. Захер"}
	_ = f.persons.Create(ctx, p)
	_ = f.persons.ReplaceCredits(ctx, f.article.ID, []models.CreditInput{{Role: "author", Printed: "Я. Захер", PersonID: &p.ID}})
	if err := NewWorkRepository(testPool(t)).Delete(ctx, f.work.ID); err != nil {
		t.Fatal(err)
	}
	d, err := f.persons.Detail(ctx, p.Slug)
	if err != nil || len(d.Articles) != 0 {
		t.Fatalf("человек после удаления номера: %+v, %v", d, err)
	}
}

func TestPersonSearchByPrefix(t *testing.T) {
	ctx := context.Background()
	repo := NewPersonRepository(testPool(t))
	_ = repo.Create(ctx, &models.Person{Name: "И. Рубин"})
	_ = repo.Create(ctx, &models.Person{Name: "А. Рубин"})
	_ = repo.Create(ctx, &models.Person{Name: "Я. Захер"})
	got, err := repo.Search(ctx, "Руб", 10)
	if err != nil || len(got) != 2 {
		t.Fatalf("поиск: %+v, %v", got, err)
	}
	got, _ = repo.Search(ctx, "%", 10)
	if len(got) != 0 {
		t.Fatalf("знак шаблона сработал шаблоном: %+v", got)
	}
}

// Финальная рецензия: полное имя без инициалов находится по фамилии.
func TestPersonSearchFindsFullNameBySurname(t *testing.T) {
	ctx := context.Background()
	repo := NewPersonRepository(testPool(t))
	if err := repo.Create(ctx, &models.Person{Name: "Леопольд Авербах"}); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Search(ctx, "Авербах", 10)
	if err != nil || len(got) != 1 || got[0].Name != "Леопольд Авербах" {
		t.Fatalf("поиск по фамилии: %+v, %v", got, err)
	}
}

// Явный id человека: занятый — ErrIDTaken (обработчик отвечает 409), а не
// второй человек и не перебор слагов.
func TestPersonExplicitIDTaken(t *testing.T) {
	ctx := context.Background()
	repo := NewPersonRepository(testPool(t))
	first := &models.Person{ID: 7171, Name: "Я. Захер"}
	if err := repo.Create(ctx, first); err != nil {
		t.Fatal(err)
	}
	if first.ID != 7171 {
		t.Fatalf("id %d", first.ID)
	}
	if err := repo.Create(ctx, &models.Person{ID: 7171, Name: "И. Рубин"}); !errors.Is(err, ErrIDTaken) {
		t.Fatalf("занятый id человека: %v", err)
	}
	if err := repo.Create(ctx, &models.Person{ID: 7171, Name: "Я. Захер"}); !errors.Is(err, ErrIDTaken) {
		t.Fatalf("занятый id при том же имени: %v", err)
	}
}
