package repository

import (
	"fmt"
	"regexp"
	"strings"
)

// lexemeRe — лемма в текстовом виде tsquery: 'парт', !'меньшевик', 'd”art'.
var lexemeRe = regexp.MustCompile(`(!?)'((?:[^']|'')+)'`)

// termsFromTSQuery достаёт леммы из websearch_to_tsquery(...)::text для
// подсветки на клиенте. Исключённые (!) не входят: подсвечивать то, чего
// читатель просил не находить, нельзя. Пустой разбор — nil.
//
// Postgres исключает целую фразу в скобках — !( 'втор' <-> 'интернациона' ),
// не !'втор' <-> !'интернациона' — поэтому одной проверки «!» перед кавычкой
// мало: leximeRe находил бы леммы внутри такой группы как обычные, не
// исключённые. stripNegated вырезает такую группу целиком до разбора.
func termsFromTSQuery(tsquery string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range lexemeRe.FindAllStringSubmatch(stripNegated(tsquery), -1) {
		if m[1] == "!" {
			continue
		}
		lex := strings.ReplaceAll(m[2], "''", "'")
		if seen[lex] {
			continue
		}
		seen[lex] = true
		out = append(out, lex)
	}
	return out
}

// stripNegated вырезает из текстового tsquery каждое исключённое
// подвыражение: одиночную лемму (!'слов') или скобочную группу
// (!( 'втор' <-> 'интернациона' ) — форма, в которую websearch_to_tsquery
// заключает исключённую многословную фразу). Вложенность скобок
// отслеживается по глубине; сама websearch_to_tsquery вложенных групп не
// порождает, но скан корректен и для неё.
func stripNegated(s string) string {
	var b strings.Builder
	n := len(s)
	for i := 0; i < n; {
		if s[i] != '!' {
			b.WriteByte(s[i])
			i++
			continue
		}
		j := i + 1
		for j < n && s[j] == ' ' {
			j++
		}
		switch {
		case j < n && s[j] == '(':
			depth := 0
			for ; j < n; j++ {
				switch s[j] {
				case '(':
					depth++
				case ')':
					depth--
					if depth == 0 {
						j++
						goto doneParen
					}
				}
			}
		doneParen:
			i = j
		case j < n && s[j] == '\'':
			j++
			for j < n {
				if s[j] == '\'' {
					if j+1 < n && s[j+1] == '\'' {
						j += 2
						continue
					}
					j++
					break
				}
				j++
			}
			i = j
		default:
			// Голое «!» без опознанного продолжения — снимаем только его.
			i++
		}
	}
	return b.String()
}

// volumeLabel — подпись тома на полке: ручная, если задана, иначе «т. N»
// (тома 25 и 26 выходили в двух книгах — «т. 25·1»); у работы вне собрания
// пустая строка.
func volumeLabel(shelfLabel string, volumeNumber *int, volumePart *string) string {
	if s := strings.TrimSpace(shelfLabel); s != "" {
		return s
	}
	if volumeNumber == nil {
		return ""
	}
	if volumePart != nil && *volumePart != "" {
		return fmt.Sprintf("т. %d·%s", *volumeNumber, *volumePart)
	}
	return fmt.Sprintf("т. %d", *volumeNumber)
}

var (
	footnoteRefRe = regexp.MustCompile(`\[\^[^\]]*\]`)
	// Перечислением, а не «<[^>]+>»: угловые скобки в этом корпусе — ещё и
	// редакторская пометка восстановленного текста («<ИЗ № 3
	// «ОТЕЧЕСТВЕННЫХ ЗАПИСОК»>», «<не>», «<56>») на 129 полосах, и общая
	// регулярка вырезала такой кусок целиком — вместе со словами и вместе с
	// метками совпадения, то есть отрывок терял ровно то слово, ради
	// которого построен. Список — теги, реально встречающиеся в
	// content_markdown живого корпуса.
	htmlTagRe = regexp.MustCompile(
		`(?i)</?(?:sup|sub|b|i|u|em|strong|br|del|ins|mark|table|thead|tbody|tfoot|tr|td|th|math)\b[^>]*>`)
	headingRe    = regexp.MustCompile(`(^|\s)#{1,6}[ \t]*`)
	underLeadRe  = regexp.MustCompile(`(^|\s)_+`)
	underTrailRe = regexp.MustCompile(`_+(\s|$)`)
	spacesRe     = regexp.MustCompile(`\s+`)
)

// cleanSnippet снимает с отрывка ts_headline маркеры markdown, которые
// в поле поиска смотрятся мусором: ссылки на сноски [^12], теги <sup>,
// решётки заголовков, звёздочки и подчёркивания выделения. Разделитель
// фрагментов U+0003 становится « … », пробелы схлопываются. Метки совпадения
// U+0001/U+0002 не трогаются.
func cleanSnippet(raw string) string {
	s := footnoteRefRe.ReplaceAllString(raw, "")
	s = htmlTagRe.ReplaceAllString(s, "")
	s = headingRe.ReplaceAllString(s, "$1")
	s = strings.ReplaceAll(s, "*", "")
	// Разделитель фрагментов — до подчёркиваний: их регулярки ищут пробел
	// рядом, а chr(3) пробелом не является.
	s = strings.ReplaceAll(s, "\x03", " … ")
	s = underLeadRe.ReplaceAllString(s, "$1")
	s = underTrailRe.ReplaceAllString(s, "$1")
	s = spacesRe.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}
