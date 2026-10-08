package staticsite

import (
	"fmt"

	"proofreader/pkg/book"
)

// withoutApparatus — книга работы без того, что снимает снятие аппарата
// («N apparatus» в scripts/static-exclude.txt). Полосы задаёт pages —
// внутренние номера от ApparatusRepository.PageNumbers, то есть то же
// правило, что у DELETE /works/{id}/apparatus, а не второй разбор заголовков.
//
// Секция верхнего уровня уходит целиком, если все её полосы — аппарат, и
// остаётся, если ни одной. Смешанная — ошибка: аппарат внутри настоящей главы
// пришлось бы вырезать из её текста вместе со сносками (NotesHTML собран по
// всей секции), а этого сборка не умеет — и молча оставить его нельзя.
func withoutApparatus(b *book.Book, pages map[int]bool) (*book.Book, error) {
	out := *b
	out.Sections = nil
	for _, s := range b.Sections {
		total, hit := 0, 0
		walkPages(s, func(p book.Page) {
			total++
			if pages[p.Internal] {
				hit++
			}
		})
		switch {
		case hit == 0:
			out.Sections = append(out.Sections, s)
		case hit < total:
			return nil, fmt.Errorf("в секции %q %d полос снятого аппарата из %d — вырезать аппарат из середины главы сборка не умеет", s.Title, hit, total)
		}
	}
	return &out, nil
}

// walkPages обходит полосы секции и её подсекций в порядке чтения.
func walkPages(s book.Section, fn func(book.Page)) {
	for _, b := range s.Blocks {
		if b.Child != nil {
			walkPages(*b.Child, fn)
			continue
		}
		for _, p := range b.Pages {
			fn(p)
		}
	}
}
