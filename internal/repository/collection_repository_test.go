package repository

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"proofreader/internal/models"
)

func TestNormalizeItemOrder(t *testing.T) {
	// Порядок задаётся 1..n. Всё, что вылезло за границы, прижимается к ним:
	// стрелка «вверх» на первой строке не должна отправлять элемент в ноль.
	cases := []struct {
		name  string
		order int
		count int
		want  int
	}{
		{"внутри списка", 3, 5, 3},
		{"ниже единицы", 0, 5, 1},
		{"отрицательный", -7, 5, 1},
		{"за хвостом", 9, 5, 5},
		{"пустой список", 1, 0, 1},
	}

	for _, c := range cases {
		if got := normalizeItemOrder(c.order, c.count); got != c.want {
			t.Errorf("%s: normalizeItemOrder(%d, %d) = %d, ожидалось %d",
				c.name, c.order, c.count, got, c.want)
		}
	}
}

// TestListFiltersToStaffShowcase проверяет фильтр List на живой базе: витрина
// остаётся сотруднической — ни черновик, ни читательская подборка (пусть
// даже опубликованная) в неё не попадают.
func TestListFiltersToStaffShowcase(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewCollectionRepository(pool)

	owner := int64(1)
	published := time.Now().Add(-time.Hour)

	staffShown := &models.Collection{Title: "Витрина", Slug: "vitrina-repo", PublishedAt: &published}
	if err := repo.Create(ctx, staffShown); err != nil {
		t.Fatalf("Create(витрина): %v", err)
	}
	// published_at Create не пишет (публикация — отдельный шаг), поэтому
	// проставляем его напрямую, как это делает миграция легаси-строк.
	if _, err := pool.Exec(ctx,
		`UPDATE collections SET published_at = $2 WHERE id = $1`, staffShown.ID, published); err != nil {
		t.Fatalf("проставить published_at витрине: %v", err)
	}

	staffDraft := &models.Collection{Title: "Черновик сотрудника", Slug: "chernovik-repo"}
	if err := repo.Create(ctx, staffDraft); err != nil {
		t.Fatalf("Create(черновик): %v", err)
	}

	readerCollection := &models.Collection{
		Title: "Подборка читателя", Slug: "chitatel-repo", OwnerID: &owner, AuthorNickname: "чтец",
	}
	if err := repo.Create(ctx, readerCollection); err != nil {
		t.Fatalf("Create(читатель): %v", err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE collections SET published_at = $2 WHERE id = $1`, readerCollection.ID, published); err != nil {
		t.Fatalf("проставить published_at читателю: %v", err)
	}

	got, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].Slug != "vitrina-repo" {
		t.Fatalf("список витрины %+v, ожидалась ровно «vitrina-repo»", got)
	}
}

// TestListByOwnerIncludesDrafts проверяет, что личный список читателя
// показывает и черновики, и чужие подборки в него не попадают.
func TestListByOwnerIncludesDrafts(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewCollectionRepository(pool)

	owner := int64(1)

	mine := &models.Collection{Title: "Мой черновик", Slug: "moy-chernovik-repo", OwnerID: &owner, AuthorNickname: "я"}
	if err := repo.Create(ctx, mine); err != nil {
		t.Fatalf("Create(мой): %v", err)
	}
	staff := &models.Collection{Title: "Сотрудническая", Slug: "sotrudnicheskaya-repo"}
	if err := repo.Create(ctx, staff); err != nil {
		t.Fatalf("Create(сотрудник): %v", err)
	}

	got, err := repo.ListByOwner(ctx, owner)
	if err != nil {
		t.Fatalf("ListByOwner: %v", err)
	}
	if len(got) != 1 || got[0].Slug != "moy-chernovik-repo" {
		t.Fatalf("список владельца %+v, ожидалась ровно «moy-chernovik-repo» (черновик включён, чужая — нет)", got)
	}
}

// ListByAuthorNickname — путь администратора по жалобе: ник из письма, дальше
// подборки этого ника. Ключ именно снимок ника, а не owner_id, поэтому
// осиротевшая подборка (владелец удалён, owner_id обнулён) обязана найтись —
// она и есть тот случай, когда владельца уже не спросишь.
func TestListByAuthorNicknameFindsDraftsAndOrphans(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewCollectionRepository(pool)

	owner := int64(1)
	published := time.Now().Add(-time.Hour)

	live := &models.Collection{
		Title: "Опубликованная", Slug: "opublikovannaya-admin", OwnerID: &owner, AuthorNickname: "Чтец",
	}
	if err := repo.Create(ctx, live); err != nil {
		t.Fatalf("Create(опубликованная): %v", err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE collections SET published_at = $2 WHERE id = $1`, live.ID, published); err != nil {
		t.Fatalf("проставить published_at: %v", err)
	}

	draft := &models.Collection{
		Title: "Черновик", Slug: "chernovik-admin", OwnerID: &owner, AuthorNickname: "чтец",
	}
	if err := repo.Create(ctx, draft); err != nil {
		t.Fatalf("Create(черновик): %v", err)
	}

	orphan := &models.Collection{Title: "Осиротевшая", Slug: "osirotevshaya-admin", AuthorNickname: "ЧТЕЦ"}
	if err := repo.Create(ctx, orphan); err != nil {
		t.Fatalf("Create(осиротевшая): %v", err)
	}

	alien := &models.Collection{
		Title: "Чужая", Slug: "chuzhaya-admin", OwnerID: &owner, AuthorNickname: "книгочей",
	}
	if err := repo.Create(ctx, alien); err != nil {
		t.Fatalf("Create(чужая): %v", err)
	}
	staff := &models.Collection{Title: "Сотрудническая", Slug: "sotrudnicheskaya-admin"}
	if err := repo.Create(ctx, staff); err != nil {
		t.Fatalf("Create(сотрудник): %v", err)
	}

	got, err := repo.ListByAuthorNickname(ctx, "чтец")
	if err != nil {
		t.Fatalf("ListByAuthorNickname: %v", err)
	}
	slugs := map[string]bool{}
	for _, c := range got {
		slugs[c.Slug] = true
	}
	for _, want := range []string{"opublikovannaya-admin", "chernovik-admin", "osirotevshaya-admin"} {
		if !slugs[want] {
			t.Errorf("подборки %q нет в списке ника: %+v", want, slugs)
		}
	}
	if slugs["chuzhaya-admin"] || slugs["sotrudnicheskaya-admin"] {
		t.Errorf("в список ника попала чужая или сотрудническая подборка: %+v", slugs)
	}
	if len(got) != 3 {
		t.Fatalf("список ника содержит %d подборок, ждём 3: %+v", len(got), slugs)
	}
}

// TestGetByAuthorSlugIgnoresNicknameCase — находка рецензии: author_nickname
// хранит буквальный снимок ника на момент создания подборки, а вход по нику
// регистр не различает (UserRepository.GetByNickname сравнивает по
// nickname_key). Без нормализации здесь адрес подборки был бы чувствителен к
// регистру там, где вход в ту же учётку — нет: подписавшийся «Чтец» не нашёл
// бы свою подборку по /collections/чтец/….
func TestGetByAuthorSlugIgnoresNicknameCase(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewCollectionRepository(pool)

	owner := int64(1)
	mine := &models.Collection{
		Title: "Регистр ника", Slug: "registr-nika-repo", OwnerID: &owner, AuthorNickname: "Чтец",
	}
	if err := repo.Create(ctx, mine); err != nil {
		t.Fatalf("Create: %v", err)
	}

	for _, variant := range []string{"Чтец", "чтец", "ЧТЕЦ", "чТеЦ"} {
		got, err := repo.GetByAuthorSlug(ctx, variant, "registr-nika-repo")
		if err != nil {
			t.Fatalf("GetByAuthorSlug(%q): %v", variant, err)
		}
		if got.ID != mine.ID {
			t.Errorf("GetByAuthorSlug(%q) вернул id=%d, ожидался %d", variant, got.ID, mine.ID)
		}
	}

	// Чужой ник по-прежнему не находит строку — нормализация не должна
	// огрублять сравнение до подстроки или снятия различий помимо регистра.
	if _, err := repo.GetByAuthorSlug(ctx, "чтецы", "registr-nika-repo"); err == nil {
		t.Error("GetByAuthorSlug(чтецы) неожиданно нашёл строку с ником «Чтец»")
	}
}

// TestPublishWithinLimitEnforcesDailyCap бьёт по тому же advisory-lock
// приёму, что и auth_attempt_repository/page_suggestion_repository: предел
// считается атомарно внутри одной транзакции с блокировкой по хэшу адреса.
func TestPublishWithinLimitEnforcesDailyCap(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewCollectionRepository(pool)

	owner := int64(1)
	const ipHash = "тестовый-адрес"
	const limit = 3
	since := time.Now().Add(-24 * time.Hour)

	var ids []int64
	for i := 0; i < limit+1; i++ {
		c := &models.Collection{
			Title: "Подборка", Slug: sluggedForTest(i), OwnerID: &owner, AuthorNickname: "я",
		}
		if err := repo.Create(ctx, c); err != nil {
			t.Fatalf("Create #%d: %v", i, err)
		}
		ids = append(ids, c.ID)
	}

	for i := 0; i < limit; i++ {
		ok, publishedAt, err := repo.PublishWithinLimit(ctx, ids[i], ipHash, limit, since)
		if err != nil {
			t.Fatalf("PublishWithinLimit #%d: %v", i, err)
		}
		if !ok {
			t.Fatalf("публикация #%d отклонена раньше предела", i)
		}
		if publishedAt.IsZero() {
			t.Errorf("публикация #%d не вернула время публикации", i)
		}
	}

	// Четвёртая с того же адреса — сверх предела 3/сутки.
	ok, _, err := repo.PublishWithinLimit(ctx, ids[limit], ipHash, limit, since)
	if err != nil {
		t.Fatalf("PublishWithinLimit (сверх предела): %v", err)
	}
	if ok {
		t.Fatalf("публикация сверх предела 3/сутки должна быть отклонена")
	}

	// Тот же том с другого адреса предел не исчерпывает.
	ok, _, err = repo.PublishWithinLimit(ctx, ids[limit], "другой-адрес", limit, since)
	if err != nil {
		t.Fatalf("PublishWithinLimit (другой адрес): %v", err)
	}
	if !ok {
		t.Fatalf("публикация с другого адреса не должна упираться в чужой предел")
	}
}

// TestPublishWithinLimitRace — портирование TestPageSuggestionCreateWithinLimitRace
// на PublishWithinLimit: этот же рельс (счёт и запись в одной транзакции под
// pg_advisory_xact_lock) уже дважды оказывался сломан именно гонкой на
// соседних методах — 18 принятых из 20 при пределе 5, и оба раза это ловил
// только тест против реальной базы, а не чтение кода. Здесь тот же приём
// написан заново для своей таблицы, своего ключа блокировки
// ("collections:"+ipHash) и своего метода — структурное сходство с уже
// проверенным кодом не гарантия, что этот метод тоже устойчив.
//
// Черновики заводятся заранее (а не по одному в каждой горутине, как у
// предложений правок): PublishWithinLimit публикует существующую строку по
// id, а не создаёт новую, поэтому гонка проверяется на публикации разных
// подборок с одной и той же отметки адреса.
func TestPublishWithinLimitRace(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewCollectionRepository(pool)

	const (
		goroutines = 20
		limit      = 5
	)
	owner := int64(1)
	// Отметка своя у каждого прогона: строки живут до конца процесса, и
	// чужие остатки не должны считаться.
	ipHash := "гонка-подборок-" + time.Now().Format("150405.000000000")

	ids := make([]int64, goroutines)
	for i := 0; i < goroutines; i++ {
		c := &models.Collection{
			Title: "Подборка гонки", Slug: fmt.Sprintf("gonka-repo-%d", i),
			OwnerID: &owner, AuthorNickname: "я",
		}
		if err := repo.Create(ctx, c); err != nil {
			t.Fatalf("Create #%d: %v", i, err)
		}
		ids[i] = c.ID
	}

	var wg sync.WaitGroup
	var accepted int32
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			ok, _, err := repo.PublishWithinLimit(ctx, id, ipHash, limit, time.Now().Add(-24*time.Hour))
			if err != nil {
				t.Errorf("PublishWithinLimit: %v", err)
				return
			}
			if ok {
				atomic.AddInt32(&accepted, 1)
			}
		}(ids[i])
	}
	wg.Wait()

	if accepted != limit {
		t.Fatalf("из %d одновременных публикаций принято %d, ожидалось ровно %d",
			goroutines, accepted, limit)
	}

	// Ответ метода мало что значит сам по себе: считаем опубликованные строки
	// в базе напрямую.
	var stored int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM collections WHERE publish_ip_hash = $1 AND published_at IS NOT NULL`,
		ipHash).Scan(&stored); err != nil {
		t.Fatalf("подсчёт опубликованного: %v", err)
	}
	if stored != limit {
		t.Fatalf("в базе %d опубликованных строк с этой отметкой, ожидалось ровно %d", stored, limit)
	}
}

// TestUnpublishKeepsPublishIPHash проверяет прямо в базе, что Unpublish не
// стирает publish_ip_hash — единственный признак «было и снято», по которому
// обработчик отличает 410 от 404.
func TestUnpublishKeepsPublishIPHash(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewCollectionRepository(pool)

	owner := int64(1)
	c := &models.Collection{Title: "Уйдёт в черновик", Slug: "snyataya-repo", OwnerID: &owner, AuthorNickname: "я"}
	if err := repo.Create(ctx, c); err != nil {
		t.Fatalf("Create: %v", err)
	}

	ok, _, err := repo.PublishWithinLimit(ctx, c.ID, "адрес-снятия", 3, time.Now().Add(-24*time.Hour))
	if err != nil || !ok {
		t.Fatalf("PublishWithinLimit: ok=%v err=%v", ok, err)
	}

	if err := repo.Unpublish(ctx, c.ID); err != nil {
		t.Fatalf("Unpublish: %v", err)
	}

	// Подборка читательская (AuthorNickname непуст), поэтому ищем её парой —
	// голый GetBySlug теперь намеренно находит только сотруднические строки
	// (пустой ник), см. GetByAuthorSlug и его вызов из GetBySlug выше.
	got, err := repo.GetByAuthorSlug(ctx, c.AuthorNickname, "snyataya-repo")
	if err != nil {
		t.Fatalf("GetByAuthorSlug: %v", err)
	}
	if got.PublishedAt != nil {
		t.Errorf("published_at должен обнулиться, а он %v", got.PublishedAt)
	}
	if got.PublishIPHash != "адрес-снятия" {
		t.Errorf("publish_ip_hash %q — снятие не должно его стирать", got.PublishIPHash)
	}
}

// sluggedForTest даёт короткие уникальные слаги без завязки на пакет slug.
func sluggedForTest(i int) string {
	letters := "abcdefghij"
	return "predel-" + string(letters[i%len(letters)]) + "-repo"
}
