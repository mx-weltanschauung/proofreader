package models

import (
	"strings"
	"unicode/utf8"
)

// maxQueryRunes — потолок длины запроса: в рунах, потому что читатель пишет
// кириллицей и 200 байт — это сто букв.
const maxQueryRunes = 200

// minQueryRunes — пол длины запроса. Конфиг ru намеренно без стоп-слов,
// поэтому односимвольный запрос — это скан почти всего корпуса («и» — 49 тыс.
// полос из 50 810) ради выдачи, в которой читателю нечего искать. Порог
// назван в справке (/help#search) и повторён на клиенте
// (frontend/src/utils/searchQuery.ts).
//
// Двухсимвольные частые слова («на», «не», «по») порог не ловит и не должен:
// их честная цена — стоп-слова или потолок по частотности, и то и другое
// требует замера.
const minQueryRunes = 2

// SearchTooShortMessage — отдельные слова про короткий запрос, а не общее
// «Пустой запрос»: читателю надо сказать, что именно сделать, а «и» — не
// пустота. Общие для сайта и MCP-сервера; клиентский двойник —
// frontend/src/utils/searchQuery.ts, слова обязаны совпадать.
const SearchTooShortMessage = "Слишком короткий запрос: не меньше двух символов"

// SearchTextTooShort — запрос короче пола. Считает РУНЫ, не байты: одна
// кириллическая буква занимает два байта, и проверка по len() пропустила бы
// ровно те запросы, ради которых порог заводится.
func SearchTextTooShort(q string) bool {
	return utf8.RuneCountInString(q) < minQueryRunes
}

// NormalizeSearchText — обрезка краёв, схлопывание пробелов, потолок длины. Больше
// ничего: ё, регистр и морфологию сводит конфиг ru на стороне Postgres.
func NormalizeSearchText(raw string) string {
	q := strings.Join(strings.Fields(raw), " ")
	if r := []rune(q); len(r) > maxQueryRunes {
		q = strings.TrimSpace(string(r[:maxQueryRunes]))
	}
	return q
}

// SearchQuery — нормализованный запрос читателя (обрезка, схлопнутые пробелы,
// потолок 200 символов — см. NormalizeSearchText) и область поиска.
//
// Область — два списка, а не одно поле: собрания и тома СКЛАДЫВАЮТСЯ
// («всё собрание Плеханова и ещё тома 13, 14 Маркса»), а не пересекаются.
// Пустые оба — вся читальня. Разбор в tsquery живёт в репозитории: это
// свойство Postgres.
type SearchQuery struct {
	Text       string
	EditionIDs []int64
	WorkIDs    []int64
}

// SearchTerms — ответ на ?terms_only=1: только леммы для подсветки.
type SearchTerms struct {
	Query string   `json:"query"`
	Terms []string `json:"terms"`
}

// SearchChapter — глава, совпавшая названием.
type SearchChapter struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	// Slug и WorkSlug — хвосты адреса главы и её тома. Клиент строит по ним
	// ссылку, ничего не транслитерируя: правило одно и живёт на сервере.
	Slug         string `json:"slug"`
	WorkSlug     string `json:"work_slug"`
	WorkID       int64  `json:"work_id"`
	WorkTitle    string `json:"work_title"`
	VolumeLabel  string `json:"volume_label"`
	EditionTitle string `json:"edition_title"`
	IsApparatus  bool   `json:"is_apparatus"`
}

// SearchConcept — понятие предметного указателя, совпавшее названием.
type SearchConcept struct {
	Slug  string `json:"slug"`
	Title string `json:"title"`
}

// SearchVolume — том с числом совпавших полос в тексте и в аппарате.
//
// У служебной работы (role = front_matter) подпись тома, собрание и номер
// берутся у родителя: своих у неё нет, а в списке она стоит под ним.
type SearchVolume struct {
	WorkID int64  `json:"work_id"`
	Title  string `json:"title"`
	// WorkSlug — хвост адреса тома; см. SearchChapter.WorkSlug.
	WorkSlug      string `json:"work_slug"`
	Author        string `json:"author"`
	VolumeLabel   string `json:"volume_label"`
	EditionID     *int64 `json:"edition_id"`
	EditionTitle  string `json:"edition_title"`
	Role          string `json:"role"`
	ParentWorkID  *int64 `json:"parent_work_id"`
	TextHits      int    `json:"text_hits"`
	ApparatusHits int    `json:"apparatus_hits"`
	// NumberingStyle — как печатается колонцифра работы (arabic|roman).
	// Ездит вместе с полосами: печатную форму считает клиент общей для всей
	// читальни printedFolio, а сходить за работой ради одного поля он не
	// может — томов в выдаче бывает больше сотни.
	NumberingStyle string `json:"numbering_style"`
	// Pages — первые несколько совпавших полос тома в порядке чтения, с
	// отрывками: столько же и в том же виде, сколько показывает первый экран
	// выдачи. Не окно списка: продолжения у него нет, за остальными полосами
	// читатель идёт в /api/search/pages. Всегда non-nil — клиент зовёт .map.
	Pages []SearchPage `json:"pages"`
}

// SearchResult — первый экран поиска: каталог и тома.
//
// TotalHits — сумма TextHits по всем томам, без аппарата: клиент печатает её
// заголовком «В тексте — N полос» над строками, каждая из которых показывает
// свой TextHits, и заголовок обязан сходиться со строками. Совпадения в
// аппарате видны в строке тома отдельным числом (ApparatusHits).
type SearchResult struct {
	Query     string          `json:"query"`
	Terms     []string        `json:"terms"`
	Chapters  []SearchChapter `json:"chapters"`
	Concepts  []SearchConcept `json:"concepts"`
	Volumes   []SearchVolume  `json:"volumes"`
	TotalHits int             `json:"total_hits"`
}

// FoundCount — нашлось ли хоть что-то: текст, аппарат, главы или понятия.
// Для статистики «ищут и не находят»: TotalHits один считает только тело, и
// запрос, попавший лишь в примечания или в название понятия, выглядел бы
// ненайденным.
func (r *SearchResult) FoundCount() int {
	n := r.TotalHits + len(r.Chapters) + len(r.Concepts)
	for _, v := range r.Volumes {
		n += v.ApparatusHits
	}
	return n
}

// SearchPage — совпавшая полоса одного тома с отрывком. Границы совпадения в
// отрывке — U+0001/U+0002, HTML в нём нет: клиент экранирует сам.
type SearchPage struct {
	PageNumber    int     `json:"page_number"`
	PrintedNumber int     `json:"printed_number"`
	ChapterTitle  *string `json:"chapter_title"`
	// ChapterID — id той же главы, что и ChapterTitle: выдача делает из пары
	// ссылку на главу. Оба поля приезжают из одной выбранной строки (LATERAL),
	// иначе при совпадении диапазонов заголовок и ссылка разъехались бы.
	ChapterID   *int64 `json:"chapter_id"`
	IsApparatus bool   `json:"is_apparatus"`
	Snippet     string `json:"snippet"`
}

// SearchChapterFacet — глава тома с числом совпавших полос. Строится по тому
// же правилу, что и подпись полосы (самая узкая накрывающая глава), иначе
// список «где нашлось» и подписи в выдаче назовут разные главы.
type SearchChapterFacet struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	Hits  int    `json:"hits"`
}

// SearchPagesResult — полосы одного тома в порядке чтения.
type SearchPagesResult struct {
	Query string       `json:"query"`
	Terms []string     `json:"terms"`
	Total int          `json:"total"`
	Pages []SearchPage `json:"pages"`
	// Chapters — до facetLimit глав тома с попаданиями, по убыванию числа
	// попаданий. Считается по ВСЕМУ тому: и не по окну limit/offset (иначе
	// «Ещё» подрисовывал бы главы), и независимо от уже выбранных глав (иначе,
	// отметив одну, читатель не смог бы добавить вторую — список схлопнулся бы).
	Chapters []SearchChapterFacet `json:"chapters"`
	// ChaptersTotal — сколько всего глав тома содержат попадания; клиент
	// печатает остаток строкой «и ещё N глав».
	ChaptersTotal int `json:"chapters_total"`
}
