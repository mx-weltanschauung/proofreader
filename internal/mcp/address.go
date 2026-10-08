package mcp

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"proofreader/internal/seo"
)

// Kind — вид адреса читальни, который понимает fetch.
type Kind int

const (
	KindCatalog      Kind = iota // «/» — издания и тома
	KindWork                     // /works/{том} — оглавление тома
	KindChapter                  // /works/{том}/chapters/{глава}[/part-N]
	KindPages                    // /works/{том}/pages/{a}[-{b}]
	KindConcept                  // /concepts/{слаг}[/part-N]
	KindConceptShelf             // /concepts[/part-N] — все понятия
)

// maxPages — сколько полос fetch отдаёт за вызов. Десять плотных полос — до
// 50 тыс. знаков: столько модель прочтёт, не упёршись в предел результата.
const maxPages = 10

// errUser — ошибка, текст которой уже написан для модели: userError (deps.go)
// пропускает её дословно.
type errUser string

func (e errUser) Error() string { return string(e) }

// ErrBadAddress — адрес не той формы. Текст идёт модели как есть.
//
//nolint:staticcheck // текст ошибки — фраза модели
var ErrBadAddress = errors.New("Адрес не распознан. Бывают: «/» (каталог), " +
	"/works/{том}, /works/{том}/chapters/{глава}, …/part-{N}, " +
	"/works/{том}/pages/{a} или /pages/{a}-{b}, /concepts/{слаг}[/part-{N}], /concepts")

// Address — разобранный id. Номера — ключи (ведущее целое сегмента), слаги
// отброшены: канон по номеру восстанавливает /seo.
type Address struct {
	Kind      Kind
	WorkID    int64
	ChapterID int64
	Part      int // у главы и понятия — с единицы; у прочих 0
	From, To  int // полосы, внутренние номера
	Slug      string
	// Rubric — подрубрика понятия (?rubric_path= адреса, как у страницы
	// понятия и её .md); nil — понятие целиком. Только у KindConcept.
	Rubric []string
}

// ParseID разбирает id или url результата. Схема, хост и якорь отбрасываются:
// модель передаёт то, что видела, в любом из видов. Из строки запроса значит
// одно — ?rubric_path= у понятия: без неё модель, давшая адрес подрубрики из
// кнопки «Спросить нейросеть», молча получила бы понятие целиком.
func ParseID(id string) (Address, error) {
	p := strings.TrimSpace(id)
	query := ""
	if i := strings.Index(p, "://"); i >= 0 {
		u, err := url.Parse(p)
		if err != nil {
			return Address{}, ErrBadAddress
		}
		p, query = u.Path, u.RawQuery
	}
	if i := strings.IndexAny(p, "?#"); i >= 0 {
		if p[i] == '?' {
			query, _, _ = strings.Cut(p[i+1:], "#")
		}
		p = p[:i]
	}
	var rubric []string
	if v, err := url.ParseQuery(query); err == nil && v.Get("rubric_path") != "" {
		if rubric = seo.ParseRubricPath(v.Get("rubric_path")); rubric == nil {
			return Address{}, ErrBadAddress
		}
	}
	a, err := parsePath(p)
	if err != nil || rubric == nil {
		return a, err
	}
	if a.Kind != KindConcept {
		// Подрубрика бывает только у понятия: молча отбросить её — значит
		// отдать не то, о чём просили.
		return Address{}, ErrBadAddress
	}
	a.Rubric = rubric
	return a, nil
}

// parsePath — путь id без строки запроса и якоря.
func parsePath(p string) (Address, error) {
	p = strings.TrimSuffix(strings.Trim(p, "/"), ".md")
	if p == "" || p == "llms.txt" {
		return Address{Kind: KindCatalog}, nil
	}
	seg := strings.Split(p, "/")

	if seg[0] == "concepts" {
		switch {
		case len(seg) == 1:
			return Address{Kind: KindConceptShelf, Part: 1}, nil
		case len(seg) == 2:
			// Пустого второго сегмента не бывает: strings.Trim выше срезал
			// хвостовой слэш.
			if n, ok := partNumber(seg[1]); ok {
				return Address{Kind: KindConceptShelf, Part: n}, nil
			}
			return Address{Kind: KindConcept, Slug: seg[1], Part: 1}, nil
		case len(seg) == 3 && seg[1] != "":
			n, ok := partNumber(seg[2])
			if !ok {
				return Address{}, ErrBadAddress
			}
			return Address{Kind: KindConcept, Slug: seg[1], Part: n}, nil
		}
		return Address{}, ErrBadAddress
	}
	if seg[0] != "works" || len(seg) < 2 {
		return Address{}, ErrBadAddress
	}
	work, ok := seo.LeadingID(seg[1])
	if !ok {
		return Address{}, ErrBadAddress
	}
	switch {
	case len(seg) == 2:
		return Address{Kind: KindWork, WorkID: work}, nil
	case seg[2] == "chapters" && (len(seg) == 4 || len(seg) == 5):
		ch, ok := seo.LeadingID(seg[3])
		if !ok {
			return Address{}, ErrBadAddress
		}
		part := 1
		if len(seg) == 5 {
			n, ok := partNumber(seg[4])
			if !ok {
				return Address{}, ErrBadAddress
			}
			part = n
		}
		return Address{Kind: KindChapter, WorkID: work, ChapterID: ch, Part: part}, nil
	case (seg[2] == "pages" || seg[2] == "read") && len(seg) == 4:
		from, to, ok := pageRange(seg[3])
		if !ok {
			return Address{}, ErrBadAddress
		}
		if to-from+1 > maxPages {
			//nolint:staticcheck // текст ошибки — фраза модели
			return Address{}, errUser(fmt.Sprintf("Не больше %d полос за раз: /works/%d/pages/%d-%d",
				maxPages, work, from, from+maxPages-1))
		}
		return Address{Kind: KindPages, WorkID: work, From: from, To: to}, nil
	}
	return Address{}, ErrBadAddress
}

// partNumber — «part-N», N ≥ 1.
func partNumber(s string) (int, bool) {
	digits, ok := strings.CutPrefix(s, "part-")
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(digits)
	return n, err == nil && n >= 1
}

// pageRange — «a» или «a-b», 1 ≤ a ≤ b. Не LeadingID: у него «4-7» — номер 4
// со слагом «7».
func pageRange(s string) (int, int, bool) {
	a, b, dash := strings.Cut(s, "-")
	from, err := strconv.Atoi(a)
	if err != nil || from < 1 {
		return 0, 0, false
	}
	if !dash {
		return from, from, true
	}
	to, err := strconv.Atoi(b)
	if err != nil || to < from {
		return 0, 0, false
	}
	return from, to, true
}
