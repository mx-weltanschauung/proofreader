package api

import (
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"

	"proofreader/internal/models"
	"proofreader/pkg/markdown"
)

const (
	readingDefaultCount = 10
	readingMaxCount     = 50
)

// ReadingPage — одна страница окна потокового чтения.
//
// Ни markdown, ни id, ни статуса, ни подписанного URL превью: читалке нужен
// текст, свои сноски и один бит «рисовать ли маркер номера». На томе в 742
// страницы полная модель страницы стоила бы мегабайты.
type ReadingPage struct {
	PageNumber int    `json:"page_number"`
	HTML       string `json:"html"`
	NotesHTML  string `json:"notes_html"`
	Blank      bool   `json:"blank"`
}

// ReadingWindowResponse — окно страниц работы и указание, откуда брать
// следующее.
type ReadingWindowResponse struct {
	Pages []ReadingPage `json:"pages"`
	// Номер первой страницы следующего окна; nil — работа кончилась.
	NextFrom   *int `json:"next_from"`
	TotalPages int  `json:"total_pages"`
}

// ReadingHandler отдаёт окна страниц работы для потокового чтения.
//
// Отдельный обработчик, а не третий метод у глав: маршрут адресует работу.
// Поток идёт сквозь весь том и границу главы не замечает.
type ReadingHandler struct {
	pageRepo PageStore
	renderer *markdown.Renderer
}

// NewReadingHandler creates a new reading handler
func NewReadingHandler(pageRepo PageStore, renderer *markdown.Renderer) *ReadingHandler {
	return &ReadingHandler{pageRepo: pageRepo, renderer: renderer}
}

// readingIntParam читает целочисленный параметр запроса, зажимая его в
// [min, max].
//
// Отсутствующий, пустой и нечисловой параметр дают fallback молча: сообщать
// читателю о кривой строке в адресе нечего и некуда — читалка просто
// начинает сначала.
func readingIntParam(r *http.Request, name string, fallback, min, max int) int {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

// Window returns a run of rendered pages starting at ?from=, and where the
// next run starts.
func (h *ReadingHandler) Window(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	workID, err := strconv.ParseInt(vars["workId"], 10, 64)
	if err != nil {
		http.Error(w, "Invalid work ID", http.StatusBadRequest)
		return
	}

	from := readingIntParam(r, "from", 1, 1, math.MaxInt32)
	count := readingIntParam(r, "count", readingDefaultCount, 1, readingMaxCount)

	ctx := r.Context()

	total, err := h.pageRepo.MaxPageNumber(ctx, workID)
	if err != nil {
		http.Error(w, "Failed to retrieve work length", http.StatusInternalServerError)
		return
	}
	if total == 0 {
		http.Error(w, "Work not found", http.StatusNotFound)
		return
	}
	if from > total {
		http.Error(w, "Page out of range", http.StatusNotFound)
		return
	}

	// Спрашиваем на страницу больше, чем отдадим: пришла ли она — и есть
	// ответ на вопрос, есть ли продолжение. Отдельного запроса ради этого не
	// нужно. Приём держится на сплошной нумерации страниц; на дыре поток
	// остановился бы перед ней.
	pages, err := h.pageRepo.GetPageRange(ctx, workID, from, from+count)
	if err != nil {
		http.Error(w, "Failed to retrieve pages", http.StatusInternalServerError)
		return
	}

	var nextFrom *int
	if len(pages) > count {
		next := pages[count].PageNumber
		nextFrom = &next
		pages = pages[:count]
	}

	// make, а не var: nil-срез уехал бы в JSON как null, и фронт упал бы на
	// .map при чтении работы без страниц.
	result := make([]ReadingPage, len(pages))
	for i, page := range pages {
		// Постранично, по одной странице за вызов. Счёт подстрочных сносок
		// сквозной по переданному диапазону, а у потока диапазона нет — он
		// не кончается, и войди читатель в разных местах, одна и та же
		// сноска получила бы разные номера. Страница везёт свои сноски и ни
		// от чего не зависит.
		html, notes := renderPageRange(h.renderer, []*models.Page{page})
		result[i] = ReadingPage{
			PageNumber: page.PageNumber,
			HTML:       html[0],
			NotesHTML:  markdown.RenderNotes(notes),
			Blank:      strings.TrimSpace(page.ContentMarkdown) == "",
		}
	}

	response := ReadingWindowResponse{
		Pages:      result,
		NextFrom:   nextFrom,
		TotalPages: total,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
