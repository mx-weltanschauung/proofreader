package export

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"time"

	"proofreader/internal/models"
)

// Source is where the export gets its data. The repositories satisfy it
// through a thin adapter in internal/api; tests use an in-memory fake.
type Source interface {
	ListWorks(ctx context.Context) ([]*models.Work, error)
	ListChapters(ctx context.Context, workID int64) ([]*models.Chapter, error)
	ListPages(ctx context.Context, workID int64) ([]*models.Page, error)
}

// fixedModTime stamps every entry. Without it each run embeds the current time
// and two exports of the same data differ byte-wise for no reason.
var fixedModTime = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

// WriteZip streams the whole export into w.
//
// On a source error the archive is deliberately left unclosed: the central
// directory is never written, so a consumer sees a broken archive rather than a
// silently truncated one. By that point an HTTP response is already 200 and no
// status code can carry the failure.
func WriteZip(ctx context.Context, w io.Writer, src Source) error {
	zw := zip.NewWriter(w)

	works, err := src.ListWorks(ctx)
	if err != nil {
		return fmt.Errorf("list works: %w", err)
	}
	works = sortWorks(works)

	if err := writeEntry(zw, worksIndexPath(), renderWorksIndex(works)); err != nil {
		return err
	}

	for _, work := range works {
		if err := writeWork(ctx, zw, src, work); err != nil {
			return err
		}
	}

	return zw.Close()
}

func writeWork(ctx context.Context, zw *zip.Writer, src Source, work *models.Work) error {
	if err := writeEntry(zw, workFilePath(work.ID), renderWorkFile(work)); err != nil {
		return err
	}

	chapters, err := src.ListChapters(ctx, work.ID)
	if err != nil {
		return fmt.Errorf("list chapters of work %d: %w", work.ID, err)
	}
	if err := writeEntry(zw, chaptersFilePath(work.ID), renderChaptersFile(chapters)); err != nil {
		return err
	}

	pages, err := src.ListPages(ctx, work.ID)
	if err != nil {
		return fmt.Errorf("list pages of work %d: %w", work.ID, err)
	}
	for _, page := range sortPages(pages) {
		if err := writeEntry(zw, pageFilePath(work.ID, page.PageNumber), renderPageFile(page)); err != nil {
			return err
		}
	}

	return nil
}

func writeEntry(zw *zip.Writer, name, content string) error {
	f, err := zw.CreateHeader(&zip.FileHeader{
		Name:     name,
		Method:   zip.Deflate,
		Modified: fixedModTime,
	})
	if err != nil {
		return fmt.Errorf("create zip entry %s: %w", name, err)
	}
	if _, err := io.WriteString(f, content); err != nil {
		return fmt.Errorf("write zip entry %s: %w", name, err)
	}
	return nil
}
