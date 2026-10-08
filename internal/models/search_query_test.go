package models

import (
	"strings"
	"testing"
)

func TestNormalizeSearchText(t *testing.T) {
	cases := map[string]string{
		"  прибавочная   стоимость \n": "прибавочная стоимость",
		"\t":   "",
		"ёлка": "ёлка", // ё сводит Postgres (unaccent), не мы
	}
	for in, want := range cases {
		if got := NormalizeSearchText(in); got != want {
			t.Errorf("NormalizeSearchText(%q) = %q, want %q", in, got, want)
		}
	}
	long := strings.Repeat("а", 300)
	if got := NormalizeSearchText(long); len([]rune(got)) != maxQueryRunes {
		t.Errorf("потолок %d рун, получено %d", maxQueryRunes, len([]rune(got)))
	}
}

// Руны, а не байты: «и» — одна руна и два байта.
func TestSearchTextTooShortCountsRunes(t *testing.T) {
	if !SearchTextTooShort("и") {
		t.Error("«и» не отклонён")
	}
	if SearchTextTooShort("на") {
		t.Error("«на» отклонён, а порог — две руны")
	}
}
