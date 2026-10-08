package api

import (
	"context"
	"errors"

	"proofreader/internal/models"
)

// reanchorDocumentCuts переякоривает вклейки, держащиеся за правленую полосу.
//
// Зеркало reanchorPage (internal/api/fragment_anchor.go) для второй машины,
// висящей на той же полосе. Общий у них только слой пересчёта (reanchor):
// словари состояний разные и не смешиваются — «machine» у вырезки значит
// «нарезала машина, человек не смотрел», а у вклейки такого понятия нет
// вовсе, подтверждать в ней нечего.
//
// Одна неудачная запись не должна останавливать переякоривание остальных
// вклеек страницы: ошибки собираются через errors.Join и возвращаются все
// разом уже после того, как цикл прошёл каждую вклейку до конца.
func reanchorDocumentCuts(ctx context.Context, store DocumentCutStore, pageID int64, text string) error {
	cuts, err := store.ByPage(ctx, pageID)
	if err != nil {
		return err
	}

	hash := pageHash(text)
	var errs []error
	for _, c := range cuts {
		if (c.StartPageID != pageID || c.StartHash == hash) &&
			(c.EndPageID != pageID || c.EndHash == hash) {
			continue
		}

		start, end, ok := reanchor(c.Anchor, pageID, text)
		if !ok {
			// Смещения не затираем: автору надо видеть, где было.
			if c.Status != models.CutStatusStale {
				if err := store.SetStatus(ctx, c.ID, models.CutStatusStale); err != nil {
					errs = append(errs, err)
				}
			}
			continue
		}

		startHash, endHash := c.StartHash, c.EndHash
		if c.StartPageID == pageID {
			startHash = hash
		}
		if c.EndPageID == pageID {
			endHash = hash
		}
		if err := store.UpdateAnchor(ctx, c.ID, start, end, startHash, endHash); err != nil {
			errs = append(errs, err)
			continue
		}
		// Отвязавшаяся вклейка, снова нашедшаяся, возвращается в рабочие. У
		// вырезки на этом месте — "machine" (подтверждение относилось к
		// прежнему тексту), у вклейки подтверждать нечего с самого начала.
		if c.Status == models.CutStatusStale {
			if err := store.SetStatus(ctx, c.ID, models.CutStatusOK); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}
