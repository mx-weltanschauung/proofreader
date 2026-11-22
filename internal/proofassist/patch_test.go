package proofassist

import "testing"

func TestApplyRepeatedWord(t *testing.T) {
	got, err := Apply("мир мир[^s1]", []Edit{{StartRune: 4, EndRune: 7, Old: "мир", New: "пир"}})
	if err != nil || got != "мир пир[^s1]" {
		t.Fatalf("изменено неверное вхождение: %q, %v", got, err)
	}
}

func TestApplyPreservesMarkdown(t *testing.T) {
	got, err := Apply("# A\r\n\tмир[^s1] и $x^{2}$\u00ad\r\n", []Edit{
		{StartRune: 15, EndRune: 16, Old: "и", New: "или"},
		{StartRune: 6, EndRune: 9, Old: "мир", New: "покой"},
	})
	want := "# A\r\n\tпокой[^s1] или $x^{2}$\u00ad\r\n"
	if err != nil || got != want {
		t.Fatalf("получено %q, %v, ожидалось %q", got, err, want)
	}
}

func TestApplyRejectsWrongSpan(t *testing.T) {
	cases := map[string]Edit{
		"неверный фрагмент":    {StartRune: 0, EndRune: 3, Old: "пир", New: "тир"},
		"отрицательное начало": {StartRune: -1, EndRune: 3, Old: "мир", New: "тир"},
		"конец за границей":    {StartRune: 0, EndRune: 4, Old: "мир", New: "тир"},
		"конец раньше начала":  {StartRune: 2, EndRune: 1, New: "тир"},
	}
	for name, edit := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Apply("мир", []Edit{edit}); err == nil {
				t.Fatal("принят неверный диапазон")
			}
		})
	}
}

func TestApplyRejectsOverlap(t *testing.T) {
	edits := []Edit{
		{StartRune: 2, EndRune: 4, Old: "р ", New: "ф"},
		{StartRune: 0, EndRune: 3, Old: "мир", New: "пир"},
	}
	if _, err := Apply("мир мир", edits); err == nil {
		t.Fatal("приняты пересекающиеся правки")
	}
}

func TestApplyRejectsInvisibleRune(t *testing.T) {
	for _, replacement := range []string{"п\u200bир", "п\u202eир", "п\x00ир"} {
		if _, err := Apply("мир", []Edit{{StartRune: 0, EndRune: 3, Old: "мир", New: replacement}}); err == nil {
			t.Errorf("принята замена с невидимым символом: %q", replacement)
		}
	}
}

func TestApplyRemovesInvisibleRune(t *testing.T) {
	got, err := Apply("м\u200bир", []Edit{{StartRune: 1, EndRune: 2, Old: "\u200b"}})
	if err != nil || got != "мир" {
		t.Fatalf("получено %q, %v, ожидалось %q", got, err, "мир")
	}
}

func TestApplyRejectsInvisibleRuneOutsideEdit(t *testing.T) {
	edits := []Edit{{StartRune: 1, EndRune: 4, Old: "мир", New: "пир"}}
	if _, err := Apply("\u200bмир", edits); err == nil {
		t.Fatal("принят итоговый текст с невидимым символом")
	}
}

func TestApplyInsertsAtSpanBoundaries(t *testing.T) {
	edits := []Edit{
		{StartRune: 0, EndRune: 3, Old: "мир", New: "покой"},
		{StartRune: 3, EndRune: 3, New: "!"},
		{StartRune: 0, EndRune: 0, New: "Вот "},
	}
	got, err := Apply("мир", edits)
	if err != nil || got != "Вот покой!" {
		t.Fatalf("получено %q, %v, ожидалось %q", got, err, "Вот покой!")
	}
	if edits[0].Old != "мир" || edits[1].New != "!" || edits[2].New != "Вот " {
		t.Fatal("изменён порядок входных правок")
	}
}

func TestApplyRejectsAmbiguousInsertion(t *testing.T) {
	cases := map[string][]Edit{
		"две вставки в одной позиции": {
			{StartRune: 1, EndRune: 1, New: "а"},
			{StartRune: 1, EndRune: 1, New: "б"},
		},
		"вставка внутри замены": {
			{StartRune: 0, EndRune: 3, Old: "мир", New: "покой"},
			{StartRune: 1, EndRune: 1, New: "а"},
		},
	}
	for name, edits := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Apply("мир", edits); err == nil {
				t.Fatal("принята неоднозначная вставка")
			}
		})
	}
}
