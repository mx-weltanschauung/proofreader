package models

import (
	"strings"
	"testing"
)

// Имя раздела главной: края обрезаются (два собрания с «Раздел Б» и
// «Раздел Б » иначе встали бы двумя разделами), длина — рунами, не байтами.
func TestNormalizeShelfGroup(t *testing.T) {
	cases := []struct {
		name, in, want string
		wantErr        bool
	}{
		{"края обрезаются", "  Раздел Б  ", "Раздел Б", false},
		{"пусто — пусто", "", "", false},
		{"одни пробелы — пусто", "   ", "", false},
		{"80 кириллических букв проходят", strings.Repeat("я", 80), strings.Repeat("я", 80), false},
		{"81 — отказ", strings.Repeat("я", 81), "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := NormalizeShelfGroup(c.in)
			if c.wantErr {
				if err == nil {
					t.Fatalf("NormalizeShelfGroup(%q): ошибки нет, ожидался отказ", c.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeShelfGroup(%q): %v", c.in, err)
			}
			if got != c.want {
				t.Fatalf("NormalizeShelfGroup(%q) = %q, ожидалось %q", c.in, got, c.want)
			}
		})
	}
}
