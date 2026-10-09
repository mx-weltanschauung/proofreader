package models

import "testing"

func TestIssueLabel(t *testing.T) {
	for _, c := range []struct {
		from, to int
		want     string
	}{{5, 5, "5"}, {5, 6, "5—6"}, {10, 12, "10—12"}} {
		if got := IssueLabel(c.from, c.to); got != c.want {
			t.Errorf("IssueLabel(%d, %d) = %q, ждали %q", c.from, c.to, got, c.want)
		}
	}
}

func TestIssueTitle(t *testing.T) {
	if got := IssueTitle("Под знаменем марксизма", 1925, "5—6"); got != "Под знаменем марксизма, 1925, № 5—6" {
		t.Fatalf("got %q", got)
	}
}

func TestValidArticleKind(t *testing.T) {
	for _, k := range []string{"статья", "рецензия", "документ", "от_редакции", "выступление", "прочее"} {
		if !ValidArticleKind(k) {
			t.Errorf("%q отвергнут", k)
		}
	}
	for _, k := range []string{"", "article", "Статья"} {
		if ValidArticleKind(k) {
			t.Errorf("%q принят", k)
		}
	}
}

func TestPersonSortKey(t *testing.T) {
	for in, want := range map[string]string{
		"И. И. Рубин": "рубин и и",
		"Гр. Баммель": "баммель гр",
		"Ёлкин":       "елкин",
		"Вл. Виленский (Сибиряков)": "виленский вл",
		// Финальная рецензия: полное имя без инициалов — фамилией вперёд,
		// иначе «Авербах» в окне слияния его не находит.
		"Леопольд Авербах": "авербах леопольд",
		"Григ. Марецкий":   "марецкий григ",
		// Запрос поиска «Рубин И»: короткое последнее слово — не фамилия.
		"Рубин И": "рубин и",
	} {
		if got := PersonSortKey(in); got != want {
			t.Errorf("PersonSortKey(%q) = %q, ждали %q", in, got, want)
		}
	}
}
