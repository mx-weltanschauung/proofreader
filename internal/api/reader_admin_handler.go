package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/gorilla/mux"

	"proofreader/internal/models"
	"proofreader/internal/repository"
)

// readerListLimit — потолок читательского списка. Разбор жалобы идёт по нику,
// а не пролистыванием, поэтому страниц здесь нет: не поместившихся находит
// строка поиска.
const readerListLimit = 200

// readerStore — читатели глазами администратора.
type readerStore interface {
	ListReaders(ctx context.Context, query string, limit int) ([]repository.ReaderRow, error)
}

// authorCollectionStore — подборки по снимку подписи.
type authorCollectionStore interface {
	ListByAuthorNickname(ctx context.Context, nickname string) ([]*models.Collection, error)
}

// ReaderAdminHandler — путь администратора по жалобе: ник из письма →
// читатель → его подборки → кнопка снятия.
//
// Снятия здесь нет намеренно: подборку сносит уже существующий
// DELETE /api/collections/{ник}/{слаг}, у которого своя ветка администратора.
// Второй двери к тому же действию заводить не надо.
//
// Учётной записью этот обработчик не распоряжается вовсе — ни роли, ни пароля,
// ни удаления. Удаление читателя не прекращает выданный ему токен (он живёт до
// 90 суток), поэтому кнопка обещала бы то, чего читальня не делает.
type ReaderAdminHandler struct {
	readers     readerStore
	collections authorCollectionStore
}

// NewReaderAdminHandler creates a new reader admin handler.
func NewReaderAdminHandler(readers readerStore, collections authorCollectionStore) *ReaderAdminHandler {
	return &ReaderAdminHandler{readers: readers, collections: collections}
}

// List отдаёт читателей администратору: ник, дата заведения, число подборок.
// Строка поиска (?q=) уезжает в запрос как есть — нормализация и экранирование
// живут в репозитории, рядом с ключом уникальности ника.
func (h *ReaderAdminHandler) List(w http.ResponseWriter, r *http.Request) {
	rows, err := h.readers.ListReaders(r.Context(), r.URL.Query().Get("q"), readerListLimit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось получить список читателей")
		return
	}
	if rows == nil {
		rows = []repository.ReaderRow{}
	}
	writeJSON(w, rows)
}

// ReaderCollection — подборка читателя глазами администратора: обычная модель
// плюс признак «публиковалась когда-то».
//
// Признак нужен потому, что снятая с публикации и никогда не публиковавшаяся —
// для разбора жалобы разные вещи: снятую читатели видели, черновика не видел
// никто, кроме автора, и жалоба бывает только о первом. Различает их
// Collection.PublishIPHash, но он `json:"-"` — отметка адреса наружу не
// уезжает, — поэтому признак считает сервер.
type ReaderCollection struct {
	*models.Collection
	WasPublished bool `json:"was_published"`
}

// Collections отдаёт все подборки одного ника — черновики, снятые и
// опубликованные. Ник, у которого подборок нет, отвечает пустым списком, а не
// 404: маршрут отвечает о подборках, а не о существовании учётной записи, и
// осиротевшие подборки удалённого читателя обязаны находиться так же.
func (h *ReaderAdminHandler) Collections(w http.ResponseWriter, r *http.Request) {
	nickname := strings.TrimSpace(mux.Vars(r)["nickname"])
	if nickname == "" {
		writeError(w, http.StatusBadRequest, "Не указано имя читателя")
		return
	}

	collections, err := h.collections.ListByAuthorNickname(r.Context(), nickname)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось получить подборки читателя")
		return
	}

	out := make([]ReaderCollection, 0, len(collections))
	for _, c := range collections {
		out = append(out, ReaderCollection{
			Collection:   c,
			WasPublished: c.PublishedAt != nil || c.PublishIPHash != "",
		})
	}
	writeJSON(w, out)
}
