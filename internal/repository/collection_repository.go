package repository

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"proofreader/internal/models"
	"proofreader/pkg/slug"
)

// CollectionRepository handles collection data access
type CollectionRepository struct {
	pool *pgxpool.Pool
}

// NewCollectionRepository creates a new collection repository
func NewCollectionRepository(pool *pgxpool.Pool) *CollectionRepository {
	return &CollectionRepository{pool: pool}
}

// ItemRow — сырая строка состава вместе с данными источника: главы, работы и
// собрания. Одним запросом, потому что обход элементов с отдельным запросом
// на каждый превращается в десятки round-trip'ов.
//
// Поля главы и работы — указатели: у элемента с удалённым источником их нет.
type ItemRow struct {
	Item models.CollectionItem

	ChapterTitle     *string
	ChapterStartPage *int
	ChapterEndPage   *int

	WorkTitle    *string
	WorkAuthor   *string
	PageOffset   *int
	VolumeNumber *int
	VolumePart   *string
	EditionTitle *string
	// WorkSlug — хвост адреса работы-источника (см. Work.Slug, workSlugFrom).
	// Пусто, если источник (работа) пропал — тогда пусты и WorkTitle и всё
	// остальное, добытое тем же LEFT JOIN.
	WorkSlug string
}

const collectionColumns = `id, title, slug, description, owner_id, author_nickname, published_at, publish_ip_hash, created_at, updated_at`

// Create creates a new collection. AuthorNickname приезжает уже выставленным
// вызывающим (пусто у сотрудника, ник читателя у него самого) — здесь только
// запись.
func (r *CollectionRepository) Create(ctx context.Context, c *models.Collection) error {
	query := `
		INSERT INTO collections (title, slug, description, owner_id, author_nickname)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at, updated_at
	`

	err := r.pool.QueryRow(ctx, query, c.Title, c.Slug, c.Description, c.OwnerID, c.AuthorNickname).
		Scan(&c.ID, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to create collection: %w", err)
	}

	return nil
}

// GetBySlug retrieves a staff collection by its slug (пустой ник). Оставлен
// обёрткой над GetByAuthorSlug ради вызывающих, которым читательский адрес
// не нужен вовсе (сотрудническая витрина, SEO-карточки): голый слаг ищет
// ТОЛЬКО среди сотруднических строк и читательскую подборку не находит —
// иначе одна и та же ссылка отдавала бы разным читателям, независимо
// выбравшим одинаковый слаг, произвольную из двух чужих друг другу подборок.
func (r *CollectionRepository) GetBySlug(ctx context.Context, slug string) (*models.Collection, error) {
	return r.GetByAuthorSlug(ctx, "", slug)
}

// GetByAuthorSlug ищет подборку по паре «ник автора + слаг» — второй, более
// длинный вид адреса подборки (/collections/{ник}/{слаг}). Пустой ник —
// сотрудническая подборка и прежний адрес /collections/{слаг}; непустой —
// подборка читателя. Уникальность пары держит составной индекс
// collections_author_slug_key (миграция 000024), поэтому запрос всегда
// находит не больше одной строки.
//
// Сравнение нормализованное (lower + NFKC), тем же выражением, что и
// nickname_key у users (000024) и UserRepository.GetByNickname — находка
// рецензии: author_nickname хранит СНИМОК ника на момент создания подборки
// (буквально, регистр как ввёл читатель при регистрации), а вход по нику
// регистр не различает. Без нормализации здесь адрес подборки был
// чувствителен к регистру там, где вход в ту же учётку — нет: читатель,
// подписавшийся «Чтец», не находил бы свою подборку по /collections/чтец/…,
// хотя вошёл бы в свою учётку что «Чтец», что «чтец».
func (r *CollectionRepository) GetByAuthorSlug(
	ctx context.Context, nickname, slug string,
) (*models.Collection, error) {
	query := `SELECT ` + collectionColumns + ` FROM collections
		WHERE lower(normalize(author_nickname, NFKC)) = lower(normalize($1, NFKC)) AND slug = $2`

	var c models.Collection
	err := r.pool.QueryRow(ctx, query, nickname, slug).Scan(
		&c.ID, &c.Title, &c.Slug, &c.Description, &c.OwnerID,
		&c.AuthorNickname, &c.PublishedAt, &c.PublishIPHash, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("collection not found")
		}
		return nil, fmt.Errorf("failed to get collection: %w", err)
	}

	return &c, nil
}

// List retrieves the staff showcase: collections without a reader author,
// published. Черновики и читательские подборки сюда не попадают — у витрины
// свой канал (ListByOwner для «моих», Get по слагу для отдельной ссылки).
func (r *CollectionRepository) List(ctx context.Context) ([]*models.Collection, error) {
	query := `SELECT ` + collectionColumns + `
		FROM collections
		WHERE author_nickname = '' AND published_at IS NOT NULL
		ORDER BY title`

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list collections: %w", err)
	}
	defer rows.Close()

	var out []*models.Collection
	for rows.Next() {
		var c models.Collection
		if err := rows.Scan(
			&c.ID, &c.Title, &c.Slug, &c.Description, &c.OwnerID,
			&c.AuthorNickname, &c.PublishedAt, &c.PublishIPHash, &c.CreatedAt, &c.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan collection: %w", err)
		}
		out = append(out, &c)
	}

	return out, nil
}

// ListByOwner отдаёт все подборки читателя — черновики и опубликованные —
// для его личного списка «мои подборки». Витринному List() показывать их
// нельзя: там черновиков не должно быть видно вовсе.
func (r *CollectionRepository) ListByOwner(ctx context.Context, ownerID int64) ([]*models.Collection, error) {
	query := `SELECT ` + collectionColumns + ` FROM collections WHERE owner_id = $1 ORDER BY updated_at DESC`

	rows, err := r.pool.Query(ctx, query, ownerID)
	if err != nil {
		return nil, fmt.Errorf("failed to list owner collections: %w", err)
	}
	defer rows.Close()

	var out []*models.Collection
	for rows.Next() {
		var c models.Collection
		if err := rows.Scan(
			&c.ID, &c.Title, &c.Slug, &c.Description, &c.OwnerID,
			&c.AuthorNickname, &c.PublishedAt, &c.PublishIPHash, &c.CreatedAt, &c.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan collection: %w", err)
		}
		out = append(out, &c)
	}

	return out, nil
}

// ListByAuthorNickname отдаёт все подборки одного ника — черновики, снятые и
// опубликованные — для разбора жалобы администратором.
//
// Ключ — снимок подписи (author_nickname), а не owner_id: из письма приходит
// ник, и только снимок переживает удаление учётной записи (owner_id тогда
// обнуляется). Сравнение нормализованное, тем же выражением, что у
// GetByAuthorSlug, иначе «Чтец» из адреса не нашёлся бы по «чтец» из письма.
//
// Сотруднические подборки сюда не попадают сами собой: у них ник пустой.
func (r *CollectionRepository) ListByAuthorNickname(
	ctx context.Context, nickname string,
) ([]*models.Collection, error) {
	query := `SELECT ` + collectionColumns + ` FROM collections
		WHERE lower(normalize(author_nickname, NFKC)) = lower(normalize($1, NFKC))
		ORDER BY updated_at DESC`

	rows, err := r.pool.Query(ctx, query, nickname)
	if err != nil {
		return nil, fmt.Errorf("failed to list collections by author: %w", err)
	}
	defer rows.Close()

	out := []*models.Collection{}
	for rows.Next() {
		var c models.Collection
		if err := rows.Scan(
			&c.ID, &c.Title, &c.Slug, &c.Description, &c.OwnerID,
			&c.AuthorNickname, &c.PublishedAt, &c.PublishIPHash, &c.CreatedAt, &c.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan collection: %w", err)
		}
		out = append(out, &c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating author collections: %w", err)
	}

	return out, nil
}

// Update updates a collection
func (r *CollectionRepository) Update(ctx context.Context, c *models.Collection) error {
	query := `
		UPDATE collections
		SET title = $2, slug = $3, description = $4
		WHERE id = $1
		RETURNING updated_at
	`

	err := r.pool.QueryRow(ctx, query, c.ID, c.Title, c.Slug, c.Description).Scan(&c.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("collection not found")
		}
		return fmt.Errorf("failed to update collection: %w", err)
	}

	return nil
}

// Delete deletes a collection
func (r *CollectionRepository) Delete(ctx context.Context, id int64) error {
	result, err := r.pool.Exec(ctx, `DELETE FROM collections WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("failed to delete collection: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("collection not found")
	}

	return nil
}

// PublishWithinLimit публикует подборку, если предел частоты по отметке
// адреса ещё не исчерпан, и сообщает, приняли ли.
//
// Тот же приём, что CreateWithinLimit (page_suggestion_repository.go) и
// ReserveAttempt (auth_attempt_repository.go): счёт и запись — одна
// транзакция под консультативной блокировкой по отметке адреса, иначе это
// «проверил — записал» двумя обращениями к пулу, и залп параллельных
// публикаций проходит мимо предела целиком.
//
// publish_ip_hash не обнуляется при снятии с публикации (Unpublish) — она
// остаётся отметкой «было опубликовано», по которой Get отличает черновик
// от снятого. Поэтому здесь предел считается ПО ВСЕМ строкам с этой отметкой
// и published_at IS NOT NULL — иначе снятая-и-переопубликованная подборка не
// попала бы в собственный же счёт.
func (r *CollectionRepository) PublishWithinLimit(
	ctx context.Context, id int64, ipHash string, limit int, since time.Time,
) (bool, time.Time, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, time.Time{}, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) // откат после успешного Commit безвреден

	// Ключ блокировки считаем в Go: hashtext() в Postgres — внутренняя
	// недокументированная функция. Префикс имени таблицы в хэше обязателен —
	// консультативные блокировки делят одно пространство ключей на всю базу,
	// и без префикса публикация подборки и, скажем, письмо в обратную связь
	// с одного адреса вставали бы в очередь друг за другом без всякой на то
	// причины.
	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte("collections:" + ipHash))
	lockKey := int64(hasher.Sum64())

	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", lockKey); err != nil {
		return false, time.Time{}, fmt.Errorf("failed to acquire advisory lock: %w", err)
	}

	var count int
	if err := tx.QueryRow(ctx,
		`SELECT count(*) FROM collections WHERE publish_ip_hash = $1 AND published_at > $2`,
		ipHash, since).Scan(&count); err != nil {
		return false, time.Time{}, fmt.Errorf("failed to count recent publications: %w", err)
	}
	if count >= limit {
		return false, time.Time{}, nil
	}

	var publishedAt time.Time
	if err := tx.QueryRow(ctx, `
		UPDATE collections SET published_at = now(), publish_ip_hash = $2
		WHERE id = $1
		RETURNING published_at`, id, ipHash).Scan(&publishedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, time.Time{}, fmt.Errorf("collection not found")
		}
		return false, time.Time{}, fmt.Errorf("failed to publish collection: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return false, time.Time{}, fmt.Errorf("failed to commit transaction: %w", err)
	}
	return true, publishedAt, nil
}

// Unpublish снимает подборку с публикации: возвращает её в черновик. Отметка
// адреса (publish_ip_hash) намеренно НЕ стирается — см. комментарий у
// PublishWithinLimit и у Collection.PublishIPHash: это единственный в базе
// признак «было опубликовано», без которого Get не отличил бы снятую
// подборку (410) от никогда не публиковавшейся (404).
func (r *CollectionRepository) Unpublish(ctx context.Context, id int64) error {
	result, err := r.pool.Exec(ctx,
		`UPDATE collections SET published_at = NULL WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("failed to unpublish collection: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("collection not found")
	}

	return nil
}

// AddItem дописывает элемент в конец состава.
//
// Снимок заголовка и автора берётся здесь, из базы, а не из тела запроса:
// иначе это поле, куда клиент пишет что угодно, и битая строка оглавления
// начинает врать про то, чем она была.
func (r *CollectionRepository) AddItem(
	ctx context.Context,
	collectionID int64,
	kind string,
	chapterID, workID *int64,
	authorOverride string,
) (*models.CollectionItem, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var title, author string
	var resolvedWorkID int64

	switch kind {
	case models.CollectionItemKindChapter:
		if chapterID == nil {
			return nil, fmt.Errorf("chapter_id is required for a chapter item")
		}
		// Том берётся из самой главы: клиенту незачем его присылать, а
		// расхождение между присланным и настоящим было бы неотличимо от
		// правды.
		err = tx.QueryRow(ctx, `
			SELECT c.title, w.author, w.id
			FROM chapters c
			JOIN works w ON w.id = c.work_id
			WHERE c.id = $1`, *chapterID).Scan(&title, &author, &resolvedWorkID)
	case models.CollectionItemKindWork:
		if workID == nil {
			return nil, fmt.Errorf("work_id is required for a work item")
		}
		err = tx.QueryRow(ctx, `
			SELECT title, author, id FROM works WHERE id = $1`, *workID).
			Scan(&title, &author, &resolvedWorkID)
	default:
		return nil, fmt.Errorf("unknown item kind %q", kind)
	}

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("item source not found")
		}
		return nil, fmt.Errorf("failed to read item source: %w", err)
	}

	item := models.CollectionItem{
		CollectionID:   collectionID,
		Kind:           kind,
		ChapterID:      chapterID,
		WorkID:         &resolvedWorkID,
		SnapshotTitle:  title,
		SnapshotAuthor: author,
		AuthorOverride: authorOverride,
	}

	err = tx.QueryRow(ctx, `
		INSERT INTO collection_items
			(collection_id, kind, chapter_id, work_id,
			 snapshot_title, snapshot_author, author_override, order_number)
		VALUES ($1, $2, $3, $4, $5, $6, $7,
			COALESCE((SELECT MAX(order_number) FROM collection_items WHERE collection_id = $1), 0) + 1)
		RETURNING id, order_number, created_at, updated_at`,
		collectionID, kind, chapterID, resolvedWorkID,
		title, author, authorOverride,
	).Scan(&item.ID, &item.OrderNumber, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to add collection item: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return &item, nil
}

// UpdateItemAuthor меняет переопределение автора. Пустая строка возвращает
// строку к автору работы.
func (r *CollectionRepository) UpdateItemAuthor(
	ctx context.Context, collectionID, itemID int64, authorOverride string,
) error {
	result, err := r.pool.Exec(ctx, `
		UPDATE collection_items SET author_override = $3
		WHERE id = $2 AND collection_id = $1`,
		collectionID, itemID, authorOverride)
	if err != nil {
		return fmt.Errorf("failed to update collection item: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("collection item not found")
	}

	return nil
}

// ItemByID retrieves one item of a collection.
func (r *CollectionRepository) ItemByID(
	ctx context.Context, collectionID, itemID int64,
) (*models.CollectionItem, error) {
	var it models.CollectionItem
	err := r.pool.QueryRow(ctx, `
		SELECT id, collection_id, kind, chapter_id, work_id,
		       snapshot_title, snapshot_author, author_override, order_number,
		       created_at, updated_at
		FROM collection_items
		WHERE id = $2 AND collection_id = $1`, collectionID, itemID).Scan(
		&it.ID, &it.CollectionID, &it.Kind, &it.ChapterID, &it.WorkID,
		&it.SnapshotTitle, &it.SnapshotAuthor, &it.AuthorOverride, &it.OrderNumber,
		&it.CreatedAt, &it.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("collection item not found")
		}
		return nil, fmt.Errorf("failed to get collection item: %w", err)
	}

	return &it, nil
}

// DeleteItem удаляет элемент и подтягивает номера оставшихся, чтобы в составе
// не оставалось дыр.
func (r *CollectionRepository) DeleteItem(ctx context.Context, collectionID, itemID int64) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	result, err := tx.Exec(ctx,
		`DELETE FROM collection_items WHERE id = $2 AND collection_id = $1`,
		collectionID, itemID)
	if err != nil {
		return fmt.Errorf("failed to delete collection item: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("collection item not found")
	}

	if err := renumberItems(ctx, tx, collectionID); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// MoveItem ставит элемент на заданное место, раздвигая остальные.
func (r *CollectionRepository) MoveItem(
	ctx context.Context, collectionID, itemID int64, newOrder int,
) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var count int
	if err := tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM collection_items WHERE collection_id = $1`,
		collectionID).Scan(&count); err != nil {
		return fmt.Errorf("failed to count collection items: %w", err)
	}

	target := normalizeItemOrder(newOrder, count)

	// Элемент временно уезжает в ноль, чтобы не мешать пересчёту соседей:
	// без этого он попадает под собственный сдвиг.
	result, err := tx.Exec(ctx,
		`UPDATE collection_items SET order_number = 0 WHERE id = $2 AND collection_id = $1`,
		collectionID, itemID)
	if err != nil {
		return fmt.Errorf("failed to detach collection item: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("collection item not found")
	}

	if _, err := tx.Exec(ctx, `
		WITH ranked AS (
			SELECT id, ROW_NUMBER() OVER (ORDER BY order_number, id) AS new_order
			FROM collection_items
			WHERE collection_id = $1 AND id <> $2
		)
		UPDATE collection_items ci
		SET order_number = CASE WHEN r.new_order >= $3 THEN r.new_order + 1 ELSE r.new_order END
		FROM ranked r
		WHERE ci.id = r.id`, collectionID, itemID, target); err != nil {
		return fmt.Errorf("failed to reorder collection items: %w", err)
	}

	if _, err := tx.Exec(ctx,
		`UPDATE collection_items SET order_number = $3 WHERE id = $2 AND collection_id = $1`,
		collectionID, itemID, target); err != nil {
		return fmt.Errorf("failed to move collection item: %w", err)
	}

	if err := renumberItems(ctx, tx, collectionID); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// normalizeItemOrder прижимает запрошенное место к границам списка: стрелка
// «вверх» на первой строке не должна отправлять элемент в ноль, «вниз» на
// последней — за хвост.
func normalizeItemOrder(order, count int) int {
	if order < 1 {
		return 1
	}
	if count > 0 && order > count {
		return count
	}

	return order
}

// renumberItems сводит номера к сплошному 1..n.
func renumberItems(ctx context.Context, tx pgx.Tx, collectionID int64) error {
	_, err := tx.Exec(ctx, `
		WITH ranked AS (
			SELECT id, ROW_NUMBER() OVER (ORDER BY order_number, id) AS new_order
			FROM collection_items
			WHERE collection_id = $1
		)
		UPDATE collection_items ci
		SET order_number = r.new_order
		FROM ranked r
		WHERE ci.id = r.id AND ci.order_number <> r.new_order`, collectionID)
	if err != nil {
		return fmt.Errorf("failed to renumber collection items: %w", err)
	}

	return nil
}

// itemRowsSlugColumns — три ингредиента слага тома-источника (см.
// workSlugColumns, work_repository.go), с алиасом w вместо works: работа в
// itemRowsQuery зовётся так же. Подстановкой, а не рукописной копией — см.
// workSummariesSlugColumns (edition_repository.go).
var itemRowsSlugColumns = strings.ReplaceAll(workSlugColumns, "works.", "w.")

// ItemRows отдаёт состав вместе с источниками одним запросом.
//
// LEFT JOIN везде: у элемента с удалённой главой нет ни главы, ни, возможно,
// работы, и он всё равно обязан попасть в оглавление — снимком заголовка.
var itemRowsQuery = `
	SELECT ci.id, ci.collection_id, ci.kind, ci.chapter_id, ci.work_id,
	       ci.snapshot_title, ci.snapshot_author, ci.author_override, ci.order_number,
	       ci.created_at, ci.updated_at,
	       c.title, c.start_page, c.end_page,
	       w.title, w.author, w.page_offset, w.volume_number, w.volume_part,
	       e.title,
	       w.role, w.precedes_volume, ` + itemRowsSlugColumns + `
	FROM collection_items ci
	LEFT JOIN chapters c ON c.id = ci.chapter_id
	LEFT JOIN works    w ON w.id = ci.work_id
	LEFT JOIN editions e ON e.id = w.edition_id
	WHERE ci.collection_id = $1
	ORDER BY ci.order_number
`

// ItemRows retrieves the raw composition rows of a collection.
func (r *CollectionRepository) ItemRows(ctx context.Context, collectionID int64) ([]ItemRow, error) {
	rows, err := r.pool.Query(ctx, itemRowsQuery, collectionID)
	if err != nil {
		return nil, fmt.Errorf("failed to list collection items: %w", err)
	}
	defer rows.Close()

	var out []ItemRow
	for rows.Next() {
		var row ItemRow
		var role *string
		var precedesVolume *int
		var slugEdition string
		var slugVolume *int
		var slugPart *string
		if err := rows.Scan(
			&row.Item.ID, &row.Item.CollectionID, &row.Item.Kind,
			&row.Item.ChapterID, &row.Item.WorkID,
			&row.Item.SnapshotTitle, &row.Item.SnapshotAuthor,
			&row.Item.AuthorOverride, &row.Item.OrderNumber,
			&row.Item.CreatedAt, &row.Item.UpdatedAt,
			&row.ChapterTitle, &row.ChapterStartPage, &row.ChapterEndPage,
			&row.WorkTitle, &row.WorkAuthor, &row.PageOffset,
			&row.VolumeNumber, &row.VolumePart,
			&row.EditionTitle,
			&role, &precedesVolume, &slugEdition, &slugVolume, &slugPart,
		); err != nil {
			return nil, fmt.Errorf("failed to scan collection item: %w", err)
		}
		if row.WorkTitle != nil {
			roleVal := ""
			if role != nil {
				roleVal = *role
			}
			row.WorkSlug = workSlugFrom(roleVal, slugEdition, slugVolume, slugPart, precedesVolume, *row.WorkTitle)
		}
		out = append(out, row)
	}

	return out, nil
}

// ChaptersForWorks отдаёт главы всех задействованных томов одним запросом —
// из них собираются поддеревья оглавления.
func (r *CollectionRepository) ChaptersForWorks(
	ctx context.Context, workIDs []int64,
) ([]*models.Chapter, error) {
	if len(workIDs) == 0 {
		return nil, nil
	}

	rows, err := r.pool.Query(ctx, `
		SELECT id, work_id, parent_id, title, type, order_number,
		       start_page, end_page, created_at, updated_at
		FROM chapters
		WHERE work_id = ANY($1)
		ORDER BY work_id, order_number`, workIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to list chapters for works: %w", err)
	}
	defer rows.Close()

	var out []*models.Chapter
	for rows.Next() {
		var c models.Chapter
		if err := rows.Scan(
			&c.ID, &c.WorkID, &c.ParentID, &c.Title, &c.Type, &c.OrderNumber,
			&c.StartPage, &c.EndPage, &c.CreatedAt, &c.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan chapter: %w", err)
		}
		// Свой, урезанный набор колонок (без is_apparatus) — общий scanChapter
		// сюда не подходит; слаг считается тем же slug.Chapter, что и там.
		c.Slug = slug.Chapter(c.Title)
		out = append(out, &c)
	}

	return out, nil
}
