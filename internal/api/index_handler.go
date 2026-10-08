package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"proofreader/internal/models"
	"proofreader/internal/repository"
	"proofreader/pkg/markdown"
)

// indexImportWriteDeadline — сколько даётся ответу ввоза. Не нулевое время
// (снять дедлайн совсем): повисший клиент должен когда-нибудь отпустить
// горутину и соединение. Замер ленинского тела — около 1.2 с локально;
// десять минут это запас на боевой в два ядра, а не ожидаемое время.
const indexImportWriteDeadline = 10 * time.Minute

// indexImportReadDeadline — сколько даётся ЧТЕНИЮ тела ввоза. ReadTimeout
// в cmd/server/main.go — 15 с, и покрывает он чтение всего запроса ВМЕСТЕ
// с телом, а тело ленинского указателя это ≈43 МБ. На боевом до бэкенда
// они доезжают быстро (nginx буферизует запрос целиком: proxy_request_
// buffering не задан, то есть on), но это свойство чужого конфига, а не
// кода: смена буферизации тихо вернула бы обрыв. Приём тот же, что у
// записи строкой ниже.
const indexImportReadDeadline = 10 * time.Minute

// IndexHandler handles subject-index concept endpoints: importing a parsed
// index for a work, reading concepts back with their references resolved
// against loaded volumes, the per-page backlink list, and the rendered text
// stream behind a concept's references.
type IndexHandler struct {
	index     IndexStore
	works     WorkStore
	editions  EditionStore
	pages     PageStore
	chapters  ChapterTreeStore
	fragments FragmentStore
	renderer  *markdown.Renderer
}

// NewIndexHandler creates a new index handler.
func NewIndexHandler(index IndexStore, works WorkStore, editions EditionStore, pages PageStore, chapters ChapterTreeStore, fragments FragmentStore, renderer *markdown.Renderer) *IndexHandler {
	return &IndexHandler{index: index, works: works, editions: editions, pages: pages, chapters: chapters, fragments: fragments, renderer: renderer}
}

// IndexImportRequest is the body of POST /api/editions/{id}/index/import: the
// whole set of concepts parsed out of that index volume. The import replaces
// the edition's articles wholesale — they are derived data, not editable here.
type IndexImportRequest struct {
	Concepts []*indexConceptImport `json:"concepts"`
}

// indexConceptImport is the wire shape of one incoming concept: a flat body
// mixing catalog-level fields (Title/Slug/SortKey) with the shape of the ONE
// article this ingest run is contributing (WorkID, ArticleMarkdown, Kind,
// SourcePage*, References, Links) — the format tools/ocr_ingest/publish_volume.py's
// index_payload() actually sends, and predates the concept/article split
// (задачи 9—10). models.IndexConcept stopped carrying these article fields in
// migration 000026, so they're decoded here and wrapped into a single
// *models.IndexArticle before handing off to ReplaceForEdition — the wire
// contract is unchanged, only the in-memory shape moved.
type indexConceptImport struct {
	Title   string `json:"title"`
	Slug    string `json:"slug"`
	SortKey string `json:"sort_key"`
	// WorkID — якорь снятия аппарата тома (apparatus_repository.go находит
	// статьи по нему). Ни tools/ocr_ingest/parse_index.py, ни
	// publish_volume.py это поле в теле НЕ шлют — у ленинского и
	// плехановского указателей статьи носит издание целиком, а не отдельная
	// работа-указатель (index_repository.go, ReplaceForEdition, комментарий
	// у edition_ids). Поле принимается на случай будущего клиента,
	// который всё же знает конкретную работу-источник, но сейчас на
	// каждом ввозе приходит nil. ReplaceForEdition поэтому не присваивает
	// его голым UPDATE — при nil сохраняется прежнее значение
	// (COALESCE), а не стирается: раунд правок 1, находка 2.
	WorkID          *int64                     `json:"work_id,omitempty"`
	SourceURL       string                     `json:"source_url"`
	ArticleMarkdown string                     `json:"article_markdown"`
	Kind            string                     `json:"kind"`
	SourcePageStart int                        `json:"source_page_start"`
	SourcePageEnd   int                        `json:"source_page_end"`
	References      []*models.IndexReference   `json:"references"`
	Links           []*models.IndexConceptLink `json:"links"`
}

// IndexImportResult is what the import reports back.
type IndexImportResult struct {
	Concepts   int      `json:"concepts"`
	References int      `json:"references"`
	Links      int      `json:"links"`
	Uncertain  int      `json:"uncertain"`
	Warnings   []string `json:"warnings"`
}

// referenceResponse is a reference with its resolution against loaded volumes.
type referenceResponse struct {
	*models.IndexReference
	Resolved   bool   `json:"resolved"`
	WorkID     int64  `json:"work_id,omitempty"`
	PageNumber int    `json:"page_number,omitempty"`
	WorkSlug   string `json:"work_slug,omitempty"`
}

// articleResponse is one index article with its references resolved against
// the volumes of ITS OWN edition (a.EditionID) — a concept can carry articles
// from several index volumes, each addressing a different collection.
type articleResponse struct {
	*models.IndexArticle
	References []referenceResponse         `json:"references"`
	Links      []repository.LinkWithTarget `json:"links"`
}

// Import replaces an edition's subject-index articles with a freshly-parsed set.
func (h *IndexHandler) Import(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	editionID, err := strconv.ParseInt(vars["id"], 10, 64)
	if err != nil {
		http.Error(w, "Invalid edition ID", http.StatusBadRequest)
		return
	}

	// Ввоз ленинского указателя — 2819 статей и 199 тыс. адресов одним телом.
	// Глобальный WriteTimeout (15 с, cmd/server/main.go) оборвал бы ответ, а
	// горутину Go при этом не убивает: транзакция коммитится, клиент видит
	// обрыв и не знает, прошёл ввоз или нет. Приём тот же, что у выгрузки и
	// скачивания, и ошибка так же ЛОГИРУЕТСЯ, а не роняет ответ: под
	// httptest контроллеру не до чего дотянуться, и 500 здесь сломал бы все
	// тесты обработчика. Нужен responseWriter.Unwrap в
	// internal/middleware/logging.go, чтобы добраться до соединения.
	if err := http.NewResponseController(w).SetWriteDeadline(
		time.Now().Add(indexImportWriteDeadline)); err != nil {
		log.Printf("ввоз указателя: не удалось раздвинуть дедлайн записи: %v", err)
	}
	if err := http.NewResponseController(w).SetReadDeadline(
		time.Now().Add(indexImportReadDeadline)); err != nil {
		log.Printf("ввоз указателя: не удалось раздвинуть дедлайн чтения: %v", err)
	}

	var req IndexImportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	ctx := context.Background()

	edition, err := h.editions.GetByID(ctx, editionID)
	if err != nil || edition == nil {
		http.Error(w, "Edition not found", http.StatusNotFound)
		return
	}

	taken, err := h.index.TakenSlugs(ctx)
	if err != nil {
		http.Error(w, "Failed to check taken slugs", http.StatusInternalServerError)
		return
	}
	if taken == nil {
		taken = make(map[string]bool)
	}

	result := IndexImportResult{Warnings: []string{}}

	concepts := make([]*models.IndexConcept, 0, len(req.Concepts))
	for i, c := range req.Concepts {
		if c.Title == "" {
			http.Error(w, fmt.Sprintf("concept %d: title is required", i), http.StatusBadRequest)
			return
		}

		base := c.Slug
		if base == "" {
			base = strings.ReplaceAll(c.SortKey, " ", "-")
		}
		slug := uniqueSlug(base, taken)
		taken[slug] = true

		result.Concepts++
		result.References += len(c.References)
		result.Links += len(c.Links)
		for _, ref := range c.References {
			if ref.IsUncertain {
				result.Uncertain++
			}
		}

		concepts = append(concepts, &models.IndexConcept{
			Title:   c.Title,
			Slug:    slug,
			SortKey: c.SortKey,
			Articles: []*models.IndexArticle{{
				WorkID:          c.WorkID,
				SourceURL:       c.SourceURL,
				ArticleMarkdown: c.ArticleMarkdown,
				Kind:            c.Kind,
				SourcePageStart: c.SourcePageStart,
				SourcePageEnd:   c.SourcePageEnd,
				References:      c.References,
				Links:           c.Links,
			}},
		})
	}

	warnings, err := h.index.ReplaceForEdition(ctx, editionID, concepts)
	if err != nil {
		// Причина отказа едет оператору дословно: ветка завела громкие
		// отказы, называющие виновную строку указателя (дубль заголовка
		// понятия, дубль подрубрики), и глотать их в общую фразу — значит
		// на ввозе в 2819 статей отправлять человека в логи сервера вместо
		// строки разборщика. Форма {"message": …}: text/plain от
		// http.Error фронт (frontend/src/utils/apiError.ts) подменяет
		// запасной фразой.
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	result.Warnings = append(result.Warnings, warnings...)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// uniqueSlug returns base if it isn't already taken, otherwise base with a
// "-2", "-3", ... suffix — the first one not in taken.
func uniqueSlug(base string, taken map[string]bool) string {
	if !taken[base] {
		return base
	}
	for n := 2; ; n++ {
		candidate := fmt.Sprintf("%s-%d", base, n)
		if !taken[candidate] {
			return candidate
		}
	}
}

// GetConcept retrieves one catalog concept by slug, with each of its index
// articles carrying its own references — resolved against the volumes of
// THAT article's edition, not the concept's (now legacy) work — and its own
// links to other concepts. Incoming links stay on the concept: they are
// catalog-wide, not per-article.
func (h *IndexHandler) GetConcept(w http.ResponseWriter, r *http.Request) {
	slug := mux.Vars(r)["slug"]

	ctx := context.Background()
	// GetConceptBySlug reports a missing slug as an error (see
	// *repository.IndexRepository), the same way GetByID does elsewhere in
	// this codebase (compare ChapterHandler.Get, EditionHandler.Update) - any
	// error here means "not found", not a distinguishable server fault. The
	// fake store in tests instead returns (nil, nil) for "no concept
	// configured", so both are treated as 404.
	c, err := h.index.GetConceptBySlug(ctx, slug)
	if err != nil || c == nil {
		http.Error(w, "Concept not found", http.StatusNotFound)
		return
	}

	// Карта томов — на издание, а не на понятие: у понятия статей может быть
	// несколько, и адреса каждой разрешаются по СВОЕМУ собранию.
	maps := map[int64]map[volumeKey]models.VolumeLocation{}
	articles := make([]articleResponse, 0, len(c.Articles))
	for _, a := range c.Articles {
		vols, ok := maps[a.EditionID]
		if !ok {
			locs, err := h.index.VolumeMap(ctx, a.EditionID)
			if err != nil {
				http.Error(w, "Failed to resolve volume map", http.StatusInternalServerError)
				return
			}
			vols = buildVolumeMap(locs)
			maps[a.EditionID] = vols
		}

		refs := make([]referenceResponse, 0, len(a.References))
		for _, ref := range a.References {
			rr := referenceResponse{IndexReference: ref}
			if workID, page, ok := resolveReference(ref, vols); ok {
				rr.Resolved = true
				rr.WorkID = workID
				rr.PageNumber = page
				// Слаг берём тем же ключом из vols, а не меняем сигнатуру
				// resolveReference: она проверена отдельным тестом
				// (index_resolve_test.go) на тройке (workID, page, ok), лишнее
				// поле там ни к чему.
				rr.WorkSlug = vols[keyOf(ref.VolumeNumber, ref.VolumePart)].WorkSlug
			}
			refs = append(refs, rr)
		}

		links, err := h.index.ArticleLinks(ctx, a.ID)
		if err != nil {
			http.Error(w, "Failed to retrieve concept links", http.StatusInternalServerError)
			return
		}
		// An article with no links is common, and null here would break the
		// client the same way it would in the concept list.
		if links == nil {
			links = []repository.LinkWithTarget{}
		}

		a.References = nil
		a.Links = nil
		articles = append(articles, articleResponse{IndexArticle: a, References: refs, Links: links})
	}

	incoming, err := h.index.IncomingLinks(ctx, c.ID)
	if err != nil {
		http.Error(w, "Failed to retrieve incoming links", http.StatusInternalServerError)
		return
	}
	if incoming == nil {
		incoming = []repository.IncomingLink{}
	}

	// Articles обнуляется на самом понятии: список уезжает отдельным полем
	// ответа (articles ниже, с резолвом references/links каждой статьи).
	c.Articles = nil

	resp := struct {
		*models.IndexConcept
		Articles      []articleResponse         `json:"articles"`
		IncomingLinks []repository.IncomingLink `json:"incoming_links"`
	}{IndexConcept: c, Articles: articles, IncomingLinks: incoming}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// ListConcepts returns a page of concepts for the index navigator, optionally
// filtered by a title substring (q) and/or a sort-key first letter.
func (h *IndexHandler) ListConcepts(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	letter := strings.ToLower(r.URL.Query().Get("letter"))

	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 500 {
		limit = 500
	}

	offset := 0
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	ctx := context.Background()
	concepts, err := h.index.ListConcepts(ctx, q, letter, limit, offset)
	if err != nil {
		http.Error(w, "Failed to retrieve concepts", http.StatusInternalServerError)
		return
	}
	// An empty result set comes back from the repository as a nil slice, and
	// json encodes that as null. The client expects a list — return [].
	if concepts == nil {
		concepts = []*models.IndexConcept{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(concepts)
}

// PageConcepts returns the concepts referenced on a given page of a work,
// via the work's volume coordinates within its edition. A work with no
// volume_number or no edition can never have backlinks, so it returns an
// empty list rather than querying.
func (h *IndexHandler) PageConcepts(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	workID, err := strconv.ParseInt(vars["workId"], 10, 64)
	if err != nil {
		http.Error(w, "Invalid work ID", http.StatusBadRequest)
		return
	}
	pageID, err := strconv.ParseInt(vars["pageId"], 10, 64)
	if err != nil {
		http.Error(w, "Invalid page ID", http.StatusBadRequest)
		return
	}

	ctx := context.Background()
	page, err := h.pages.GetByID(ctx, pageID)
	if err != nil || page == nil || page.WorkID != workID {
		http.Error(w, "Page not found", http.StatusNotFound)
		return
	}

	work, err := h.works.GetByID(ctx, workID)
	if err != nil || work == nil {
		http.Error(w, "Work not found", http.StatusNotFound)
		return
	}

	out := []models.ConceptBacklink{}
	if work.VolumeNumber == nil || work.EditionID == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(out)
		return
	}

	printed := page.PageNumber + work.PageOffset
	backlinks, err := h.index.Backlinks(ctx, *work.EditionID, *work.VolumeNumber, work.VolumePart, printed)
	if err != nil {
		http.Error(w, "Failed to retrieve backlinks", http.StatusInternalServerError)
		return
	}
	if backlinks != nil {
		out = backlinks
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}
