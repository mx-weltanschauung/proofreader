package repository

import (
	"context"
	"testing"

	"proofreader/internal/models"
)

// newVolume создаёт работу-том и удаляет её по завершении теста.
func newVolume(t *testing.T, repo *WorkRepository, title string) *models.Work {
	t.Helper()
	w := &models.Work{
		Title: title, Author: "проба", Language: "ru", Country: "ru",
		Status: models.WorkStatusDraft, Role: models.WorkRoleVolume,
		NumberingStyle: models.NumberingArabic, OwnerID: 1,
	}
	if err := repo.Create(context.Background(), w); err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _ = repo.Delete(context.Background(), w.ID) })
	return w
}

// Главная показывает отдельным списком работы, не приписанные ни к одному
// собранию: приписать их через интерфейс нечем, и без этого списка они не
// попадали бы никуда. Раньше список набирался на клиенте — «дай двести работ,
// оставь те, у кого edition_id пуст», — и вёз 59 КБ полных строк ради имени и
// ссылки. Отбор должен повторять List слово в слово: только тома верхнего
// уровня, и в том же порядке, иначе список на главной переставится.
func TestWorkRepository_ListWithoutEdition(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	repo := NewWorkRepository(pool)
	editions := NewEditionRepository(pool)

	edition := &models.Edition{Title: "проба-вне", Slug: "proba-vne"}
	if err := editions.Create(ctx, edition); err != nil {
		t.Fatalf("create edition: %v", err)
	}
	t.Cleanup(func() { _ = editions.Delete(ctx, edition.ID) })

	loose := newVolume(t, repo, "вне-сама-по-себе")

	attached := newVolume(t, repo, "вне-в-собрании")
	attached.EditionID = &edition.ID
	if err := repo.Update(ctx, attached); err != nil {
		t.Fatalf("update attached: %v", err)
	}

	// Служебная работа тома (передние листы) собранию тоже не принадлежит, но
	// она уже видна с карточки родителя и в общий список не годится.
	child := &models.Work{
		Title: "вне-передние листы", Language: "ru", Country: "ru",
		Status: models.WorkStatusDraft, Role: models.WorkRoleFrontMatter,
		NumberingStyle: models.NumberingRoman, ParentWorkID: &loose.ID, OwnerID: 1,
	}
	if err := repo.Create(ctx, child); err != nil {
		t.Fatalf("create child: %v", err)
	}
	t.Cleanup(func() { _ = repo.Delete(ctx, child.ID) })

	got, err := repo.ListWithoutEdition(ctx)
	if err != nil {
		t.Fatalf("list without edition: %v", err)
	}

	titles := map[string]bool{}
	for _, w := range got {
		titles[w.Title] = true
		if w.ID == 0 {
			t.Errorf("работа %q без id", w.Title)
		}
	}
	if !titles["вне-сама-по-себе"] {
		t.Errorf("работа вне собраний не попала в список: %v", titles)
	}
	if titles["вне-в-собрании"] {
		t.Errorf("работа из собрания попала в список вне собраний")
	}
	if titles["вне-передние листы"] {
		t.Errorf("служебная работа тома попала в список вне собраний")
	}
}

func TestWorkRepository_ChildRoundTrip(t *testing.T) {
	ctx := context.Background()
	repo := NewWorkRepository(testPool(t))
	parent := newVolume(t, repo, "roundtrip-родитель")

	child := &models.Work{
		Title: "roundtrip-передние листы", Language: "ru", Country: "ru",
		Status: models.WorkStatusDraft, Role: models.WorkRoleFrontMatter,
		NumberingStyle: models.NumberingRoman, PageOffset: -1,
		ParentWorkID: &parent.ID, OwnerID: 1,
	}
	if err := repo.Create(ctx, child); err != nil {
		t.Fatalf("create child: %v", err)
	}
	t.Cleanup(func() { _ = repo.Delete(ctx, child.ID) })

	got, err := repo.GetByID(ctx, child.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Role != models.WorkRoleFrontMatter {
		t.Errorf("Role = %q, want %q", got.Role, models.WorkRoleFrontMatter)
	}
	if got.NumberingStyle != models.NumberingRoman {
		t.Errorf("NumberingStyle = %q, want %q", got.NumberingStyle, models.NumberingRoman)
	}
	if got.PageOffset != -1 {
		t.Errorf("PageOffset = %d, want -1", got.PageOffset)
	}
	if got.ParentWorkID == nil || *got.ParentWorkID != parent.ID {
		t.Errorf("ParentWorkID = %v, want %d", got.ParentWorkID, parent.ID)
	}

	kids, err := repo.ListChildren(ctx, parent.ID)
	if err != nil {
		t.Fatalf("ListChildren: %v", err)
	}
	if len(kids) != 1 || kids[0].ID != child.ID {
		t.Fatalf("ListChildren = %+v, want [%d]", kids, child.ID)
	}
}

// Каталог показывает только тома: служебная работа в общий список не попадает.
func TestWorkRepository_ListSkipsChildren(t *testing.T) {
	ctx := context.Background()
	repo := NewWorkRepository(testPool(t))
	parent := newVolume(t, repo, "listskip-родитель")

	child := &models.Work{
		Title: "listskip-передние листы", Language: "ru", Country: "ru",
		Status: models.WorkStatusDraft, Role: models.WorkRoleFrontMatter,
		NumberingStyle: models.NumberingRoman, ParentWorkID: &parent.ID, OwnerID: 1,
	}
	if err := repo.Create(ctx, child); err != nil {
		t.Fatalf("create child: %v", err)
	}
	t.Cleanup(func() { _ = repo.Delete(ctx, child.ID) })

	works, err := repo.List(ctx, 500, 0, nil, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, w := range works {
		if w.ID == child.ID {
			t.Fatalf("служебная работа %d попала в каталог", child.ID)
		}
	}
}

// Регрессия: обработчик POST /api/works (ещё не знающий о ролях) собирает
// models.Work без Role/NumberingStyle. Create обязан нормализовать пустые
// значения сам — иначе они уходят в базу пустой строкой и падают на
// works_role_check/works_numbering_style_check, ломая создание любой работы.
func TestWorkRepository_CreateDefaultsEmptyRole(t *testing.T) {
	ctx := context.Background()
	repo := NewWorkRepository(testPool(t))

	w := &models.Work{
		Title: "defaultrole-без-роли", Author: "проба", Language: "ru", Country: "ru",
		Status: models.WorkStatusDraft, OwnerID: 1,
	}
	if err := repo.Create(ctx, w); err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _ = repo.Delete(ctx, w.ID) })

	got, err := repo.GetByID(ctx, w.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Role != models.WorkRoleVolume {
		t.Errorf("Role = %q, want %q", got.Role, models.WorkRoleVolume)
	}
	if got.NumberingStyle != models.NumberingArabic {
		t.Errorf("NumberingStyle = %q, want %q", got.NumberingStyle, models.NumberingArabic)
	}
}

// Констрейнт: служебная работа без родителя недопустима.
func TestWorkRepository_ChildWithoutParentRejected(t *testing.T) {
	ctx := context.Background()
	repo := NewWorkRepository(testPool(t))
	orphan := &models.Work{
		Title: "orphan-передние листы", Language: "ru", Country: "ru",
		Status: models.WorkStatusDraft, Role: models.WorkRoleFrontMatter,
		NumberingStyle: models.NumberingRoman, OwnerID: 1,
	}
	if err := repo.Create(ctx, orphan); err == nil {
		_ = repo.Delete(ctx, orphan.ID)
		t.Fatal("создание служебной работы без parent_work_id должно падать")
	}
}

// Подпись корешка обязана переживать полный оборот через базу. Пустая строка,
// а не NULL: «подписи нет» должно записываться ровно одним способом, иначе
// каждая проверка на фронте ветвится дважды.
func TestWorkRepository_ShelfLabelRoundTrip(t *testing.T) {
	ctx := context.Background()
	repo := NewWorkRepository(testPool(t))

	work := newVolume(t, repo, "подпись-оборот")
	if work.ShelfLabel != "" {
		t.Errorf("свежая работа пришла с подписью %q, ожидалась пустая", work.ShelfLabel)
	}

	work.ShelfLabel = "1893—1894"
	if err := repo.Update(ctx, work); err != nil {
		t.Fatalf("update: %v", err)
	}

	got, err := repo.GetByID(ctx, work.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ShelfLabel != "1893—1894" {
		t.Errorf("ShelfLabel = %q, ожидалось %q", got.ShelfLabel, "1893—1894")
	}

	got.ShelfLabel = ""
	if err := repo.Update(ctx, got); err != nil {
		t.Fatalf("update-сброс: %v", err)
	}
	cleared, err := repo.GetByID(ctx, work.ID)
	if err != nil {
		t.Fatalf("get после сброса: %v", err)
	}
	if cleared.ShelfLabel != "" {
		t.Errorf("после сброса ShelfLabel = %q, ожидалась пустая", cleared.ShelfLabel)
	}
}

// Описание тома обязано переживать полный оборот через базу тем же образом,
// что и shelf_label: этот тест — самый дешёвый способ поймать забытый
// плейсхолдер $18 в Update или сдвиг порядка колонок в workColumns/scanWork.
func TestWorkRepository_DescriptionRoundTrip(t *testing.T) {
	ctx := context.Background()
	repo := NewWorkRepository(testPool(t))

	work := newVolume(t, repo, "описание-оборот")
	if work.Description != "" {
		t.Errorf("свежая работа пришла с описанием %q, ожидалось пустое", work.Description)
	}

	work.Description = "Скан утерял несколько страниц в середине тома"
	if err := repo.Update(ctx, work); err != nil {
		t.Fatalf("update: %v", err)
	}

	got, err := repo.GetByID(ctx, work.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Description != "Скан утерял несколько страниц в середине тома" {
		t.Errorf("Description = %q, ожидалось %q", got.Description, "Скан утерял несколько страниц в середине тома")
	}

	got.Description = ""
	if err := repo.Update(ctx, got); err != nil {
		t.Fatalf("update-сброс: %v", err)
	}
	cleared, err := repo.GetByID(ctx, work.ID)
	if err != nil {
		t.Fatalf("get после сброса: %v", err)
	}
	if cleared.Description != "" {
		t.Errorf("после сброса Description = %q, ожидалась пустая", cleared.Description)
	}
}

// Название издания едет вместе с работой: подпись цитаты собирается на
// клиенте в пределах жеста копирования, и четвёртого запроса в этот момент нет.
// Читается на чтении, в works не хранится — иначе копия разошлась бы с
// editions.title молча (то же правило, что у Work.Slug).
func TestWorkRepository_EditionTitleTravelsWithWork(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	repo := NewWorkRepository(pool)
	editions := NewEditionRepository(pool)

	edition := &models.Edition{Title: "Проба. Сочинения, 2-е изд.", Slug: "proba-izd"}
	if err := editions.Create(ctx, edition); err != nil {
		t.Fatalf("create edition: %v", err)
	}
	t.Cleanup(func() { _ = editions.Delete(ctx, edition.ID) })

	volume := newVolume(t, repo, "проба-издание-том")
	volume.EditionID = &edition.ID
	if err := repo.Update(ctx, volume); err != nil {
		t.Fatalf("update volume: %v", err)
	}

	got, err := repo.GetByID(ctx, volume.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.EditionTitle != edition.Title {
		t.Errorf("издание тома %q, ожидалось %q", got.EditionTitle, edition.Title)
	}

	// Передние листы своего издания не имеют — координаты им достаются от
	// родителя тем же COALESCE, что и слаг.
	child := &models.Work{
		Title: "проба-издание-листы", Language: "ru", Country: "ru",
		Status: models.WorkStatusDraft, Role: models.WorkRoleFrontMatter,
		NumberingStyle: models.NumberingRoman, ParentWorkID: &volume.ID, OwnerID: 1,
	}
	if err := repo.Create(ctx, child); err != nil {
		t.Fatalf("create child: %v", err)
	}
	t.Cleanup(func() { _ = repo.Delete(ctx, child.ID) })

	gotChild, err := repo.GetByID(ctx, child.ID)
	if err != nil {
		t.Fatalf("get child: %v", err)
	}
	if gotChild.EditionTitle != edition.Title {
		t.Errorf("издание передних листов %q, ожидалось %q", gotChild.EditionTitle, edition.Title)
	}

	// Работа вне собрания: пустая строка, а не отказ запроса.
	loose := newVolume(t, repo, "проба-издание-нет")
	gotLoose, err := repo.GetByID(ctx, loose.ID)
	if err != nil {
		t.Fatalf("get loose: %v", err)
	}
	if gotLoose.EditionTitle != "" {
		t.Errorf("издание работы вне собрания %q, ожидалась пустая строка", gotLoose.EditionTitle)
	}
}
