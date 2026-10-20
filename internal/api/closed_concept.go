package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"proofreader/internal/models"
	"proofreader/internal/repository"
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
}

func (v closedView) none() bool { return len(v.editions) == 0 && len(v.works) == 0 }

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
	return closedView{editions: editions, works: ClosedSet(works)}, nil
}

// visibleConcept — копия понятия без статей закрытых изданий и закрытых работ
// (a.work_id) и без адресов, что разрешаются в закрытую работу. vols — карты
// томов по изданиям статей. Второй результат false только тогда, когда
// фильтр что-то СНЯЛ и от понятия ничего не осталось — «ни видимой статьи, ни
// видимого адреса»; понятие, пустое с самого начала (у ленинского указателя
// нет статей-текстов, только адреса — но и у него есть статья-носитель),
// возвращается как было. Статья, у которой все адреса скрыты, а своего текста
// нет, ничего не несёт и уходит вместе с ними — иначе понятие, чьи адреса
// лежат целиком в закрытом томе, оставалось бы пустой оболочкой с заглавием.
func visibleConcept(c *models.IndexConcept, closedEditions map[int64]bool, closedWorks ClosedSet, vols map[int64][]models.VolumeLocation) (*models.IndexConcept, bool) {
	if c == nil {
		return nil, false
	}
	if len(closedEditions) == 0 && len(closedWorks) == 0 {
		return c, true
	}
	out := *c
	out.Articles = make([]*models.IndexArticle, 0, len(c.Articles))
	removed := false
	for _, a := range c.Articles {
		if closedEditions[a.EditionID] || (a.WorkID != nil && closedWorks.Has(*a.WorkID)) {
			removed = true
			continue
		}
		na := *a
		if len(a.References) > 0 {
			volMap := buildVolumeMap(vols[a.EditionID])
			kept := make([]*models.IndexReference, 0, len(a.References))
			for _, ref := range a.References {
				if loc, ok := volMap[keyOf(ref.VolumeNumber, ref.VolumePart)]; ok && closedWorks.Has(loc.WorkID) {
					continue
				}
				kept = append(kept, ref)
			}
			if len(kept) < len(a.References) {
				removed = true
				if len(kept) == 0 && strings.TrimSpace(a.ArticleMarkdown) == "" {
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
	for _, a := range c.Articles {
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
	}
	out, ok := visibleConcept(c, v.editions, v.works, vols)
	return out, ok, nil
}

// hidden — скрыто ли понятие с этим слагом; нет такого понятия — не скрыто
// (отсылка в никуда остаётся как была).
func (v closedView) hidden(ctx context.Context, idx conceptLookup, slug string, cache map[string]bool) (bool, error) {
	if v.none() || slug == "" {
		return false, nil
	}
	if h, ok := cache[slug]; ok {
		return h, nil
	}
	c, err := idx.GetConceptBySlug(ctx, slug)
	if err != nil || c == nil {
		cache[slug] = false
		return false, nil
	}
	_, ok, err := v.filter(ctx, idx, c)
	if err != nil {
		return false, err
	}
	cache[slug] = !ok
	return !ok, nil
}

// filterLinks убирает отсылки на скрытые понятия: заглавие и слаг цели — тот
// же рассказ о закрытом.
func (v closedView) filterLinks(ctx context.Context, idx conceptLookup, links []repository.LinkWithTarget, cache map[string]bool) ([]repository.LinkWithTarget, error) {
	if v.none() {
		return links, nil
	}
	out := make([]repository.LinkWithTarget, 0, len(links))
	for _, l := range links {
		h, err := v.hidden(ctx, idx, l.TargetSlug, cache)
		if err != nil {
			return nil, err
		}
		if !h {
			out = append(out, l)
		}
	}
	return out, nil
}

func (v closedView) filterIncoming(ctx context.Context, idx conceptLookup, links []repository.IncomingLink, cache map[string]bool) ([]repository.IncomingLink, error) {
	if v.none() {
		return links, nil
	}
	out := make([]repository.IncomingLink, 0, len(links))
	for _, l := range links {
		h, err := v.hidden(ctx, idx, l.Slug, cache)
		if err != nil {
			return nil, err
		}
		if !h {
			out = append(out, l)
		}
	}
	return out, nil
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

// errConceptHidden — тот же текст, что у репозитория: isNotFound (internal/seo)
// узнаёт отсутствие по суффиксу «not found».
var errConceptHidden = errors.New("concept not found")

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
	if err != nil {
		return nil, err
	}
	v, err := t.view(ctx)
	if err != nil {
		return nil, err
	}
	return v.filterLinks(ctx, t.index, links, map[string]bool{})
}
