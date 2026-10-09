package models

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// ArticleKinds — виды статьи журнала; те же строки держит CHECK
// chapters_article_kind_check (миграция 000038).
var ArticleKinds = []string{"статья", "рецензия", "документ", "от_редакции", "выступление", "прочее"}

// ValidArticleKind — вид из закрытого перечня ArticleKinds.
func ValidArticleKind(k string) bool {
	for _, v := range ArticleKinds {
		if v == k {
			return true
		}
	}
	return false
}

// Роли подписи статьи.
const (
	CreditRoleAuthor     = "author"
	CreditRoleTranslator = "translator"
)

// ArticleCredit — строка подписи статьи.
type ArticleCredit struct {
	Position   int    `json:"position"`
	Role       string `json:"role"`
	Printed    string `json:"printed"`
	PersonID   *int64 `json:"person_id,omitempty"`
	PersonSlug string `json:"person_slug,omitempty"`
}

// Journal — журнал («Под знаменем марксизма»).
type Journal struct {
	ID          int64     `json:"id"`
	Slug        string    `json:"slug"`
	Title       string    `json:"title"`
	Subtitle    string    `json:"subtitle"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// JournalIssue — номер журнала; полосы и статьи — у работы WorkID.
// Составной номер («№ 7—8») — одна строка: NumberFrom 7, NumberTo 8.
type JournalIssue struct {
	ID         int64     `json:"id"`
	JournalID  int64     `json:"journal_id"`
	Year       int       `json:"year"`
	NumberFrom int       `json:"number_from"`
	NumberTo   int       `json:"number_to"`
	Label      string    `json:"label"`
	Months     string    `json:"months"`
	WorkID     int64     `json:"work_id"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// JournalSummary — журнал на полке: годы и число загруженных номеров.
type JournalSummary struct {
	Journal
	IssuesTotal int  `json:"issues_total"`
	YearFrom    *int `json:"year_from"`
	YearTo      *int `json:"year_to"`
}

// JournalIssueRef — клетка номера на странице журнала.
type JournalIssueRef struct {
	ID       int64  `json:"id"`
	Label    string `json:"label"`
	Months   string `json:"months"`
	WorkID   int64  `json:"work_id"`
	WorkSlug string `json:"work_slug"`
}

// JournalYear — строка года на странице журнала.
type JournalYear struct {
	Year   int               `json:"year"`
	Issues []JournalIssueRef `json:"issues"`
}

// JournalDetail — страница журнала.
type JournalDetail struct {
	Journal *Journal      `json:"journal"`
	Years   []JournalYear `json:"years"`
}

// WorkJournalIssue — журнальные координаты работы-номера для шапки карточки
// и подписи цитаты (GET /works/{id}, поле journal_issue).
type WorkJournalIssue struct {
	IssueID      int64  `json:"issue_id"`
	JournalID    int64  `json:"journal_id"`
	JournalSlug  string `json:"journal_slug"`
	JournalTitle string `json:"journal_title"`
	Year         int    `json:"year"`
	Label        string `json:"label"`
	Months       string `json:"months"`
}

// IssueLabel — подпись номера: «5» или «5—6» (длинное тире).
func IssueLabel(from, to int) string {
	if to <= from {
		return fmt.Sprintf("%d", from)
	}
	return fmt.Sprintf("%d—%d", from, to)
}

// IssueTitle — заглавие работы номера: «Под знаменем марксизма, 1925, № 5—6».
func IssueTitle(journal string, year int, label string) string {
	return fmt.Sprintf("%s, %d, № %s", journal, year, label)
}

// Person — человек, подписавший статьи. Даты (сроки охраны) — спека Б.
type Person struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	SortKey   string    `json:"sort_key"`
	Slug      string    `json:"slug"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// CreditInput — строка подписи в теле PUT …/credits.
type CreditInput struct {
	Role     string `json:"role"`
	Printed  string `json:"printed"`
	PersonID *int64 `json:"person_id"`
}

// PersonArticle — статья человека на его странице.
type PersonArticle struct {
	ChapterID    int64  `json:"chapter_id"`
	ChapterSlug  string `json:"chapter_slug"`
	Title        string `json:"title"`
	ArticleKind  string `json:"article_kind"`
	Role         string `json:"role"`
	WorkID       int64  `json:"work_id"`
	WorkSlug     string `json:"work_slug"`
	JournalSlug  string `json:"journal_slug"`
	JournalTitle string `json:"journal_title"`
	Year         int    `json:"year"`
	Label        string `json:"label"`
	StartPage    int    `json:"start_page"`
	EndPage      int    `json:"end_page"`
}

// PersonDetail — страница автора.
type PersonDetail struct {
	Person   *Person         `json:"person"`
	Articles []PersonArticle `json:"articles"`
}

var parenthetical = regexp.MustCompile(`\s*\([^)]*\)`)

// PersonSortKey — «фамилия инициалы» строчными, без точек, ё -> е:
// «И. И. Рубин» -> «рубин и и». Фамилия — последнее слово без точки;
// раскрытие псевдонима в скобках в ключ не входит. Полное имя без инициалов
// — фамилией вперёд: «Леопольд Авербах» -> «авербах леопольд».
func PersonSortKey(name string) string {
	s := strings.ToLower(parenthetical.ReplaceAllString(name, ""))
	s = strings.ReplaceAll(s, "ё", "е")
	words := strings.Fields(strings.ReplaceAll(s, ".", ". "))
	var initials, surname []string
	for _, w := range words {
		w = strings.Trim(w, ".,")
		if w == "" {
			continue
		}
		if len([]rune(w)) <= 3 && isInitialToken(name, w) {
			initials = append(initials, w)
		} else {
			surname = append(surname, w)
		}
	}
	// Полное имя без инициалов («Леопольд Авербах», «Григ. Марецкий»):
	// фамилия — последнее слово, она и идёт вперёд, иначе поиск по фамилии в
	// окне слияния его не находит. Последнее слово в три буквы и короче
	// фамилией не считается: запрос «Рубин И» — фамилия и начало инициала.
	if len(initials) == 0 && len(surname) > 1 && len([]rune(surname[len(surname)-1])) > 3 {
		last := surname[len(surname)-1]
		surname = append([]string{last}, surname[:len(surname)-1]...)
	}
	return strings.TrimSpace(strings.Join(append(surname, initials...), " "))
}

// isInitialToken — слово стоит в имени с точкой после него («Гр.»).
func isInitialToken(name, lowerWord string) bool {
	for _, w := range strings.Fields(strings.ReplaceAll(strings.ToLower(name), "ё", "е")) {
		if strings.HasSuffix(w, ".") && strings.TrimSuffix(w, ".") == lowerWord {
			return true
		}
	}
	return false
}
