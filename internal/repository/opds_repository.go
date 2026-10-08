package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"proofreader/internal/models"
)

// OPDSRepository — метки каталога OPDS одним запросом на ленту. Каталог
// показывает у каждой записи updated той же формулой, что ETag её файла
// (book.Meta.CacheKey в api.DownloadSource), но сборка книги ради метки
// стоила бы рендера на запись — у тома 50 Ленина их 654.
type OPDSRepository struct {
	pool *pgxpool.Pool
}

func NewOPDSRepository(pool *pgxpool.Pool) *OPDSRepository {
	return &OPDSRepository{pool: pool}
}

// PageStamp — что каталогу нужно знать о полосах тома или главы.
type PageStamp struct {
	Updated time.Time // max(pages.updated_at)
	First   int       // наименьший page_number (у главы не заполняется)
	Last    int       // наибольший page_number (у главы не заполняется)
	Pages   int       // сколько полос; 0 — выгрузка ответит 404
}

// VolumeStamps — метки томов по их полосам. Тома без полос в ответе нет.
// Замер 03.10.2026 на локальном корпусе: все 175 томов — 63 мс.
func (r *OPDSRepository) VolumeStamps(ctx context.Context, workIDs []int64) (map[int64]PageStamp, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT work_id, max(updated_at), min(page_number), max(page_number), count(*)
		FROM pages
		WHERE work_id = ANY($1)
		GROUP BY work_id`, workIDs)
	if err != nil {
		return nil, fmt.Errorf("метки томов: %w", err)
	}
	defer rows.Close()
	out := make(map[int64]PageStamp, len(workIDs))
	for rows.Next() {
		var id int64
		var s PageStamp
		if err := rows.Scan(&id, &s.Updated, &s.First, &s.Last, &s.Pages); err != nil {
			return nil, fmt.Errorf("метки томов: %w", err)
		}
		out[id] = s
	}
	return out, rows.Err()
}

// ChapterStamps — метки всех глав тома по полосам их диапазонов, тем же
// условием, что PageRepository.GetPageRange (границы включительно). Глава
// без полос в диапазоне получает Pages = 0 и нулевое Updated.
// Замер 03.10.2026: том 50 Ленина, 755 глав — 12 мс.
func (r *OPDSRepository) ChapterStamps(ctx context.Context, workID int64) (map[int64]PageStamp, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT c.id, max(p.updated_at), count(p.id)
		FROM chapters c
		LEFT JOIN pages p
		  ON p.work_id = c.work_id AND p.page_number BETWEEN c.start_page AND c.end_page
		WHERE c.work_id = $1
		GROUP BY c.id`, workID)
	if err != nil {
		return nil, fmt.Errorf("метки глав работы %d: %w", workID, err)
	}
	defer rows.Close()
	out := map[int64]PageStamp{}
	for rows.Next() {
		var id int64
		var updated *time.Time
		var s PageStamp
		if err := rows.Scan(&id, &updated, &s.Pages); err != nil {
			return nil, fmt.Errorf("метки глав работы %d: %w", workID, err)
		}
		if updated != nil {
			s.Updated = *updated
		}
		out[id] = s
	}
	return out, rows.Err()
}

// volumeWithPages — том верхнего уровня, у которого есть хотя бы одна
// полоса. Условие стоит в самом запросе, до LIMIT: том без полос — это
// работа, которую публикатор завёл и ещё не залил, и она всегда самая
// свежая; отсев после LIMIT ломал разбивку «Новых поступлений».
const volumeWithPages = `parent_work_id IS NULL AND role = 'volume'
	AND EXISTS (SELECT 1 FROM pages p WHERE p.work_id = works.id)`

func (r *OPDSRepository) scanWorks(ctx context.Context, what, query string, args ...any) ([]*models.Work, error) {
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	defer rows.Close()
	var out []*models.Work
	for rows.Next() {
		w, err := scanWork(rows)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", what, err)
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// RecentVolumes — тома с полосами, свежие сверху (works.created_at; на
// боевом это момент публикации). id DESC добивает порядок при равных
// метках, иначе страницы теряли бы или повторяли тома — тот же довод, что у
// WorkRepository.List.
func (r *OPDSRepository) RecentVolumes(ctx context.Context, limit, offset int) ([]*models.Work, error) {
	return r.scanWorks(ctx, "новые тома", `SELECT `+workColumns+` FROM `+worksWithSlug+`
		WHERE `+volumeWithPages+`
		ORDER BY created_at DESC, id DESC LIMIT $1 OFFSET $2`, limit, offset)
}

// VolumesByID — строки томов с полосами по списку id: выдаче поиска нужны
// только тома, которые есть в каталоге, а полка целиком (сводки по всему
// корпусу) ради них не нужна. Порядок не обещается — его держит выдача.
func (r *OPDSRepository) VolumesByID(ctx context.Context, ids []int64) ([]*models.Work, error) {
	return r.scanWorks(ctx, "тома по id", `SELECT `+workColumns+` FROM `+worksWithSlug+`
		WHERE id = ANY($1) AND `+volumeWithPages, ids)
}
