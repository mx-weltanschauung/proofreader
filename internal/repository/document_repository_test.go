package repository

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"proofreader/internal/models"
)

func containsDocument(docs []*models.Document, id int64) bool {
	for _, d := range docs {
		if d.ID == id {
			return true
		}
	}
	return false
}

func TestDocumentReviewRoundTrip(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewDocumentRepository(pool)

	doc := &models.Document{
		Title: "Черновик", MarkdownContent: "тело черновика",
		AuthorNickname: "читатель", Slug: "chernovik",
	}
	if err := repo.Create(ctx, doc); err != nil {
		t.Fatalf("создать разбор: %v", err)
	}
	if doc.ReviewStatus != models.DocumentDraft {
		t.Fatalf("новый разбор обязан быть черновиком, получено %q", doc.ReviewStatus)
	}
	if doc.PublishedAt != nil || doc.WasPublished {
		t.Fatal("новый разбор не может быть опубликованным")
	}

	ok, err := repo.SubmitWithinLimit(ctx, doc.ID, "отметка-адреса", 3, time.Now().Add(-24*time.Hour))
	if err != nil || !ok {
		t.Fatalf("отправка на рассмотрение: ok=%v err=%v", ok, err)
	}
	pending, err := repo.ListForReview(ctx)
	if err != nil {
		t.Fatalf("очередь: %v", err)
	}
	if !containsDocument(pending, doc.ID) {
		t.Fatal("отправленный разбор не попал в очередь")
	}

	if err := repo.Approve(ctx, doc.ID, 1); err != nil {
		t.Fatalf("одобрение: %v", err)
	}
	after, err := repo.GetByID(ctx, doc.ID)
	if err != nil {
		t.Fatalf("перечитать: %v", err)
	}
	// Одобрение переносит черновик в показываемую редакцию целиком, вместе с
	// заглавием: иначе на людях останется старое имя при новом теле.
	if after.PublishedMarkdown != "тело черновика" || after.PublishedTitle != "Черновик" {
		t.Fatalf("одобренная редакция не снята с черновика: %+v", after)
	}
	if after.PublishedAt == nil || !after.WasPublished {
		t.Fatal("одобрение обязано публиковать")
	}
	if after.ReviewStatus != models.DocumentApproved {
		t.Fatalf("состояние после одобрения: %q", after.ReviewStatus)
	}

	// Снятие убирает с людей, но НЕ стирает следа публикации: по нему
	// читатель отличает «снято» (410) от «никогда не было» (404).
	if err := repo.Unpublish(ctx, doc.ID); err != nil {
		t.Fatalf("снятие: %v", err)
	}
	taken, err := repo.GetByID(ctx, doc.ID)
	if err != nil {
		t.Fatalf("перечитать после снятия: %v", err)
	}
	if taken.PublishedAt != nil {
		t.Fatal("снятый разбор остался опубликованным")
	}
	if !taken.WasPublished {
		t.Fatal("след публикации стёрт — снятое не отличить от небывшего")
	}
	if taken.MarkdownContent != "тело черновика" {
		t.Fatal("снятие тронуло черновик автора")
	}
}

// TestDocumentReviewMutationsNotFoundOnMissingID — фикс-раунд 1: Approve,
// Reject, Unpublish обязаны отличать «не нашлось» от «сделано» тем же
// суффиксом "not found", каким уже отличают Update и Delete в этом файле —
// несуществующий id раньше молча не менял ничего и отвечал nil.
func TestDocumentReviewMutationsNotFoundOnMissingID(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewDocumentRepository(pool)

	const missingID int64 = -1

	if err := repo.Approve(ctx, missingID, 1); err == nil || !strings.HasSuffix(err.Error(), "not found") {
		t.Fatalf("Approve на несуществующий id: получено %v, ожидался суффикс \"not found\"", err)
	}
	if err := repo.Reject(ctx, missingID, 1, models.DocumentRejectOffTopic); err == nil || !strings.HasSuffix(err.Error(), "not found") {
		t.Fatalf("Reject на несуществующий id: получено %v, ожидался суффикс \"not found\"", err)
	}
	if err := repo.Unpublish(ctx, missingID); err == nil || !strings.HasSuffix(err.Error(), "not found") {
		t.Fatalf("Unpublish на несуществующий id: получено %v, ожидался суффикс \"not found\"", err)
	}
}

// TestDocumentApproveIsAtomicallyGuardedAgainstDoubleApproval — фикс-раунд 1
// задачи 6, фикс 1, уровень репозитория: гвардия сидит прямо в WHERE
// UPDATE-запроса, поэтому второй Approve на уже одобренную строку не находит
// ни одной строки атомарно, а не по прочитанному заранее статусу (который
// racy). Настоящей параллельности здесь не нужно — важно, что состояние ПОСЛЕ
// первого Approve уже не 'на_рассмотрении', и WHERE это ловит.
func TestDocumentApproveIsAtomicallyGuardedAgainstDoubleApproval(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewDocumentRepository(pool)

	doc := &models.Document{Title: "Разбор", MarkdownContent: "тело", AuthorNickname: "чтец", Slug: "razbor-approve"}
	if err := repo.Create(ctx, doc); err != nil {
		t.Fatalf("создать: %v", err)
	}
	if ok, err := repo.SubmitWithinLimit(ctx, doc.ID, "адрес", 3, time.Now().Add(-24*time.Hour)); err != nil || !ok {
		t.Fatalf("отправка: ok=%v err=%v", ok, err)
	}
	if err := repo.Approve(ctx, doc.ID, 1); err != nil {
		t.Fatalf("первое одобрение: %v", err)
	}
	if err := repo.Approve(ctx, doc.ID, 2); !errors.Is(err, ErrDocumentNotPending) {
		t.Fatalf("второе одобрение: получено %v, ожидался ErrDocumentNotPending", err)
	}
}

// TestDocumentRejectIsAtomicallyGuardedAgainstDoubleResolution — тот же
// приём для Reject: отклонить уже одобренный разбор (гонка двух модераторов
// из фикс-раунда) не находит строк.
func TestDocumentRejectIsAtomicallyGuardedAgainstDoubleResolution(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewDocumentRepository(pool)

	doc := &models.Document{Title: "Разбор", MarkdownContent: "тело", AuthorNickname: "чтец", Slug: "razbor-reject"}
	if err := repo.Create(ctx, doc); err != nil {
		t.Fatalf("создать: %v", err)
	}
	if ok, err := repo.SubmitWithinLimit(ctx, doc.ID, "адрес", 3, time.Now().Add(-24*time.Hour)); err != nil || !ok {
		t.Fatalf("отправка: ok=%v err=%v", ok, err)
	}
	if err := repo.Approve(ctx, doc.ID, 1); err != nil {
		t.Fatalf("одобрение: %v", err)
	}
	if err := repo.Reject(ctx, doc.ID, 2, models.DocumentRejectOffTopic); !errors.Is(err, ErrDocumentNotPending) {
		t.Fatalf("отказ одобренному: получено %v, ожидался ErrDocumentNotPending", err)
	}
}

// TestDocumentUnpublishIsAtomicallyGuardedAgainstDoubleUnpublish — фикс 3:
// второй Unpublish на уже снятую публикацию не находит строк.
func TestDocumentUnpublishIsAtomicallyGuardedAgainstDoubleUnpublish(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewDocumentRepository(pool)

	doc := &models.Document{Title: "Разбор", MarkdownContent: "тело", AuthorNickname: "чтец", Slug: "razbor-unpublish"}
	if err := repo.Create(ctx, doc); err != nil {
		t.Fatalf("создать: %v", err)
	}
	if ok, err := repo.SubmitWithinLimit(ctx, doc.ID, "адрес", 3, time.Now().Add(-24*time.Hour)); err != nil || !ok {
		t.Fatalf("отправка: ok=%v err=%v", ok, err)
	}
	if err := repo.Approve(ctx, doc.ID, 1); err != nil {
		t.Fatalf("одобрение: %v", err)
	}
	if err := repo.Unpublish(ctx, doc.ID); err != nil {
		t.Fatalf("первое снятие: %v", err)
	}
	if err := repo.Unpublish(ctx, doc.ID); !errors.Is(err, ErrDocumentNotPublished) {
		t.Fatalf("второе снятие: получено %v, ожидался ErrDocumentNotPublished", err)
	}
}

// TestDocumentUpdateOnPendingRevertsToDraft — фикс 4 на настоящем Postgres,
// не на фейке: правка тела разбора, лежащего в очереди, обязана откатить его
// в черновик и стереть submitted_at/submit_ip_hash тем же UPDATE — семантика
// SET-выражений, читающих СТАРОЕ значение review_status, проверяется здесь
// против реальной базы, а не предположения о ней.
func TestDocumentUpdateOnPendingRevertsToDraft(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewDocumentRepository(pool)

	doc := &models.Document{Title: "Разбор", MarkdownContent: "тело", AuthorNickname: "чтец", Slug: "razbor-update"}
	if err := repo.Create(ctx, doc); err != nil {
		t.Fatalf("создать: %v", err)
	}
	if ok, err := repo.SubmitWithinLimit(ctx, doc.ID, "адрес", 3, time.Now().Add(-24*time.Hour)); err != nil || !ok {
		t.Fatalf("отправка: ok=%v err=%v", ok, err)
	}
	pending, err := repo.GetByID(ctx, doc.ID)
	if err != nil {
		t.Fatalf("перечитать после отправки: %v", err)
	}
	if pending.ReviewStatus != models.DocumentPending || pending.SubmittedAt == nil {
		t.Fatalf("отправка не перевела разбор в «на_рассмотрении»: %+v", pending)
	}

	pending.Title = "Правленное заглавие"
	pending.MarkdownContent = "правленное тело"
	if err := repo.Update(ctx, pending); err != nil {
		t.Fatalf("правка: %v", err)
	}
	if pending.ReviewStatus != models.DocumentDraft {
		t.Fatalf("Update не вернул review_status в поле документа: %q", pending.ReviewStatus)
	}

	after, err := repo.GetByID(ctx, doc.ID)
	if err != nil {
		t.Fatalf("перечитать после правки: %v", err)
	}
	if after.ReviewStatus != models.DocumentDraft {
		t.Fatalf("после правки состояние %q, ожидался «черновик»", after.ReviewStatus)
	}
	if after.SubmittedAt != nil {
		t.Fatal("submitted_at пережил откат в черновик")
	}
	if after.SubmitIPHash != "" {
		t.Fatal("submit_ip_hash пережил откат в черновик")
	}
	if after.MarkdownContent != "правленное тело" || after.Title != "Правленное заглавие" {
		t.Fatal("правка тела/заглавия не сохранилась вместе с откатом состояния")
	}

	// Повторная отправка того же разбора не должна упираться в предел частоты
	// — SubmitWithinLimit считает по `id <> $3`, место в очереди теряется
	// без последствий для предела.
	if ok, err := repo.SubmitWithinLimit(ctx, doc.ID, "адрес", 3, time.Now().Add(-24*time.Hour)); err != nil || !ok {
		t.Fatalf("повторная отправка после отката: ok=%v err=%v", ok, err)
	}
}

// TestDocumentSubmitWithinLimitNotFoundOnMissingID — фикс-раунд 2: несуществующий
// id хуже трёх предыдущих случаев — SubmitWithinLimit отвечал (true, nil),
// то есть не молчал, а ЛОЖНО утверждал «отправлено на рассмотрение». false
// здесь не должно путаться с «упёрся в предел частоты» (тоже false, но без
// ошибки) — различает суффикс "not found".
//
// Второй половиной теста проверяется то, чего не было в трёх предыдущих
// методах: отказ случается ПОСЛЕ взятия advisory-блокировки и подсчёта,
// внутри той же транзакции. Если бы ранний return не докатывался до
// defer tx.Rollback(ctx) (например, из-за забытого прежде return или
// повисшей блокировки), следующий вызов с тем же ip_hash был бы вынужден
// ждать снятия блокировки — тест это ловит таймаутом, а не чтением кода.
func TestDocumentSubmitWithinLimitNotFoundOnMissingID(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewDocumentRepository(pool)

	const missingID int64 = -1
	const ipHash = "отметка-несуществующего-разбора"
	since := time.Now().Add(-24 * time.Hour)

	ok, err := repo.SubmitWithinLimit(ctx, missingID, ipHash, 3, since)
	if ok {
		t.Fatal("SubmitWithinLimit на несуществующий id вернул true — ложное подтверждение отправки")
	}
	if err == nil || !strings.HasSuffix(err.Error(), "not found") {
		t.Fatalf("SubmitWithinLimit на несуществующий id: получено %v, ожидался суффикс \"not found\"", err)
	}

	doc := &models.Document{Title: "Настоящий", MarkdownContent: "тело", AuthorNickname: "чтец", Slug: "nastoyaschiy"}
	if err := repo.Create(ctx, doc); err != nil {
		t.Fatalf("создать: %v", err)
	}

	done := make(chan struct{})
	var ok2 bool
	var err2 error
	go func() {
		ok2, err2 = repo.SubmitWithinLimit(ctx, doc.ID, ipHash, 3, since)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("вторая отправка с того же адреса зависла — отказ первой не отпустил advisory-блокировку")
	}
	if err2 != nil || !ok2 {
		t.Fatalf("вторая отправка после отказа первой: ok=%v err=%v", ok2, err2)
	}
}

func TestDocumentSubmitLimitHoldsUnderLimit(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewDocumentRepository(pool)

	const ipHash = "общая-отметка"
	since := time.Now().Add(-24 * time.Hour)
	var ids []int64
	for i := 0; i < 3; i++ {
		d := &models.Document{Title: "Разбор", MarkdownContent: "тело", AuthorNickname: "чтец",
			Slug: fmt.Sprintf("razbor-limit-%d", i)}
		if err := repo.Create(ctx, d); err != nil {
			t.Fatalf("создать: %v", err)
		}
		ids = append(ids, d.ID)
		ok, err := repo.SubmitWithinLimit(ctx, d.ID, ipHash, 3, since)
		if err != nil || !ok {
			t.Fatalf("отправка %d: ok=%v err=%v", i, ok, err)
		}
	}

	fourth := &models.Document{Title: "Четвёртый", MarkdownContent: "тело", AuthorNickname: "чтец", Slug: "chetvyorty"}
	if err := repo.Create(ctx, fourth); err != nil {
		t.Fatalf("создать четвёртый: %v", err)
	}
	ok, err := repo.SubmitWithinLimit(ctx, fourth.ID, ipHash, 3, since)
	if err != nil {
		t.Fatalf("четвёртая отправка: %v", err)
	}
	if ok {
		t.Fatal("предел не сработал: четвёртая отправка прошла")
	}

	// Повторная отправка УЖЕ отправленного разбора лимита не тратит:
	// submitted_at перезаписывается, и счёт по строкам его не задваивает.
	// Предел защищает очередь от наплыва разборов, а не кнопку от нажатий.
	ok, err = repo.SubmitWithinLimit(ctx, ids[0], ipHash, 3, since)
	if err != nil {
		t.Fatalf("повторная отправка: %v", err)
	}
	if !ok {
		t.Fatal("повторная отправка того же разбора съела лимит")
	}
}

// checkConstraintLiteralPattern вытаскивает строковые литералы из текста
// CHECK-ограничения (pg_get_constraintdef): 'значение'::text или
// 'значение'::character varying — в обоих случаях литерал между кавычками.
var checkConstraintLiteralPattern = regexp.MustCompile(`'([^']*)'`)

// checkConstraintLiterals читает список допустимых литералов из НАСТОЯЩЕГО
// CHECK-ограничения схемы (pg_constraint/pg_get_constraintdef), а не из
// исходника — иначе тест сверял бы Go сам с собой и не заметил бы, что база
// разошлась со схемой, описанной в миграции.
func checkConstraintLiterals(t *testing.T, pool *pgxpool.Pool, table, constraint string) []string {
	t.Helper()
	var def string
	err := pool.QueryRow(context.Background(), `
		SELECT pg_get_constraintdef(oid) FROM pg_constraint
		WHERE conrelid = $1::regclass AND conname = $2`, table, constraint,
	).Scan(&def)
	if err != nil {
		t.Fatalf("не удалось прочитать ограничение %s.%s: %v", table, constraint, err)
	}
	matches := checkConstraintLiteralPattern.FindAllStringSubmatch(def, -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, m[1])
	}
	return out
}

// assertSameSet сверяет МНОЖЕСТВА, а не включение в одну сторону: проверка
// «каждая константа Go принимается базой» пропустила бы значение, оставшееся
// в CHECK после того, как из Go его убрали, — выбор, которого модератор уже
// не сделает, а база всё ещё ждёт.
func assertSameSet(t *testing.T, label string, gotFromSchema, wantFromGo []string) {
	t.Helper()
	inSchema := make(map[string]bool, len(gotFromSchema))
	for _, v := range gotFromSchema {
		inSchema[v] = true
	}
	inGo := make(map[string]bool, len(wantFromGo))
	for _, v := range wantFromGo {
		inGo[v] = true
	}
	for v := range inSchema {
		if !inGo[v] {
			t.Errorf("%s: значение %q принимает CHECK базы, но среди констант Go его нет", label, v)
		}
	}
	for v := range inGo {
		if !inSchema[v] {
			t.Errorf("%s: константа Go %q не входит в список CHECK базы", label, v)
		}
	}
}

// TestDocumentReviewStatusConstantsMatchSchema — сторожевой тест: список
// значений models.DocumentReviewStatus обязан ПОЭЛЕМЕНТНО совпадать со
// списком в documents_review_status_check. Разойтись они могут молча — до
// первого падения 23514 на боевом.
func TestDocumentReviewStatusConstantsMatchSchema(t *testing.T) {
	pool := testPool(t)
	fromSchema := checkConstraintLiterals(t, pool, "documents", "documents_review_status_check")
	fromGo := []string{
		string(models.DocumentDraft),
		string(models.DocumentPending),
		string(models.DocumentApproved),
		string(models.DocumentRejected),
	}
	assertSameSet(t, "review_status", fromSchema, fromGo)
}

// TestDocumentRejectReasonConstantsMatchSchema — то же самое для причин
// отказа (documents_reject_reason_check).
func TestDocumentRejectReasonConstantsMatchSchema(t *testing.T) {
	pool := testPool(t)
	fromSchema := checkConstraintLiterals(t, pool, "documents", "documents_reject_reason_check")
	fromGo := []string{
		string(models.DocumentRejectOffTopic),
		string(models.DocumentRejectNoCommentary),
		string(models.DocumentRejectAbuse),
		string(models.DocumentRejectUnlawful),
	}
	assertSameSet(t, "reject_reason", fromSchema, fromGo)
}

func TestDocumentGetByAuthorSlugSeparatesNamespaces(t *testing.T) {
	pool := testPool(t)
	repo := NewDocumentRepository(pool)
	ctx := context.Background()

	staff := &models.Document{Title: "О государстве", Slug: "o-gosudarstve"}
	reader := &models.Document{Title: "О государстве", Slug: "o-gosudarstve",
		AuthorNickname: "chitatel"}
	if err := repo.Create(ctx, staff); err != nil {
		t.Fatalf("сотруднический разбор: %v", err)
	}
	if err := repo.Create(ctx, reader); err != nil {
		t.Fatalf("читательский разбор: %v", err)
	}

	// Один слаг под разными подписями — две разные строки, а не столкновение.
	if staff.Slug != "o-gosudarstve" || reader.Slug != "o-gosudarstve" {
		t.Fatalf("слаг разведён зря: %q и %q", staff.Slug, reader.Slug)
	}

	got, err := repo.GetByAuthorSlug(ctx, "", "o-gosudarstve")
	if err != nil || got == nil || got.ID != staff.ID {
		t.Fatalf("короткий адрес отдал не сотруднический разбор: %v, %v", got, err)
	}
	got, err = repo.GetByAuthorSlug(ctx, "chitatel", "o-gosudarstve")
	if err != nil || got == nil || got.ID != reader.ID {
		t.Fatalf("длинный адрес отдал не читательский разбор: %v, %v", got, err)
	}
	// Чужой ник с верным слагом — не находка, а отсутствие.
	if _, err := repo.GetByAuthorSlug(ctx, "drugoj", "o-gosudarstve"); err == nil {
		t.Fatal("чужой ник нашёл чужой разбор")
	}
}

func TestDocumentCreateResolvesSlugCollision(t *testing.T) {
	pool := testPool(t)
	repo := NewDocumentRepository(pool)

	ctx := context.Background()
	first := &models.Document{Title: "Что делать?", Slug: "chto-delat", AuthorNickname: "chitatel"}
	second := &models.Document{Title: "Что делать?", Slug: "chto-delat", AuthorNickname: "chitatel"}
	if err := repo.Create(ctx, first); err != nil {
		t.Fatalf("первый разбор: %v", err)
	}
	if err := repo.Create(ctx, second); err != nil {
		t.Fatalf("второй разбор: %v", err)
	}

	if first.Slug != "chto-delat" {
		t.Fatalf("первый разбор получил не основу: %q", first.Slug)
	}
	if second.Slug != "chto-delat-2" {
		t.Fatalf("столкновение разведено не суффиксом: %q", second.Slug)
	}
}
