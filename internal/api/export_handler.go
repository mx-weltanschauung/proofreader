package api

import (
	"context"
	"log"
	"net/http"
	"time"

	"proofreader/internal/models"
	"proofreader/pkg/export"
)

// exportPageSize is how many works are fetched per List call: the repository
// requires limit/offset, so ListWorks pages through them.
const exportPageSize = 100

// ExportHandler streams the whole corpus as a zip of markdown files.
type ExportHandler struct {
	source export.Source
}

// NewExportHandler adapts the API's stores into an export.Source.
func NewExportHandler(works WorkStore, chapters ChapterStore, pages PageStore) *ExportHandler {
	return &ExportHandler{source: &storeSource{works: works, chapters: chapters, pages: pages}}
}

// storeSource is the only place that knows the export reads through the same
// stores the rest of the API uses.
type storeSource struct {
	works    WorkStore
	chapters ChapterStore
	pages    PageStore
}

func (s *storeSource) ListWorks(ctx context.Context) ([]*models.Work, error) {
	var all []*models.Work
	for offset := 0; ; offset += exportPageSize {
		batch, err := s.works.List(ctx, exportPageSize, offset, nil, nil)
		if err != nil {
			return nil, err
		}
		all = append(all, batch...)
		if len(batch) < exportPageSize {
			return all, nil
		}
	}
}

func (s *storeSource) ListChapters(ctx context.Context, workID int64) ([]*models.Chapter, error) {
	return s.chapters.ListByWorkHierarchical(ctx, workID)
}

func (s *storeSource) ListPages(ctx context.Context, workID int64) ([]*models.Page, error) {
	return s.pages.ListByWork(ctx, workID)
}

// Export streams the export archive. Administrator-only — gated in router.go.
func (h *ExportHandler) Export(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="proofreader-export.zip"`)

	// The server's 15s WriteTimeout would cut a large export short. Pushing the
	// deadline out (rather than clearing it with a zero time.Time) still buys
	// the export effectively unlimited time while keeping a backstop: a client
	// that hangs mid-download still gets its goroutine and connection reclaimed
	// eventually instead of held open forever. Needs responseWriter.Unwrap in
	// internal/middleware/logging.go to reach the real connection.
	if err := http.NewResponseController(w).SetWriteDeadline(time.Now().Add(30 * time.Minute)); err != nil {
		log.Printf("export: could not extend write deadline: %v", err)
	}

	if err := export.WriteZip(r.Context(), w, h.source); err != nil {
		// Bytes are already on the wire, so no status can carry this. WriteZip
		// left the archive unclosed on purpose — the client sees a broken zip.
		log.Printf("export: aborted: %v", err)
	}
}
