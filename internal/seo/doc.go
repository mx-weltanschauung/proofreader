// Пакет seo печатает страницы читальни для краулеров: поисковых роботов,
// которые плохо исполняют JS, и краулеров мессенджеров, которые не исполняют
// его вовсе. Живой читатель сюда не попадает — его запрос nginx отдаёт SPA
// (см. frontend/nginx.conf, карта $is_crawler).
package seo

import (
	"encoding/json"
	"errors"
	"html"
	"proofreader/internal/site"
	"strconv"
	"strings"
	"time"
)

// Имя читальни во всех превью — site.Name(), не «proofreader»: наружу проект
// говорит от лица библиотеки, а имя в коде и API остаётся техническим.

const (
	RobotsIndex   = "index,follow"
	RobotsNoIndex = "noindex,follow"
)

// ErrNotFound — сущности по адресу нет. writeRenderError отвечает на него
// 410 («было и нет»), отличая эту ветку от внутренней ошибки (500). Свой, а
// не из internal/api: router.go держит *seo.Handler, и импорт api сюда
// замкнул бы цикл.
var ErrNotFound = errors.New("не найдено")

// ErrNeverPublished — отдельный сигнал для черновика подборки: сущность НЕ
// отсутствует (в отличие от ErrNotFound), она существует и скрыта от
// постороннего. writeRenderError отвечает на него 404, а не 410 — 410
// означает «было и снято» и тем самым выдал бы краулеру сам факт
// существования черновика, чего решение не допускает. См. Source.Collection.
var ErrNeverPublished = errors.New("никогда не публиковалось")

// Doc — то, что уедет краулеру. Заполняется рендерером сущности, печатается
// Render. Рендерер про HTML не знает: единственное место, где встречаются
// угловые скобки, — этот файл.
type Doc struct {
	Title       string
	Description string
	Canonical   string // абсолютный адрес; идёт и в <link rel=canonical>, и в og:url
	// Alternate — абсолютный адрес текста этой страницы для нейросетей
	// (render_llm.go); пусто — ссылки rel=alternate не будет.
	Alternate string
	OGType    string // "book" | "article" | "website"
	ImageURL  string // абсолютный адрес карточки; пусто — тегов картинки не будет
	Robots    string // RobotsIndex либо RobotsNoIndex
	JSONLD    any    // микроразметка schema.org; nil — блока не будет
	Body      string // готовый HTML тела, экранированный своим автором
	CacheKey  time.Time
}

// opdsLink — ссылка на каталог OPDS в шапке каждой страницы.
const opdsLink = `<link rel="related" type="application/atom+xml;profile=opds-catalog;kind=navigation" href="/opds" title="Каталог OPDS">`

func Render(d *Doc) string {
	var out strings.Builder

	out.WriteString("<!doctype html>\n<html lang=\"ru\">\n<head>\n")
	out.WriteString(`<meta charset="utf-8">` + "\n")
	out.WriteString(`<meta name="viewport" content="width=device-width, initial-scale=1">` + "\n")
	out.WriteString("<title>" + Esc(OneLine(d.Title)) + "</title>\n")

	meta(&out, "name", "description", d.Description)
	meta(&out, "name", "robots", d.Robots)
	if d.Canonical != "" {
		out.WriteString(`<link rel="canonical" href="` + Esc(d.Canonical) + `">` + "\n")
	}
	if d.Alternate != "" {
		out.WriteString(`<link rel="alternate" type="text/plain" href="` + Esc(d.Alternate) + `">` + "\n")
	}
	// Автообнаружение каталога OPDS: по этой ссылке агрегаторы находят
	// каталог сами. Та же строка стоит в frontend/index.html.
	out.WriteString(opdsLink + "\n")

	meta(&out, "property", "og:site_name", site.Name())
	meta(&out, "property", "og:locale", "ru_RU")
	meta(&out, "property", "og:type", d.OGType)
	meta(&out, "property", "og:title", d.Title)
	meta(&out, "property", "og:description", d.Description)
	meta(&out, "property", "og:url", d.Canonical)

	// Пустой og:image Телеграм показывает битой картинкой, а не отсутствием
	// картинки, поэтому теги изображения идут только вместе со ссылкой.
	if d.ImageURL != "" {
		meta(&out, "property", "og:image", d.ImageURL)
		// Ссылаемся на константы card.go, а не повторяем числа: разойдись они
		// с настоящим размером PNG, Телеграм обрежет картинку по тегам.
		meta(&out, "property", "og:image:width", strconv.Itoa(cardWidth))
		meta(&out, "property", "og:image:height", strconv.Itoa(cardHeight))
		meta(&out, "name", "twitter:card", "summary_large_image")
	}

	if d.JSONLD != nil {
		// encoding/json по умолчанию превращает < > & в \u003c \u003e \u0026,
		// поэтому закрыть <script> изнутри значением нельзя. Ошибку маршалинга
		// глотаем осознанно: микроразметка — украшение, из-за неё страница не
		// должна перестать отдаваться.
		if b, err := json.Marshal(d.JSONLD); err == nil {
			out.WriteString(`<script type="application/ld+json">` + string(b) + "</script>\n")
		}
	}

	out.WriteString("<style>\n" + docCSS + "</style>\n")
	out.WriteString("</head>\n<body>\n")
	out.WriteString(d.Body)
	out.WriteString("\n</body>\n</html>\n")

	return out.String()
}

func meta(out *strings.Builder, attr, name, content string) {
	if content == "" {
		return
	}
	out.WriteString(`<meta ` + attr + `="` + name + `" content="` + Esc(OneLine(content)) + `">` + "\n")
}

// Esc закрывает и текст, и значение атрибута в кавычках: html.EscapeString
// экранирует < > & ' " — этого хватает для обоих мест, и одна функция вместо
// двух убирает возможность взять не ту.
func Esc(s string) string { return html.EscapeString(s) }

// OneLine сводит значение к одной строке. Перевод строки внутри content=""
// формально допустим, но часть краулеров на нём обрывает разбор атрибута.
func OneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// Excerpt — описание страницы из её же текста: limit знаков без разметки,
// обрезанные по границе слова.
//
// Резка по рунам, а не по байтам: на кириллице каждая буква — два байта, и
// байтовая резка оставила бы в описании половину буквы.
func Excerpt(htmlText string, limit int) string {
	text := OneLine(html.UnescapeString(stripTags(htmlText)))
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}

	cut := string(runes[:limit])
	if i := strings.LastIndex(cut, " "); i > 0 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " ,;:—-") + "…"
}

// stripTags снимает разметку, подставляя на место тега пробел: без него
// «<p>Государство</p><p>и революция</p>» склеилось бы в «Государствои».
//
// Одинокая угловая скобка тегом не считается. В корпусе она встречается как
// знак сравнения («5 < 10») и как мусор распознавания, и если принимать её за
// открытие тега, то текст до следующей закрывающей скобки — а при её
// отсутствии и весь остаток строки — молча исчезнет из описания страницы.
func stripTags(s string) string {
	var out strings.Builder
	for {
		open := strings.IndexByte(s, '<')
		if open < 0 {
			out.WriteString(s)
			return out.String()
		}
		out.WriteString(s[:open])

		rest := s[open+1:]
		// Тег начинается с буквы, косой черты, «!» или «?». За знаком
		// сравнения стоит пробел — это не тег.
		if rest == "" || !isTagStart(rest[0]) {
			out.WriteByte('<')
			s = rest
			continue
		}
		end := strings.IndexByte(rest, '>')
		if end < 0 {
			// Закрытия нет: остаток — обычный текст, а не тег до конца строки.
			out.WriteByte('<')
			out.WriteString(rest)
			return out.String()
		}
		out.WriteByte(' ')
		s = rest[end+1:]
	}
}

func isTagStart(c byte) bool {
	return c == '/' || c == '!' || c == '?' ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// docCSS — минимум, чтобы страница читалась глазами при разборе руками
// (curl с ботовским User-Agent). Краулеру стили безразличны, но проверять
// вывод приходится человеку.
const docCSS = `
body { margin: 0 auto; padding: 2rem 1.25rem; max-width: 42em;
       font-family: Georgia, "Times New Roman", serif; line-height: 1.6; }
nav.toc ul { list-style: none; padding-left: 1rem; }
.page-marker { color: #8a8578; font-size: 0.75rem; }
`
