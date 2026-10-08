package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"proofreader/internal/limit"
	"proofreader/internal/models"
	"proofreader/internal/repository"
	"proofreader/internal/seo"
	"proofreader/internal/stats"
)

const (
	// searchBudget — потолок ответа search в знаках JSON. Сырой каталог —
	// 100—550 КБ, то есть до 150 тыс. токенов в контекст модели за вызов.
	searchBudget   = 10000
	maxVolumes     = 10
	pagesPerVolume = 3
	maxChapters    = 10
	maxConcepts    = 5
	snippetRunes   = 300
	// volumePageLimit и maxVolumeChapters — окно search_volume.
	volumePageLimit   = 20
	maxVolumeChapters = 10
)

// Hit — один результат. id, title, url — контракт deep research ChatGPT;
// остальное — чтобы модель видела отрывок, не открывая полосу.
type Hit struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	URL         string `json:"url"`
	Kind        string `json:"kind" jsonschema:"page, chapter или concept"`
	Volume      string `json:"volume,omitempty"`
	PrintedPage int    `json:"printed_page,omitempty" jsonschema:"страница печатного издания"`
	Chapter     string `json:"chapter,omitempty"`
	Apparatus   bool   `json:"apparatus,omitempty" jsonschema:"полоса из примечаний редакции, не текст автора"`
	Snippet     string `json:"snippet,omitempty"`
}

type SearchInput struct {
	Query    string  `json:"query" jsonschema:"поисковый запрос; слова ищутся с учётом русской морфологии, \"фраза в кавычках\" — точно, -слово — исключить"`
	Editions []int64 `json:"editions,omitempty" jsonschema:"номера изданий, чтобы искать только в них (см. fetch(\"/\"))"`
	Works    []int64 `json:"works,omitempty" jsonschema:"номера томов, чтобы искать только в них (число в начале адреса тома)"`
}

type SearchOutput struct {
	Summary string `json:"summary"`
	Results []Hit  `json:"results"`
}

type VolumeSearchInput struct {
	Query    string  `json:"query" jsonschema:"поисковый запрос, как у search"`
	Work     int64   `json:"work" jsonschema:"номер тома (число в начале адреса /works/114-…)"`
	Chapters []int64 `json:"chapters,omitempty" jsonschema:"номера глав этого тома, чтобы сузить поиск"`
	Offset   int     `json:"offset,omitempty" jsonschema:"сколько полос пропустить — для продолжения (next_offset)"`
}

type ChapterFacet struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	Hits  int    `json:"hits"`
}

type VolumeSearchOutput struct {
	Summary    string         `json:"summary"`
	Total      int            `json:"total"`
	Results    []Hit          `json:"results"`
	Chapters   []ChapterFacet `json:"chapters"`
	NextOffset int            `json:"next_offset,omitempty"`
}

// query — разбор запроса тем же правилом, что у /api/search.
func query(raw string) (string, error) {
	q := models.NormalizeSearchText(raw)
	if q == "" {
		return "", errUser(msgEmpty)
	}
	if models.SearchTextTooShort(q) {
		return "", errUser(models.SearchTooShortMessage)
	}
	return q, nil
}

// underSearch — f под воротами MCP и общим слотом поиска. defer, а не вызов
// после f: паника иначе унесла бы слоты навсегда.
func (s *service) underSearch(ctx context.Context, f func() error) error {
	rel, ok := limit.Nested(ctx, s.g.search, s.g.gateWait, s.d.SearchSlots, s.g.sharedWait)
	if !ok {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errBusy
	}
	defer rel()
	return f()
}

func (s *service) search(ctx context.Context, in SearchInput) (*SearchOutput, error) {
	q, err := query(in.Query)
	if err != nil {
		return nil, err
	}
	var res *models.SearchResult
	err = s.underSearch(ctx, func() error {
		var err error
		res, err = s.d.Search.Search(ctx, models.SearchQuery{Text: q, EditionIDs: in.Editions, WorkIDs: in.Works})
		return err
	})
	if err != nil {
		return nil, err
	}
	if len(res.Terms) == 0 {
		return nil, errUser(msgEmpty)
	}
	hits := res.FoundCount()
	s.record(stats.Row{Channel: stats.ChannelSearch, Kind: "search", Query: stats.CleanQuery(q), Hits: &hits})
	return s.compactSearch(res), nil
}

// compactSearch ужимает каталог до бюджета: сперва по одной полосе на том
// меньше, потом по тому меньше.
func (s *service) compactSearch(res *models.SearchResult) *SearchOutput {
	vols := append([]models.SearchVolume(nil), res.Volumes...)
	sort.SliceStable(vols, func(i, j int) bool { return vols[i].TextHits > vols[j].TextHits })
	nv, np := min(maxVolumes, len(vols)), pagesPerVolume
	for {
		out := s.buildSearch(res, vols, nv, np)
		raw, _ := json.Marshal(out)
		if utf8.RuneCount(raw) <= searchBudget || (nv <= 1 && np <= 1) {
			return out
		}
		if np > 1 {
			np--
		} else {
			nv--
		}
	}
}

func (s *service) buildSearch(res *models.SearchResult, vols []models.SearchVolume, nv, np int) *SearchOutput {
	out := &SearchOutput{Results: []Hit{}}
	for i, c := range res.Concepts {
		if i == maxConcepts {
			break
		}
		id := "/concepts/" + c.Slug
		out.Results = append(out.Results, Hit{ID: id, URL: s.base + id, Kind: "concept",
			Title: "Предметный указатель: " + c.Title})
	}
	for i, c := range res.Chapters {
		if i == maxChapters {
			break
		}
		id := seo.ChapterPath(c.WorkID, c.WorkSlug, c.ID, c.Slug)
		vol := joinNonEmpty(", ", c.EditionTitle, c.VolumeLabel)
		if vol == "" {
			vol = c.WorkTitle
		}
		out.Results = append(out.Results, Hit{ID: id, URL: s.base + id, Kind: "chapter",
			Title: c.Title + " — " + vol, Volume: vol, Apparatus: c.IsApparatus})
	}

	var sum strings.Builder
	fmt.Fprintf(&sum, "Совпадения в тексте: %d полос, томов с совпадениями: %d. ", res.TotalHits, len(vols))
	fmt.Fprintf(&sum, "Показаны %d томов с наибольшим числом совпадений в тексте, у каждого до %d полос с отрывками.\n", nv, np)
	for _, v := range vols[:nv] {
		name := volumeName(v)
		fmt.Fprintf(&sum, "- %s (work %d): в тексте %d, в примечаниях редакции %d\n", name, v.WorkID, v.TextHits, v.ApparatusHits)
		for k, p := range v.Pages {
			if k == np {
				break
			}
			out.Results = append(out.Results, s.pageHit(v.WorkID, v.WorkSlug, name, p))
		}
	}
	if rest := len(vols) - nv; rest > 0 {
		fmt.Fprintf(&sum, "Ещё %d томов с совпадениями: сузьте область параметром works или ищите внутри тома инструментом search_volume.", rest)
	}
	out.Summary = sum.String()
	return out
}

// pageHit — результат-полоса.
func (s *service) pageHit(workID int64, workSlug, volume string, p models.SearchPage) Hit {
	id := seo.PagePath(workID, workSlug, p.PageNumber)
	h := Hit{ID: id, URL: s.base + id, Kind: "page", Volume: volume, PrintedPage: p.PrintedNumber,
		Apparatus: p.IsApparatus, Snippet: snippet(p.Snippet),
		Title: fmt.Sprintf("%s, с. %d", volume, p.PrintedNumber)}
	if p.ChapterTitle != nil {
		h.Chapter = *p.ChapterTitle
		h.Title += " — " + h.Chapter
	}
	if p.IsApparatus {
		h.Title += " — примечания редакции, не текст автора"
	}
	return h
}

// volumeName — «ПСС Ленина, т. 43»; без номера тома — заглавие работы.
// Передние листы помечены: в выдаче они стоят рядом со своим томом.
func volumeName(v models.SearchVolume) string {
	name := v.Title
	if v.VolumeLabel != "" {
		name = joinNonEmpty(", ", v.EditionTitle, v.VolumeLabel)
	}
	if v.Role == models.WorkRoleFrontMatter {
		name += " (предваряющие материалы)"
	}
	return name
}

func joinNonEmpty(sep string, parts ...string) string {
	kept := parts[:0:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, sep)
}

// snippet — отрывок ts_headline для модели: границы совпадения — **, пробелы
// схлопнуты, длина — snippetRunes; разрезанное выделение закрывается.
func snippet(raw string) string {
	s := strings.NewReplacer("\x01", "**", "\x02", "**").Replace(raw)
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= snippetRunes {
		return s
	}
	s = string(r[:snippetRunes])
	if strings.Count(s, "**")%2 == 1 {
		s += "**"
	}
	return s + "…"
}

func (s *service) searchVolume(ctx context.Context, in VolumeSearchInput) (*VolumeSearchOutput, error) {
	q, err := query(in.Query)
	if err != nil {
		return nil, err
	}
	work, err := s.d.Works.GetByID(ctx, in.Work)
	if err != nil || work == nil {
		if err == nil || seo.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	offset := max(in.Offset, 0)
	var res *models.SearchPagesResult
	err = s.underSearch(ctx, func() error {
		var err error
		res, err = s.d.Search.SearchPages(ctx, models.SearchQuery{Text: q}, in.Work, in.Chapters, volumePageLimit, offset)
		return err
	})
	if errors.Is(err, repository.ErrSearchTimeout) {
		return nil, errUser(msgTimeoutVolume)
	}
	if err != nil {
		return nil, err
	}
	if len(res.Terms) == 0 {
		return nil, errUser(msgEmpty)
	}
	name := joinNonEmpty(". ", work.Author, work.Title)
	out := &VolumeSearchOutput{Total: res.Total, Results: []Hit{}, Chapters: []ChapterFacet{}}
	for _, p := range res.Pages {
		out.Results = append(out.Results, s.pageHit(work.ID, work.Slug, name, p))
	}
	for i, c := range res.Chapters {
		if i == maxVolumeChapters {
			break
		}
		out.Chapters = append(out.Chapters, ChapterFacet{ID: c.ID, Title: c.Title, Hits: c.Hits})
	}
	if next := offset + len(res.Pages); next < res.Total {
		out.NextOffset = next
	}
	out.Summary = fmt.Sprintf("%s: совпадений на %d полосах, показаны %d—%d. Глав с совпадениями: %d.",
		name, res.Total, offset+1, offset+len(res.Pages), res.ChaptersTotal)
	return out, nil
}
