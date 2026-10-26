package models

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// ShelfGroupMaxRunes — предел длины имени раздела главной
// (editions.shelf_group, event_organizations.shelf_group; миграция 000043).
const ShelfGroupMaxRunes = 80

// NormalizeShelfGroup — имя раздела главной в том виде, в каком его хранят:
// края обрезаны, длина не больше ShelfGroupMaxRunes рун. Пусто — раздел по
// умолчанию. Края режутся здесь, а не в форме: «Раздел Б» и «Раздел Б » у
// двух собраний иначе встали бы на главной двумя разделами с одним видимым
// именем.
func NormalizeShelfGroup(s string) (string, error) {
	s = strings.TrimSpace(s)
	if n := utf8.RuneCountInString(s); n > ShelfGroupMaxRunes {
		return "", fmt.Errorf("shelf_group: не длиннее %d знаков, пришло %d", ShelfGroupMaxRunes, n)
	}
	return s, nil
}
