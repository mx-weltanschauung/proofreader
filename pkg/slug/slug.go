// Пакет slug превращает человеческий заголовок в кусок адреса. Ключом адреса
// он не является: сущность ищется по номеру, а слаг — украшение, поэтому ни
// уникальность, ни устойчивость к переименованию здесь не требуются.
//
// Отображение транслитерации взято из tools/ocr_ingest/index_parser.py
// (_TRANSLIT): им сделаны существующие слаги понятий (/concepts/abstraktnyj-trud),
// и второе правило в одной читальне означало бы, что «voprosy» в адресе
// понятия и «voprosi» в адресе главы оба правильные.
package slug

import (
	"fmt"
	"strings"
)

// MaxLen — потолок слага в знаках. Шестьдесят: адрес целиком влезает в превью
// мессенджера и в выдачу поисковика без обрезки многоточием, а заголовки
// корпуса доходят до 375 знаков (одна глава тома 25) и в адресе неуместны.
const MaxLen = 60

var translit = map[rune]string{
	'а': "a", 'б': "b", 'в': "v", 'г': "g", 'д': "d", 'е': "e", 'ё': "e",
	'ж': "zh", 'з': "z", 'и': "i", 'й': "j", 'к': "k", 'л': "l", 'м': "m",
	'н': "n", 'о': "o", 'п': "p", 'р': "r", 'с': "s", 'т': "t", 'у': "u",
	'ф': "f", 'х': "h", 'ц': "c", 'ч': "ch", 'ш': "sh", 'щ': "sch",
	'ъ': "", 'ы': "y", 'ь': "", 'э': "e", 'ю': "yu", 'я': "ya",
}

// Text транслитерирует заголовок и режет по потолку. Пустая строка на входе
// без букв и цифр — законный ответ: адрес тогда остаётся голым номером.
func Text(title string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(title) {
		if rep, ok := translit[r]; ok {
			b.WriteString(rep)
			continue
		}
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}

	// Схлопывание повторов и обрезка краёв: «Абстракция, абстрактное» даёт
	// подряд идущие дефисы от запятой и пробела.
	out := strings.Trim(collapse(b.String()), "-")
	if len(out) <= MaxLen {
		return out
	}

	head := out[:MaxLen]
	// Рез по последнему дефису; если слово длиннее потолка целиком — по знаку.
	if i := strings.LastIndexByte(head, '-'); i > 0 {
		return head[:i]
	}
	return head
}

// Chapter — слаг главы. У главы-нумератора («2», «II») слага нет: «-ii» в
// адресе ничего читателю не сообщает, а таких глав в корпусе почти три сотни.
func Chapter(title string) string {
	if isEnumerator(title) {
		return ""
	}
	return Text(title)
}

// isEnumerator — заголовок состоит только из печатной нумерации. Смотрит на
// ИСХОДНЫЙ заголовок, а не на результат: кириллическое «Ми» после
// транслитерации выглядит римским MI, и проверка по результату отняла бы у
// него слаг.
func isEnumerator(title string) bool {
	// Проверка пустого результата ставится ПЕРВОЙ: символы типа «§»
	// прошедшие через Text() дают пустую строку и должны быть отбракованы.
	if Text(title) == "" {
		return true
	}

	seen := false
	for _, r := range strings.TrimSpace(title) {
		switch {
		case r >= '0' && r <= '9':
			seen = true
		case strings.ContainsRune("IVXLCDM", r):
			seen = true
		case r == ' ' || r == '.' || r == ')' || r == '-':
			// Разделители нумерации: «1.», «2)».
		default:
			return false
		}
	}
	return seen
}

func collapse(s string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		if r == '-' {
			if prevDash {
				continue
			}
			prevDash = true
		} else {
			prevDash = false
		}
		b.WriteRune(r)
	}
	return b.String()
}

// VolumeTag — координатная часть слага тома: «t06», «t26-ii». Номер добивается
// нулём до двух знаков, чтобы t06 стояло рядом с t10 в любом лексическом
// перечне (выдача поисковика, история браузера, список файлов выгрузки).
func VolumeTag(number *int, part *string) string {
	if number == nil {
		return ""
	}
	tag := fmt.Sprintf("t%02d", *number)
	if part != nil {
		if p := Text(*part); p != "" {
			tag += "-" + p
		}
	}
	return tag
}
