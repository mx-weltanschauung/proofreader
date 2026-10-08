package markdown

import (
	"strings"
	"testing"
)

// ForUntrustedAuthor — рендерер для разметки, которую пишет вошедший
// читатель (тело разбора). Закрывает ЧЕТЫРЕ стока, и тест подаёт каждый своим
// способом: сырым HTML, блочным атрибутом, схемой ссылки и картинкой.
//
// Урок, ради которого тест разложен по строкам таблицы: первая редакция
// перечисляла верные приметы (`onclick`, `javascript:`), но подавала их
// ТОЛЬКО сырым HTML — и осталась зелёной при двух открытых стоках. Третий
// (картинка-маячок) закрыт следующим заходом; строк в таблице ровно столько,
// сколько способов подать нагрузку, а не сколько флагов стоит в методе.
func TestForUntrustedAuthorClosesEveryExecutableSink(t *testing.T) {
	r := NewRenderer().ForUntrustedAuthor()

	for _, tc := range []struct {
		name string
		src  string
	}{
		{"сырой HTML, блочный", "<script>alert(1)</script>\n"},
		{"сырой HTML, встроенный", "Текст с <img src=x onerror=\"alert(2)\"> внутри.\n"},
		{"блочный атрибут на абзаце",
			"{onmouseover=\"alert(3)\" style=\"position:fixed;inset:0\"}\nАбзац.\n"},
		{"блочный атрибут на заголовке", "{onclick=\"alert(4)\"}\n# Заголовок\n"},
		{"блочный атрибут на цитате", "{onmouseover=\"alert(5)\"}\n> Цитата\n"},
		{"блочный атрибут на блоке кода", "{onload=\"alert(6)\"}\n```\nкод\n```\n"},
		{"блочный атрибут на таблице",
			"{onmouseover=\"alert(7)\"}\n| а |\n| --- |\n| 1 |\n"},
		{"схема ссылки", "[текст](javascript:alert(8))\n"},
		{"схема ссылки с табуляцией внутри", "[текст](java\tscript:alert(9))\n"},
		// Маячок: не исполнение, но при открытии предпросмотра браузер
		// модератора сходит на чужой хост и выдаст его адрес и клиента.
		{"картинка с чужого хоста", "![подпись](https://evil.example/track.gif)\n"},
		{"картинка без подписи", "![](https://evil.example/bare.gif)\n"},
		{"картинка внутри ссылки",
			"[текст ![alt](https://evil.example/p.gif)](https://example.org)\n"},
		{"картинка со схемой javascript", "![подпись](javascript:alert(10))\n"},
		{"картинка схемой data", "![подпись](data:text/html;base64,PHNjcmlwdD4=)\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := r.Render(tc.src)
			if bad := unsafeMarkupIn(got); len(bad) > 0 {
				t.Errorf("сток открыт: %v\n%s", bad, got)
			}
		})
	}
}

// Контрольная группа: подавляется исполняемое, а не разметка вообще. Без неё
// «починка», выбросившая из авторского текста всё, прошла бы зелёной.
func TestForUntrustedAuthorKeepsHarmlessMarkup(t *testing.T) {
	const src = "*курсив*, **жирный**, [ссылка](https://example.org), " +
		"[почта](mailto:a@example.org).\n\n" +
		"# Заголовок\n\n> Цитата\n\n| а | б |\n| --- | --- |\n| 1 | 2 |\n\n" +
		"Текст до ![выброшенная](https://example.org/p.gif) текст после.\n\n" +
		"Сноска[^1].\n\n[^1]: тело сноски\n"

	got := NewRenderer().ForUntrustedAuthor().Render(src)

	for _, want := range []string{
		"<em>курсив</em>", "<strong>жирный</strong>",
		`href="https://example.org"`, "mailto:a@example.org",
		"<h1", "<blockquote>", "<table>", "<td", "тело сноски",
		// Картинка выброшена, а текст ВОКРУГ неё — нет: подавление снимает
		// подресурс, а не абзац, в котором он стоял.
		"Текст до", "текст после.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("подавление задело безобидную разметку, нет %q:\n%s", want, got)
		}
	}
	if bad := unsafeMarkupIn(got); len(bad) > 0 {
		t.Errorf("в безобидной фикстуре осталось недоверенное: %v\n%s", bad, got)
	}
}

// Метод отдаёт КОПИЮ: исходный рендерер — один на процесс (cmd/server), и
// корпус (глава, полоса, вклейка, скачивание, страница краулеру) обязан
// продолжать видеть и свой ручной HTML, и свои блочные атрибуты, и свои
// картинки. Разъедься это — тома потеряли бы печатные таблицы молча.
func TestForUntrustedAuthorLeavesOriginalRendererAlone(t *testing.T) {
	const corpus = "<table><tr><td>товар<br>в центнерах</td></tr></table>\n\n" +
		"{.verse}\nСтрока стиха\n\n![илл.](/works/1/pages/4.png)\n"

	r := NewRenderer()
	before := r.Render(corpus)
	for _, want := range []string{"<table>", `class="verse"`, "<img"} {
		if !strings.Contains(before, want) {
			t.Fatalf("корпусная разметка не дошла до вывода и без подавления, нет %q:\n%s",
				want, before)
		}
	}

	safe := r.ForUntrustedAuthor().Render(corpus)
	for _, gone := range []string{"<table>", `class="verse"`, "<img"} {
		if strings.Contains(safe, gone) {
			t.Fatalf("подавление не сработало, осталось %q:\n%s", gone, safe)
		}
	}

	if after := r.Render(corpus); after != before {
		t.Fatalf("исходный рендерер изменился:\nбыло:\n%s\nстало:\n%s", before, after)
	}
}
