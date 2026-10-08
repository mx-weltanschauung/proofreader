// Package site хранит имя и описание экземпляра читальни — то, чем одна
// читальня отличается от другой в каждом тексте, который она печатает
// наружу: страницы для краулеров, OPDS, MCP, титул выгрузок, статическая
// копия. Значения ставит Set один раз на старте процесса (из SITE_NAME и
// SITE_DESCRIPTION), до приёма запросов; дальше их только читают.
package site

const (
	DefaultName        = "Читальня"
	DefaultDescription = "Книги, вычитанные по сканам постранично."
)

var (
	name        = DefaultName
	description = DefaultDescription
)

// Set ставит имя и описание; пустое значение — умолчание.
func Set(n, d string) {
	name, description = DefaultName, DefaultDescription
	if n != "" {
		name = n
	}
	if d != "" {
		description = d
	}
}

// Name — имя читальни.
func Name() string { return name }

// Description — одна фраза о собрании.
func Description() string { return description }
