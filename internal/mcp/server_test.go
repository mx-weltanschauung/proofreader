package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"proofreader/internal/site"
	"regexp"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"proofreader/internal/limit"
	"proofreader/internal/seo"
)

func serverDeps() Deps {
	return Deps{
		BaseURL: base, Search: &fakeSearch{result: leninResult()}, SearchSlots: limit.New(2),
		Text:  &fakeText{byPath: map[string]*seo.TextResult{"/llms.txt": {Path: "/llms.txt", Body: "# Читальня"}}},
		Works: fakeWorks{}, Chapters: fakeChapters{}, Library: &fakeLibrary{},
	}
}

func connect(t *testing.T, d Deps, g gates) (*sdk.ClientSession, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(newHandler(d, g))
	t.Cleanup(srv.Close)
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "1"}, nil).
		Connect(context.Background(), &sdk.StreamableClientTransport{Endpoint: srv.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs, srv
}

func TestToolsAreDeclaredReadOnlyWithSchemas(t *testing.T) {
	cs, _ := connect(t, serverDeps(), testGates())
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]*sdk.Tool{}
	for _, tl := range res.Tools {
		names[tl.Name] = tl
	}
	for _, n := range []string{"search", "search_volume", "fetch"} {
		tl, ok := names[n]
		if !ok {
			t.Fatalf("нет инструмента %s", n)
		}
		a := tl.Annotations
		if a == nil || !a.ReadOnlyHint || a.DestructiveHint == nil || *a.DestructiveHint || tl.Title == "" || tl.OutputSchema == nil {
			t.Errorf("%s: пометки или схема не объявлены: %+v", n, tl)
		}
	}
	if v, ok := names["fetch"].Meta["anthropic/maxResultSizeChars"]; !ok || v.(float64) != 100000 {
		t.Errorf("у fetch нет maxResultSizeChars: %v", names["fetch"].Meta)
	}
}

// Контракт OpenAI: structuredContent и тот же объект строкой в content.
func TestSearchResultIsStructuredAndDuplicatedAsJSON(t *testing.T) {
	cs, _ := connect(t, serverDeps(), testGates())
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "search", Arguments: map[string]any{"query": "кооперация"}})
	if err != nil || res.IsError {
		t.Fatalf("%v %+v", err, res)
	}
	var fromText, fromStruct map[string]any
	if err := json.Unmarshal([]byte(res.Content[0].(*sdk.TextContent).Text), &fromText); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(res.StructuredContent)
	json.Unmarshal(raw, &fromStruct)
	if !jsonEqual(fromText, fromStruct) {
		t.Errorf("content и structuredContent разошлись")
	}
	results := fromStruct["results"].([]any)
	first := results[0].(map[string]any)
	if first["id"] == "" || !strings.HasPrefix(first["url"].(string), base) || first["title"] == "" {
		t.Errorf("результат без id/url/title: %v", first)
	}
}

// Клиент ушёл, пока ждал общих ворот: в журнале это не отказ по занятости.
func TestDisconnectWhileWaitingIsNotLoggedAsBusy(t *testing.T) {
	g := testGates()
	for i := 0; i < cap(g.total); i++ {
		g.total <- struct{}{}
	}
	s := newService(serverDeps(), g)
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := tool(s, "search", s.search)(ctx, nil, SearchInput{Query: "кооперация"})
	if err == nil {
		t.Fatal("ожидалась ошибка")
	}
	got := buf.String()
	if strings.Contains(got, errBusy.Error()) || !strings.Contains(got, "context canceled") {
		t.Errorf("журнал: %q — ушедший клиент записан как занятость", got)
	}
}

// Занятые ворота при живом клиенте — по-прежнему «занято».
func TestBusyGateIsLoggedAsBusy(t *testing.T) {
	g := testGates()
	g.totalWait = 10 * time.Millisecond
	for i := 0; i < cap(g.total); i++ {
		g.total <- struct{}{}
	}
	s := newService(serverDeps(), g)
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)
	_, _, err := tool(s, "search", s.search)(context.Background(), nil, SearchInput{Query: "кооперация"})
	if err == nil || !strings.Contains(err.Error(), "занята") || !strings.Contains(buf.String(), errBusy.Error()) {
		t.Errorf("err %v, журнал %q", err, buf.String())
	}
}

// Строка журнала разбирается регуляркой: по ним после выкатки считаются вызовы.
func TestCallLogLineIsParseable(t *testing.T) {
	s := newService(serverDeps(), testGates())
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)
	tool(s, "search", s.search)(context.Background(), nil, SearchInput{Query: "кооперация и труд"})
	re := regexp.MustCompile(`mcp tool=search dur=\d+ms size=\d+B outcome="ok" args=\{"query":"кооперация и труд"\}\n$`)
	if !re.MatchString(buf.String()) || strings.Count(buf.String(), "\n") != 1 {
		t.Errorf("журнал: %q", buf.String())
	}
}

func jsonEqual(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

func TestToolErrorIsResultNotProtocolError(t *testing.T) {
	cs, _ := connect(t, serverDeps(), testGates())
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "fetch", Arguments: map[string]any{"id": "/editions/4"}})
	if err != nil {
		t.Fatalf("ошибка протокола вместо ответа: %v", err)
	}
	if !res.IsError || !strings.Contains(res.Content[0].(*sdk.TextContent).Text, "Адрес не распознан") {
		t.Errorf("%+v", res)
	}
}

// Review Focus 5: паника — ответ с ошибкой, слоты свободны, сервер жив.
func TestPanicInToolIsContained(t *testing.T) {
	d := serverDeps()
	d.Search = &fakeSearch{panics: true}
	g := testGates()
	cs, _ := connect(t, d, g)
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "search", Arguments: map[string]any{"query": "кооперация"}})
	if err != nil || !res.IsError {
		t.Fatalf("%v %+v", err, res)
	}
	if got := res.Content[0].(*sdk.TextContent).Text; got != msgInternal {
		t.Errorf("текст ошибки %q, ожидалось %q — внутренности паники модели не показываются", got, msgInternal)
	}
	if len(g.total) != 0 || len(g.search) != 0 || len(d.SearchSlots) != 0 {
		t.Fatalf("после паники занято: total %d, ворота %d, общие %d", len(g.total), len(g.search), len(d.SearchSlots))
	}
	res, err = cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "fetch", Arguments: map[string]any{"id": "/"}})
	if err != nil || res.IsError {
		t.Fatalf("сервер не пережил панику: %v %+v", err, res)
	}
}

// Сервер с WriteTimeout короче ожидания у ворот: без раздвинутого дедлайна
// соединение рвётся, и модель видит обрыв вместо фразы «занята».
func TestBusyAnswerSurvivesServerWriteTimeout(t *testing.T) {
	g := testGates()
	g.totalWait = 600 * time.Millisecond
	for i := 0; i < cap(g.total); i++ {
		g.total <- struct{}{}
	}
	srv := httptest.NewUnstartedServer(newHandler(serverDeps(), g))
	srv.Config.WriteTimeout = 300 * time.Millisecond
	srv.Start()
	t.Cleanup(srv.Close)
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "1"}, nil).
		Connect(context.Background(), &sdk.StreamableClientTransport{Endpoint: srv.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "search", Arguments: map[string]any{"query": "кооперация"}})
	if err != nil {
		t.Fatalf("обрыв соединения вместо ответа: %v", err)
	}
	if !res.IsError || !strings.Contains(res.Content[0].(*sdk.TextContent).Text, "занята") {
		t.Errorf("ожидалась фраза «занята»: %+v", res)
	}
}

func TestGetExplainsWhereToReadAbout(t *testing.T) {
	srv := httptest.NewServer(newHandler(serverDeps(), testGates()))
	defer srv.Close()
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed || !strings.Contains(string(body), base+"/help#mcp") {
		t.Errorf("GET: %d %q", resp.StatusCode, body)
	}
}

// Запрос со страницы чужого сайта отклоняется; без Origin и со своим — нет.
func TestForeignOriginIsRejected(t *testing.T) {
	srv := httptest.NewServer(newHandler(serverDeps(), testGates()))
	defer srv.Close()
	post := func(origin string) int {
		req, _ := http.NewRequest(http.MethodPost, srv.URL,
			strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25"}}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if c := post("https://evil.example"); c != http.StatusForbidden {
		t.Errorf("чужой Origin: %d, ожидался 403", c)
	}
	if c := post(""); c != http.StatusOK {
		t.Errorf("без Origin: %d, ожидался 200", c)
	}
	if c := post(srv.URL); c != http.StatusOK {
		t.Errorf("свой Origin: %d, ожидался 200", c)
	}
}

// Модель узнаёт о собрании из инструкции сервера — у чужой читальни там её
// имя и описание, а не наши.
func TestInstructionsUseSite(t *testing.T) {
	site.Set("Тестовая", "Тестовое собрание.")
	t.Cleanup(func() { site.Set("", "") })
	got := serverInstructions()
	if !strings.HasPrefix(got, "Тестовая — Тестовое собрание. ") {
		t.Errorf("инструкция: %q", got)
	}
	if strings.Contains(got, "Читальня") {
		t.Errorf("имя по умолчанию в инструкции: %q", got)
	}
}
