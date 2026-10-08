package seo

import "testing"

// sentences склеивает куски описания через ". ", обрезая хвостовую точку у
// каждого куска — иначе результат начинался бы с «. , ». Excerpt (doc.go)
// обрезает длинный текст многоточием «…», отдельной руной, а не тремя
// точками, и до этой правки TrimRight её не трогал: «Много текста…» плюс
// «Адресов в корпусе: 141» склеивались в «Много текста…. Адресов в
// корпусе: 141» — лишняя точка сразу после многоточия.
func TestSentencesTrimsEllipsisBeforeJoining(t *testing.T) {
	got := sentences("Много текста…", "Адресов в корпусе: 141")
	want := "Много текста. Адресов в корпусе: 141"
	if got != want {
		t.Errorf("sentences: %q, ожидалось %q", got, want)
	}
}

// Обычная точка по-прежнему обрезается, как и раньше.
func TestSentencesTrimsTrailingPeriod(t *testing.T) {
	got := sentences("Первое предложение.", "Второе")
	want := "Первое предложение. Второе"
	if got != want {
		t.Errorf("sentences: %q, ожидалось %q", got, want)
	}
}

// Пустые куски — норма (у части томов нет автора, у части — издания) — не
// должны оставлять пустых «. , » в результате.
func TestSentencesSkipsEmptyParts(t *testing.T) {
	got := sentences("Название", "", "  ", "Автор")
	want := "Название. Автор"
	if got != want {
		t.Errorf("sentences: %q, ожидалось %q", got, want)
	}
}
