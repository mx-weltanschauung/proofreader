package models

import (
	"strings"
	"testing"
)

func TestValidateNicknameAccepts(t *testing.T) {
	for _, nick := range []string{"чтец", "reader", "Чтец_1917", "an-na", "Маркс2"} {
		if err := ValidateNickname(nick); err != nil {
			t.Errorf("ValidateNickname(%q) = %v, want nil", nick, err)
		}
	}
}

func TestValidateNicknameRejects(t *testing.T) {
	cases := map[string]string{
		"пусто":    "",
		"короткий": "я",
		// Длину считает Repeat, а не глаз: литерал из тридцати трёх одинаковых
		// букв ломается молча и не о том, что проверяет тест.
		"длинный":               strings.Repeat("а", NicknameMaxRunes+1),
		"пробел внутри":         "два слова",
		"собака":                "чтец@дома",
		"только цифры":          "1917",
		"смешение раскладок":    "маrкс",
		"смешение наоборот":     "mарx",
		"занятое имя":           "admin",
		"занятое имя по-русски": "Редактор",
	}
	for name, nick := range cases {
		t.Run(name, func(t *testing.T) {
			if err := ValidateNickname(nick); err == nil {
				t.Errorf("ValidateNickname(%q) = nil, want error", nick)
			}
		})
	}
}

// Смешение раскладок запрещено не из вкусовщины: подпись стоит под текстом, за
// который отвечает читальня, и гомоглиф — ровно способ приписать чужому имени
// чужой текст.
//
// В строке ниже третий знак — ЛАТИНСКАЯ "e" (U+0065) внутри кириллического
// «Ленин». Глазом это не отличить, и в том вся суть: по закрытому списку ник не
// ловится (нормализованная форма не равна «ленин»), ловит его только правило
// смешения раскладок.
// Имена авторов корпуса и имя самой читальни — не свойство платформы, а
// экземпляра: базовый список их не знает, их добавляет RESERVED_NICKNAMES.
func TestExtraReservedNicknames(t *testing.T) {
	t.Cleanup(func() { SetExtraReservedNicknames(nil) })
	if err := ValidateNickname("Ленин"); err != nil {
		t.Fatalf("без списка экземпляра ник свободен, а получили: %v", err)
	}
	SetExtraReservedNicknames([]string{" ЛЕНИН ", "Читальня"})
	for _, nick := range []string{"ленин", "ЧИТАЛЬНЯ", "admin"} {
		if err := ValidateNickname(nick); err == nil {
			t.Errorf("ValidateNickname(%q) = nil, ник занят", nick)
		}
	}
	SetExtraReservedNicknames(nil)
	if err := ValidateNickname("ленин"); err != nil {
		t.Fatalf("сброс списка не освободил ник: %v", err)
	}
}

func TestValidateNicknameRejectsHomoglyph(t *testing.T) {
	const homoglyph = "Лeнин"
	if err := ValidateNickname(homoglyph); err == nil {
		t.Error("ник с латинской e внутри кириллицы принят, а должен быть отвергнут")
	}
}

func TestNormalizeNicknameFoldsCase(t *testing.T) {
	if NormalizeNickname("Чтец") != NormalizeNickname("чТеЦ") {
		t.Error("нормализация не сводит регистр")
	}
}
