package api

import (
	"context"
	"fmt"
	"net/http"

	"proofreader/internal/models"
	"proofreader/internal/repository"
	"proofreader/internal/seo"
)

// Закрытое в предметном указателе. Понятие каталога составлено из статей
// указателей разных изданий, а адреса статьи — ссылки на полосы томов, поэтому
// закрытость доходит до него двумя путями: закрыто издание статьи (или её
// работа-указатель), либо адрес разрешается в закрытый том. Скрытый адрес
// исчезает целиком — ни текста, ни номера страницы, ни названия тома: иначе
// «т. 50, с. 12» рассказало бы, что том 50 есть.

// closedView — что скрыто от этого читателя: закрытые издания и закрытые
// работы. Нулевое значение ничего не скрывает (сотрудник, экземпляр без
// хранилища закрытого).
type closedView struct {
	editions map[int64]bool
	works    ClosedSet
	// chapterWorks — работы, у которых закрыта хоть одна глава; spans читает
	// их диапазоны (nil у хранилища без закрытых глав), spanCache держит
	// прочитанное на время запроса.
	chapterWorks map[int64]bool
	spans        ClosedSpansStore
	spanCache    map[int64][]models.PageSpan
}

func (v closedView) none() bool {
	return len(v.editions) == 0 && len(v.works) == 0 && len(v.chapterWorks) == 0
}

// chapterWorksStore — необязательное умение хранилища закрытого: какие работы
// несут закрытые главы. Есть у *repository.ClosedRepository.
type chapterWorksStore interface {
	WorksWithClosedChapters(ctx context.Context) (map[int64]bool, error)
}

var (
	_ chapterWorksStore = (*repository.ClosedRepository)(nil)
	_ ClosedSpansStore  = (*repository.ClosedRepository)(nil)
)

// closedPagesFunc читает диапазоны закрытых глав тех работ, что в них нуждаются
// (workIDs), и возвращает closedPages для visibleConcept. Ничего не закрыто —
// nil. Диапазон работы читается один раз на запрос.
func (v closedView) closedPagesFunc(ctx context.Context, workIDs []int64) (func(int64, int) bool, error) {
	if len(v.chapterWorks) == 0 || v.spans == nil {
		return nil, nil
	}
	for _, id := range workIDs {
		if !v.chapterWorks[id] {
			continue
		}
		if _, ok := v.spanCache[id]; ok {
			continue
		}
		sp, err := v.spans.ClosedSpans(ctx, id)
		if err != nil {
			return nil, err
		}
		v.spanCache[id] = sp
	}
	return func(workID int64, page int) bool { return inSpans(v.spanCache[workID], page) }, nil
}

// closedViewFor — закрытое для запроса API: сотруднику не скрывается ничего.
func closedViewFor(r *http.Request, store ClosedStore) (closedView, error) {
	if store == nil || isStaffRequest(r) {
		return closedView{}, nil
	}
	return fullClosedView(r.Context(), store)
}

// fullClosedView — всё закрытое, без оглядки на токен. Для каналов, где
// сотрудника нет вовсе (.md и MCP, страницы краулера, статическая копия).
func fullClosedView(ctx context.Context, store ClosedStore) (closedView, error) {
	if store == nil {
		return closedView{}, nil
	}
	works, err := store.ClosedIDs(ctx)
	if err != nil {
		return closedView{}, err
	}
	editions, err := store.ClosedEditionIDs(ctx)
	if err != nil {
		return closedView{}, err
	}
	v := closedView{editions: editions, works: ClosedSet(works)}
	if cw, ok := store.(chapterWorksStore); ok {
		if v.chapterWorks, err = cw.WorksWithClosedChapters(ctx); err != nil {
			return closedView{}, err
		}
		v.spans, _ = store.(ClosedSpansStore)
		v.spanCache = map[int64][]models.PageSpan{}
	}
	return v, nil
}

// visibleConcept — копия понятия без статей закрытых изданий и закрытых работ
// (a.work_id) и без адресов, что разрешаются в закрытую работу. vols — карты
// томов по изданиям статей. closedPages — закрыта ли полоса работы (закрытая
// глава накрывает её); nil — полосы не закрыты. Адрес, чей диапазон хоть
// одной полосой попадает в закрытую, скрыт; статья, чей источник
// [SourcePageStart, SourcePageEnd] (при Start > 0) накрыт закрытой полосой,
// уходит целиком. Второй результат false только тогда, когда
// фильтр что-то СНЯЛ и от понятия ничего не осталось — «ни видимой статьи, ни
// видимого адреса»; понятие, пустое с самого начала (у ленинского указателя
// нет статей-текстов, только адреса — но и у него есть статья-носитель),
// возвращается как было. Статья, у которой все адреса скрыты, а своего текста
// нет, ничего не несёт и уходит вместе с ними — иначе понятие, чьи адреса
// лежат целиком в закрытом томе, оставалось бы пустой оболочкой с заглавием.
func visibleConcept(c *models.IndexConcept, closedEditions map[int64]bool, closedWorks ClosedSet, vols map[int64][]models.VolumeLocation, closedPages func(workID int64, page int) bool) (*models.IndexConcept, bool) {
	if c == nil {
		return nil, false
	}
	if len(closedEditions) == 0 && len(closedWorks) == 0 && closedPages == nil {
		return c, true
	}
	out := *c
	out.Articles = make([]*models.IndexArticle, 0, len(c.Articles))
	removed := false
	for _, a := range c.Articles {
		if closedEditions[a.EditionID] || (a.WorkID != nil && closedWorks.Has(*a.WorkID)) ||
			(a.WorkID != nil && a.SourcePageStart > 0 && closedPages != nil &&
				anyPageClosed(closedPages, *a.WorkID, a.SourcePageStart, a.SourcePageEnd)) {
			removed = true
			continue
		}
		na := *a
		if len(a.References) > 0 {
			volMap := buildVolumeMap(vols[a.EditionID])
			kept := make([]*models.IndexReference, 0, len(a.References))
			for _, ref := range a.References {
				if loc, ok := volMap[keyOf(ref.VolumeNumber, ref.VolumePart)]; ok &&
					(closedWorks.Has(loc.WorkID) ||
						(closedPages != nil && anyPageClosed(closedPages, loc.WorkID, ref.PageStart-loc.PageOffset, ref.PageEnd-loc.PageOffset))) {
					continue
				}
				kept = append(kept, ref)
			}
			if len(kept) < len(a.References) {
				removed = true
				if len(kept) == 0 && models.IsBlankArticleText(a.ArticleMarkdown) {
					continue
				}
			}
			na.References = kept
		}
		out.Articles = append(out.Articles, &na)
	}
	if !removed {
		return c, true
	}
	if len(out.Articles) == 0 {
		return nil, false
	}
	// Издания, статей которых не осталось, из агрегата уходят: id закрытого
	// собрания сам по себе говорит, что оно есть.
	if len(c.EditionIDs) > 0 {
		out.EditionIDs = nil
		for _, a := range out.Articles {
			out.EditionIDs = appendUniqueInt64(out.EditionIDs, a.EditionID)
		}
	}
	return &out, true
}

// anyPageClosed — закрыта ли хоть одна полоса [from, to] (to < from читается
// как одна полоса from). Страница ниже первой закрытой быть не может.
func anyPageClosed(closedPages func(int64, int) bool, workID int64, from, to int) bool {
	if to < from {
		to = from
	}
	for p := max(from, 1); p <= to; p++ {
		if closedPages(workID, p) {
			return true
		}
	}
	return false
}

func appendUniqueInt64(xs []int64, x int64) []int64 {
	for _, y := range xs {
		if y == x {
			return xs
		}
	}
	return append(xs, x)
}

// conceptLookup — то, что фильтру нужно от указателя.
type conceptLookup interface {
	GetConceptBySlug(ctx context.Context, slug string) (*models.IndexConcept, error)
	VolumeMap(ctx context.Context, editionID int64) ([]models.VolumeLocation, error)
}

// filter применяет закрытое к понятию; карты томов берутся только для статей,
// которые не ушли целиком, и только когда что-то закрыто.
func (v closedView) filter(ctx context.Context, idx volumeMapper, c *models.IndexConcept) (*models.IndexConcept, bool, error) {
	if c == nil {
		return nil, false, nil
	}
	if v.none() {
		return c, true, nil
	}
	vols := map[int64][]models.VolumeLocation{}
	var chapterWorkIDs []int64
	for _, a := range c.Articles {
		if a.WorkID != nil {
			chapterWorkIDs = append(chapterWorkIDs, *a.WorkID)
		}
		if v.editions[a.EditionID] || len(a.References) == 0 {
			continue
		}
		if _, ok := vols[a.EditionID]; ok {
			continue
		}
		locs, err := idx.VolumeMap(ctx, a.EditionID)
		if err != nil {
			return nil, false, err
		}
		vols[a.EditionID] = locs
		for _, l := range locs {
			chapterWorkIDs = append(chapterWorkIDs, l.WorkID)
		}
	}
	closedPages, err := v.closedPagesFunc(ctx, chapterWorkIDs)
	if err != nil {
		return nil, false, err
	}
	out, ok := visibleConcept(c, v.editions, v.works, vols, closedPages)
	return out, ok, nil
}

// hiddenStore — какие из слагов скрыты закрытым (IndexRepository.HiddenConceptSlugs).
type hiddenStore interface {
	HiddenConceptSlugs(ctx context.Context, slugs []string) (map[string]bool, error)
}

// hiddenSlugs — какие из слагов скрыты, ОДНИМ запросом на пачку. Когда для
// читателя ничего не закрыто (сотрудник, нет хранилища, нет закрытого), в базу
// не ходит. Ошибка не читается как «не скрыто»: отвечает 500.
func (v closedView) hiddenSlugs(ctx context.Context, idx hiddenStore, slugs []string) (map[string]bool, error) {
	if v.none() || len(slugs) == 0 {
		return map[string]bool{}, nil
	}
	return idx.HiddenConceptSlugs(ctx, slugs)
}

// TokenlessIndex — указатель для каналов без токена: текст понятия по .md и
// MCP, страницы и карточки краулера, статическая копия. Сотрудника на них нет,
// поэтому закрытое скрывается всегда, целиком. Удовлетворяет ConceptIndex,
// StaticIndex и seo.ConceptSource.
type TokenlessIndex struct {
	index  tokenlessInner
	closed ClosedStore
}

type tokenlessInner interface {
	conceptLookup
	hiddenStore
	ArticleLinks(ctx context.Context, articleID int64) ([]repository.LinkWithTarget, error)
	ListConcepts(ctx context.Context, query, letter string, limit, offset int, includeClosed bool) ([]*models.IndexConcept, error)
}

func NewTokenlessIndex(index tokenlessInner, closed ClosedStore) *TokenlessIndex {
	return &TokenlessIndex{index: index, closed: closed}
}

func (t *TokenlessIndex) view(ctx context.Context) (closedView, error) {
	return fullClosedView(ctx, t.closed)
}

// GetConceptBySlug — скрытое понятие отвечает так же, как несуществующее.
func (t *TokenlessIndex) GetConceptBySlug(ctx context.Context, slug string) (*models.IndexConcept, error) {
	c, err := t.index.GetConceptBySlug(ctx, slug)
	if err != nil || c == nil {
		return c, err
	}
	v, err := t.view(ctx)
	if err != nil {
		return nil, err
	}
	out, ok, err := v.filter(ctx, t.index, c)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errConceptHidden
	}
	return out, nil
}

// errConceptHidden заворачивает seo.ErrNotFound: isNotFound (internal/seo)
// узнаёт отсутствие по цепочке, а не по тексту — страница отвечает 410.
var errConceptHidden = fmt.Errorf("concept %w", seo.ErrNotFound)

func (t *TokenlessIndex) VolumeMap(ctx context.Context, editionID int64) ([]models.VolumeLocation, error) {
	return t.index.VolumeMap(ctx, editionID)
}

// ListConcepts — закрытое исключено всегда.
func (t *TokenlessIndex) ListConcepts(ctx context.Context, query, letter string, limit, offset int) ([]*models.IndexConcept, error) {
	return t.index.ListConcepts(ctx, query, letter, limit, offset, false)
}

// ArticleLinks — без отсылок на скрытые понятия.
func (t *TokenlessIndex) ArticleLinks(ctx context.Context, articleID int64) ([]repository.LinkWithTarget, error) {
	links, err := t.index.ArticleLinks(ctx, articleID)
	if err != nil || len(links) == 0 {
		return links, err
	}
	// Закрытое здесь скрыто всегда, поэтому вид закрытого не читается:
	// один запрос о скрытых слагах отвечает и за «ничего не закрыто».
	slugs := make([]string, 0, len(links))
	for _, l := range links {
		slugs = append(slugs, l.TargetSlug)
	}
	hidden, err := t.index.HiddenConceptSlugs(ctx, slugs)
	if err != nil {
		return nil, err
	}
	out := make([]repository.LinkWithTarget, 0, len(links))
	for _, l := range links {
		if !hidden[l.TargetSlug] {
			out = append(out, l)
		}
	}
	return out, nil
}
