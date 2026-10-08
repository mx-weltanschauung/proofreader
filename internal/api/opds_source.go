package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"proofreader/internal/models"
	"proofreader/internal/opds"
	"proofreader/internal/repository"
	"proofreader/internal/seo"
	"proofreader/internal/stats"
)

// OPDSEditions — то, что каталогу нужно от собраний: список и сводки томов
// (тот же запрос, что у /api/shelf — workSummariesQueryTemplate не форкать).
type OPDSEditions interface {
	List(ctx context.Context) ([]*models.Edition, error)
	GetByID(ctx context.Context, id int64) (*models.Edition, error)
	ListAllWorkSummaries(ctx context.Context) ([]*models.VolumeSummary, error)
}

// OPDSWorks — тома: один по номеру и тома вне собраний.
type OPDSWorks interface {
	GetByID(ctx context.Context, id int64) (*models.Work, error)
	ListWithoutEdition(ctx context.Context) ([]models.ShelfWork, error)
}

// OPDSChapters — дерево глав тома.
type OPDSChapters interface {
	ListByWorkHierarchical(ctx context.Context, workID int64) ([]*models.Chapter, error)
}

// OPDSCatalog — метки полос и выборки томов с полосами
// (*repository.OPDSRepository). Выборки отсеивают тома без полос сами, до
// LIMIT, — иначе разбивка «Новых поступлений» разъезжалась на время каждой
// публикации тома.
type OPDSCatalog interface {
	VolumeStamps(ctx context.Context, workIDs []int64) (map[int64]repository.PageStamp, error)
	ChapterStamps(ctx context.Context, workID int64) (map[int64]repository.PageStamp, error)
	RecentVolumes(ctx context.Context, limit, offset int) ([]*models.Work, error)
	VolumesByID(ctx context.Context, ids []int64) ([]*models.Work, error)
}

// OPDSSource переводит репозитории читальни в данные каталога OPDS
// (internal/opds). Метка updated каждой записи — формула CacheKey выгрузки
// (download_source.go): у тома max(полосы, works.updated_at), у главы
// max(полосы диапазона, chapters.updated_at, works.updated_at).
type OPDSSource struct {
	editions OPDSEditions
	works    OPDSWorks
	chapters OPDSChapters
	catalog  OPDSCatalog
	search   *SearchHandler
}

func NewOPDSSource(editions OPDSEditions, works OPDSWorks, chapters OPDSChapters,
	catalog OPDSCatalog, search *SearchHandler) *OPDSSource {
	return &OPDSSource{editions: editions, works: works, chapters: chapters, catalog: catalog, search: search}
}

var _ opds.Source = (*OPDSSource)(nil)

func opdsVolume(w *models.Work, editionTitle string, st repository.PageStamp) opds.Volume {
	v := opds.Volume{
		ID: w.ID, Title: w.Title, Author: w.Author, Lang: w.Language,
		EditionTitle: editionTitle, VolumeLabel: volumeLabel(w),
		PageFrom: st.First + w.PageOffset, PageTo: st.Last + w.PageOffset,
		HTMLPath: seo.WorkPath(w.ID, w.Slug),
		Updated:  latestOf(st.Updated, w.UpdatedAt),
	}
	if w.EditionID != nil {
		v.EditionID = *w.EditionID
	}
	if w.PublicationDate != nil {
		v.Year = w.PublicationDate.Year()
	}
	return v
}

// isVolume — том верхнего уровня, а не передние листы. Каталог показывает
// только тома: служебные работы доступны с карточки тома в читальне.
func isVolume(w *models.Work) bool {
	return w != nil && w.ParentWorkID == nil && w.Role == models.WorkRoleVolume
}

func isWorkNotFound(err error) bool {
	return err != nil && strings.HasSuffix(err.Error(), "not found")
}

func (s *OPDSSource) editionTitles(ctx context.Context) (map[int64]string, []*models.Edition, error) {
	eds, err := s.editions.List(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("собрания: %w", err)
	}
	titles := make(map[int64]string, len(eds))
	for _, e := range eds {
		titles[e.ID] = e.Title
	}
	return titles, eds, nil
}

// volumes — тома с метками; тома без полос выпадают (их выгрузка — 404).
func (s *OPDSSource) volumes(ctx context.Context, works []*models.Work, titles map[int64]string) ([]opds.Volume, error) {
	ids := make([]int64, 0, len(works))
	for _, w := range works {
		ids = append(ids, w.ID)
	}
	stamps, err := s.catalog.VolumeStamps(ctx, ids)
	if err != nil {
		return nil, err
	}
	var out []opds.Volume
	for _, w := range works {
		st, ok := stamps[w.ID]
		if !ok || !isVolume(w) {
			continue
		}
		title := ""
		if w.EditionID != nil {
			title = titles[*w.EditionID]
		}
		out = append(out, opdsVolume(w, title, st))
	}
	return out, nil
}

func (s *OPDSSource) Shelf(ctx context.Context) (*opds.Shelf, error) {
	titles, eds, err := s.editionTitles(ctx)
	if err != nil {
		return nil, err
	}
	sums, err := s.editions.ListAllWorkSummaries(ctx)
	if err != nil {
		return nil, fmt.Errorf("тома собраний: %w", err)
	}
	loose, err := s.works.ListWithoutEdition(ctx)
	if err != nil {
		return nil, fmt.Errorf("тома вне собраний: %w", err)
	}

	works := make([]*models.Work, 0, len(sums)+len(loose))
	for _, sm := range sums {
		w := sm.Work
		works = append(works, &w)
	}
	// Тома вне собраний в корпусе редки (локально 0): полная строка — по
	// одному запросу на том, сводка их не несёт.
	for _, lw := range loose {
		w, err := s.works.GetByID(ctx, lw.ID)
		if err != nil {
			if isWorkNotFound(err) {
				continue // снят между двумя запросами
			}
			return nil, err
		}
		works = append(works, w)
	}
	vols, err := s.volumes(ctx, works, titles)
	if err != nil {
		return nil, err
	}

	byEdition := map[int64][]opds.Volume{}
	shelf := &opds.Shelf{}
	for _, v := range vols {
		if v.EditionID == 0 {
			shelf.Loose = append(shelf.Loose, v)
		} else {
			byEdition[v.EditionID] = append(byEdition[v.EditionID], v)
		}
	}
	for _, e := range eds {
		shelf.Editions = append(shelf.Editions, opds.Edition{ID: e.ID, Title: e.Title, Volumes: byEdition[e.ID]})
	}
	return shelf, nil
}

// Recent — тома по works.created_at, свежие сверху. Тома без полос (на
// боевом — заведённый публикатором и ещё не залитый) отсеивает сам запрос до
// LIMIT: отсев после LIMIT давал страницу на запись короче, next пропадал
// на всё время заливки, а следующий том повторялся на второй странице.
func (s *OPDSSource) Recent(ctx context.Context, limit, offset int) ([]opds.Volume, error) {
	titles, _, err := s.editionTitles(ctx)
	if err != nil {
		return nil, err
	}
	works, err := s.catalog.RecentVolumes(ctx, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("новые тома: %w", err)
	}
	return s.volumes(ctx, works, titles)
}

func (s *OPDSSource) Tree(ctx context.Context, workID int64) (*opds.Tree, error) {
	w, err := s.works.GetByID(ctx, workID)
	if isWorkNotFound(err) || (err == nil && !isVolume(w)) {
		return nil, opds.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	title := ""
	if w.EditionID != nil {
		ed, err := s.editions.GetByID(ctx, *w.EditionID)
		if err != nil {
			return nil, fmt.Errorf("собрание тома %d: %w", w.ID, err)
		}
		title = ed.Title
	}
	vs, err := s.volumes(ctx, []*models.Work{w}, map[int64]string{derefID(w.EditionID): title})
	if err != nil {
		return nil, err
	}
	if len(vs) == 0 {
		return nil, opds.ErrNotFound // том без полос
	}
	chapters, err := s.chapters.ListByWorkHierarchical(ctx, w.ID)
	if err != nil {
		return nil, fmt.Errorf("главы тома %d: %w", w.ID, err)
	}
	cst, err := s.catalog.ChapterStamps(ctx, w.ID)
	if err != nil {
		return nil, err
	}
	return &opds.Tree{Volume: vs[0], Nodes: opdsNodes(w, chapters, cst)}, nil
}

func derefID(id *int64) int64 {
	if id == nil {
		return 0
	}
	return *id
}

func opdsNodes(w *models.Work, chapters []*models.Chapter, cst map[int64]repository.PageStamp) []*opds.Node {
	var out []*opds.Node
	for _, c := range chapters {
		st := cst[c.ID]
		out = append(out, &opds.Node{
			ID: c.ID, Title: c.Title, IsApparatus: c.IsApparatus,
			PageFrom: c.StartPage + w.PageOffset, PageTo: c.EndPage + w.PageOffset,
			HTMLPath: seo.ChapterPath(w.ID, w.Slug, c.ID, c.Slug),
			Updated:  latestOf(st.Updated, c.UpdatedAt, w.UpdatedAt),
			HasPages: st.Pages > 0,
			Children: opdsNodes(w, c.Children, cst),
		})
	}
	return out
}

// Search — тот же поиск, что у сайта: та же нормализация, те же два слота,
// тот же statement_timeout и те же слова отказа (searchRefusal). Счёт —
// channel = search, agent = opds: как у MCP, в «Что ищут» не попадает.
func (s *OPDSSource) Search(r *http.Request, q string, chapters bool) (*opds.SearchResult, error) {
	q = models.NormalizeSearchText(q)
	if q == "" {
		return nil, &opds.RequestError{Status: http.StatusBadRequest, Message: "Пустой запрос"}
	}
	if models.SearchTextTooShort(q) {
		return nil, &opds.RequestError{Status: http.StatusBadRequest, Message: models.SearchTooShortMessage}
	}
	ctx := r.Context()
	var res *models.SearchResult
	err := s.search.underSlot(r, func() error {
		var err error
		res, err = s.search.store.Search(ctx, models.SearchQuery{Text: q})
		return err
	})
	if err != nil {
		if status, msg, retry, ok := searchRefusal(err); ok {
			return nil, &opds.RequestError{Status: status, Message: msg, RetryAfter: retry}
		}
		return nil, err
	}
	if len(res.Terms) == 0 {
		return nil, &opds.RequestError{Status: http.StatusBadRequest, Message: "Пустой запрос"}
	}
	if s.search.rec != nil && stats.BotFamily(r.UserAgent()) == "" {
		hits := res.FoundCount()
		s.search.rec.Record(stats.Event{
			Row: stats.Row{TS: time.Now(), Channel: stats.ChannelSearch, Kind: "search",
				Agent: "opds", Query: stats.CleanQuery(q), Hits: &hits},
			IP: clientIP(r, s.search.trustProxy), UA: r.UserAgent(),
		})
	}

	out := &opds.SearchResult{}
	trees := map[int64]*opds.Tree{}
	hits := res.Chapters
	if !chapters {
		hits = nil // дерево тома — пять запросов; странице без глав они ни к чему
	}
	for _, c := range hits {
		if c.IsApparatus {
			continue
		}
		tree, seen := trees[c.WorkID]
		if !seen {
			t, err := s.Tree(ctx, c.WorkID)
			if err != nil && !errors.Is(err, opds.ErrNotFound) {
				return nil, err
			}
			tree, trees[c.WorkID] = t, t // nil — передние листы или нет полос
		}
		if tree != nil {
			out.Chapters = append(out.Chapters, opds.ChapterHit{Tree: tree, ChapterID: c.ID})
		}
	}

	// Тома выдачи — строками только этих томов, а не полкой по всему корпусу:
	// поиск по редкому слову находит один-два тома, и сводки с метками всех
	// 175 ради них шли бы мимо слота поиска на каждой странице.
	var ids []int64
	for _, sv := range res.Volumes {
		if sv.TextHits > 0 {
			ids = append(ids, sv.WorkID)
		}
	}
	if len(ids) > 0 {
		titles, _, err := s.editionTitles(ctx)
		if err != nil {
			return nil, err
		}
		works, err := s.catalog.VolumesByID(ctx, ids)
		if err != nil {
			return nil, err
		}
		vols, err := s.volumes(ctx, works, titles)
		if err != nil {
			return nil, err
		}
		byID := make(map[int64]opds.Volume, len(vols))
		for _, v := range vols {
			byID[v.ID] = v
		}
		for _, sv := range res.Volumes {
			if v, ok := byID[sv.WorkID]; ok && sv.TextHits > 0 {
				out.Volumes = append(out.Volumes, opds.VolumeHit{Volume: v, TextHits: sv.TextHits})
			}
		}
	}
	return out, nil
}
