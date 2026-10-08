package api

import (
	"context"
	"errors"
	"strings"
	"testing"

	"proofreader/internal/mcp"
	"proofreader/internal/models"
	"proofreader/pkg/book"
)

// Кусок из середины главы: заголовок главы остаётся перед первой полосой,
// печатные номера — со смещением тома, полос ровно столько, сколько просили.
func TestPageRangeKeepsChapterHeadingAndPrintedNumbers(t *testing.T) {
	b, err := downloadFixture().PageRange(context.Background(), 1, 3, 4)
	if err != nil {
		t.Fatal(err)
	}
	if got := b.PageCount(); got != 2 {
		t.Errorf("полос %d, ожидалось 2", got)
	}
	if b.Meta.PageFrom != 103 || b.Meta.PageTo != 104 {
		t.Errorf("печатные границы %d—%d, ожидалось 103—104", b.Meta.PageFrom, b.Meta.PageTo)
	}
	text, starts := book.MarkdownWithPageStarts(b)
	body := text[starts[0].Offset:]
	if !strings.HasPrefix(body, "## Глава\n") {
		t.Errorf("кусок не начинается заголовком своей главы:\n%s", body)
	}
	if !strings.Contains(body, "[103]") || !strings.Contains(body, "Середина.") {
		t.Errorf("нет полосы 103:\n%s", body)
	}
	if strings.Contains(body, "Начало главы.") {
		t.Error("в кусок попала полоса вне диапазона")
	}
}

// Review Focus 2 на уровне источника: за концом тома — сколько есть.
func TestPageRangePastTheEndReturnsWhatExists(t *testing.T) {
	b, err := downloadFixture().PageRange(context.Background(), 1, 4, 13)
	if err != nil {
		t.Fatal(err)
	}
	if b.Meta.PageFrom != 104 || b.Meta.PageTo != 105 {
		t.Errorf("границы %d—%d, ожидалось 104—105", b.Meta.PageFrom, b.Meta.PageTo)
	}
}

func TestPageRangeOutsideVolumeIsNotFound(t *testing.T) {
	_, err := NewMCPSource(downloadFixture()).PageRange(context.Background(), 1, 50, 55)
	if !errors.Is(err, mcp.ErrNotFound) {
		t.Fatalf("%v, ожидался mcp.ErrNotFound", err)
	}
}

// Кусок из начала тома, за которым идут ещё главы: секций ровно столько,
// сколько глав задевает диапазон. Главы после конца куска не должны давать
// пустых «Без заглавия» — так полоса 5 тома 11 Чернышевского приходила в MCP
// с тремя лишними заголовками, по одному на каждую главу тома дальше по тексту.
func TestPageRangeIgnoresChaptersAfterTheRange(t *testing.T) {
	src := downloadFixture()
	src.chapters.(*fakeChapterStore).listHierarchicalFn = func(context.Context, int64) ([]*models.Chapter, error) {
		return []*models.Chapter{
			{ID: 10, WorkID: 1, Title: "Первая", StartPage: 2, EndPage: 3},
			{ID: 11, WorkID: 1, Title: "Вторая", StartPage: 4, EndPage: 4},
			{ID: 12, WorkID: 1, Title: "Третья", StartPage: 5, EndPage: 5},
		}, nil
	}
	b, err := src.PageRange(context.Background(), 1, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, s := range b.Sections {
		titles = append(titles, s.Title)
	}
	if len(titles) != 1 || titles[0] != "Первая" {
		t.Errorf("секции %q, ожидалась одна «Первая»", titles)
	}
	if text, _ := book.MarkdownWithPageStarts(b); strings.Contains(text, untitledSectionTitle) {
		t.Errorf("в куске пустая секция-заглушка:\n%s", text)
	}
}
