package opds

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"proofreader/internal/site"
	"strconv"
	"strings"
	"time"
)

// Handler отдаёт ленты каталога под /opds. Своего кэша и ETag нет: самая
// тяжёлая лента стоит запроса /api/shelf.
type Handler struct {
	src Source
	b   builder
	now func() time.Time
}

// NewHandler — baseURL тот же, что у карты сайта и титульного листа
// выгрузки (PUBLIC_BASE_URL).
func NewHandler(src Source, baseURL string) *Handler {
	base := strings.TrimSuffix(baseURL, "/")
	host := "localhost"
	if u, err := url.Parse(base); err == nil && u.Hostname() != "" {
		host = u.Hostname()
	}
	return &Handler{src: src, b: builder{base: base, host: host}, now: time.Now}
}

// ServeHTTP разбирает путь и собирает ленту целиком в память до первого
// байта ответа: сбой источника посреди ленты обязан стать 500, а не
// обрезанным XML с кодом 200.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	route, ok := ParsePath(r.URL.Path)
	if !ok {
		http.Error(w, "Нет такого раздела каталога", http.StatusNotFound)
		return
	}
	page, ok := parsePage(r)
	if !ok {
		http.Error(w, "Нет такой страницы каталога", http.StatusNotFound)
		return
	}

	var (
		doc   any
		ctype string
		err   error
	)
	switch route.Kind {
	case KindOpenSearch:
		doc, ctype = h.openSearch(), typeOpenSearch
	case KindSearch:
		doc, err = h.search(r, page)
		ctype = typeAcquisition
	default:
		var f *feed
		f, ctype, err = h.feed(r, route, page)
		doc = f
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}

	var buf bytes.Buffer
	if err := writeXML(&buf, doc); err != nil {
		h.fail(w, r, err)
		return
	}
	// charset — в заголовке, а не только в XML-объявлении: часть клиентов
	// судит о кодировке по HTTP и без него берёт кодировку системы. В
	// атрибутах type ссылок ленты он не нужен — там тип по спецификации OPDS.
	w.Header().Set("Content-Type", ctype+";charset=utf-8")
	w.Header().Set("X-Robots-Tag", "noindex")
	w.Header().Set("Cache-Control", "public, max-age=300")
	_, _ = w.Write(buf.Bytes())
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	var re *RequestError
	switch {
	case errors.Is(err, ErrNotFound):
		http.Error(w, "Нет такого раздела каталога", http.StatusNotFound)
	case errors.As(err, &re):
		if re.RetryAfter > 0 {
			w.Header().Set("Retry-After", strconv.Itoa(re.RetryAfter))
		}
		http.Error(w, re.Message, re.Status)
	default:
		log.Printf("opds %s: %v", r.URL.Path, err)
		http.Error(w, "Каталог временно недоступен", http.StatusInternalServerError)
	}
}

// parsePage — ?page=N с единицы; без параметра — 1. Мусор — не страница.
func parsePage(r *http.Request) (int, bool) {
	s := r.URL.Query().Get("page")
	if s == "" {
		return 1, true
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return 0, false
	}
	return n, true
}

func (h *Handler) feed(r *http.Request, route Route, page int) (*feed, string, error) {
	ctx := r.Context()
	switch route.Kind {
	case KindRoot, KindEditions, KindEdition, KindLoose:
		if page != 1 {
			return nil, "", ErrNotFound
		}
		shelf, err := h.src.Shelf(ctx)
		if err != nil {
			return nil, "", err
		}
		f, err := h.shelfFeed(route, shelf)
		return f, typeNavigation, err
	case KindNew:
		f, err := h.recent(r, page)
		return f, typeAcquisition, err
	case KindWork, KindChapter:
		f, err := h.treeFeed(r, route, page)
		return f, typeAcquisition, err
	}
	return nil, "", ErrNotFound
}

// newFeed — общая шапка: self, start, up и поиск у каждой ленты.
// upKind — тип ленты, в которую ведёт up: лента тома — книги, а не навигация.
func (h *Handler) newFeed(path, query, title, kind, up, upKind string, updated time.Time) *feed {
	self := path
	if query != "" {
		self += "?" + query
	}
	f := &feed{
		Xmlns: nsAtom, XmlnsDC: nsDC,
		ID: h.b.feedID(self), Title: title, Updated: stamp(updated),
		Author: author{Name: site.Name(), URI: h.b.base},
		Links: []link{
			{Rel: "self", Href: h.b.abs(self), Type: kind},
			{Rel: "start", Href: h.b.abs("/opds"), Type: typeNavigation},
			{Rel: "search", Href: h.b.abs("/opds/search.xml"), Type: typeOpenSearch},
		},
	}
	if up != "" {
		f.Links = append(f.Links, link{Rel: "up", Href: h.b.abs(up), Type: upKind})
	}
	return f
}

// pager дописывает first/previous/next. more — есть ли записи дальше page;
// query — параметры ленты помимо page (у поиска — q), пусто — нет.
func (h *Handler) pager(f *feed, path, query string, page int, more bool, kind string) {
	at := func(p int) string {
		q := query
		if p > 1 {
			if q != "" {
				q += "&"
			}
			q += "page=" + strconv.Itoa(p)
		}
		if q == "" {
			return h.b.abs(path)
		}
		return h.b.abs(path + "?" + q)
	}
	if page > 1 {
		f.Links = append(f.Links,
			link{Rel: "first", Href: at(1), Type: kind},
			link{Rel: "previous", Href: at(page - 1), Type: kind})
	}
	if more {
		f.Links = append(f.Links, link{Rel: "next", Href: at(page + 1), Type: kind})
	}
}

func pageQuery(page int) string {
	if page == 1 {
		return ""
	}
	return "page=" + strconv.Itoa(page)
}

func (h *Handler) shelfFeed(route Route, shelf *Shelf) (*feed, error) {
	all := append([]Volume(nil), shelf.Loose...)
	for _, e := range shelf.Editions {
		all = append(all, e.Volumes...)
	}
	switch route.Kind {
	case KindRoot:
		f := h.newFeed("/opds", "", site.Name(), typeNavigation, "", "", volumesLatest(all))
		var edUpdated time.Time
		for _, e := range shelf.Editions {
			edUpdated = latest(edUpdated, volumesLatest(e.Volumes))
		}
		f.Entries = append(f.Entries, entry{
			ID: h.b.feedID("/opds/editions"), Title: "Собрания сочинений",
			Updated: stamp(edUpdated), Summary: fmt.Sprintf("Собраний: %d", len(nonEmpty(shelf.Editions))),
			Links: []link{{Rel: relSubsection, Href: h.b.abs("/opds/editions"), Type: typeNavigation}},
		})
		if len(shelf.Loose) > 0 {
			f.Entries = append(f.Entries, entry{
				ID: h.b.feedID("/opds/loose"), Title: "Отдельные тома",
				Updated: stamp(volumesLatest(shelf.Loose)), Summary: fmt.Sprintf("Томов: %d", len(shelf.Loose)),
				Links: []link{{Rel: relSubsection, Href: h.b.abs("/opds/loose"), Type: typeNavigation}},
			})
		}
		f.Entries = append(f.Entries, entry{
			ID: h.b.feedID("/opds/new"), Title: "Новые поступления",
			Updated: stamp(volumesLatest(all)), Summary: "Тома, недавно появившиеся в читальне",
			Links: []link{{Rel: relSubsection, Href: h.b.abs("/opds/new"), Type: typeAcquisition}},
		})
		return f, nil
	case KindEditions:
		f := h.newFeed("/opds/editions", "", "Собрания сочинений", typeNavigation, "/opds", typeNavigation, volumesLatest(all))
		for _, e := range nonEmpty(shelf.Editions) {
			f.Entries = append(f.Entries, entry{
				ID: h.b.tag("edition", e.ID), Title: e.Title, Updated: stamp(volumesLatest(e.Volumes)),
				Summary: fmt.Sprintf("Томов: %d", len(e.Volumes)),
				Links:   []link{{Rel: relSubsection, Href: h.b.abs(editionPath(e.ID)), Type: typeNavigation}},
			})
		}
		return f, nil
	case KindEdition:
		for _, e := range shelf.Editions {
			if e.ID != route.EditionID {
				continue
			}
			f := h.newFeed(editionPath(e.ID), "", e.Title, typeNavigation, "/opds/editions", typeNavigation, volumesLatest(e.Volumes))
			for _, v := range e.Volumes {
				f.Entries = append(f.Entries, h.b.volumeNav(v))
			}
			return f, nil
		}
		return nil, ErrNotFound
	case KindLoose:
		f := h.newFeed("/opds/loose", "", "Отдельные тома", typeNavigation, "/opds", typeNavigation, volumesLatest(shelf.Loose))
		for _, v := range shelf.Loose {
			f.Entries = append(f.Entries, h.b.volumeNav(v))
		}
		return f, nil
	}
	return nil, ErrNotFound
}

func nonEmpty(eds []Edition) []Edition {
	var out []Edition
	for _, e := range eds {
		if len(e.Volumes) > 0 {
			out = append(out, e)
		}
	}
	return out
}

func (h *Handler) recent(r *http.Request, page int) (*feed, error) {
	offset := (page - 1) * recentPageSize
	vs, err := h.src.Recent(r.Context(), recentPageSize+1, offset)
	if err != nil {
		return nil, err
	}
	if page > 1 && len(vs) == 0 {
		return nil, ErrNotFound
	}
	more := len(vs) > recentPageSize
	if more {
		vs = vs[:recentPageSize]
	}
	updated := volumesLatest(vs)
	if updated.IsZero() {
		updated = h.now()
	}
	f := h.newFeed("/opds/new", pageQuery(page), "Новые поступления", typeAcquisition, "/opds", typeNavigation, updated)
	for _, v := range vs {
		f.Entries = append(f.Entries, h.b.volumeBook(v, false, true, ""))
	}
	h.pager(f, "/opds/new", "", page, more, typeAcquisition)
	return f, nil
}

// treeFeed — лента тома или узла: на первой странице «Целиком», затем дети
// по nodePageSize.
func (h *Handler) treeFeed(r *http.Request, route Route, page int) (*feed, error) {
	tree, err := h.src.Tree(r.Context(), route.WorkID)
	if err != nil {
		return nil, err
	}
	v := tree.Volume
	nodes := Prune(tree.Nodes)

	path, title, up, upKind := workPath(v.ID), v.Title, "/opds", typeNavigation
	if v.EditionID != 0 {
		up = editionPath(v.EditionID)
	}
	whole := h.b.volumeBook(v, true, false, "")
	updated := v.Updated
	children := nodes
	if route.Kind == KindChapter {
		n := find(nodes, route.ChapterID)
		if n == nil {
			return nil, ErrNotFound
		}
		path, title, up, upKind = chapterPath(v.ID, n.ID), n.Title, workPath(v.ID), typeAcquisition
		whole = h.b.nodeBook(v, n, true)
		updated = n.Updated
		children = n.Children
	}

	from, to, ok := window(len(children), nodePageSize, page)
	if !ok {
		return nil, ErrNotFound
	}
	f := h.newFeed(path, pageQuery(page), title, typeAcquisition, up, upKind, updated)
	if page == 1 {
		f.Entries = append(f.Entries, whole)
	}
	for _, n := range children[from:to] {
		f.Entries = append(f.Entries, h.b.node(v, n))
	}
	h.pager(f, path, "", page, to < len(children), typeAcquisition)
	return f, nil
}

// search — главы на первой странице, тома по searchPageSize: по частому
// слову томов с совпадениями в тексте — почти весь корпус, и лента целиком
// весила бы 190 КБ (замер 03.10.2026, «Манифест», 170 томов).
func (h *Handler) search(r *http.Request, page int) (*feed, error) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	res, err := h.src.Search(r, q, page == 1)
	if err != nil {
		return nil, err
	}
	from, to, ok := window(len(res.Volumes), searchPageSize, page)
	if !ok {
		return nil, ErrNotFound
	}
	query := "q=" + url.QueryEscape(q)
	self := query
	if page > 1 {
		self += "&page=" + strconv.Itoa(page)
	}
	f := h.newFeed("/opds/search", self, "Поиск: "+q, typeAcquisition, "/opds", typeNavigation, h.now())
	pruned := map[*Tree][]*Node{}
	chapters := res.Chapters
	if page > 1 {
		chapters = nil
	}
	for _, hit := range chapters {
		nodes, ok := pruned[hit.Tree]
		if !ok {
			nodes = Prune(hit.Tree.Nodes)
			pruned[hit.Tree] = nodes
		}
		if n := find(nodes, hit.ChapterID); n != nil {
			f.Entries = append(f.Entries, h.b.node(hit.Tree.Volume, n))
		}
	}
	for _, hit := range res.Volumes[from:to] {
		summary := joinSummary(volumeSummary(hit.Volume), fmt.Sprintf("в тексте — %d %s", hit.TextHits, plural(hit.TextHits, "полоса", "полосы", "полос")))
		f.Entries = append(f.Entries, h.b.volumeBook(hit.Volume, false, true, summary))
	}
	h.pager(f, "/opds/search", query, page, to < len(res.Volumes), typeAcquisition)
	return f, nil
}

func (h *Handler) openSearch() *openSearch {
	return &openSearch{
		Xmlns:       nsOS,
		ShortName:   site.Name(),
		Description: "Поиск по названиям глав и тексту томов читальни",
		InputEnc:    "UTF-8",
		OutputEnc:   "UTF-8",
		URL: osURL{
			Type:     typeAcquisition,
			Template: h.b.abs("/opds/search?q={searchTerms}"),
		},
	}
}
