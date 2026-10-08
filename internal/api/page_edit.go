package api

import (
	"context"
	"fmt"
	"log"

	"proofreader/internal/models"
)

// applyPageEdit — единственный путь изменения текста полосы: снимок прежнего
// текста в версии и запись нового (одной транзакцией, PageStore.SaveEdit),
// затем переякоривание ДВУХ машин, держащихся за текст полосы: вырезок
// предметного указателя и вклеек разбора.
//
// Третья машина — звук (audio_tracks.stale) — метится внутри SaveEdit, в той
// же транзакции, что и правка.
//
// Переякоривание намеренно ВНЕ транзакции: его сбой не должен терять уже
// сохранённую правку — обе машины пересчитываются заново при следующей
// правке, потерянный текст нет.
//
// Зовут трое: PageHandler.Update (редактор правит полосу), PageHandler.RestoreVersion
// (возврат к прежней версии) и PageSuggestionHandler.Accept (принятие предложения
// читателя). Четвёртого способа менять текст полосы заводить нельзя — любой из двух
// шагов переякоривания легко забыть, и забытым он не падает: смещения останутся
// привязаны к прежнему тексту, и читатель увидит на месте вырезки или вклейки чужой
// кусок. Именно так один из них и был забыт в RestoreVersion, у которого до правки
// этой ветки стояла своя копия всех шагов (см.
// TestPageHandler_RestoreVersion_GoesThroughApplyPageEdit).
func applyPageEdit(
	ctx context.Context,
	pages PageStore,
	versions PageVersionStore,
	fragments FragmentStore,
	cuts DocumentCutStore,
	page *models.Page,
	newMarkdown string,
	newStatus models.PageStatus,
	userID int64,
	comment string,
) error {
	latest, err := versions.GetLatestVersionNumber(ctx, page.ID)
	if err != nil {
		return fmt.Errorf("latest version for page %d: %w", page.ID, err)
	}

	// В снимок едет ПРЕЖНИЙ текст полосы — история хранит то, что заменили.
	version := &models.PageVersion{
		PageID:          page.ID,
		ContentMarkdown: page.ContentMarkdown,
		VersionNumber:   latest + 1,
		UserID:          userID,
		Comment:         comment,
	}

	page.ContentMarkdown = newMarkdown
	page.Status = newStatus
	// Снимок и запись — одно действие: порознь провалившаяся правка оставляла
	// в истории версию, которой не было (см. PageRepository.SaveEdit).
	if err := pages.SaveEdit(ctx, page, version); err != nil {
		return fmt.Errorf("save edit of page %d: %w", page.ID, err)
	}

	// Сбой переякоривания не откатывает правку — страница уже сохранена, и
	// потерять её хуже, чем оставить вырезки на пересчёт.
	if err := reanchorPage(ctx, fragments, page.ID, newMarkdown); err != nil {
		log.Printf("reanchor page %d: %v", page.ID, err)
	}

	// Вторая машина, держащаяся за ту же полосу. Забытая — не падает:
	// вклейка осталась бы на прежних смещениях, и читатель увидел бы на её
	// месте чужой кусок. Ровно так однажды был забыт первый вызов — в
	// RestoreVersion, у которого стояла своя копия всех шагов.
	if err := reanchorDocumentCuts(ctx, cuts, page.ID, newMarkdown); err != nil {
		log.Printf("reanchor cuts of page %d: %v", page.ID, err)
	}
	return nil
}
