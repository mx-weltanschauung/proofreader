package api

import (
	"testing"

	"proofreader/internal/models"
	"proofreader/internal/repository"
)

// Том с ненулевым смещением: печатная = внутренняя + page_offset. Ноль здесь
// был бы бесполезной проверкой — ошибка в формуле на нём невидима.
func chapterRow(itemID int64, order int, chapterID, workID int64) repository.ItemRow {
	return repository.ItemRow{
		Item: models.CollectionItem{
			ID:             itemID,
			Kind:           models.CollectionItemKindChapter,
			ChapterID:      ptrInt64(chapterID),
			WorkID:         ptrInt64(workID),
			SnapshotTitle:  "снимок",
			SnapshotAuthor: "снимок-автор",
			OrderNumber:    order,
		},
		ChapterTitle:     ptrStr("Людвиг Фейербах"),
		ChapterStartPage: ptrInt(265),
		ChapterEndPage:   ptrInt(313),
		WorkTitle:        ptrStr("Том 21"),
		WorkAuthor:       ptrStr("К. Маркс, Ф. Энгельс"),
		PageOffset:       ptrInt(4),
		VolumeNumber:     ptrInt(21),
		EditionTitle:     ptrStr("Сочинения, 2-е изд."),
	}
}

func TestBuildCollectionTOCPrintedPages(t *testing.T) {
	rows := []repository.ItemRow{chapterRow(1, 1, 100, 41)}
	chapters := []*models.Chapter{
		{ID: 100, WorkID: 41, Title: "Людвиг Фейербах", OrderNumber: 1, StartPage: 265, EndPage: 313},
		{ID: 101, WorkID: 41, ParentID: ptrInt64(100), Title: "I", OrderNumber: 1, StartPage: 265, EndPage: 271},
		{ID: 102, WorkID: 41, ParentID: ptrInt64(100), Title: "II. Идеализм и материализм",
			OrderNumber: 2, StartPage: 272, EndPage: 283},
		// Внук — глубина 2 при ненулевом смещении: тест ловит и потерю offset
		// на втором уровне рекурсии (дало бы 276 вместо 280), и его
		// накопление (дало бы 284 вместо 280).
		{ID: 103, WorkID: 41, ParentID: ptrInt64(102), Title: "1. Гегель",
			OrderNumber: 1, StartPage: 276, EndPage: 280},
	}

	entries := buildCollectionTOC(rows, chapters)

	if len(entries) != 1 {
		t.Fatalf("строк оглавления %d, ожидалась 1", len(entries))
	}
	e := entries[0]
	if e.Title != "Людвиг Фейербах" {
		t.Errorf("заголовок %q — должен быть живым, из chapters", e.Title)
	}
	if e.Source == nil {
		t.Fatal("источник не заполнен")
	}
	// 265 + 4 и 313 + 4: печатные, а не внутренние.
	if e.Source.PageStart != 269 || e.Source.PageEnd != 317 {
		t.Errorf("страницы %d—%d, ожидались печатные 269—317",
			e.Source.PageStart, e.Source.PageEnd)
	}
	if len(e.Children) != 2 {
		t.Fatalf("подглав %d, ожидалось 2", len(e.Children))
	}
	if e.Children[0].PageStart != 269 {
		t.Errorf("подглава начинается с %d, ожидалась печатная 269", e.Children[0].PageStart)
	}
	if e.Children[1].Title != "II. Идеализм и материализм" {
		t.Errorf("вторая подглава — %q", e.Children[1].Title)
	}
	if len(e.Children[1].Children) != 1 {
		t.Fatalf("внуков у второй подглавы %d, ожидался 1", len(e.Children[1].Children))
	}
	// 276 + 4: смещение применяется на глубине 2 ровно один раз.
	if e.Children[1].Children[0].PageStart != 280 {
		t.Errorf("внук начинается с %d, ожидалась печатная 280 (276+4 ровно один раз)",
			e.Children[1].Children[0].PageStart)
	}
}

func TestBuildCollectionTOCKeepsItemOrder(t *testing.T) {
	// Порядок задаёт составитель, а не номера томов и не алфавит.
	rows := []repository.ItemRow{chapterRow(7, 1, 100, 41), chapterRow(3, 2, 200, 42)}
	rows[1].ChapterTitle = ptrStr("Диалектика идеального")
	rows[1].WorkAuthor = ptrStr("Э. В. Ильенков")

	chapters := []*models.Chapter{
		{ID: 100, WorkID: 41, Title: "Людвиг Фейербах", OrderNumber: 1, StartPage: 265, EndPage: 313},
		{ID: 200, WorkID: 42, Title: "Диалектика идеального", OrderNumber: 1, StartPage: 8, EndPage: 77},
	}

	entries := buildCollectionTOC(rows, chapters)

	if len(entries) != 2 {
		t.Fatalf("строк %d, ожидалось 2", len(entries))
	}
	if entries[0].ID != 7 || entries[1].ID != 3 {
		t.Errorf("порядок строк %d,%d — ожидался порядок состава 7,3",
			entries[0].ID, entries[1].ID)
	}
}

func TestBuildCollectionTOCBrokenItem(t *testing.T) {
	// Главу пересоздал toc-chapters: chapter_id обнулился, элемент остался.
	row := chapterRow(5, 1, 0, 41)
	row.Item.ChapterID = nil
	row.ChapterTitle = nil
	row.ChapterStartPage = nil
	row.ChapterEndPage = nil

	entries := buildCollectionTOC([]repository.ItemRow{row}, nil)

	e := entries[0]
	if !e.Broken {
		t.Error("элемент с удалённым источником должен быть broken")
	}
	if e.Title != "снимок" {
		t.Errorf("заголовок %q, ожидался снимок", e.Title)
	}
	if len(e.Children) != 0 {
		t.Errorf("у битого элемента не может быть поддерева, а их %d", len(e.Children))
	}
	// Том известен и у битой строки — ради этого work_id и хранится отдельно.
	if e.Source == nil || e.Source.VolumeNumber == nil || *e.Source.VolumeNumber != 21 {
		t.Error("битая строка обязана помнить том")
	}
}

func TestBuildCollectionTOCWorkItem(t *testing.T) {
	// Элемент-работа: заголовок из works, поддерево — главы верхнего уровня.
	row := repository.ItemRow{
		Item: models.CollectionItem{
			ID: 9, Kind: models.CollectionItemKindWork, WorkID: ptrInt64(42),
			SnapshotTitle: "снимок", OrderNumber: 1,
		},
		WorkTitle:  ptrStr("Диалектическая логика"),
		WorkAuthor: ptrStr("Э. В. Ильенков"),
		PageOffset: ptrInt(0),
	}
	chapters := []*models.Chapter{
		{ID: 300, WorkID: 42, Title: "Очерк первый", OrderNumber: 1, StartPage: 5, EndPage: 40},
		{ID: 301, WorkID: 42, ParentID: ptrInt64(300), Title: "Вводные замечания",
			OrderNumber: 1, StartPage: 5, EndPage: 12},
		{ID: 302, WorkID: 42, Title: "Очерк второй", OrderNumber: 2, StartPage: 41, EndPage: 90},
	}

	entries := buildCollectionTOC([]repository.ItemRow{row}, chapters)

	e := entries[0]
	if e.Title != "Диалектическая логика" {
		t.Errorf("заголовок %q, ожидался из works", e.Title)
	}
	if len(e.Children) != 2 {
		t.Fatalf("глав верхнего уровня %d, ожидалось 2", len(e.Children))
	}
	if len(e.Children[0].Children) != 1 {
		t.Errorf("вложенность не собралась: у первой главы %d потомков",
			len(e.Children[0].Children))
	}
	if e.Source.PageStart != 5 || e.Source.PageEnd != 90 {
		t.Errorf("границы работы %d—%d, ожидались 5—90 по крайним главам",
			e.Source.PageStart, e.Source.PageEnd)
	}
}

func TestBuildCollectionTOCWorkItemBroken(t *testing.T) {
	// Элемент-работа с удалённым источником: том пропал, WorkTitle пуст.
	// Ветка отдельная от главы (row.WorkTitle == nil для kind=work) и не
	// покрывалась ни одним из прежних тестов.
	row := repository.ItemRow{
		Item: models.CollectionItem{
			ID: 11, Kind: models.CollectionItemKindWork, WorkID: ptrInt64(42),
			SnapshotTitle: "снимок-работы", SnapshotAuthor: "снимок-автор", OrderNumber: 1,
		},
	}

	entries := buildCollectionTOC([]repository.ItemRow{row}, nil)

	e := entries[0]
	if !e.Broken {
		t.Error("элемент-работа с удалённым source должен быть broken")
	}
	if e.Title != "снимок-работы" {
		t.Errorf("заголовок %q, ожидался снимок", e.Title)
	}
	if len(e.Children) != 0 {
		t.Errorf("у битого элемента-работы не может быть поддерева, а их %d", len(e.Children))
	}
}

func TestBuildCollectionTOCAuthorOverride(t *testing.T) {
	row := chapterRow(1, 1, 100, 41)
	row.Item.AuthorOverride = "Ф. Энгельс"
	chapters := []*models.Chapter{
		{ID: 100, WorkID: 41, Title: "Людвиг Фейербах", OrderNumber: 1, StartPage: 265, EndPage: 313},
	}

	e := buildCollectionTOC([]repository.ItemRow{row}, chapters)[0]

	if e.Author != "Ф. Энгельс" {
		t.Errorf("автор %q, ожидалось переопределение", e.Author)
	}
	if e.WorkAuthor != "К. Маркс, Ф. Энгельс" {
		t.Errorf("автор работы %q — форме состава он нужен как подсказка", e.WorkAuthor)
	}
}

func TestCollectionWorkIDsDeduplicates(t *testing.T) {
	rows := []repository.ItemRow{
		chapterRow(1, 1, 100, 41),
		chapterRow(2, 2, 101, 41),
		chapterRow(3, 3, 200, 42),
	}

	ids := collectionWorkIDs(rows)

	if len(ids) != 2 {
		t.Fatalf("томов %d, ожидалось 2 (41 повторяется дважды): %v", len(ids), ids)
	}
}

// Узлу поддерева нужен адрес: без chapter_id вложенную главу не на что
// повесить ссылкой, и в оглавлении подборки она остаётся мёртвым текстом.
func TestBuildCollectionTOCNodesCarryChapterID(t *testing.T) {
	rows := []repository.ItemRow{chapterRow(1, 1, 100, 41)}
	chapters := []*models.Chapter{
		{ID: 100, WorkID: 41, Title: "Людвиг Фейербах", OrderNumber: 1, StartPage: 265, EndPage: 313},
		{ID: 101, WorkID: 41, ParentID: ptrInt64(100), Title: "I", OrderNumber: 1, StartPage: 265, EndPage: 271},
		{ID: 103, WorkID: 41, ParentID: ptrInt64(101), Title: "1. Гегель",
			OrderNumber: 1, StartPage: 266, EndPage: 270},
	}

	entries := buildCollectionTOC(rows, chapters)

	if len(entries) != 1 || len(entries[0].Children) != 1 {
		t.Fatalf("оглавление собралось не так: строк %d", len(entries))
	}
	child := entries[0].Children[0]
	if child.ChapterID != 101 {
		t.Errorf("chapter_id подглавы %d, ожидался 101", child.ChapterID)
	}
	if len(child.Children) != 1 {
		t.Fatalf("внуков %d, ожидался 1", len(child.Children))
	}
	// Глубина 2: id не должен унаследоваться от родителя.
	if child.Children[0].ChapterID != 103 {
		t.Errorf("chapter_id внука %d, ожидался 103", child.Children[0].ChapterID)
	}
}
