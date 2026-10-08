package book

import (
	"fmt"
	"proofreader/internal/site"
	"slices"
	"strings"
	"testing"
	"time"

	"proofreader/internal/models"
)

func TestCountProofreadExcludesEmptyPages(t *testing.T) {
	got := CountProofread([]models.PageStatus{
		models.PageStatusProofread,
		models.PageStatusProofread,
		models.PageStatusMachineProofread,
		models.PageStatusNotProofread,
		models.PageStatusInProgress,
		models.PageStatusHasIssues,
		models.PageStatusNeedsAttention,
		models.PageStatusEmpty, // в знаменатель не входит
	})

	want := Proofread{Total: 7, Human: 2, Machine: 1, Raw: 4}
	if got != want {
		t.Errorf("CountProofread() = %+v, хотел %+v", got, want)
	}
}

func TestRussianDate(t *testing.T) {
	// Все двенадцать месяцев: таблица родительного падежа проверялась
	// пробой на августе одном, а ошибиться можно в любой из двенадцати
	// строк — например перепутать "марта"/"март" или сдвиг индекса.
	cases := []struct {
		t    time.Time
		want string
	}{
		{time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC), "1 января 2026 г."},
		{time.Date(2026, time.February, 3, 12, 0, 0, 0, time.UTC), "3 февраля 2026 г."},
		{time.Date(2026, time.March, 8, 12, 0, 0, 0, time.UTC), "8 марта 2026 г."},
		{time.Date(2026, time.April, 12, 12, 0, 0, 0, time.UTC), "12 апреля 2026 г."},
		{time.Date(2026, time.May, 9, 12, 0, 0, 0, time.UTC), "9 мая 2026 г."},
		{time.Date(2026, time.June, 22, 12, 0, 0, 0, time.UTC), "22 июня 2026 г."},
		{time.Date(2026, time.July, 4, 12, 0, 0, 0, time.UTC), "4 июля 2026 г."},
		{time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC), "14 августа 2026 г."},
		{time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC), "1 сентября 2026 г."},
		{time.Date(2026, time.October, 31, 12, 0, 0, 0, time.UTC), "31 октября 2026 г."},
		{time.Date(2026, time.November, 7, 12, 0, 0, 0, time.UTC), "7 ноября 2026 г."},
		{time.Date(2026, time.December, 31, 12, 0, 0, 0, time.UTC), "31 декабря 2026 г."},
	}
	for _, c := range cases {
		got := RussianDate(c.t)
		if got != c.want {
			t.Errorf("RussianDate(%s) = %q, хотел %q", c.t, got, c.want)
		}
	}
}

// TestPluralForm — таблица склонения по стандартному правилу русского
// числительного. Пишется первой: от неё зависит согласование в обеих фразах
// TitleLines, а собственный пример брифа (742 → "страницы") этот тест и
// должен был поймать.
func TestPluralForm(t *testing.T) {
	ns := []int{0, 1, 2, 4, 5, 11, 12, 14, 21, 22, 25, 101, 111, 742}

	// «все N ... выверены» — именительный падеж.
	nominative := map[int]string{
		0: "страниц", 1: "страница", 2: "страницы", 4: "страницы", 5: "страниц",
		11: "страниц", 12: "страниц", 14: "страниц", 21: "страница", 22: "страницы", 25: "страниц",
		101: "страница", 111: "страниц", 742: "страницы",
	}
	// «Из N ... вычитано» — родительный падеж при предлоге «из»: единица
	// (кроме одиннадцати) отличается, а «мало» и «много» совпадают.
	genitiveIz := map[int]string{
		0: "страниц", 1: "страницы", 2: "страниц", 4: "страниц", 5: "страниц",
		11: "страниц", 12: "страниц", 14: "страниц", 21: "страницы", 22: "страниц", 25: "страниц",
		101: "страницы", 111: "страниц", 742: "страниц",
	}

	for _, n := range ns {
		if got, want := pluralForm(n, "страница", "страницы", "страниц"), nominative[n]; got != want {
			t.Errorf("pluralForm(%d, именительный) = %q, хотел %q", n, got, want)
		}
		if got, want := pluralForm(n, "страницы", "страниц", "страниц"), genitiveIz[n]; got != want {
			t.Errorf("pluralForm(%d, «из») = %q, хотел %q", n, got, want)
		}
	}
}

// TestProofreadLinesSentences проверяет полный текст предложения, а не
// наличие ключевого слова: именно contains-проверка пропустила собственную
// ошибку брифа на 742 страницах. Числа подобраны так, чтобы развести все три
// формы счёта и оба тин-исключения (11..14, 111): 1, 2, 5, 11, 21, 22, 25,
// 101, 111, 742.
func TestProofreadLinesSentences(t *testing.T) {
	fullyProofread := []struct {
		total int
		want  string
	}{
		{1, "Вычитано полностью: 1 страница выверена человеком."},
		{2, "Вычитано полностью: 2 страницы выверены человеком."},
		{5, "Вычитано полностью: 5 страниц выверены человеком."},
		{11, "Вычитано полностью: 11 страниц выверены человеком."},
		{21, "Вычитано полностью: 21 страница выверена человеком."},
		{22, "Вычитано полностью: 22 страницы выверены человеком."},
		{25, "Вычитано полностью: 25 страниц выверены человеком."},
		{101, "Вычитано полностью: 101 страница выверена человеком."},
		{111, "Вычитано полностью: 111 страниц выверены человеком."},
		{742, "Вычитано полностью: 742 страницы выверены человеком."}, // собственный пример брифа
	}
	for _, c := range fullyProofread {
		t.Run(fmt.Sprintf("вычитано полностью, %d", c.total), func(t *testing.T) {
			got := proofreadLines(Proofread{Total: c.total, Human: c.total})
			want := []string{c.want}
			if !slices.Equal(got, want) {
				t.Errorf("proofreadLines(Total:%d, Human:%d) =\n%#v\nхотел\n%#v", c.total, c.total, got, want)
			}
		})
	}

	// «Из N ...» — родительный падеж при предлоге, формы отличаются от
	// именительного выше: не спутать их при рефакторинге в одну функцию.
	mixed := []struct {
		total int
		want  string
	}{
		{1, "Из 1 страницы вычитано человеком — 0, машиной — 0, не вычитано — 1."},
		{2, "Из 2 страниц вычитано человеком — 0, машиной — 0, не вычитано — 2."},
		{5, "Из 5 страниц вычитано человеком — 0, машиной — 0, не вычитано — 5."},
		{11, "Из 11 страниц вычитано человеком — 0, машиной — 0, не вычитано — 11."},
		{21, "Из 21 страницы вычитано человеком — 0, машиной — 0, не вычитано — 21."},
		{22, "Из 22 страниц вычитано человеком — 0, машиной — 0, не вычитано — 22."},
		{25, "Из 25 страниц вычитано человеком — 0, машиной — 0, не вычитано — 25."},
		{101, "Из 101 страницы вычитано человеком — 0, машиной — 0, не вычитано — 101."},
		{111, "Из 111 страниц вычитано человеком — 0, машиной — 0, не вычитано — 111."},
		{742, "Из 742 страниц вычитано человеком — 12, машиной — 715, не вычитано — 15."}, // собственный пример брифа, смешанное состояние
	}
	for _, c := range mixed {
		t.Run(fmt.Sprintf("не вычитано полностью, %d", c.total), func(t *testing.T) {
			var p Proofread
			if c.total == 742 {
				p = Proofread{Total: 742, Human: 12, Machine: 715, Raw: 15}
			} else {
				p = Proofread{Total: c.total, Raw: c.total}
			}
			got := proofreadLines(p)
			wantLines := []string{
				c.want,
				"Текст распознан автоматически; человеческая вычитка не завершена.",
				"Возможны ошибки — сверяйтесь со сканом в читальне.",
			}
			if !slices.Equal(got, wantLines) {
				t.Errorf("proofreadLines(%+v) =\n%#v\nхотел\n%#v", p, got, wantLines)
			}
		})
	}
}

func titleText(m Meta) string { return strings.Join(TitleLines(m), "\n") }

func sampleMeta() Meta {
	return Meta{
		Title:    "Капитал. Книга первая",
		Authors:  []string{"К. Маркс", "Ф. Энгельс"},
		Edition:  "К. Маркс и Ф. Энгельс. Сочинения",
		Volume:   "Том 23",
		PageFrom: 43,
		PageTo:   784,
		URL:      "https://lib.example.org/works/47/chapters/2066",
		Modified: time.Date(2026, time.August, 14, 0, 0, 0, 0, time.UTC),
		Lang:     "ru",
	}
}

func TestTitleLinesCarrySourceAndState(t *testing.T) {
	m := sampleMeta()
	m.Proofread = Proofread{Total: 742, Human: 12, Machine: 715, Raw: 15}

	got := titleText(m)

	for _, want := range []string{
		"К. Маркс, Ф. Энгельс",
		"Капитал. Книга первая",
		"Источник: К. Маркс и Ф. Энгельс. Сочинения. Том 23, с. 43—784",
		"Читальня: https://lib.example.org/works/47/chapters/2066",
		"Состояние текста на 14 августа 2026 г.",
		"Из 742 страниц вычитано человеком — 12, машиной — 715, не вычитано — 15.",
		"Возможны ошибки",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("на титуле нет %q:\n%s", want, got)
		}
	}
}

// Имя на титуле — имя экземпляра, а не нашей читальни.
func TestTitleLinesUseSiteName(t *testing.T) {
	site.Set("Тестовая", "")
	t.Cleanup(func() { site.Set("", "") })
	got := titleText(sampleMeta())
	if !strings.Contains(got, "Тестовая: https://lib.example.org/works/47/chapters/2066") {
		t.Errorf("на титуле нет имени экземпляра:\n%s", got)
	}
}

func TestTitleLinesDropWarningWhenFullyProofread(t *testing.T) {
	m := sampleMeta()
	m.Proofread = Proofread{Total: 742, Human: 742}

	got := titleText(m)

	if strings.Contains(got, "Возможны ошибки") {
		t.Errorf("предупреждение осталось на полностью вычитанной книге:\n%s", got)
	}
	if !strings.Contains(got, "Вычитано полностью") {
		t.Errorf("нет отметки о полной вычитке:\n%s", got)
	}
}

// У подборки нет ни издания, ни единого диапазона страниц — строки о
// источнике быть не должно, а не должно быть строки-огрызка «Источник: , с. 0—0».
func TestTitleLinesOmitSourceForCollection(t *testing.T) {
	m := Meta{
		Title:     "Моя подборка",
		URL:       "https://lib.example.org/collections/moi",
		Modified:  time.Date(2026, time.August, 14, 0, 0, 0, 0, time.UTC),
		Proofread: Proofread{Total: 10, Human: 10},
	}

	got := titleText(m)

	if strings.Contains(got, "Источник:") {
		t.Errorf("у подборки напечатан источник:\n%s", got)
	}
	if !strings.Contains(got, "Моя подборка") {
		t.Errorf("название подборки потеряно:\n%s", got)
	}
}

// TestTitleLinesSourceLine проверяет все комбинации Edition/Volume точной
// строкой: у одного тома в живой базе есть издание, но нет номера тома, и
// наивное склеивание ". " через фиксированный двухэлементный срез печатало
// огрызок вида "Источник: . Том 23, с. 10—20" или "Источник: Изд., с. 10—20"
// с лишней точкой.
//
// Отдельная забота — двойная точка: 21 из 22 работ с изданием и томом в живой
// базе используют издание, оканчивающееся точкой ("...Сочинения, 2-е изд."),
// и наивная склейка через ". " печатала "...2-е изд.. Том 23". Разделитель
// должен зависеть от того, чем уже кончается предыдущая часть — точкой,
// восклицательным, вопросительным знаком или многоточием.
func TestTitleLinesSourceLine(t *testing.T) {
	cases := []struct {
		name    string
		edition string
		volume  string
		want    string
	}{
		{
			name:    "издание без точки на конце и том",
			edition: "К. Маркс и Ф. Энгельс. Сочинения",
			volume:  "Том 23",
			want:    "Источник: К. Маркс и Ф. Энгельс. Сочинения. Том 23, с. 43—784",
		},
		{
			name:    "только издание",
			edition: "К. Маркс и Ф. Энгельс. Сочинения",
			volume:  "",
			want:    "Источник: К. Маркс и Ф. Энгельс. Сочинения, с. 43—784",
		},
		{
			name:    "только том",
			edition: "",
			volume:  "Том 23",
			want:    "Источник: Том 23, с. 43—784",
		},
		{
			// Доминирующий случай в живой базе: 21 из 22 работ с изданием и
			// томом устроены так.
			name:    "издание с точкой на конце и том — доминирующий случай",
			edition: "К. Маркс и Ф. Энгельс. Сочинения, 2-е изд.",
			volume:  "Том 23",
			want:    "Источник: К. Маркс и Ф. Энгельс. Сочинения, 2-е изд. Том 23, с. 43—784",
		},
		{
			name:    "издание с точкой на конце, без тома",
			edition: "К. Маркс и Ф. Энгельс. Сочинения, 2-е изд.",
			volume:  "",
			want:    "Источник: К. Маркс и Ф. Энгельс. Сочинения, 2-е изд., с. 43—784",
		},
		{
			name:    "издание с многоточием на конце и том",
			edition: "Собрание сочинений в незавершённом виде…",
			volume:  "Том 23",
			want:    "Источник: Собрание сочинений в незавершённом виде… Том 23, с. 43—784",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := sampleMeta()
			m.Edition = c.edition
			m.Volume = c.volume
			m.Proofread = Proofread{Total: 1, Human: 1}

			lines := TitleLines(m)
			if !slices.Contains(lines, c.want) {
				t.Errorf("нет строки %q среди:\n%s", c.want, strings.Join(lines, "\n"))
			}
		})
	}
}

func TestTitleLinesListMissingItems(t *testing.T) {
	m := sampleMeta()
	m.Proofread = Proofread{Total: 5, Human: 5}
	m.Missing = []string{"Тезисы о Фейербахе"}

	got := titleText(m)

	// Регистр не проверяем: на титуле это заголовок с большой буквы
	// («Недоступно…»), проверка — по содержанию, а не по капитализации.
	if !strings.Contains(strings.ToLower(got), "недоступно на момент выгрузки") {
		t.Errorf("нет отметки о пропущенном элементе:\n%s", got)
	}
	if !strings.Contains(got, "Тезисы о Фейербахе") {
		t.Errorf("не назван пропущенный элемент:\n%s", got)
	}
}

// TestTitleLinesNoBlankEdges — контракт "без пустых строк по краям" держится
// на пустой книге тоже: пустая страница (пустая_страница) — легальное
// состояние, и Proofread.Total == 0 не должен оставлять хвостовой "" в срезе.
func TestTitleLinesNoBlankEdges(t *testing.T) {
	t.Run("пустой Meta целиком", func(t *testing.T) {
		got := TitleLines(Meta{})
		if len(got) != 0 {
			t.Errorf("TitleLines(Meta{}) = %#v, хотел пустой срез", got)
		}
	})
	t.Run("Proofread.Total == 0", func(t *testing.T) {
		m := sampleMeta()
		m.Proofread = Proofread{}

		got := TitleLines(m)
		if len(got) > 0 && got[0] == "" {
			t.Errorf("пустая строка в начале:\n%#v", got)
		}
		if len(got) > 0 && got[len(got)-1] == "" {
			t.Errorf("пустая строка в конце:\n%#v", got)
		}
	})
}

func TestSourceLine(t *testing.T) {
	cases := []struct {
		name string
		m    Meta
		want string
	}{
		{"издание, том, страницы", Meta{Edition: "Сочинения", Volume: "Том 1", PageFrom: 101, PageTo: 103}, "Сочинения. Том 1, с. 101—103"},
		{"издание с точкой", Meta{Edition: "Сочинения, 2-е изд.", Volume: "Том 23"}, "Сочинения, 2-е изд. Том 23"},
		{"только том", Meta{Volume: "Том 5", PageFrom: 1, PageTo: 2}, "Том 5, с. 1—2"},
		{"пусто", Meta{PageFrom: 1, PageTo: 2}, ""},
	}
	for _, c := range cases {
		if got := SourceLine(c.m); got != c.want {
			t.Errorf("%s: %q, ожидалось %q", c.name, got, c.want)
		}
	}
}
