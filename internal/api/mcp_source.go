package api

import (
	"context"
	"errors"
	"fmt"

	"proofreader/internal/mcp"
	"proofreader/pkg/book"
)

// MCPSource — переводчик для MCP-сервера читальни (internal/mcp): полосы по
// диапазону книгой. Живёт здесь по той же причине, что SEOBookSource: сборка
// книги принадлежит этому пакету, и копия в internal/mcp разошлась бы с ней
// молча.
type MCPSource struct {
	books *DownloadSource
}

func NewMCPSource(books *DownloadSource) *MCPSource {
	return &MCPSource{books: books}
}

// PageRange — DownloadSource.PageRange с отказом на языке internal/mcp.
func (s *MCPSource) PageRange(ctx context.Context, workID int64, from, to int) (*book.Book, error) {
	b, err := s.books.PageRange(ctx, workID, from, to)
	if errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("%w: %v", mcp.ErrNotFound, err)
	}
	return b, err
}
