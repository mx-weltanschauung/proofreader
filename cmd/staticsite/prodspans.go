package main

import (
	"context"
	"sort"

	"proofreader/internal/api"
	"proofreader/internal/models"
)

// prodSpans — закрытые диапазоны для обёрток зрителя (api.NewViewPages,
// api.NewViewChapters): настоящие закрытые главы локальной базы плюс полосы,
// которых нет на боевом (policy.closedPages). Так архив не опережает сайт на
// уровне полосы тем же путём, каким от не-сотрудника прячется локально
// закрытое: книга собирается без этих полос, а не вырезает их потом из уже
// отрендеренных сносок.
type prodSpans struct {
	base  api.ClosedSpansStore
	extra map[int64]map[int]bool
}

var _ api.ClosedSpansStore = (*prodSpans)(nil)

func (s *prodSpans) ClosedSpans(ctx context.Context, workID int64) ([]models.PageSpan, error) {
	spans, err := s.base.ClosedSpans(ctx, workID)
	if err != nil {
		return nil, err
	}
	extra := pageSpans(s.extra[workID])
	if len(extra) == 0 {
		return spans, nil
	}
	out := append(append([]models.PageSpan(nil), spans...), extra...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Start != out[j].Start {
			return out[i].Start < out[j].Start
		}
		return out[i].End < out[j].End
	})
	return out, nil
}

func (s *prodSpans) ClosedChapterIDs(ctx context.Context, workID int64) (map[int64]bool, error) {
	return s.base.ClosedChapterIDs(ctx, workID)
}
