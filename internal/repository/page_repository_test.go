package repository

import (
	"context"
	"testing"
	"time"

	"proofreader/internal/models"
)

// TestGetPagesByNumbers проверяет главное свойство выборки: она берёт ровно
// перечисленные номера, а не диапазон между крайними. Адреса указателя
// разбросаны по тому, и расширение до min…max вытянуло бы сотни страниц.
func TestGetPagesByNumbers(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	works := NewWorkRepository(pool)
	pages := NewPageRepository(pool)

	vol := newVolume(t, works, "проба выборки страниц")
	for _, n := range []int{1, 2, 3, 4, 5} {
		p := &models.Page{
			WorkID:          vol.ID,
			PageNumber:      n,
			ContentMarkdown: "стр. " + string(rune('0'+n)),
			Status:          models.PageStatusNotProofread,
		}
		if err := pages.Create(ctx, p); err != nil {
			t.Fatalf("создать страницу %d: %v", n, err)
		}
	}

	got, err := pages.GetPagesByNumbers(ctx, vol.ID, []int{1, 5})
	if err != nil {
		t.Fatalf("GetPagesByNumbers: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("страниц %d, ожидалось 2", len(got))
	}
	if got[0].PageNumber != 1 || got[1].PageNumber != 5 {
		t.Fatalf("номера %d, %d; ожидались 1 и 5", got[0].PageNumber, got[1].PageNumber)
	}

	empty, err := pages.GetPagesByNumbers(ctx, vol.ID, nil)
	if err != nil {
		t.Fatalf("пустой список: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("пустой список вернул %d страниц", len(empty))
	}
}

// realEdit воспроизводит ровно то, что делает applyPageEdit (internal/api),
// не завися от него самого (это репозиторный тест, зависеть от internal/api
// нельзя — импорт по кругу): снимок ПРЕЖНЕГО текста версией, затем запись
// нового текста в саму полосу. Возвращает созданную версию — её CreatedAt
// нужен тестам для проверки порядка.
func realEdit(t *testing.T, pages *PageRepository, versions *PageVersionRepository, page *models.Page, versionNumber int, newContent string) *models.PageVersion {
	t.Helper()
	ctx := context.Background()
	v := &models.PageVersion{
		PageID: page.ID, ContentMarkdown: page.ContentMarkdown,
		VersionNumber: versionNumber, UserID: 1,
	}
	if err := versions.Create(ctx, v); err != nil {
		t.Fatalf("create version %d: %v", versionNumber, err)
	}
	page.ContentMarkdown = newContent
	if err := pages.Update(ctx, page); err != nil {
		t.Fatalf("update page to version %d: %v", versionNumber, err)
	}
	return v
}

// noopEdit — холостая запись page_versions: содержимое версии равно
// ТЕКУЩЕМУ тексту полосы, то есть текст она не меняла. Так шлёт машинная
// вычитка, проставляя статус без единой правки, — applyPageEdit пишет версию
// безусловно, в том числе на такой вызов.
func noopEdit(t *testing.T, versions *PageVersionRepository, page *models.Page, versionNumber int) *models.PageVersion {
	t.Helper()
	v := &models.PageVersion{
		PageID: page.ID, ContentMarkdown: page.ContentMarkdown,
		VersionNumber: versionNumber, UserID: 1,
	}
	if err := versions.Create(context.Background(), v); err != nil {
		t.Fatalf("create noop version %d: %v", versionNumber, err)
	}
	return v
}

// Дата последней правки текста — MAX(page_versions.created_at) СРЕДИ ВЕРСИЙ,
// чей текст отличается от текущего; а НЕ pages.updated_at (триггер двигает
// его при любой записи в строку, включая смену статуса без правки текста) и
// НЕ голый MAX по всем версиям (холостая запись двигала бы дату вслед за
// собой — см. TestPageRepository_TextEditedAtIgnoresNoopVersions). Полоса без
// версий правок не знала — там nil, и это честное «не правилась».
func TestPageRepository_TextEditedAtIsVersionTime(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	works := NewWorkRepository(pool)
	pages := NewPageRepository(pool)
	versions := NewPageVersionRepository(pool)

	volume := newVolume(t, works, "проба-дата-правки")
	page := &models.Page{
		WorkID: volume.ID, PageNumber: 1, ContentMarkdown: "текст",
		Status: models.PageStatusNotProofread,
	}
	if err := pages.Create(ctx, page); err != nil {
		t.Fatalf("create page: %v", err)
	}

	fresh, err := pages.GetByID(ctx, page.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if fresh.TextEditedAt != nil {
		t.Errorf("не правленная полоса несёт дату %v, ожидался nil", fresh.TextEditedAt)
	}

	v1 := realEdit(t, pages, versions, page, 1, "текст-2")

	edited, err := pages.GetByID(ctx, page.ID)
	if err != nil {
		t.Fatalf("get after version: %v", err)
	}
	if edited.TextEditedAt == nil {
		t.Fatal("правленная полоса без даты правки")
	}
	if !edited.TextEditedAt.Equal(v1.CreatedAt) {
		t.Errorf("дата правки %v, ожидалась дата версии %v", edited.TextEditedAt, v1.CreatedAt)
	}

	// То же самое вторым чтением — фронт ходит именно им (usePageByNumber).
	byNumber, err := pages.GetByWorkAndPageNumber(ctx, volume.ID, 1)
	if err != nil {
		t.Fatalf("get by number: %v", err)
	}
	if byNumber.TextEditedAt == nil || !byNumber.TextEditedAt.Equal(*edited.TextEditedAt) {
		t.Errorf("по номеру дата %v, по id %v — должны совпадать", byNumber.TextEditedAt, edited.TextEditedAt)
	}

	// Списочное чтение поле не везёт: коррелированный подзапрос на каждую из
	// 840 строк тома платится ни за что, а на экране полосы его нет.
	list, err := pages.ListByWork(ctx, volume.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].TextEditedAt != nil {
		t.Errorf("списочное чтение везёт дату правки, а не должно")
	}
}

// Холостые версии (текст версии равен текущему тексту полосы — так шлёт
// машинная вычитка, меняя только статус) не подтверждают промах цитаты
// датой, к тексту не имеющей отношения: 37% корпуса показывали такую дату
// (замер живого корпуса, средним опозданием 21,3 дня) до этого исправления.
func TestPageRepository_TextEditedAtIgnoresNoopVersions(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	works := NewWorkRepository(pool)
	pages := NewPageRepository(pool)
	versions := NewPageVersionRepository(pool)

	volume := newVolume(t, works, "проба-холостая-правка")

	t.Run("все версии холостые -> nil", func(t *testing.T) {
		page := &models.Page{
			WorkID: volume.ID, PageNumber: 1, ContentMarkdown: "текст",
			Status: models.PageStatusNotProofread,
		}
		if err := pages.Create(ctx, page); err != nil {
			t.Fatalf("create page: %v", err)
		}
		noopEdit(t, versions, page, 1)

		got, err := pages.GetByID(ctx, page.ID)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if got.TextEditedAt != nil {
			t.Errorf("холостая версия дала дату %v, ожидался nil", got.TextEditedAt)
		}
	})

	t.Run("холостая вторая версия не двигает дату", func(t *testing.T) {
		page := &models.Page{
			WorkID: volume.ID, PageNumber: 2, ContentMarkdown: "было",
			Status: models.PageStatusNotProofread,
		}
		if err := pages.Create(ctx, page); err != nil {
			t.Fatalf("create page: %v", err)
		}

		v1 := realEdit(t, pages, versions, page, 1, "стало")
		time.Sleep(10 * time.Millisecond) // порядок created_at должен быть однозначным
		noopEdit(t, versions, page, 2)    // машинная вычитка проставляет статус без правки

		got, err := pages.GetByID(ctx, page.ID)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if got.TextEditedAt == nil {
			t.Fatal("настоящая правка потеряна за холостой версией")
		}
		if !got.TextEditedAt.Equal(v1.CreatedAt) {
			t.Errorf("дата %v, ожидалась дата ПЕРВОЙ (настоящей) правки %v", got.TextEditedAt, v1.CreatedAt)
		}
	})

	t.Run("две настоящие правки -> дата поздней", func(t *testing.T) {
		page := &models.Page{
			WorkID: volume.ID, PageNumber: 3, ContentMarkdown: "v1",
			Status: models.PageStatusNotProofread,
		}
		if err := pages.Create(ctx, page); err != nil {
			t.Fatalf("create page: %v", err)
		}

		realEdit(t, pages, versions, page, 1, "v2")
		time.Sleep(10 * time.Millisecond)
		v2 := realEdit(t, pages, versions, page, 2, "v3")

		got, err := pages.GetByID(ctx, page.ID)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if got.TextEditedAt == nil || !got.TextEditedAt.Equal(v2.CreatedAt) {
			t.Errorf("дата %v, ожидалась дата ПОЗДНЕЙ правки %v", got.TextEditedAt, v2.CreatedAt)
		}
	})
}
