package api

import (
	"net/http"
	"strconv"

	"github.com/gorilla/mux"

	"proofreader/internal/pagecache"
)

// CacheHandler — сброс файлового кэша руками и сводка о нём.
//
// Кэш держится сроком годности в час, путей инвалидации у него нет. Это
// сознательный размен, и кнопка сброса — его вторая половина: редактор,
// увидевший в главе прежний текст, чинит это сам, не дожидаясь часа.
type CacheHandler struct {
	cache    *pagecache.Store
	chapters ChapterGetter
}

func NewCacheHandler(cache *pagecache.Store, chapters ChapterGetter) *CacheHandler {
	return &CacheHandler{cache: cache, chapters: chapters}
}

// PurgeChapter сбрасывает кэш одной главы.
//
// Принимает id главы, а не диапазон: диапазон резолвит сервер, клиенту про
// устройство ключа знать незачем. Побочно сброс освежает и элемент подборки
// с тем же диапазоном — файл-то один.
func (h *CacheHandler) PurgeChapter(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	workID, err := strconv.ParseInt(vars["workId"], 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Неверный идентификатор тома")
		return
	}
	chapterID, err := strconv.ParseInt(vars["id"], 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Неверный идентификатор главы")
		return
	}

	chapter, err := h.chapters.GetByID(r.Context(), chapterID)
	if err != nil || chapter == nil {
		writeError(w, http.StatusNotFound, "Глава не найдена")
		return
	}
	if chapter.WorkID != workID {
		writeError(w, http.StatusBadRequest, "Глава принадлежит другому тому")
		return
	}

	removed, err := h.cache.Delete(pagecache.Key{
		WorkID: chapter.WorkID,
		Start:  chapter.StartPage,
		End:    chapter.EndPage,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось сбросить кэш главы")
		return
	}

	count := 0
	if removed {
		count = 1
	}
	writeJSONStatus(w, http.StatusOK, map[string]int{"removed": count})
}

// PurgeAll сносит весь кэш. Только администратору: на боевом это два ядра
// под полным перерендером корпуса.
func (h *CacheHandler) PurgeAll(w http.ResponseWriter, r *http.Request) {
	files, bytes, err := h.cache.Purge()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось сбросить кэш")
		return
	}
	writeJSONStatus(w, http.StatusOK, map[string]any{"removed": files, "bytes": bytes})
}

// Stats отдаёт сводку. Выключенный кэш — не ошибка: администратор должен
// увидеть «выключен», а не пятисотку.
func (h *CacheHandler) Stats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.cache.Stats()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось прочитать состояние кэша")
		return
	}
	writeJSONStatus(w, http.StatusOK, stats)
}
