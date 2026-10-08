package models

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

const (
	// Ник короче двух знаков неразличим, длиннее тридцати двух не влезает в
	// подпись и в адрес подборки.
	NicknameMinRunes = 2
	NicknameMaxRunes = 32
)

// reservedNicknames — базовый закрытый список: служебные имена, которыми
// подпись выдавала бы себя за сотрудника любой читальни. Сравнение идёт по
// нормализованной форме.
var reservedNicknames = map[string]bool{
	"admin": true, "administrator": true, "editor": true, "редактор": true,
	"moderator": true, "модератор": true,
}

// extraReserved — занятые имена экземпляра: имя самой читальни и авторов её
// корпуса (без них появится подборка, подписанная «В. И. Ленин»).
var extraReserved map[string]bool

// SetExtraReservedNicknames дополняет базовый список именами экземпляра
// (RESERVED_NICKNAMES и SITE_NAME). Зовётся один раз на старте, до приёма
// запросов; nil сбрасывает дополнение.
func SetExtraReservedNicknames(names []string) {
	extraReserved = make(map[string]bool, len(names))
	for _, n := range names {
		extraReserved[NormalizeNickname(n)] = true
	}
}

// NormalizeNickname приводит ник к форме сравнения.
//
// Обязана совпадать с выражением вычисляемого столбца users.nickname_key
// (lower(normalize(nickname, NFKC))): уникальность держит Postgres, а эта
// функция нужна только для проверки по закрытому списку.
//
// ВАЖНО: Go делает TrimSpace ДО нормализации, а SQL-функция normalize() в Postgres
// пробелы не срезает. Если записать в базу сырую строку со скобами, ключ разойдётся
// молча: `"  Чтец  "` в Go даст ключ `"чтец"`, а в Postgres — `"  чтец  "`. Вызывающий
// обязан писать в базу триммленную форму (strings.TrimSpace(nickname)), иначе
// уникальность разорвётся, и две записи с пробелами по-разному окажут в одной строке
// или с разными ключами при одном видимом нике.
func NormalizeNickname(raw string) string {
	return strings.ToLower(norm.NFKC.String(strings.TrimSpace(raw)))
}

// ValidateNickname проверяет форму ника и возвращает отказ словами читателя.
func ValidateNickname(raw string) error {
	nick := strings.TrimSpace(raw)
	runes := []rune(nick)

	if len(runes) < NicknameMinRunes {
		return fmt.Errorf("Имя короче %d знаков", NicknameMinRunes)
	}
	if len(runes) > NicknameMaxRunes {
		return fmt.Errorf("Имя длиннее %d знаков", NicknameMaxRunes)
	}

	var hasLetter, hasCyrillic, hasLatin bool
	for _, r := range runes {
		switch {
		case unicode.Is(unicode.Cyrillic, r):
			hasLetter, hasCyrillic = true, true
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
			hasLetter, hasLatin = true, true
		case unicode.IsDigit(r), r == '_', r == '-':
			// допустимо
		default:
			return errors.New("В имени можно использовать буквы, цифры, дефис и подчёркивание")
		}
	}

	if !hasLetter {
		return errors.New("В имени должна быть хотя бы одна буква")
	}
	// Гомоглиф — способ приписать чужому имени чужой текст, а подпись стоит под
	// тем, за что отвечает читальня.
	if hasCyrillic && hasLatin {
		return errors.New("В имени нельзя смешивать русские и латинские буквы")
	}
	if k := NormalizeNickname(nick); reservedNicknames[k] || extraReserved[k] {
		return errors.New("Это имя занято читальней")
	}

	return nil
}
