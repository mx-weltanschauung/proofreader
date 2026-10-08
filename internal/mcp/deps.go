// Package mcp — MCP-сервер читальни: вопросы нейросети по всей читальне.
// Спека — docs/superpowers/specs/2026-09-29-corpus-mcp-design.md.
package mcp

import (
	"errors"
	"fmt"
	"log"
	"proofreader/internal/site"
	"strings"
	"time"

	"context"

	"proofreader/internal/limit"
	"proofreader/internal/models"
	"proofreader/internal/repository"
	"proofreader/internal/seo"
	"proofreader/internal/stats"
	"proofreader/pkg/book"
)

// ErrNotFound — того, что просили, в читальне нет (или том снят). Модель
// получает на него «Этого в читальне нет»: снятое и несуществующее не
// различаются, как и в /seo.
var ErrNotFound = errors.New("нет в читальне")

// Интерфейсы — у потребителя, как в internal/seo: пакет собирается и
// проверяется без базы. Репозитории и seo.Handler удовлетворяют им как есть.

type SearchStore interface {
	Search(ctx context.Context, q models.SearchQuery) (*models.SearchResult, error)
	SearchPages(ctx context.Context, q models.SearchQuery, workID int64, chapterIDs []int64, limit, offset int) (*models.SearchPagesResult, error)
}

// TextSource — *seo.Handler: текст глав, частей, томов и llms.txt тем же
// путём, что у /seo, с его кэшем, который сбрасывает снятие тома.
type TextSource interface {
	Text(ctx context.Context, path string, acquire seo.AcquireFunc) (*seo.TextResult, error)
}

type WorkSource interface {
	GetByID(ctx context.Context, id int64) (*models.Work, error)
}

// ChapterSource — самая узкая глава, накрывающая полосу, или nil.
type ChapterSource interface {
	FindByPage(ctx context.Context, workID int64, pageNumber int) (*models.Chapter, error)
}

// LibrarySource — api.MCPSource.
type LibrarySource interface {
	PageRange(ctx context.Context, workID int64, from, to int) (*book.Book, error)
}

// EventRecorder — *stats.Recorder.
type EventRecorder interface {
	Record(stats.Event)
}

// Deps — всё, что нужно серверу. SearchSlots — ограничитель поиска сайта
// (api.SearchHandler.Slots): MCP встаёт в те же слоты.
type Deps struct {
	BaseURL     string
	Search      SearchStore
	SearchSlots limit.Slots
	Text        TextSource
	Works       WorkSource
	Chapters    ChapterSource
	Library     LibrarySource
	// Stats — посещаемость: вызовы инструментов и запросы поиска. nil — не пишем.
	Stats EventRecorder
}

// gates — ограничители MCP поверх общих (спека, «Ограничители»): total — все
// вызовы разом; search и heavy — ворота ёмкостью 1 перед общими слотами поиска
// и рендера глав, чтобы второй общий слот всегда оставался сайту.
type gates struct {
	total, search, heavy            limit.Slots
	totalWait, gateWait, sharedWait time.Duration
}

func defaultGates() gates {
	return gates{
		total: limit.New(4), search: limit.New(1), heavy: limit.New(1),
		totalWait: 20 * time.Second, gateWait: 20 * time.Second, sharedWait: 10 * time.Second,
	}
}

type service struct {
	d    Deps
	base string
	g    gates
}

func newService(d Deps, g gates) *service {
	return &service{d: d, base: strings.TrimSuffix(d.BaseURL, "/"), g: g}
}

// errBusy — ворота или общий слот не дождались.
var errBusy = errors.New("занято")

// busyRetrySeconds — что сказать модели при отказе: слот освобождается за
// секунды.
const busyRetrySeconds = 10

const (
	msgEmpty    = "Пустой запрос"
	msgNotFound = "Этого в читальне нет"
	msgTimeout  = "Поиск занял слишком долго: уточните запрос или сузьте область (works, editions)"
	// У search_volume нет параметров works и editions — сужать можно главами.
	msgTimeoutVolume = "Поиск занял слишком долго: уточните запрос или сузьте поиск главами (chapters)"
	msgInternal      = "Не удалось выполнить запрос"
)

// userError — ошибка для модели: одна фраза, что делать. Фразы, уже
// написанные для модели (ErrBadAddress, «не больше 10 полос», пустой и
// короткий запрос), проходят как есть: их помечает errUser.
func userError(err error) error {
	var u errUser
	switch {
	case errors.As(err, &u):
		return err
	case errors.Is(err, ErrBadAddress):
		return err
	case errors.Is(err, errBusy), errors.Is(err, seo.ErrBusy):
		return fmt.Errorf(site.Name()+" сейчас занята, повторите через %d с", busyRetrySeconds)
	case errors.Is(err, repository.ErrSearchTimeout):
		return errors.New(msgTimeout)
	case errors.Is(err, ErrNotFound), errors.Is(err, seo.ErrNotFound), seo.IsNotFound(err):
		return errors.New(msgNotFound)
	default:
		log.Printf("mcp: %v", err)
		return errors.New(msgInternal)
	}
}

// Сторож при сборке: *seo.Handler обязан удовлетворять TextSource, иначе main.go не соберётся.
var _ TextSource = (*seo.Handler)(nil)
