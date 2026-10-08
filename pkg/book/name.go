package book

import (
	"fmt"
	"strings"
	"unicode"
)

// maxNameRunes — предел длины человеческого имени файла, в символах.
// Ограничение файловых систем — 255 байт; кириллица занимает два байта на
// букву, так что сотня символов с запасом влезает вместе с расширением.
const maxNameRunes = 100

// ContentDisposition собирает заголовок отдачи файла.
//
// Заголовки корпуса кириллические, а filename= по RFC 6266 обязан быть ASCII:
// на голом UTF-8 Safari и старый Firefox отдают крокозябры. Поэтому пишутся
// оба поля — техническая ASCII-запаска и человеческое имя в filename*.
// Браузер, понимающий filename*, берёт его; остальные получают читаемую,
// пусть и безликую, запаску.
func ContentDisposition(asciiBase, humanBase, ext string) string {
	ascii := asciiBase + "." + ext

	human := sanitizeFileName(humanBase)
	if human == "" {
		return fmt.Sprintf("attachment; filename=%q", ascii)
	}
	human += "." + ext

	// mime.FormatMediaType кодирует в filename* только при наличии не-ASCII
	// символов: на чисто латинском имени он молча пишет второй filename=, и
	// заголовок с двумя одноимёнными параметрами не разбирается вовсе
	// (mime: duplicate parameter name). В корпусе хватает глав с латинскими
	// или чисто цифровыми названиями («I.», «Introduction to Philosophy»),
	// так что кодировать нужно безусловно, вручную, а не полагаться на
	// эвристику стандартной библиотеки.
	return fmt.Sprintf("attachment; filename=%q; filename*=UTF-8''%s", ascii, encodeRFC8187(human))
}

// encodeRFC8187 percent-кодирует строку по правилу attr-char из RFC 8187 —
// том самом, что используется в ext-value параметра filename*. Пропускает
// без изменений ASCII-буквы, цифры и узкий набор «безопасных» знаков,
// остальное кодирует байт за байтом поверх UTF-8, как требует стандарт.
func encodeRFC8187(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if isRFC8187AttrChar(c) {
			b.WriteByte(c)
		} else {
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&0x0F])
		}
	}
	return b.String()
}

// isRFC8187AttrChar — тот самый attr-char: ALPHA / DIGIT и набор из
// "!#$&+-.^_`|~". Пробел, кавычки, двоеточие, точка с запятой и всё
// не-ASCII в него не входят и уходят в percent-encoding.
func isRFC8187AttrChar(c byte) bool {
	switch {
	case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		return true
	}
	switch c {
	case '!', '#', '$', '&', '+', '-', '.', '^', '_', '`', '|', '~':
		return true
	}
	return false
}

// sanitizeFileName выкидывает из заголовка всё, чем давится файловая система,
// схлопывает пробелы и обрезает результат по границе символа.
func sanitizeFileName(s string) string {
	replaced := strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			return ' '
		}
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)

	name := strings.Join(strings.Fields(replaced), " ")

	// Обрезка по рунам, не по байтам: на кириллице срез по байту рассекает
	// букву пополам и даёт невалидный UTF-8.
	runes := []rune(name)
	if len(runes) > maxNameRunes {
		name = strings.TrimRight(string(runes[:maxNameRunes]), " ")
	}
	return name
}
