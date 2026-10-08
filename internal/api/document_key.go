package api

import (
	"context"
	"net/http"

	"github.com/gorilla/mux"

	"proofreader/internal/models"
)

// documentKey достаёт из пути адрес разбора — пару «подпись, слаг». Ник пуст,
// если адрес односегментный: /documents/{слаг} — сотруднический разбор. Один
// в один collectionKey (collection_handler.go): у подборок эта развилка уже
// стоит, и второго её вида в читальне быть не должно.
func documentKey(r *http.Request) (nickname, slug string) {
	vars := mux.Vars(r)
	return vars["nickname"], vars["slug"]
}

// loadDocumentByKey читает разбор по адресу и разводит «нет такого» и «сбой
// хранилища»: 404 обязан значить «разбора нет», а не «база недоступна прямо
// сейчас». Одна дверь на семь обработчиков — до этой задачи каждый парсил
// номер сам, и семь копий разошлись бы при первом же изменении формы адреса.
//
// Второй результат false означает «ответ уже написан, обработчику остаётся
// только выйти».
func loadDocumentByKey(w http.ResponseWriter, r *http.Request, store DocumentStore) (*models.Document, bool) {
	nickname, slug := documentKey(r)
	document, err := store.GetByAuthorSlug(context.Background(), nickname, slug)
	if err != nil {
		if storageNotFound(err) {
			writeError(w, http.StatusNotFound, "Разбор не найден")
		} else {
			writeError(w, http.StatusInternalServerError, "Не удалось прочитать разбор")
		}
		return nil, false
	}
	return document, true
}
