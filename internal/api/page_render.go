package api

import (
	"proofreader/internal/models"
	"proofreader/pkg/markdown"
)

// renderPageRange рендерит подряд идущие страницы в поштучный HTML и общий
// для всего диапазона набор сносок.
//
// Общий блок трёх обработчиков: страниц главы, главы целиком и окна
// потокового чтения. Раньше стоял дословным дублем в ListPages и Render.
//
// Область нумерации подстрочных сносок — весь переданный срез: маркеры
// (1), (2), … идут сквозняком. Кому нужна постраничная нумерация, тот зовёт
// функцию на срезе из одной страницы.
func renderPageRange(r *markdown.Renderer, pages []*models.Page) ([]string, markdown.NoteSet) {
	contents := make([]markdown.PageContent, len(pages))
	for i, page := range pages {
		contents[i] = markdown.PageContent{
			PageNumber: page.PageNumber,
			Content:    page.ContentMarkdown,
		}
	}
	return r.CollectPages(contents)
}
