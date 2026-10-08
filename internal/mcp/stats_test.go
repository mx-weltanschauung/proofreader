package mcp

import (
	"context"
	"sync"
	"testing"

	"proofreader/internal/limit"
	"proofreader/internal/stats"
)

type captureStats struct {
	mu sync.Mutex
	ev []stats.Event
}

func (c *captureStats) Record(e stats.Event) { c.mu.Lock(); c.ev = append(c.ev, e); c.mu.Unlock() }

func TestToolRecordsCallAndSearchQuery(t *testing.T) {
	cs := &captureStats{}
	d := Deps{BaseURL: base, Search: &fakeSearch{result: leninResult()}, SearchSlots: limit.New(2), Stats: cs}
	s := newService(d, testGates())
	h := tool(s, "search", s.search)
	if _, _, err := h(context.Background(), nil, SearchInput{Query: "Прибавочная"}); err != nil {
		t.Fatal(err)
	}
	var call, query *stats.Event
	for i := range cs.ev {
		switch cs.ev[i].Channel {
		case stats.ChannelMCP:
			call = &cs.ev[i]
		case stats.ChannelSearch:
			query = &cs.ev[i]
		}
	}
	if call == nil || call.Kind != "search" || call.SlugKey != "ok" || call.Agent != "mcp" || call.IP != "" {
		t.Fatalf("вызов: %+v", call)
	}
	if query == nil || query.Query != "прибавочная" || query.Agent != "mcp" || query.Hits == nil {
		t.Fatalf("запрос: %+v", query)
	}

	// Исход «занято» отличается от «ok» и от «error»: по нему видно отказы ворот.
	busy := tool(s, "fetch", func(context.Context, SearchInput) (*SearchOutput, error) { return nil, errBusy })
	_, _, _ = busy(context.Background(), nil, SearchInput{})
	last := cs.ev[len(cs.ev)-1]
	if last.Channel != stats.ChannelMCP || last.Kind != "fetch" || last.SlugKey != "busy" {
		t.Fatalf("занятость: %+v", last)
	}
}
