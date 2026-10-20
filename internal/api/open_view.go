package api

import (
	"context"
	"fmt"

	"proofreader/internal/models"
)

// Открытый вид репозиториев работ и изданий — для каналов без токена
// (краулер /seo, тексты для нейросетей, llms.txt, MCP, OPDS, статическая
// копия, выгрузка не-сотруднику). Сотрудника там нет, поэтому закрытого не
// видит никто: закрытая работа и закрытое издание выглядят несуществующими.
//
// Отбор идёт по множествам ClosedStore (по id), а не по полю ClosedBy строки:
// поле заполняют не все запросы (ListWithoutEdition, например, его не несёт),
// и обёртка, судящая по полю, пропустила бы закрытое молча. Сбой хранилища
// закрытых — ошибка, а не «ничего не закрыто»: канал без токена иначе отдал бы
// закрытое всем. nil-хранилище — ничего не закрыто (экземпляр без закрытых
// работ), как у closedSetFor и fullClosedSet.

// openWorkSource — то, что открытый вид берёт у репозитория работ.
// *repository.WorkRepository удовлетворяет ему как есть.
type openWorkSource interface {
	GetByID(ctx context.Context, id int64) (*models.Work, error)
	List(ctx context.Context, limit, offset int, status *models.WorkStatus, ownerID *int64) ([]*models.Work, error)
	ListChildren(ctx context.Context, parentID int64) ([]*models.Work, error)
	ListWithoutEdition(ctx context.Context) ([]models.ShelfWork, error)
}

// OpenWorks — работы без закрытых.
type OpenWorks struct {
	works  openWorkSource
	closed ClosedStore
}

func NewOpenWorks(works openWorkSource, closed ClosedStore) *OpenWorks {
	return &OpenWorks{works: works, closed: closed}
}

func (o *OpenWorks) set(ctx context.Context) (ClosedSet, error) {
	set, err := fullClosedSet(ctx, o.closed)
	if err != nil {
		return nil, fmt.Errorf("закрытые работы: %w", err)
	}
	return set, nil
}

// GetByID закрытой работы отвечает ровно "work not found" — тем же текстом,
// что репозиторий на отсутствующую: на этот суффикс смотрят seo.isNotFound и
// isWorkNotFound, и закрытое уходит наружу 410/404, как снятое.
func (o *OpenWorks) GetByID(ctx context.Context, id int64) (*models.Work, error) {
	if o.closed != nil {
		closedBy, err := o.closed.ClosedBy(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("закрытость работы %d: %w", id, err)
		}
		if closedBy != "" {
			return nil, fmt.Errorf("work not found")
		}
	}
	return o.works.GetByID(ctx, id)
}

// List отбирает после LIMIT: страница выходит короче. Каналам без токена
// разбивка List не нужна — ни один из них её не листает.
func (o *OpenWorks) List(ctx context.Context, limit, offset int, status *models.WorkStatus, ownerID *int64) ([]*models.Work, error) {
	set, err := o.set(ctx)
	if err != nil {
		return nil, err
	}
	list, err := o.works.List(ctx, limit, offset, status, ownerID)
	if err != nil {
		return nil, err
	}
	return openWorksOf(list, set), nil
}

func (o *OpenWorks) ListChildren(ctx context.Context, parentID int64) ([]*models.Work, error) {
	set, err := o.set(ctx)
	if err != nil {
		return nil, err
	}
	list, err := o.works.ListChildren(ctx, parentID)
	if err != nil {
		return nil, err
	}
	return openWorksOf(list, set), nil
}

func (o *OpenWorks) ListWithoutEdition(ctx context.Context) ([]models.ShelfWork, error) {
	set, err := o.set(ctx)
	if err != nil {
		return nil, err
	}
	list, err := o.works.ListWithoutEdition(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]models.ShelfWork, 0, len(list))
	for _, w := range list {
		if !set.Has(w.ID) {
			out = append(out, w)
		}
	}
	return out, nil
}

func openWorksOf(list []*models.Work, set ClosedSet) []*models.Work {
	out := make([]*models.Work, 0, len(list))
	for _, w := range list {
		if w != nil && !set.Has(w.ID) {
			out = append(out, w)
		}
	}
	return out
}

// openEditionSource — то, что открытый вид берёт у репозитория изданий.
// *repository.EditionRepository удовлетворяет ему как есть.
type openEditionSource interface {
	GetByID(ctx context.Context, id int64) (*models.Edition, error)
	List(ctx context.Context) ([]*models.Edition, error)
	ListWorks(ctx context.Context, editionID int64) ([]*models.Work, error)
	ListWorkSummaries(ctx context.Context, editionID int64) ([]*models.VolumeSummary, error)
	ListAllWorkSummaries(ctx context.Context) ([]*models.VolumeSummary, error)
	ListAllHighlights(ctx context.Context) ([]models.EditionHighlight, error)
}

// OpenEditions — издания без закрытых, тома изданий без закрытых томов.
// Закрытый том открытого издания отсеивается множеством работ, тома
// закрытого издания — им же (closed_works их включает).
type OpenEditions struct {
	editions openEditionSource
	closed   ClosedStore
}

func NewOpenEditions(editions openEditionSource, closed ClosedStore) *OpenEditions {
	return &OpenEditions{editions: editions, closed: closed}
}

func (o *OpenEditions) closedEditions(ctx context.Context) (ClosedSet, error) {
	if o.closed == nil {
		return nil, nil
	}
	ids, err := o.closed.ClosedEditionIDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("закрытые издания: %w", err)
	}
	return ClosedSet(ids), nil
}

func (o *OpenEditions) closedWorks(ctx context.Context) (ClosedSet, error) {
	set, err := fullClosedSet(ctx, o.closed)
	if err != nil {
		return nil, fmt.Errorf("закрытые работы: %w", err)
	}
	return set, nil
}

// GetByID закрытого издания отвечает ровно "edition not found" — текстом
// репозитория на отсутствующее (см. OpenWorks.GetByID).
func (o *OpenEditions) GetByID(ctx context.Context, id int64) (*models.Edition, error) {
	set, err := o.closedEditions(ctx)
	if err != nil {
		return nil, err
	}
	if set.Has(id) {
		return nil, fmt.Errorf("edition not found")
	}
	return o.editions.GetByID(ctx, id)
}

func (o *OpenEditions) List(ctx context.Context) ([]*models.Edition, error) {
	set, err := o.closedEditions(ctx)
	if err != nil {
		return nil, err
	}
	list, err := o.editions.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*models.Edition, 0, len(list))
	for _, e := range list {
		if e != nil && !set.Has(e.ID) {
			out = append(out, e)
		}
	}
	return out, nil
}

func (o *OpenEditions) ListWorks(ctx context.Context, editionID int64) ([]*models.Work, error) {
	set, err := o.closedWorks(ctx)
	if err != nil {
		return nil, err
	}
	list, err := o.editions.ListWorks(ctx, editionID)
	if err != nil {
		return nil, err
	}
	return openWorksOf(list, set), nil
}

func (o *OpenEditions) ListWorkSummaries(ctx context.Context, editionID int64) ([]*models.VolumeSummary, error) {
	set, err := o.closedWorks(ctx)
	if err != nil {
		return nil, err
	}
	list, err := o.editions.ListWorkSummaries(ctx, editionID)
	if err != nil {
		return nil, err
	}
	return openSummariesOf(list, set), nil
}

func (o *OpenEditions) ListAllWorkSummaries(ctx context.Context) ([]*models.VolumeSummary, error) {
	set, err := o.closedWorks(ctx)
	if err != nil {
		return nil, err
	}
	list, err := o.editions.ListAllWorkSummaries(ctx)
	if err != nil {
		return nil, err
	}
	return openSummariesOf(list, set), nil
}

// ListAllHighlights — избранное без пунктов закрытых томов и закрытых изданий.
func (o *OpenEditions) ListAllHighlights(ctx context.Context) ([]models.EditionHighlight, error) {
	works, err := o.closedWorks(ctx)
	if err != nil {
		return nil, err
	}
	editions, err := o.closedEditions(ctx)
	if err != nil {
		return nil, err
	}
	list, err := o.editions.ListAllHighlights(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]models.EditionHighlight, 0, len(list))
	for _, h := range list {
		if !works.Has(h.WorkID) && !editions.Has(h.EditionID) {
			out = append(out, h)
		}
	}
	return out, nil
}

func openSummariesOf(list []*models.VolumeSummary, set ClosedSet) []*models.VolumeSummary {
	out := make([]*models.VolumeSummary, 0, len(list))
	for _, s := range list {
		if s != nil && !set.Has(s.ID) {
			out = append(out, s)
		}
	}
	return out
}
