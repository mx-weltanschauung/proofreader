// Package stats — посещаемость читальни: события, их разбор, буфер записи.
// Спека — docs/superpowers/specs/2026-09-29-traffic-and-health-metrics-design.md.
package stats

import (
	"strings"
	"time"
)

// MaxQueryRunes — потолок хранимого текста запроса.
const MaxQueryRunes = 200

// CleanQuery — запрос для статистики: без краевых пробелов, в нижнем
// регистре, не длиннее MaxQueryRunes рун. Нормализацию поиска
// (models.NormalizeSearchText) вызывает обработчик до этого.
func CleanQuery(q string) string {
	r := []rune(strings.ToLower(strings.TrimSpace(q)))
	if len(r) > MaxQueryRunes {
		r = r[:MaxQueryRunes]
	}
	return string(r)
}

// Каналы: откуда пришло событие.
const (
	ChannelSPA      = "spa"      // маячок из браузера
	ChannelMD       = "md"       // .md глав, томов, понятий и витрины указателя, llms.txt
	ChannelMCP      = "mcp"      // вызов инструмента MCP
	ChannelDownload = "download" // скачивание тома, главы, подборки
	ChannelSEO      = "seo"      // HTML краулеру
	ChannelSearch   = "search"   // поисковый запрос с числом найденного
	ChannelOPDS     = "opds"     // лента каталога OPDS (поиск каталога — channel search, agent opds)
)

// Классы ширины окна читателя — корзины, а не число: точная ширина вместе с
// прочим сужала бы круг до человека, а карте нужна только доля телефонов.
// Границы считает клиент (frontend/src/services/hit.ts, deviceClass).
const (
	DeviceNarrow = "narrow" // < 600 CSS px — телефон
	DeviceMedium = "medium" // 600—1023 — планшет, узкое окно
	DeviceWide   = "wide"   // ≥ 1024 — компьютер
)

// DeviceClass — класс из маячка, если он из закрытого списка, иначе "".
// Свёртка хранится вечно, мусору в ней не место.
func DeviceClass(s string) string {
	switch s {
	case DeviceNarrow, DeviceMedium, DeviceWide:
		return s
	}
	return ""
}

// msk — московское время. FixedZone, а не LoadLocation: в alpine-образе нет
// tzdata, а перехода на летнее время в Москве нет с 2014 года.
var msk = time.FixedZone("MSK", 3*60*60)

// Day — московские сутки момента t, как полночь в UTC (так их пишет pgx в date).
func Day(t time.Time) time.Time {
	y, m, d := t.In(msk).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// Row — строка page_views. Нулевые значения пишутся как есть: в схеме у
// колонок ключа DEFAULT (0 или пустая строка) и NOT NULL.
type Row struct {
	Day      time.Time
	TS       time.Time
	Channel  string
	Kind     string
	WorkID   int64
	EntityID int64
	SlugKey  string
	Agent    string // семейство бота, "mcp" или "" для человека
	Visitor  string // дневная отметка или ""
	RefHost  string
	Query    string
	Hits     *int
	Device   string // класс ширины окна (канал spa) или ""
}

// Event — то, что кладут в Recorder. IP и UA нужны только для отметки и в
// базу не попадают никогда.
type Event struct {
	Row
	IP string
	UA string
}
