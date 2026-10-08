// mcpsmoke — сквозная проверка MCP-сервера читальни настоящим клиентом
// go-sdk: список инструментов и сценарий из спеки
// (docs/superpowers/specs/2026-09-29-corpus-mcp-design.md, «Смоук до выкатки»).
//
//	go run ./cmd/mcpsmoke                                    # http://localhost:8093/mcp
//	go run ./cmd/mcpsmoke -url https://lib.example.org/mcp -edition <id Ленина на боевом> -chapter ''
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	endpoint := flag.String("url", "http://localhost:8093/mcp", "адрес /mcp")
	edition := flag.Int64("edition", 4, "издание для первого поиска (локально Ленин — 4)")
	query := flag.String("q", "кооперация", "запрос")
	chapter := flag.String("chapter", "/works/47/chapters/2066/part-3", "длинная глава для fetch части; пусто — пропустить")
	flag.Parse()

	ctx := context.Background()
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "mcpsmoke", Version: "1"}, nil).
		Connect(ctx, &sdk.StreamableClientTransport{Endpoint: *endpoint}, nil)
	if err != nil {
		log.Fatalf("подключение: %v", err)
	}
	defer cs.Close()

	tools, err := cs.ListTools(ctx, nil)
	if err != nil {
		log.Fatalf("tools/list: %v", err)
	}
	for _, t := range tools.Tools {
		fmt.Printf("инструмент %s — %s\n", t.Name, t.Title)
	}

	failed := false
	// call — вызов инструмента; wantError — ответ-ошибка здесь ожидаем
	// (проверка отказа), и провалом прогона считается как раз её отсутствие.
	call := func(name string, args map[string]any, wantError bool) map[string]any {
		start := time.Now()
		res, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			fmt.Printf("FAIL %s %v: %v\n", name, args, err)
			failed = true
			return nil
		}
		text := res.Content[0].(*sdk.TextContent).Text
		status := "ok"
		switch {
		case res.IsError && wantError:
			status = "ожидаемый отказ: " + text
		case res.IsError:
			status, failed = "ОШИБКА: "+text, true
		case wantError:
			status, failed = "ОШИБКА: ждали отказа, пришёл ответ", true
		}
		fmt.Printf("%-13s %v  %v  %d знаков  %s\n", name, args, time.Since(start).Round(time.Millisecond), len([]rune(text)), status)
		var m map[string]any
		_ = json.Unmarshal([]byte(text), &m)
		return m
	}

	first := func(m map[string]any, kind string) string {
		results, _ := m["results"].([]any)
		for _, r := range results {
			h, _ := r.(map[string]any)
			if h["kind"] == kind {
				return h["id"].(string)
			}
		}
		return ""
	}

	found := call("search", map[string]any{"query": *query, "editions": []int64{*edition}}, false)
	page := first(found, "page")
	if page == "" {
		log.Fatal("поиск не нашёл ни одной полосы")
	}
	var workID int64
	fmt.Sscanf(page, "/works/%d", &workID)
	inVolume := call("search_volume", map[string]any{"query": *query, "work": workID}, false)
	if p := first(inVolume, "page"); p != "" {
		page = p
	}
	call("fetch", map[string]any{"id": page}, false)
	if ch := first(found, "chapter"); ch != "" {
		call("fetch", map[string]any{"id": ch}, false)
	}
	if *chapter != "" {
		call("fetch", map[string]any{"id": *chapter}, false)
	}
	call("fetch", map[string]any{"id": "/"}, false)
	if c := first(found, "concept"); c != "" {
		call("fetch", map[string]any{"id": c}, false)
	}
	call("fetch", map[string]any{"id": "/editions/1"}, true)
	if failed {
		os.Exit(1)
	}
}
