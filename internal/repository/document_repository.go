package repository

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"proofreader/internal/models"
)

// ErrDocumentNotPending — Approve/Reject наткнулись на строку, которая
// СУЩЕСТВУЕТ, но уже не «на_рассмотрении»: гонка двух модераторов (один
// одобрил, у второго в устаревшей вкладке осталась кнопка «отклонить») —
// второй получает эту ошибку атомарно, ноль строк перезаписи чужого решения.
// Обработчик уже отсекает заведомо неверный запрос собственной проверкой ДО
// похода в репозиторий; сюда долетает только гонка (или снятие документа
// автором между чтением и записью — Delete стоит на маршруте читателя).
var ErrDocumentNotPending = errors.New("document not pending review")

// ErrDocumentNotPublished — Unpublish наткнулся на строку, которая
// существует, но уже не на людях: снимать с публикации то, чего на ней нет, —
// бессмысленный жест, и заодно закрывает саму поверхность, на которой
// сотрудник тыкался бы в чужой неопубликованный черновик через Unpublish.
var ErrDocumentNotPublished = errors.New("document not published")

// DocumentRepository handles document data access
type DocumentRepository struct {
	pool *pgxpool.Pool
}

// NewDocumentRepository creates a new document repository
func NewDocumentRepository(pool *pgxpool.Pool) *DocumentRepository {
	return &DocumentRepository{pool: pool}
}

// documentColumns — единый список колонок разбора: пять выборок ниже читают
// одно и то же, и разъехаться им нельзя (scanDocument ждёт этот порядок).
const documentColumns = `id, title, markdown_content, owner_id, author_nickname, slug,
	published_title, published_markdown, published_at, was_published,
	review_status, reject_reason, moderator_id, reviewed_at, submitted_at,
	submit_ip_hash, created_at, updated_at`

func scanDocument(row pgx.Row, d *models.Document) error {
	return row.Scan(
		&d.ID, &d.Title, &d.MarkdownContent, &d.OwnerID, &d.AuthorNickname, &d.Slug,
		&d.PublishedTitle, &d.PublishedMarkdown, &d.PublishedAt, &d.WasPublished,
		&d.ReviewStatus, &d.RejectReason, &d.ModeratorID, &d.ReviewedAt,
		&d.SubmittedAt, &d.SubmitIPHash, &d.CreatedAt, &d.UpdatedAt,
	)
}

// documentSlugAttempts — сколько раз Create пробует развести столкновение
// суффиксом. Разведение идёт ВСТАВКОЙ, а не предварительным запросом «занято
// ли»: между проверкой и записью влезает чужая вставка, и проверка соврала бы
// молча. Потолок нужен только чтобы цикл не стал вечным на сломанной схеме.
const documentSlugAttempts = 50

// Create creates a new document
func (r *DocumentRepository) Create(ctx context.Context, document *models.Document) error {
	base := document.Slug
	// Пустой слаг — не адрес: /documents/ это витрина, и разбор занял бы её
	// собой. Отказ здесь, а не молчаливая запись пустой строки: слаг лепит
	// вызывающий (documentSlugBase), и забывший его путь обязан упасть на
	// первой же вставке, а не отдать читателю неоткрываемый разбор.
	if base == "" {
		return fmt.Errorf("failed to create document: слаг обязателен")
	}
	for attempt := 1; attempt <= documentSlugAttempts; attempt++ {
		slug := base
		if attempt > 1 {
			slug = fmt.Sprintf("%s-%d", base, attempt)
		}
		err := r.insertDocument(ctx, document, slug)
		if err == nil {
			document.Slug = slug
			return nil
		}
		if !isDocumentSlugTaken(err) {
			return err
		}
	}
	return fmt.Errorf("failed to create document: слаг %q занят %d раз подряд",
		base, documentSlugAttempts)
}

func (r *DocumentRepository) insertDocument(ctx context.Context, document *models.Document, slug string) error {
	query := `
		INSERT INTO documents (title, markdown_content, owner_id, author_nickname, slug)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING ` + documentColumns
	err := scanDocument(r.pool.QueryRow(ctx, query,
		document.Title, document.MarkdownContent, document.OwnerID, document.AuthorNickname, slug,
	), document)
	if err != nil {
		return fmt.Errorf("failed to create document: %w", err)
	}
	return nil
}

// documentSlugUniqueConstraint — имя составного индекса (миграция 000031).
// Сверяется по ИМЕНИ, а не по одному коду 23505: вставка разбора может упереться
// и в другой уникальный индекс, и принять чужое столкновение за своё значило бы
// крутить цикл разведения слага вокруг совсем другой беды. Тот же приём, что
// isCollectionSlugTakenError (collection_handler.go).
const documentSlugUniqueConstraint = "documents_author_slug_key"

func isDocumentSlugTaken(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == "23505" && pgErr.ConstraintName == documentSlugUniqueConstraint
}

// GetByAuthorSlug читает разбор по его адресу — паре «подпись + слаг».
// Пустой ник означает сотруднический разбор (короткий адрес), непустой —
// читательский. Сравнение точное: ник в адресе приходит из пути, а
// нормализацию его формы держит сама подпись (она снята с claims.Nickname,
// уже прошедшего models.ValidateNickname при заведении учётной записи).
func (r *DocumentRepository) GetByAuthorSlug(ctx context.Context, nickname, slug string) (*models.Document, error) {
	query := `SELECT ` + documentColumns + ` FROM documents
		WHERE author_nickname = $1 AND slug = $2`
	var d models.Document
	if err := scanDocument(r.pool.QueryRow(ctx, query, nickname, slug), &d); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("document not found")
		}
		return nil, fmt.Errorf("failed to get document: %w", err)
	}
	return &d, nil
}

// GetByID retrieves a document by ID
func (r *DocumentRepository) GetByID(ctx context.Context, id int64) (*models.Document, error) {
	query := `SELECT ` + documentColumns + ` FROM documents WHERE id = $1`

	var document models.Document
	err := scanDocument(r.pool.QueryRow(ctx, query, id), &document)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("document not found")
		}
		return nil, fmt.Errorf("failed to get document: %w", err)
	}

	return &document, nil
}

// List retrieves documents with optional filters
func (r *DocumentRepository) List(ctx context.Context, limit, offset int, ownerID *int64) ([]*models.Document, error) {
	query := `SELECT ` + documentColumns + ` FROM documents WHERE 1=1`
	args := []interface{}{}
	argIdx := 1

	if ownerID != nil {
		query += fmt.Sprintf(" AND owner_id = $%d", argIdx)
		args = append(args, *ownerID)
		argIdx++
	}

	query += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d OFFSET $%d", argIdx, argIdx+1)
	args = append(args, limit, offset)

	return r.queryDocuments(ctx, query, args...)
}

// ListPublished — витрина: только то, что на людях, свежим вперёд.
func (r *DocumentRepository) ListPublished(ctx context.Context, limit, offset int) ([]*models.Document, error) {
	query := `SELECT ` + documentColumns + ` FROM documents
		WHERE published_at IS NOT NULL
		ORDER BY published_at DESC LIMIT $1 OFFSET $2`
	return r.queryDocuments(ctx, query, limit, offset)
}

// ListByOwner — «моё» автора: и черновики, и опубликованное.
func (r *DocumentRepository) ListByOwner(ctx context.Context, ownerID int64) ([]*models.Document, error) {
	query := `SELECT ` + documentColumns + ` FROM documents
		WHERE owner_id = $1 ORDER BY updated_at DESC`
	return r.queryDocuments(ctx, query, ownerID)
}

// ListForReview — очередь модератора. Порядок по submitted_at возрастанию:
// первым пришёл — первым рассмотрен.
func (r *DocumentRepository) ListForReview(ctx context.Context) ([]*models.Document, error) {
	query := `SELECT ` + documentColumns + ` FROM documents
		WHERE review_status = $1 ORDER BY submitted_at ASC`
	return r.queryDocuments(ctx, query, models.DocumentPending)
}

func (r *DocumentRepository) queryDocuments(ctx context.Context, query string, args ...any) ([]*models.Document, error) {
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list documents: %w", err)
	}
	defer rows.Close()
	var out []*models.Document
	for rows.Next() {
		var d models.Document
		if err := scanDocument(rows, &d); err != nil {
			return nil, fmt.Errorf("failed to scan document: %w", err)
		}
		out = append(out, &d)
	}
	return out, rows.Err()
}

// Update правит ЧЕРНОВИК. published_* остаются как были: правка
// опубликованного не меняет показываемого, пока её не одобрили.
//
// Правка разбора, лежащего в очереди («на_рассмотрении»), возвращает его в
// «черновик» и стирает след отправки (submitted_at/submit_ip_hash) — фикс-
// раунд 1 задачи 6: смысл модерации в том, что одобрено ровно прочитанное, а
// сохранять заявку поверх изменившегося тела значит держать обещание,
// которого система не выполняет. Место в очереди теряется, автор отправляет
// заново — предела частоты это не тратит (SubmitWithinLimit считает по
// `id <> $3`). Черновик и уже рассмотренное (одобрено/отклонено) правятся как
// раньше — CASE трогает только «на_рассмотрении»; все три выражения в SET
// читают СТАРОЕ значение review_status (семантика Postgres — присваивания в
// одном UPDATE смотрят на строку до правки), так что порядок веток не важен.
//
// ВНИМАНИЕ фронту (задачи 11—12): это молчаливый откат в интерфейсе. Автор,
// правящий тело, пока модератор держит вкладку с очередью, должен увидеть,
// что его правка вернула разбор в черновики и отправку нужно повторить —
// сейчас ответ на PUT просто содержит новый review_status, а объяснение в
// UI ещё предстоит написать.
func (r *DocumentRepository) Update(ctx context.Context, document *models.Document) error {
	query := `
		UPDATE documents
		SET title = $2, markdown_content = $3,
		    review_status = CASE WHEN review_status = $4 THEN $5 ELSE review_status END,
		    submitted_at = CASE WHEN review_status = $4 THEN NULL ELSE submitted_at END,
		    submit_ip_hash = CASE WHEN review_status = $4 THEN '' ELSE submit_ip_hash END
		WHERE id = $1
		RETURNING updated_at, review_status
	`

	err := r.pool.QueryRow(
		ctx, query,
		document.ID, document.Title, document.MarkdownContent,
		models.DocumentPending, models.DocumentDraft,
	).Scan(&document.UpdatedAt, &document.ReviewStatus)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("document not found")
		}
		return fmt.Errorf("failed to update document: %w", err)
	}

	return nil
}

// SubmitWithinLimit ставит разбор в очередь модерации, держа счёт и запись
// ОДНОЙ транзакцией под консультативной блокировкой. Порознь залп
// параллельных запросов проходит мимо предела почти целиком (замер по
// соседнему рельсу: 18 из 20 при пределе 5).
//
// Префикс "documents:" перед отметкой адреса обязателен: консультативные
// блокировки — общее на всю базу пространство ключей, и без него отправка
// разбора и публикация подборки с одного адреса вставали бы в очередь друг
// за другом без причины.
//
// Счёт идёт по СТРОКАМ разборов, а не по нажатиям: submitted_at
// перезаписывается, поэтому повторная отправка того же разбора лимита не
// тратит (условие id <> $3). Предел защищает очередь от наплыва новых
// разборов.
func (r *DocumentRepository) SubmitWithinLimit(
	ctx context.Context, id int64, ipHash string, limit int, since time.Time,
) (bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) // откат после успешного Commit безвреден

	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte("documents:" + ipHash))
	lockKey := int64(hasher.Sum64())
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", lockKey); err != nil {
		return false, fmt.Errorf("failed to acquire advisory lock: %w", err)
	}

	var count int
	if err := tx.QueryRow(ctx,
		`SELECT count(*) FROM documents
		 WHERE submit_ip_hash = $1 AND submitted_at > $2 AND id <> $3`,
		ipHash, since, id).Scan(&count); err != nil {
		return false, fmt.Errorf("failed to count recent submissions: %w", err)
	}
	if count >= limit {
		return false, nil
	}

	result, err := tx.Exec(ctx, `
		UPDATE documents
		SET review_status = $2, submitted_at = now(), submit_ip_hash = $3,
		    reject_reason = NULL
		WHERE id = $1`, id, models.DocumentPending, ipHash)
	if err != nil {
		return false, fmt.Errorf("failed to submit document: %w", err)
	}
	// false здесь — не «упёрся в предел» (это уже отдано выше как (false,
	// nil)), а «не нашлось»: тот же суффикс, что у Approve/Reject/Unpublish,
	// иначе обработчик задачи 6 подтвердил бы автору отправку разбора,
	// которого не существует. defer tx.Rollback(ctx) откатывает счёт и
	// освобождает advisory-блокировку — досюда транзакция ничего не
	// закоммитила.
	if result.RowsAffected() == 0 {
		return false, fmt.Errorf("document not found")
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("failed to commit transaction: %w", err)
	}
	return true, nil
}

// documentExists — дешёвая проверка «строка вообще есть». Зовётся ТОЛЬКО на
// нулевом RowsAffected() гвардированного запроса (Approve/Reject/Unpublish),
// чтобы различить «нет такого id» от «есть, но не в том состоянии» без
// второй записи — цена этого похода несётся исключительно веткой отказа.
func (r *DocumentRepository) documentExists(ctx context.Context, id int64) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM documents WHERE id = $1)`, id).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check document existence: %w", err)
	}
	return exists, nil
}

// approve — общий SQL для Approve и PublishOwn. requirePending добавляет в
// WHERE условие review_status = 'на_рассмотрении' атомарно (не отдельным
// SELECT перед UPDATE): без него гонка двух модераторов (один одобрил, у
// второго в устаревшей вкладке осталась кнопка «отклонить») перезаписывала
// бы чужое решение молча.
//
// Сотруднический самопубликующийся путь (PublishOwn, зовётся из Submit)
// публикует СВОЙ ЖЕ черновик и guard'а не несёт — это пропуск очереди тем,
// кто и есть модератор, а не модерация чужого поданного разбора, и требовать
// от него «на_рассмотрении» значило бы ломать уже отгруженную фичу
// (TestStaffSubmitPublishesImmediately отправляет ЧЕРНОВИК).
func (r *DocumentRepository) approve(ctx context.Context, id, moderatorID int64, requirePending bool) error {
	query := `
		UPDATE documents
		SET published_title = title,
		    published_markdown = markdown_content,
		    published_at = now(),
		    was_published = true,
		    review_status = $2,
		    reject_reason = NULL,
		    moderator_id = $3,
		    reviewed_at = now()
		WHERE id = $1`
	args := []any{id, models.DocumentApproved, moderatorID}
	if requirePending {
		query += ` AND review_status = $4`
		args = append(args, models.DocumentPending)
	}
	result, err := r.pool.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to approve document: %w", err)
	}
	if result.RowsAffected() == 0 {
		if !requirePending {
			return fmt.Errorf("document not found")
		}
		exists, existsErr := r.documentExists(ctx, id)
		if existsErr != nil {
			return existsErr
		}
		if exists {
			return ErrDocumentNotPending
		}
		return fmt.Errorf("document not found")
	}
	return nil
}

// Approve переносит черновик в показываемую редакцию и публикует его —
// реальная модерация чужого поданного разбора. Строка обязана быть СЕЙЧАС
// «на_рассмотрении»; см. approve.
func (r *DocumentRepository) Approve(ctx context.Context, id, moderatorID int64) error {
	return r.approve(ctx, id, moderatorID, true)
}

// PublishOwn публикует разбор сразу, минуя очередь и без проверки состояния —
// путь сотрудника, отправляющего свой же (сотруднический, без подписи автора)
// разбор из ЧЕРНОВИКА: он и есть модератор, ждать самого себя незачем
// (DocumentReviewHandler.Submit). Модерации чужого это не заменяет — только
// Approve несёт guard «на_рассмотрении».
func (r *DocumentRepository) PublishOwn(ctx context.Context, id, moderatorID int64) error {
	return r.approve(ctx, id, moderatorID, false)
}

// Reject оставляет показываемую редакцию как была: отказ касается правки, а
// не того, что уже одобрено и стоит на людях. Строка обязана быть СЕЙЧАС
// «на_рассмотрении» — тот же атомарный guard и то же основание, что у
// Approve (гонка двух модераторов над одной поданной правкой).
func (r *DocumentRepository) Reject(
	ctx context.Context, id, moderatorID int64, reason models.DocumentRejectReason,
) error {
	result, err := r.pool.Exec(ctx, `
		UPDATE documents
		SET review_status = $2, reject_reason = $3, moderator_id = $4, reviewed_at = now()
		WHERE id = $1 AND review_status = $5`,
		id, models.DocumentRejected, reason, moderatorID, models.DocumentPending)
	if err != nil {
		return fmt.Errorf("failed to reject document: %w", err)
	}
	if result.RowsAffected() == 0 {
		exists, existsErr := r.documentExists(ctx, id)
		if existsErr != nil {
			return existsErr
		}
		if exists {
			return ErrDocumentNotPending
		}
		return fmt.Errorf("document not found")
	}
	return nil
}

// Unpublish убирает с людей, оставляя автору его черновик и след публикации
// (was_published): по нему читатель отличает снятое от небывшего. Строка
// обязана быть СЕЙЧАС на публикации — снимать с публикации то, чего на ней
// нет, бессмысленный жест, и заодно закрывает поверхность, на которой
// сотрудник тыкался бы в чужой неопубликованный черновик через Unpublish.
//
// Снятие ОТЗЫВАЕТ и заявку: разбор, лежавший в очереди, возвращается в
// «черновик», отметка подачи стирается. Без этого правилась только дата
// публикации — строка оставалась «на_рассмотрении» и в очереди, и обычное
// «Принять» публиковало разбор обратно: редактор снял по письму, назавтра
// кто-то разобрал очередь и вернул текст на люди, не спросив автора. Автор,
// желающий снова, отправляет заново, и это верно — снятое по жалобе не
// должно возвращаться само. Действует одинаково для снятия владельцем и
// снятия редакцией: обе двери ведут сюда.
//
// Условие CASE, а не безусловное «черновик»: отзывается ЗАЯВКА, а не история
// модерации. У «одобрено»/«отклонено» заявки нет — переписать их состояние
// значило бы потерять решение модератора, а у «отклонено» ещё и порвать
// CHECK documents_reject_reason_matches_status, который держит причину отказа
// ровно при этом состоянии. Все три выражения SET читают СТАРОЕ значение
// review_status (семантика Postgres — присваивания в одном UPDATE смотрят на
// строку до правки), поэтому порядок веток не важен; тот же приём и та же
// оговорка, что в Update выше.
func (r *DocumentRepository) Unpublish(ctx context.Context, id int64) error {
	result, err := r.pool.Exec(ctx, `
		UPDATE documents
		SET published_at = NULL,
		    review_status = CASE WHEN review_status = $2 THEN $3 ELSE review_status END,
		    submitted_at = CASE WHEN review_status = $2 THEN NULL ELSE submitted_at END,
		    submit_ip_hash = CASE WHEN review_status = $2 THEN '' ELSE submit_ip_hash END
		WHERE id = $1 AND published_at IS NOT NULL`,
		id, models.DocumentPending, models.DocumentDraft)
	if err != nil {
		return fmt.Errorf("failed to unpublish document: %w", err)
	}
	if result.RowsAffected() == 0 {
		exists, existsErr := r.documentExists(ctx, id)
		if existsErr != nil {
			return existsErr
		}
		if exists {
			return ErrDocumentNotPublished
		}
		return fmt.Errorf("document not found")
	}
	return nil
}

// Delete deletes a document
func (r *DocumentRepository) Delete(ctx context.Context, id int64) error {
	query := `DELETE FROM documents WHERE id = $1`

	result, err := r.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete document: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("document not found")
	}

	return nil
}
