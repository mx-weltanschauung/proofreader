package seo

import (
	"net/http"
	"strings"
)

// closedPaths — что закрыто от ОБХОДА. Это не то же самое, что закрыто от
// индексации, и путать их дорого: обход запрещают здесь, из индекса убирают
// тегом noindex на самой странице, и первое отменяет второе — краулер,
// которого не пустили, тега не прочтёт.
//
// Поэтому полос (/works/N/pages/M) в списке нет, хотя в индексе им не место.
// Полоса — то, чем цитирует читатель, и именно на неё приходит внешняя
// ссылка; её задача — донести до краулера noindex с canonical на главу и
// теги превью для мессенджера, а закрытая в robots полоса не показывает
// этого никому. Гугл на запрещённый обходом, но слинкованный адрес заводит
// запись «проиндексировано, несмотря на блокировку robots.txt»: голый URL без
// заголовка и без склейки с главой — ровно то, чего noindex должен был не
// допустить. Краулеры Телеграма и ВК robots соблюдают, так что цитата уходила
// в чат ещё и без карточки. Разборы (/documents) сняты с запрета по той же
// логике.
//
// Остаются редакторские маршруты, которым в выдаче делать нечего, и выдача
// поиска (/search): пространство её адресов не ограничено ничем, каждый стоит
// запроса к базе по всему корпусу, а показывает она то, что и так лежит на
// страницах тома и главы.
//
// Настоящая защита от чрезмерного обхода — не Crawl-delay, который Гугл
// игнорирует, а 503 с Retry-After из handler.go.
var closedPaths = []string{
	"/login",
	"/admin/",
	"/works/new",
	// Форма создания разбора: в выдаче ей делать нечего, а правку уже
	// закрывает "/*/edit". Сам /documents остаётся открытым — витрина и
	// страницы разборов обходятся и индексируются.
	"/documents/new",
	"/*/edit",
	"/search",
}

func (h *Handler) Robots(w http.ResponseWriter, r *http.Request) {
	var out strings.Builder
	out.WriteString("User-agent: *\n")
	for _, p := range closedPaths {
		out.WriteString("Disallow: " + p + "\n")
	}
	out.WriteString("Allow: /\n\n")
	for _, bot := range trainingBots {
		out.WriteString("User-agent: " + bot + "\n")
	}
	out.WriteString("Disallow: /\n\n")
	out.WriteString("Sitemap: " + h.src.abs("/sitemap.xml") + "\n")

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	if _, err := w.Write([]byte(out.String())); err != nil {
		return
	}
}

// trainingBots — роботы, собирающие тексты в обучающие наборы моделей.
// Закрыты целиком: текст, ушедший в набор, оттуда не снять, а читальня
// снимает тома по первому письму (scripts/takedown.sh). Сборщиков, которые
// открывают страницу по просьбе читателя в чате (ChatGPT-User, Claude-User,
// Perplexity-User), здесь нет намеренно — их пускает группа «*».
// Google-Extended и Applebot-Extended — метки только для robots.txt: обычный
// поиск Гугла и Apple они не трогают.
var trainingBots = []string{
	"GPTBot",
	"ClaudeBot",
	"CCBot",
	"Google-Extended",
	"Applebot-Extended",
	"meta-externalagent",
	"Bytespider",
	"Amazonbot",
}
