package api

import (
	"context"

	"proofreader/internal/models"
)

// audioView — что из озвучки тома видит зритель. Дорожка, чей диапазон
// пересекает закрытую полосу, и запись человека в закрытой главе не-сотруднику
// не существуют: ни в списке, ни в плейлисте, ни по адресу. Зритель берётся из
// контекста, как у ViewPages. Нулевое значение (нет хранилища) ничего не прячет.
type audioView struct {
	closed ClosedSpansStore
}

func (v audioView) active(ctx context.Context) bool {
	return v.closed != nil && !staffContext(ctx)
}

func (v audioView) tracks(ctx context.Context, workID int64, in []models.AudioTrack) ([]models.AudioTrack, error) {
	if !v.active(ctx) {
		return in, nil
	}
	spans, err := v.closed.ClosedSpans(ctx, workID)
	if err != nil || len(spans) == 0 {
		return in, err
	}
	out := make([]models.AudioTrack, 0, len(in))
	for _, t := range in {
		if !spansOverlap(spans, t.StartPage, t.EndPage) {
			out = append(out, t)
		}
	}
	return out, nil
}

func (v audioView) recordings(ctx context.Context, workID int64, in []models.AudioRecording) ([]models.AudioRecording, error) {
	if !v.active(ctx) {
		return in, nil
	}
	spans, err := v.closed.ClosedSpans(ctx, workID)
	if err != nil || len(spans) == 0 {
		return in, err
	}
	// Запись открытой родительской главы озвучивает и текст закрытой
	// подглавы, поэтому прячется по пересечению диапазона, как дорожка.
	out := make([]models.AudioRecording, 0, len(in))
	for _, x := range in {
		if !spansOverlap(spans, x.ChapterStartPage, x.ChapterEndPage) {
			out = append(out, x)
		}
	}
	return out, nil
}

// fileHidden — закрыт ли для зрителя файл звука: дорожка по диапазону полос,
// запись по главе.
func (v audioView) fileHidden(ctx context.Context, f audioFile) (bool, error) {
	if !v.active(ctx) {
		return false, nil
	}
	spans, err := v.closed.ClosedSpans(ctx, f.workID)
	return spansOverlap(spans, f.start, f.end), err
}
