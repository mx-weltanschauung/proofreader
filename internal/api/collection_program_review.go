package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"proofreader/internal/auth"
	"proofreader/internal/middleware"
	"proofreader/internal/models"
	"proofreader/internal/repository"
)

func mayReviewCollection(claims *auth.Claims, collection *models.Collection) bool {
	return isStaffClaims(claims) && collection.AuthorNickname != "" && collection.OwnerID != nil &&
		collection.ProgramReviewStatus == models.CollectionProgramPending
}

type collectionViewError struct {
	status  int
	message string
}

func (e *collectionViewError) Error() string { return e.message }

// После загрузки состава сверяем версию, чтобы параллельный отзыв или снятие
// с публикации не отдали новую редакцию по разрешению на прежнюю.
func checkCollectionView(ctx context.Context, store CollectionStore, claims *auth.Claims, initial *models.Collection) *collectionViewError {
	if initial.AuthorNickname == "" || mayEdit(claims, initial) {
		return nil
	}
	current, err := store.GetByAuthorSlug(ctx, initial.AuthorNickname, initial.Slug)
	if err != nil || current == nil || current.ID != initial.ID {
		return &collectionViewError{http.StatusNotFound, "Подборка не найдена"}
	}
	if status, message, ok := collectionVisibleTo(claims, current); !ok {
		return &collectionViewError{status, message}
	}
	if current.Revision != initial.Revision {
		return &collectionViewError{http.StatusConflict, "Подборка изменилась. Откройте её заново."}
	}
	return nil
}

func writeCollectionReviewConflict(w http.ResponseWriter, err error) bool {
	var message string
	switch {
	case errors.Is(err, repository.ErrCollectionProgramReviewRequired):
		message = "Читательскую программу нужно отправить на проверку"
	case errors.Is(err, repository.ErrCollectionProgramPending):
		message = "Программа на проверке. Для правки отзовите её."
	case errors.Is(err, repository.ErrCollectionProgramPublished):
		message = "Снимите подборку с публикации, чтобы изменить программу"
	case errors.Is(err, repository.ErrCollectionProgramNotDraft):
		message = "На проверку можно отправить черновик программы с занятиями"
	case errors.Is(err, repository.ErrCollectionProgramNotPending):
		message = "Эта версия программы уже не ждёт рассмотрения. Обновите очередь."
	default:
		return false
	}
	writeError(w, http.StatusConflict, message)
	return true
}

func (h *CollectionHandler) SubmitProgram(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	nickname, slug := collectionKey(r)
	collection, err := h.collectionStore.GetByAuthorSlug(ctx, nickname, slug)
	if err != nil || collection == nil {
		writeError(w, http.StatusNotFound, "Подборка не найдена")
		return
	}
	claims, _ := middleware.GetUserFromContext(ctx)
	if !mayEdit(claims, collection) {
		writeError(w, http.StatusForbidden, "Отправить на проверку можно только свою программу")
		return
	}
	var req struct {
		Revision *int64 `json:"revision"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&req); err != nil || req.Revision == nil {
		writeError(w, http.StatusBadRequest, "Укажите версию программы")
		return
	}
	ipHash := hashIP(clientIP(r, h.trustProxy), h.jwtSecret)
	submitted, err := h.collectionStore.SubmitProgram(ctx, collection.ID, *req.Revision, ipHash,
		collectionPublishesPerDay, time.Now().Add(-24*time.Hour))
	switch {
	case writeCollectionReviewConflict(w, err):
		return
	case errors.Is(err, repository.ErrCollectionCompositionChanged):
		writeError(w, http.StatusConflict, "Программа изменилась. Откройте её заново.")
	case err != nil:
		log.Printf("submit collection program: %v", err)
		writeError(w, http.StatusInternalServerError, "Не удалось отправить программу на проверку")
	case !submitted:
		writeError(w, http.StatusTooManyRequests, "С этого адреса сегодня уже отправляли программы. Попробуйте завтра.")
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (h *CollectionHandler) ProgramQueue(w http.ResponseWriter, r *http.Request) {
	queue, err := h.collectionStore.ProgramsForReview(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось загрузить очередь программ")
		return
	}
	if queue == nil {
		queue = []models.CollectionProgramReviewEntry{}
	}
	writeJSON(w, queue)
}

func (h *CollectionHandler) ResolveProgram(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "Некорректный номер программы")
		return
	}
	var req struct {
		Revision *int64 `json:"revision"`
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&req); err != nil || req.Revision == nil {
		writeError(w, http.StatusBadRequest, "Укажите версию и решение")
		return
	}
	reason := strings.TrimSpace(req.Reason)
	switch req.Decision {
	case "approve":
		reason = ""
	case "reject":
		if reason == "" {
			writeError(w, http.StatusBadRequest, "Укажите причину возврата автору")
			return
		}
	default:
		writeError(w, http.StatusBadRequest, "Укажите решение по программе")
		return
	}
	if err := h.collectionStore.ResolveProgram(r.Context(), id, *req.Revision, reason); err != nil {
		if !writeCollectionReviewConflict(w, err) {
			log.Printf("resolve collection program: %v", err)
			writeError(w, http.StatusInternalServerError, "Не удалось сохранить решение")
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
