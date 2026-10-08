package api

import (
	"context"
	"errors"

	"proofreader/internal/models"
)

// reanchorPage re-anchors every fragment touching a page after its body
// changed. Called from the page write paths — a page edit that silently left
// cuts pointing at old offsets would show the reader the wrong passage.
//
// Одна неудачная запись не должна останавливать переякоривание остальных
// вырезок страницы: ошибки собираются через errors.Join и возвращаются все
// разом уже после того, как цикл прошёл каждый фрагмент до конца.
func reanchorPage(ctx context.Context, store FragmentStore, pageID int64, text string) error {
	fragments, err := store.ByPage(ctx, pageID)
	if err != nil {
		return err
	}

	hash := pageHash(text)
	var errs []error
	for _, f := range fragments {
		if (f.StartPageID != pageID || f.StartHash == hash) &&
			(f.EndPageID != pageID || f.EndHash == hash) {
			continue
		}

		start, end, ok := reanchor(f.Anchor(), pageID, text)
		if !ok {
			// Смещения не затираем: человеку надо видеть, где было.
			if f.Status != models.FragmentStatusStale {
				if err := store.SetStatus(ctx, f.ID, models.FragmentStatusStale); err != nil {
					errs = append(errs, err)
				}
			}
			continue
		}

		startHash, endHash := f.StartHash, f.EndHash
		if f.StartPageID == pageID {
			startHash = hash
		}
		if f.EndPageID == pageID {
			endHash = hash
		}
		if err := store.UpdateAnchor(ctx, f.ID, start, end, startHash, endHash); err != nil {
			errs = append(errs, err)
			continue
		}
		// Отвязавшаяся вырезка, снова нашедшаяся, возвращается в машинные:
		// подтверждение человека относилось к прежнему тексту.
		if f.Status == models.FragmentStatusStale {
			if err := store.SetStatus(ctx, f.ID, models.FragmentStatusMachine); err != nil {
				errs = append(errs, err)
			}
		}
	}

	return errors.Join(errs...)
}
