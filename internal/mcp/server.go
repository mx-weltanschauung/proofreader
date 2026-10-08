package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"proofreader/internal/site"
	"runtime/debug"
	"time"

	"proofreader/internal/limit"
	"proofreader/internal/seo"
	"proofreader/internal/stats"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// instructions — что сервер говорит модели при подключении. Первые 512 знаков
// самодостаточны (совет OpenAI); указаний «как поступать» нет (ревью Anthropic
// считает их инъекцией).
// Начало — имя и описание экземпляра (SITE_NAME/SITE_DESCRIPTION), хвост
// про устройство текстов общий для любой читальни.
func serverInstructions() string {
	return site.Name() + " — " + site.Description() + " " +
		"Состав изданий и томов — fetch(\"/\"). В текстах [123] — номер страницы печатного издания; " +
		"сноски помечены [^123-1], их текст на той же странице. У каждого результата есть адрес " +
		"(url) места в читальне."
}

func boolPtr(b bool) *bool { return &b }

// NewHandler — MCP-сервер читальни на /mcp.
func NewHandler(d Deps) http.Handler { return newHandler(d, defaultGates()) }

// NewHandlerWithGates — то же, что NewHandler, плюс ворота для метрик занятости.
func NewHandlerWithGates(d Deps) (http.Handler, map[string]limit.Slots) {
	g := defaultGates()
	return newHandler(d, g), map[string]limit.Slots{"mcp_all": g.total, "mcp_search": g.search, "mcp_render": g.heavy}
}

func newHandler(d Deps, g gates) http.Handler {
	s := newService(d, g)
	server := sdk.NewServer(&sdk.Implementation{Name: "chitalnya", Title: site.Name(), Version: "1"},
		&sdk.ServerOptions{Instructions: serverInstructions()})
	ro := func(title string) *sdk.ToolAnnotations {
		return &sdk.ToolAnnotations{Title: title, ReadOnlyHint: true, DestructiveHint: boolPtr(false),
			IdempotentHint: true, OpenWorldHint: boolPtr(false)}
	}

	sdk.AddTool(server, &sdk.Tool{
		Name: "search", Title: "Поиск по читальне", Annotations: ro("Поиск по читальне"),
		Description: "Полнотекстовый поиск по всем томам читальни с учётом русской морфологии. " +
			"Отвечает сводкой по томам с числом совпадений, полосами с отрывками, главами и понятиями " +
			"предметного указателя, совпавшими названием. Область сужается номерами изданий и томов.",
	}, tool(s, "search", s.search))
	sdk.AddTool(server, &sdk.Tool{
		Name: "search_volume", Title: "Поиск внутри тома", Annotations: ro("Поиск внутри тома"),
		Description: "Все совпавшие полосы одного тома в порядке чтения, по 20 за раз, с отрывками, " +
			"и главы тома с числом совпадений.",
	}, tool(s, "search_volume", s.searchVolume))
	sdk.AddTool(server, &sdk.Tool{
		Name: "fetch", Title: "Открыть текст", Annotations: ro("Открыть текст"),
		Description: "Текст по адресу читальни: каталог изданий и томов («/»), оглавление тома, глава или её " +
			"часть, до 10 полос подряд, понятие предметного указателя (статья, подрубрики и текст всех мест в " +
			"томах) и список всех понятий («/concepts»). Длинная глава и большое понятие отдаются частями, " +
			"адрес следующей части — в тексте.",
		Meta: sdk.Meta{"anthropic/maxResultSizeChars": 100000},
	}, tool(s, "fetch", s.fetch))

	h := sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return server },
		&sdk.StreamableHTTPOptions{
			Stateless:                    true,
			JSONResponse:                 true,
			MaxRequestBodyBytes:          64 << 10,
			PropagateRequestCancellation: true,
		})
	// Запрос со страницы чужого сайта (DNS rebinding, CSRF) отклоняется; запросы
	// серверов Anthropic и OpenAI идут без Origin и проходят.
	protected := http.NewCrossOriginProtection().Handler(h)
	help := s.base + "/help#mcp"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			// 405, как велит спецификация 2026-07-28, но с адресом справки:
			// сюда придёт и человек, открывший адрес в браузере.
			w.Header().Set("Allow", "POST")
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusMethodNotAllowed)
			fmt.Fprintf(w, "Это адрес для подключения нейросети к читальне: %s\n", help)
			return
		}
		// Вызов ждёт ворот и слотов до ~55 с (defaultGates), а WriteTimeout
		// сервера — 15 с: без раздвижки ожидавший дольше терял бы соединение
		// (502 у nginx) вместо фразы «Читальня сейчас занята» — именно под
		// той нагрузкой, ради которой ворота и стоят. Тем же приёмом, что в
		// download_handler; Unwrap у Logging и Gzip доводит контроллер до
		// настоящего соединения.
		if err := http.NewResponseController(w).SetWriteDeadline(time.Now().Add(mcpWriteDeadline)); err != nil {
			log.Printf("mcp: не удалось раздвинуть дедлайн записи: %v", err)
		}
		protected.ServeHTTP(w, r)
	})
}

// mcpWriteDeadline — дедлайн записи ответа /mcp: с запасом над самым долгим
// ожиданием вызова, но меньше proxy_read_timeout 120s у nginx для /mcp.
const mcpWriteDeadline = 100 * time.Second

// errInternal — паника внутри инструмента.
var errInternal = errors.New("паника")

// tool — обёртка инструмента: общий потолок вызовов, перехват паники, журнал и
// перевод ошибки в фразу для модели.
func tool[In any, Out any](s *service, name string, f func(context.Context, In) (Out, error)) sdk.ToolHandlerFor[In, Out] {
	return func(ctx context.Context, _ *sdk.CallToolRequest, in In) (*sdk.CallToolResult, Out, error) {
		start := time.Now()
		var out Out
		var err error
		if rel, ok := s.g.total.Acquire(ctx, s.g.totalWait); !ok {
			// Ушедший клиент — не отказ по занятости: по строкам журнала
			// после выкатки считаются именно отказы.
			if ctx.Err() != nil {
				err = ctx.Err()
			} else {
				err = errBusy
			}
		} else {
			func() {
				defer rel()
				// Обработчик инструмента идёт не в горутине net/http, и
				// неперехваченная паника уронила бы весь процесс.
				defer func() {
					if p := recover(); p != nil {
						log.Printf("mcp %s: паника: %v\n%s", name, p, debug.Stack())
						err = errInternal
					}
				}()
				out, err = f(ctx, in)
			}()
		}
		logCall(name, in, out, err, time.Since(start))
		s.record(stats.Row{Channel: stats.ChannelMCP, Kind: name, SlugKey: outcomeOf(err)})
		if err != nil {
			var zero Out
			return nil, zero, userError(err)
		}
		return nil, out, nil
	}
}

// outcomeOf — исход вызова для статистики: занятость отдельно от поломки,
// по ней видно, как часто ворота MCP отказывают.
func outcomeOf(err error) string {
	switch {
	case err == nil:
		return "ok"
	case errors.Is(err, errBusy), errors.Is(err, seo.ErrBusy):
		return "busy"
	default:
		return "error"
	}
}

// record пишет событие MCP. Адреса нет: вызов идёт из подсети Anthropic или
// OpenAI, отметка по нему считала бы одного «посетителя» на всех.
func (s *service) record(row stats.Row) {
	if s.d.Stats == nil {
		return
	}
	row.TS = time.Now()
	row.Agent = "mcp"
	s.d.Stats.Record(stats.Event{Row: row})
}

// logCall — строка журнала на вызов: по ним после выкатки меряется, сколько
// поисков агент делает на вопрос (спека, «После выкатки»). Адрес не пишется:
// он Anthropic или OpenAI.
func logCall(name string, in, out any, err error, d time.Duration) {
	args, _ := json.Marshal(in)
	size := 0
	if err == nil {
		raw, _ := json.Marshal(out)
		size = len(raw)
	}
	outcome := "ok"
	if err != nil {
		outcome = err.Error()
	}
	// Аргументы последними: сырой JSON может содержать пробелы.
	log.Printf("mcp tool=%s dur=%dms size=%dB outcome=%q args=%s", name, d.Milliseconds(), size, outcome, args)
}
