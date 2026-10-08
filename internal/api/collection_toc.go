package api

import (
	"proofreader/internal/models"
	"proofreader/internal/repository"
)

// collectionWorkIDs — тома, задействованные составом, без повторов. Главы
// всех томов забираются одним запросом, поэтому список нужен заранее.
func collectionWorkIDs(rows []repository.ItemRow) []int64 {
	seen := make(map[int64]bool, len(rows))
	var ids []int64
	for _, row := range rows {
		if row.Item.WorkID == nil || seen[*row.Item.WorkID] {
			continue
		}
		seen[*row.Item.WorkID] = true
		ids = append(ids, *row.Item.WorkID)
	}

	return ids
}

// buildCollectionTOC собирает оглавление: строка на элемент состава, под ней
// поддерево глав источника.
//
// Заголовок берётся живым, из chapters/works: переименование главы в томе
// доезжает до подборки само. Снимок подставляется только когда источник
// пропал — тогда строка помечается broken, а поддерева у неё нет.
func buildCollectionTOC(rows []repository.ItemRow, chapters []*models.Chapter) []*models.CollectionEntry {
	byID, childrenOf, topLevel := indexChapters(chapters)

	entries := make([]*models.CollectionEntry, 0, len(rows))
	for _, row := range rows {
		offset := 0
		if row.PageOffset != nil {
			offset = *row.PageOffset
		}

		entry := &models.CollectionEntry{
			ID:             row.Item.ID,
			Kind:           row.Item.Kind,
			OrderNumber:    row.Item.OrderNumber,
			AuthorOverride: row.Item.AuthorOverride,
		}

		if row.WorkAuthor != nil {
			entry.WorkAuthor = *row.WorkAuthor
		} else {
			// Работы нет — автора взять неоткуда, кроме снимка.
			entry.WorkAuthor = row.Item.SnapshotAuthor
		}
		entry.Author = entry.ResolvedAuthor()

		switch row.Item.Kind {
		case models.CollectionItemKindChapter:
			chapter := chapterOf(byID, row.Item.ChapterID)
			if chapter == nil {
				// Источник пересоздан или удалён: строка живёт снимком.
				entry.Broken = true
				entry.Title = row.Item.SnapshotTitle
			} else {
				entry.Title = chapter.Title
				entry.Children = tocNodes(childrenOf[chapter.ID], childrenOf, offset)
			}
		case models.CollectionItemKindWork:
			if row.WorkTitle == nil {
				entry.Broken = true
				entry.Title = row.Item.SnapshotTitle
			} else {
				entry.Title = *row.WorkTitle
				if row.Item.WorkID != nil {
					entry.Children = tocNodes(topLevel[*row.Item.WorkID], childrenOf, offset)
				}
			}
		}

		entry.Source = buildSource(row, byID, topLevel, offset)
		entries = append(entries, entry)
	}

	return entries
}

// indexChapters раскладывает плоский список глав в три указателя: по id, по
// родителю и по тому (верхний уровень). Один проход вместо трёх обходов на
// каждый элемент состава.
func indexChapters(chapters []*models.Chapter) (
	byID map[int64]*models.Chapter,
	childrenOf map[int64][]*models.Chapter,
	topLevel map[int64][]*models.Chapter,
) {
	byID = make(map[int64]*models.Chapter, len(chapters))
	childrenOf = make(map[int64][]*models.Chapter)
	topLevel = make(map[int64][]*models.Chapter)

	for _, c := range chapters {
		byID[c.ID] = c
	}
	for _, c := range chapters {
		if c.ParentID == nil {
			topLevel[c.WorkID] = append(topLevel[c.WorkID], c)
			continue
		}
		childrenOf[*c.ParentID] = append(childrenOf[*c.ParentID], c)
	}

	return byID, childrenOf, topLevel
}

func chapterOf(byID map[int64]*models.Chapter, id *int64) *models.Chapter {
	if id == nil {
		return nil
	}

	return byID[*id]
}

// tocNodes переводит главы в узлы оглавления, рекурсивно вглубь. Номера
// печатные: внутренний номер плюс смещение тома.
func tocNodes(
	chapters []*models.Chapter,
	childrenOf map[int64][]*models.Chapter,
	offset int,
) []models.CollectionTocNode {
	if len(chapters) == 0 {
		return nil
	}

	nodes := make([]models.CollectionTocNode, 0, len(chapters))
	for _, c := range chapters {
		nodes = append(nodes, models.CollectionTocNode{
			ChapterID:   c.ID,
			Title:       c.Title,
			ChapterSlug: c.Slug,
			PageStart:   c.StartPage + offset,
			Children:    tocNodes(childrenOf[c.ID], childrenOf, offset),
		})
	}

	return nodes
}

// buildSource — откуда взят элемент. Границы: у главы свои, у работы —
// крайние страницы её глав верхнего уровня.
func buildSource(
	row repository.ItemRow,
	byID map[int64]*models.Chapter,
	topLevel map[int64][]*models.Chapter,
	offset int,
) *models.CollectionSource {
	if row.Item.WorkID == nil {
		// Пропал и том — подписывать строку нечем.
		return nil
	}

	src := &models.CollectionSource{
		WorkID:       *row.Item.WorkID,
		WorkSlug:     row.WorkSlug,
		VolumeNumber: row.VolumeNumber,
		VolumePart:   row.VolumePart,
	}
	if row.WorkTitle != nil {
		src.WorkTitle = *row.WorkTitle
	}
	if row.EditionTitle != nil {
		src.EditionTitle = *row.EditionTitle
	}

	if chapter := chapterOf(byID, row.Item.ChapterID); chapter != nil {
		src.PageStart = chapter.StartPage + offset
		src.PageEnd = chapter.EndPage + offset

		return src
	}

	if row.Item.Kind == models.CollectionItemKindWork {
		for i, c := range topLevel[*row.Item.WorkID] {
			if i == 0 || c.StartPage+offset < src.PageStart {
				src.PageStart = c.StartPage + offset
			}
			if c.EndPage+offset > src.PageEnd {
				src.PageEnd = c.EndPage + offset
			}
		}
	}

	return src
}
