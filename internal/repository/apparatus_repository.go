package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"proofreader/internal/models"
)

// ErrWorkNotFound — тома с таким номером нет.
//
// Отдельной ошибкой, а не по тексту: обработчик отвечает на неё 404, и без
// различения туда же уезжали бы исчерпанный пул, отвалившаяся сеть и
// statement_timeout. «Тома нет» — единственный диагноз, после которого
// оператор перестаёт искать, и получить его во время срочного снятия из-за
// мигнувшей базы значит остановить снятие не там.
var ErrWorkNotFound = errors.New("work not found")

// ApparatusRepository снимает аппарат тома: главы с is_apparatus вместе с их
// полосами, предметный указатель тома и служебные передние листы.
//
// Отдельным типом, а не методом WorkRepository: это единственная операция над
// аппаратом как целым, необратимая, и держать её среди обычного чтения работ
// значило бы её спрятать.
type ApparatusRepository struct {
	pool *pgxpool.Pool
}

func NewApparatusRepository(pool *pgxpool.Pool) *ApparatusRepository {
	return &ApparatusRepository{pool: pool}
}

// runner — то общее у пула и транзакции, что нужно здесь. План читается и
// снаружи транзакции (для --plan), и внутри неё (перед сносом), и это обязан
// быть один и тот же код: разойдись они, подтверждение оператора относилось бы
// к другому набору, чем снос.
type runner interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// apparatusPagesCond — условие «полоса попадает в диапазон главы-аппарата».
// Одна строка на два места: счёт в плане и снос. Разметка берётся из колонки
// is_apparatus, а не пересчитывается классификатором по заголовку — колонка
// правится руками (ChapterRepository.Update пишет значение как есть), и
// пересчёт затёр бы ручную поправку спорного случая.
const apparatusPagesCond = `
	p.work_id = $1 AND EXISTS (
		SELECT 1 FROM chapters c
		WHERE c.work_id = $1 AND c.is_apparatus
		  AND p.page_number BETWEEN c.start_page AND c.end_page
	)`

// apparatusTracksCond — «дорожка синтеза задевает полосу, которую снимает
// аппарат»: то же apparatusPagesCond, приложенное к диапазону дорожки. Читать
// его можно только ДО сноса полос — после него условие не найдёт ничего.
const apparatusTracksCond = `
	t.work_id = $1 AND EXISTS (
		SELECT 1 FROM pages p WHERE` + apparatusPagesCond + `
		  AND p.page_number BETWEEN t.start_page AND t.end_page
	)`

// Plan читает, что снимется, ничего не меняя.
func (r *ApparatusRepository) Plan(ctx context.Context, workID int64) (models.ApparatusPlan, error) {
	return apparatusPlan(ctx, r.pool, workID)
}

// PageNumbers — внутренние номера полос, которые снятие аппарата унесло бы,
// по возрастанию. Тем же apparatusPagesCond, что Plan и Remove, — нужен
// статической читальне: том со снятым аппаратом идёт в архив без этих полос
// (scripts/static-exclude.txt, строка «N apparatus»), и вторая копия правила
// разошлась бы с первой при первой же правке.
func (r *ApparatusRepository) PageNumbers(ctx context.Context, workID int64) ([]int, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT p.page_number FROM pages p WHERE`+apparatusPagesCond+` ORDER BY p.page_number`, workID)
	if err != nil {
		return nil, fmt.Errorf("failed to read apparatus pages: %w", err)
	}
	defer rows.Close()
	var out []int
	for rows.Next() {
		var n int
		if err := rows.Scan(&n); err != nil {
			return nil, fmt.Errorf("failed to scan apparatus page: %w", err)
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// Remove снимает аппарат одной транзакцией и отдаёт то, что сняло.
//
// План читается внутри той же транзакции, что и сносит: снаружи между планом и
// сносом конвейер мог бы перестроить главы тома, и снялось бы не то, что
// показано оператору.
func (r *ApparatusRepository) Remove(ctx context.Context, workID int64) (models.ApparatusPlan, error) {
	var plan models.ApparatusPlan

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return plan, fmt.Errorf("failed to begin apparatus removal: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // после Commit это no-op

	plan, err = apparatusPlan(ctx, tx, workID)
	if err != nil {
		return plan, err
	}
	if plan.Empty() {
		// Пустой план не ошибка репозитория: отказывать вслух — дело
		// обработчика, он знает, что сказать оператору.
		return plan, nil
	}

	// Замки — полосы раньше дорожек, тем же порядком, что у SaveEdit (правка
	// полосы, затем пометка stale её дорожек). Снос дорожек первым запер бы их
	// раньше полос, и правка полосы аппарата в ту же секунду давала бы
	// взаимную блокировку: Postgres обрывал одну сторону.
	// ORDER BY — полосы запираются по возрастанию, как их берёт RegisterTracks.
	if _, err := tx.Exec(ctx, `SELECT 1 FROM pages p WHERE`+apparatusPagesCond+`
		ORDER BY p.page_number FOR UPDATE`, workID); err != nil {
		return plan, fmt.Errorf("failed to lock apparatus pages: %w", err)
	}

	// Порядок: дорожки, полосы, затем главы. Наоборот нельзя — условие дорожек
	// смотрит на полосы, условие полос — на диапазоны глав, и после сноса
	// любого звена следующее перестало бы находить хоть что-нибудь.
	//
	// Ключи дорожек берутся из RETURNING самого сноса, а не из плана: объект
	// удаляется только тот, чью строку этот снос действительно унёс.
	trackKeys, err := collectKeys(ctx, tx, `
		WITH gone AS (
			DELETE FROM audio_tracks t WHERE`+apparatusTracksCond+`
			RETURNING t.id, t.start_page, t.s3_key)
		SELECT s3_key FROM gone ORDER BY start_page, id`, workID)
	if err != nil {
		return plan, fmt.Errorf("failed to delete apparatus audio tracks: %w", err)
	}
	plan.TrackPaths = trackKeys
	plan.TrackCount = len(trackKeys)
	if _, err := tx.Exec(ctx, `DELETE FROM pages p WHERE`+apparatusPagesCond, workID); err != nil {
		return plan, fmt.Errorf("failed to delete apparatus pages: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM chapters WHERE work_id = $1 AND is_apparatus`, workID); err != nil {
		return plan, fmt.Errorf("failed to delete apparatus chapters: %w", err)
	}
	// work_id снят с index_concepts миграцией 000026 (задача 11) — это
	// свойство статьи (index_concept_articles), не понятия каталога: у
	// понятия бывает статья и от другого издания, чей источник — не этот том,
	// и её снятие вместе с понятием было бы чужой потерей. Снимаем только
	// статью(и), пришедшую из этого тома, и следом — только те понятия из
	// затронутых этим сносом, что остались без единой статьи вообще (тем же
	// условием, что и в ReplaceForEdition, шаг 6); рубрики, адреса и отсылки
	// статьи уходят каскадом (ON DELETE CASCADE от article_id).
	//
	// Уборка ограничена RETURNING concept_id этого DELETE, а не всей таблицей
	// index_concepts — понятие без единой статьи бывает и штатно (удаление
	// издания каскадом сносит его статьи, а понятие остаётся сиротой до
	// следующего ввоза), и снятие тома, к этому понятию отношения не имеющее,
	// не вправе унести его заодно. Раунд правок 1: прежняя форма подчищала
	// ЛЮБОЕ осиротевшее понятие в базе — деструктивная операция под правовую
	// претензию не должна трогать больше, чем показывает план.
	rows, err := tx.Query(ctx,
		`DELETE FROM index_concept_articles WHERE work_id = $1 RETURNING concept_id`, workID)
	if err != nil {
		return plan, fmt.Errorf("failed to delete subject index: %w", err)
	}
	var affectedConceptIDs []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return plan, fmt.Errorf("failed to scan affected concept: %w", err)
		}
		affectedConceptIDs = append(affectedConceptIDs, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return plan, fmt.Errorf("failed to delete subject index: %w", err)
	}
	if len(affectedConceptIDs) > 0 {
		if _, err := tx.Exec(ctx, `
			DELETE FROM index_concepts c
			WHERE c.id = ANY($1)
			  AND NOT EXISTS (SELECT 1 FROM index_concept_articles a WHERE a.concept_id = c.id)
		`, affectedConceptIDs); err != nil {
			return plan, fmt.Errorf("failed to drop orphan concepts: %w", err)
		}
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM works WHERE parent_work_id = $1`, workID); err != nil {
		return plan, fmt.Errorf("failed to delete service works: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return plan, fmt.Errorf("failed to commit apparatus removal: %w", err)
	}
	return plan, nil
}

func apparatusPlan(ctx context.Context, q runner, workID int64) (models.ApparatusPlan, error) {
	plan := models.ApparatusPlan{WorkID: workID}

	if err := q.QueryRow(ctx,
		`SELECT title FROM works WHERE id = $1`, workID,
	).Scan(&plan.WorkTitle); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return plan, fmt.Errorf("work %d: %w", workID, ErrWorkNotFound)
		}
		return plan, fmt.Errorf("failed to read work: %w", err)
	}

	// Главы: в список — КОРНИ снимаемых поддеревьев, счёт — по всем.
	//
	// Корень это глава-аппарат, чей родитель аппаратом не помечен: либо
	// верхний уровень, либо «Приложение» подглавой внутри настоящей работы —
	// префиксное правило классификатора срабатывает на любой глубине, и таких
	// по корпусу 39, среди них сто полос текста Маркса внутри «Теорий
	// прибавочной стоимости». Снос берёт их наравне с остальными, поэтому
	// план обязан их называть; родитель печатается рядом, чтобы подмену было
	// видно.
	//
	// Подглава ВНУТРИ аппарата своей строки не получает — она уходит вместе с
	// родителем, и иначе у ленинского тома план был бы на сотню строк.
	rows, err := q.Query(ctx, `
		SELECT c.id, c.title, c.start_page, c.end_page,
		       (SELECT count(*) FROM chapters s WHERE s.parent_id = c.id) AS subchapters,
		       COALESCE((SELECT p.title FROM chapters p
		                 WHERE p.id = c.parent_id AND NOT p.is_apparatus), '') AS parent_title
		FROM chapters c
		WHERE c.work_id = $1 AND c.is_apparatus
		  AND (c.parent_id IS NULL
		       OR NOT EXISTS (SELECT 1 FROM chapters p
		                      WHERE p.id = c.parent_id AND p.is_apparatus))
		ORDER BY c.start_page, c.id`, workID)
	if err != nil {
		return plan, fmt.Errorf("failed to read apparatus chapters: %w", err)
	}
	for rows.Next() {
		var ch models.ApparatusChapter
		if err := rows.Scan(&ch.ID, &ch.Title, &ch.StartPage, &ch.EndPage,
			&ch.Subchapters, &ch.ParentTitle); err != nil {
			rows.Close()
			return plan, fmt.Errorf("failed to scan apparatus chapter: %w", err)
		}
		plan.Chapters = append(plan.Chapters, ch)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return plan, fmt.Errorf("failed to read apparatus chapters: %w", err)
	}

	if err := q.QueryRow(ctx,
		`SELECT count(*)::int FROM chapters WHERE work_id = $1 AND is_apparatus`, workID,
	).Scan(&plan.ChapterCount); err != nil {
		return plan, fmt.Errorf("failed to count apparatus chapters: %w", err)
	}

	// Полосы: счёт и ключи превью одним проходом. Ключи нужны обработчику —
	// каскад Postgres про хранилище не знает.
	pageRows, err := q.Query(ctx,
		`SELECT p.preview_path FROM pages p WHERE`+apparatusPagesCond+` ORDER BY p.page_number`, workID)
	if err != nil {
		return plan, fmt.Errorf("failed to read apparatus pages: %w", err)
	}
	for pageRows.Next() {
		var path string
		if err := pageRows.Scan(&path); err != nil {
			pageRows.Close()
			return plan, fmt.Errorf("failed to scan apparatus page: %w", err)
		}
		plan.PageCount++
		if path != "" {
			plan.StoragePaths = append(plan.StoragePaths, path)
		}
	}
	pageRows.Close()
	if err := pageRows.Err(); err != nil {
		return plan, fmt.Errorf("failed to read apparatus pages: %w", err)
	}

	// Считаем статьи, пришедшие из этого тома (index_concept_articles.work_id),
	// а не понятия целиком — см. комментарий у DELETE в Remove.
	if err := q.QueryRow(ctx,
		`SELECT count(*)::int FROM index_concept_articles WHERE work_id = $1`, workID,
	).Scan(&plan.ConceptCount); err != nil {
		return plan, fmt.Errorf("failed to count subject index: %w", err)
	}

	// Полосы вне всех глав. Снятие их не тронет — и именно поэтому план обязан
	// о них сказать: у ленинского тома 45 это «Содержание» и колофон, аппарат
	// чистой воды, который иначе остался бы лежать молча.
	if err := q.QueryRow(ctx, `
		SELECT count(*)::int FROM pages p
		WHERE p.work_id = $1 AND NOT EXISTS (
			SELECT 1 FROM chapters c
			WHERE c.work_id = $1
			  AND p.page_number BETWEEN c.start_page AND c.end_page
		)`, workID,
	).Scan(&plan.PagesOutsideChapters); err != nil {
		return plan, fmt.Errorf("failed to count uncovered pages: %w", err)
	}

	childRows, err := q.Query(ctx, `
		SELECT w.id, w.title, w.role,
		       (SELECT count(*)::int FROM pages p WHERE p.work_id = w.id) AS pages
		FROM works w WHERE w.parent_work_id = $1 ORDER BY w.id`, workID)
	if err != nil {
		return plan, fmt.Errorf("failed to read service works: %w", err)
	}
	for childRows.Next() {
		var ch models.ApparatusChild
		if err := childRows.Scan(&ch.ID, &ch.Title, &ch.Role, &ch.Pages); err != nil {
			childRows.Close()
			return plan, fmt.Errorf("failed to scan service work: %w", err)
		}
		plan.Children = append(plan.Children, ch)
	}
	childRows.Close()
	if err := childRows.Err(); err != nil {
		return plan, fmt.Errorf("failed to read service works: %w", err)
	}

	// Записи человека в главах аппарата и их подглавах (снос идёт каскадом по
	// parent_id, флаг у подглавы может быть не выставлен): строки уйдут каскадом вместе с
	// главами, а объекты в аудиобакете каскад не видит.
	plan.AudioPaths, err = collectKeys(ctx, q, `
		WITH RECURSIVE sub AS (
			SELECT id FROM chapters WHERE work_id = $1 AND is_apparatus
			UNION
			SELECT c.id FROM chapters c JOIN sub ON c.parent_id = sub.id)
		SELECT s3_key FROM audio_recordings WHERE chapter_id IN (SELECT id FROM sub) ORDER BY id`, workID)
	if err != nil {
		return plan, fmt.Errorf("failed to read apparatus recordings: %w", err)
	}

	// Синтез, задевающий снимаемые полосы. Раскладка аппарат не озвучивает, и
	// в штатном томе здесь пусто; Remove сносит эти строки тем же условием.
	plan.TrackPaths, err = collectKeys(ctx, q, `
		SELECT t.s3_key FROM audio_tracks t WHERE`+apparatusTracksCond+`
		ORDER BY t.start_page, t.id`, workID)
	if err != nil {
		return plan, fmt.Errorf("failed to read apparatus audio tracks: %w", err)
	}
	plan.TrackCount = len(plan.TrackPaths)

	return plan, nil
}

// collectKeys читает один текстовый столбец выборки в срез.
func collectKeys(ctx context.Context, q runner, sql string, args ...any) ([]string, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}
