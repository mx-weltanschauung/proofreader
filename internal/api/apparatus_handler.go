package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"

	"proofreader/internal/models"
	"proofreader/internal/repository"
)

// ApparatusStore — та часть репозитория аппарата, которой пользуется
// обработчик. План и снос читают один набор одним условием.
type ApparatusStore interface {
	Plan(ctx context.Context, workID int64) (models.ApparatusPlan, error)
	Remove(ctx context.Context, workID int64) (models.ApparatusPlan, error)
}

// ApparatusPlan отдаёт план снятия аппарата, ничего не трогая.
//
// Маршрут на чтение, но не публичный: план перечисляет поимённо то, что
// собираются снять, и читателю это не адресовано.
func (h *WorkHandler) ApparatusPlan(w http.ResponseWriter, r *http.Request) {
	id, ok := apparatusWorkID(w, r)
	if !ok {
		return
	}

	plan, err := h.apparatus.Plan(r.Context(), id)
	if err != nil {
		writeApparatusPlanError(w, err)
		return
	}
	writeJSONStatus(w, http.StatusOK, plan)
}

// DeleteApparatus снимает аппарат тома: главы с is_apparatus вместе с их
// полосами, предметный указатель тома и служебные передние листы.
//
// Необратимо. Обратный ход — не откат, а повторная публикация тома из
// локального корпуса (tools/ocr_ingest/publish_volume.py), и адреса при этом
// меняются: id выдаются заново.
//
// Отказ на пустом плане — не придирчивость. Разметка аппарата в корпусе
// неполна, она ставится классификатором по заголовку, и «снято ноль» на
// неразмеченном томе выглядит как успешное снятие. Ровно так рычаг и сломался
// бы на издании Выготского, ради которого он заведён: до миграции 000019 там
// не было размечено ни одной главы из 169.
func (h *WorkHandler) DeleteApparatus(w http.ResponseWriter, r *http.Request) {
	id, ok := apparatusWorkID(w, r)
	if !ok {
		return
	}
	ctx := r.Context()

	plan, err := h.apparatus.Plan(ctx, id)
	if err != nil {
		writeApparatusPlanError(w, err)
		return
	}
	if plan.Empty() {
		writeError(w, http.StatusConflict,
			"У тома не размечено ни одной главы аппарата и нет служебных работ — "+
				"снимать нечего. Разметь главы (is_apparatus) или убедись, что аппарата в томе нет.")
		return
	}

	plan, err = h.apparatus.Remove(ctx, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось снять аппарат тома")
		return
	}

	// Кэши отдачи сбрасываются в defer, а не последней строкой. База уже
	// закоммичена, и выйти отсюда можно на любом отказе хранилища ниже; выйди
	// мы мимо сброса — снятый аппарат отдавался бы готовыми главами до часа, а
	// карточкой og:image до суток, то есть ровно та утечка, ради которой
	// ServingCache и заведён. Повтора не будет: план теперь пуст, и повторный
	// вызов ответит 409.
	defer func() {
		for _, kid := range plan.Children {
			h.cache.DropWork(kid.ID)
		}
		h.cache.DropWork(id)
	}()

	// Хранилище — после базы: каскад Postgres про S3 не знает. Ребёнок уходит
	// префиксом целиком (там и его превью, и производный оригинал), полосы
	// аппарата — поштучно, потому что остальные полосы тома остаются жить под
	// тем же префиксом works/{id}/pages/.
	for _, kid := range plan.Children {
		if err := h.store.DeletePrefix(ctx, fmt.Sprintf("works/%d/", kid.ID)); err != nil {
			log.Printf("снятие аппарата тома %d: файлы служебной работы %d: %v", id, kid.ID, err)
			writeError(w, http.StatusInternalServerError,
				"Аппарат снят из базы, но файлы служебной работы остались в хранилище — убери их вручную")
			return
		}
	}
	for _, path := range plan.StoragePaths {
		if err := h.store.Delete(ctx, path); err != nil {
			log.Printf("снятие аппарата тома %d: превью %s: %v", id, path, err)
			writeError(w, http.StatusInternalServerError,
				"Аппарат снят из базы, но часть превью осталась в хранилище — убери их вручную")
			return
		}
	}
	// Записи человека в главах аппарата и дорожки синтеза, задевшие его
	// полосы, — поштучно, как превью: прочий синтез тома лежит под тем же
	// префиксом и остаётся жить. Раскладка аппарат не озвучивает, так что
	// дорожек здесь штатно нет — это страховка от прежней раскладки и от
	// главы, размеченной аппаратом после синтеза. Служебные работы —
	// префиксом, как их превью выше.
	if h.audioStore != nil {
		for _, kid := range plan.Children {
			if err := h.audioStore.DeletePrefix(ctx, fmt.Sprintf("works/%d/", kid.ID)); err != nil {
				log.Printf("снятие аппарата тома %d: звук служебной работы %d: %v", id, kid.ID, err)
				writeError(w, http.StatusInternalServerError,
					"Аппарат снят из базы, но звук служебной работы остался в хранилище — убери его вручную")
				return
			}
		}
		for _, key := range plan.AudioPaths {
			if err := h.audioStore.Delete(ctx, key); err != nil {
				log.Printf("снятие аппарата тома %d: запись %s: %v", id, key, err)
				writeError(w, http.StatusInternalServerError,
					"Аппарат снят из базы, но часть записей осталась в хранилище — убери их вручную")
				return
			}
		}
		for _, key := range plan.TrackPaths {
			if err := h.audioStore.Delete(ctx, key); err != nil {
				log.Printf("снятие аппарата тома %d: дорожка %s: %v", id, key, err)
				writeError(w, http.StatusInternalServerError,
					"Аппарат снят из базы, но часть звука осталась в хранилище — убери его вручную")
				return
			}
		}
	}

	writeJSONStatus(w, http.StatusOK, plan)
}

// writeApparatusPlanError различает «тома нет» и «база не ответила».
//
// Без различения мигнувший пул или statement_timeout отвечали бы «Том не
// найден» — единственным диагнозом, после которого оператор перестаёт искать.
// Во время срочного снятия это останавливает снятие не там.
func writeApparatusPlanError(w http.ResponseWriter, err error) {
	if errors.Is(err, repository.ErrWorkNotFound) {
		writeError(w, http.StatusNotFound, "Том не найден")
		return
	}
	log.Printf("план снятия аппарата: %v", err)
	writeError(w, http.StatusInternalServerError, "Не удалось прочитать состав аппарата тома")
}

func apparatusWorkID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Неверный идентификатор тома")
		return 0, false
	}
	return id, true
}
