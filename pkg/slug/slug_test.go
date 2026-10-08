package slug

import "strings"
import "testing"

func TestTextMatchesPipelineTransliteration(t *testing.T) {
	// Эти две строки — из tools/ocr_ingest/test_index_parser.py: ими сделаны
	// существующие слаги понятий, и расхождение здесь означало бы, что в
	// читальне два правила транслитерации.
	cases := map[string]string{
		"Абстрактный труд":                               "abstraktnyj-trud",
		"Абстракция, абстрактное и конкретное":           "abstrakciya-abstraktnoe-i-konkretnoe",
		"Ещё раз о профсоюзах":                           "esche-raz-o-profsoyuzah",
		"ЧТО ДЕЛАТЬ? Наболевшие вопросы нашего движения": "chto-delat-nabolevshie-voprosy-nashego-dvizheniya",
		// Настоящий дефис внутри слова остаётся дефисом, а сокращение
		// «с.-д.» рассыпается в отдельные куски — обе строки из корпуса.
		"Материализм и эмпирио-критицизм":              "materializm-i-empirio-kriticizm",
		"ПРОТИВ БОЙКОТА (Из заметок с.-д. публициста)": "protiv-bojkota-iz-zametok-s-d-publicista",
	}
	for in, want := range cases {
		if got := Text(in); got != want {
			t.Errorf("Text(%q) = %q, ожидалось %q", in, got, want)
		}
	}
}

func TestTextCutsAtWordBoundary(t *testing.T) {
	// Полный слаг — 68 знаков; рез по последнему дефису до 60 даёт 58.
	in := "ПАМЯТИ ГРАФА ГЕЙДЕНА (Чему учат народ наши беспартийные «демократы»?)"
	want := "pamyati-grafa-gejdena-chemu-uchat-narod-nashi-bespartijnye"
	if got := Text(in); got != want {
		t.Errorf("Text(...) = %q (%d знаков), ожидалось %q", got, len(got), want)
	}
	if len(want) > MaxLen {
		t.Fatalf("ожидание длиннее потолка: %d", len(want))
	}
}

func TestTextHardCutsSingleLongWord(t *testing.T) {
	// Слова длиннее потолка в корпусе нет, но правило обязано быть
	// определено: без дефиса до предела рез идёт по знаку.
	got := Text(strings.Repeat("а", 80))
	if len(got) != MaxLen {
		t.Errorf("длина %d, ожидался ровно потолок %d: %q", len(got), MaxLen, got)
	}
}

func TestChapterDropsEnumeratorTitles(t *testing.T) {
	// 153 главы корпуса называются «2», 136 — «II». Слаг «-ii» в адресе
	// украшением не является; такие главы остаются голым номером.
	for _, in := range []string{"2", "II", "III", "IV", "V", "§", "  1  ", "1905"} {
		if got := Chapter(in); got != "" {
			t.Errorf("Chapter(%q) = %q, ожидалась пустая строка", in, got)
		}
	}
}

func TestChapterKeepsCyrillicThatLooksRoman(t *testing.T) {
	// «Ми» латиницей было бы MI, то есть 1001, но здесь кириллица: отбраковка
	// нумераторов смотрит на исходный заголовок, а не на результат.
	if got := Chapter("Ми"); got != "mi" {
		t.Errorf("Chapter(\"Ми\") = %q, ожидалось \"mi\"", got)
	}
}

func TestVolumeTag(t *testing.T) {
	six, twentySix, hundred := 6, 26, 100
	part2 := "II"
	cases := []struct {
		number *int
		part   *string
		want   string
	}{
		{&six, nil, "t06"},
		{&twentySix, &part2, "t26-ii"},
		// Трёхзначный номер не добивается: добивка нужна лишь чтобы t06
		// сортировалось рядом с t10, и на трёх знаках теряет смысл.
		{&hundred, nil, "t100"},
		// Без номера тега нет — у работы вне собрания слаг соберётся из
		// заголовка, и пустая строка здесь именно этот случай.
		{nil, nil, ""},
	}
	for _, c := range cases {
		if got := VolumeTag(c.number, c.part); got != c.want {
			t.Errorf("VolumeTag(%v, %v) = %q, ожидалось %q", c.number, c.part, got, c.want)
		}
	}
}
