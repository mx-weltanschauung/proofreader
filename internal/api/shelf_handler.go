package api

import (
	"context"
	"encoding/json"
	"net/http"

	"proofreader/internal/models"
)

// ShelfHandler отдаёт всё, что рисует главная, одним ответом.
//
// Прежде страница собирала это сама: список собраний и каталог работ, а затем
// — только дождавшись списка — по запросу на каждое собрание за его томами.
// Шесть запросов в две волны, и вторая волна стоила по 350 мс на собрание,
// потому что тяжёлые CTE запроса сводок считаются по всему корпусу и от
// фильтра по собранию не зависят. Здесь тот же счёт делается один раз на все
// собрания и обходится ровно во столько же, во сколько прежде один из
// четырёх.
type ShelfHandler struct {
	editions ShelfEditions
	works    ShelfWorks
	journals ShelfJournals
}

// WithJournals подключает журналы; без них полка отдаёт пустой массив.
func (h *ShelfHandler) WithJournals(j ShelfJournals) *ShelfHandler {
	h.journals = j
	return h
}

// NewShelfHandler creates a new shelf handler.
func NewShelfHandler(editions ShelfEditions, works ShelfWorks) *ShelfHandler {
	return &ShelfHandler{editions: editions, works: works}
}

// Get returns the whole home page payload: every edition with its volumes, plus
// the works that belong to no edition. Public, like every read in this project.
func (h *ShelfHandler) Get(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()

	editions, err := h.editions.List(ctx)
	if err != nil {
		http.Error(w, "Failed to retrieve editions", http.StatusInternalServerError)
		return
	}

	summaries, err := h.editions.ListAllWorkSummaries(ctx)
	if err != nil {
		http.Error(w, "Failed to retrieve volume summaries", http.StatusInternalServerError)
		return
	}

	loose, err := h.works.ListWithoutEdition(ctx)
	if err != nil {
		http.Error(w, "Failed to retrieve works", http.StatusInternalServerError)
		return
	}

	shelf := models.Shelf{
		Editions:   groupByEdition(editions, summaries),
		LooseWorks: loose,
	}
	// Пустой срез из репозитория приходит nil, а json пишет его как null;
	// клиент зовёт по этим спискам .map сразу.
	if shelf.LooseWorks == nil {
		shelf.LooseWorks = []models.ShelfWork{}
	}
	shelf.Journals = []models.JournalSummary{}
	if h.journals != nil {
		list, err := h.journals.List(ctx)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Не удалось прочитать журналы")
			return
		}
		for _, j := range list {
			if j.IssuesTotal > 0 {
				shelf.Journals = append(shelf.Journals, j)
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(shelf)
}

// groupByEdition раскладывает общий, отсортированный по собранию список сводок
// по полкам. Порядок собраний берётся у List, порядок томов внутри полки —
// у запроса сводок: он и есть порядок издания. Сводка, чьего собрания в списке
// нет, отбрасывается — полки без заголовка быть не должно.
func groupByEdition(
	editions []*models.Edition, summaries []*models.VolumeSummary,
) []models.ShelfEdition {
	byID := make(map[int64]int, len(editions))
	shelves := make([]models.ShelfEdition, len(editions))
	for i, edition := range editions {
		byID[edition.ID] = i
		// Не nil: собрание без томов — обычное состояние сразу после
		// заведения, и главная рисует по нему приглашение загрузить первый.
		shelves[i] = models.ShelfEdition{
			Edition: edition,
			Volumes: []*models.VolumeSummary{},
		}
	}

	for _, s := range summaries {
		if s.EditionID == nil {
			continue
		}
		i, ok := byID[*s.EditionID]
		if !ok {
			continue
		}
		shelves[i].Volumes = append(shelves[i].Volumes, s)
	}

	return shelves
}
