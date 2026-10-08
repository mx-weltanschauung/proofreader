package repository

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"proofreader/internal/models"
)

// seedWorkWithPage создаёт работу-том и одну страницу с page_number = 1,
// заполненную переданным текстом, и удаляет их по завершении теста (страница
// уходит каскадом вместе с работой).
func seedWorkWithPage(t *testing.T, pool *pgxpool.Pool, markdown string) (workID, pageID int64) {
	t.Helper()
	ctx := context.Background()

	workRepo := NewWorkRepository(pool)
	w := &models.Work{
		Title: "проба-предложений", Author: "проба", Language: "ru", Country: "ru",
		Status: models.WorkStatusDraft, Role: models.WorkRoleVolume,
		NumberingStyle: models.NumberingArabic, OwnerID: 1,
	}
	if err := workRepo.Create(ctx, w); err != nil {
		t.Fatalf("seedWorkWithPage: create work: %v", err)
	}
	t.Cleanup(func() { _ = workRepo.Delete(context.Background(), w.ID) })

	pageRepo := NewPageRepository(pool)
	p := &models.Page{
		WorkID: w.ID, PageNumber: 1, ContentMarkdown: markdown,
		Status: models.PageStatusNotProofread,
	}
	if err := pageRepo.Create(ctx, p); err != nil {
		t.Fatalf("seedWorkWithPage: create page: %v", err)
	}

	return w.ID, p.ID
}

func TestPageSuggestionCreateAndList(t *testing.T) {
	pool := testPool(t) // пропустит тест без PROOFREADER_TEST_DB_URL
	ctx := context.Background()
	repo := NewPageSuggestionRepository(pool)

	workID, pageID := seedWorkWithPage(t, pool, "Текст полосы до правки")

	s := &models.PageSuggestion{
		PageID:           pageID,
		BaseMarkdown:     "Текст полосы до правки",
		ProposedMarkdown: "Текст полосы после правки",
		Note:             "опечатка в третьей строке",
		IPHash:           "deadbeef",
	}
	if err := repo.Create(ctx, s); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if s.ID == 0 || s.Status != models.SuggestionNew {
		t.Fatalf("Create не заполнил id/статус: %+v", s)
	}

	newStatus := models.SuggestionNew
	rows, total, err := repo.List(ctx, &newStatus, 10, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 1 || len(rows) != 1 {
		t.Fatalf("ожидалась одна строка, получено total=%d len=%d", total, len(rows))
	}
	if rows[0].WorkID != workID || rows[0].PageNumber != 1 {
		t.Fatalf("координаты полосы не подтянулись: %+v", rows[0])
	}
	if rows[0].Stale {
		t.Fatal("основа совпадает с текущим текстом, устареванию взяться неоткуда")
	}
}

func TestPageSuggestionStaleWhenPageChanged(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewPageSuggestionRepository(pool)

	_, pageID := seedWorkWithPage(t, pool, "Исходный текст")
	s := &models.PageSuggestion{
		PageID: pageID, BaseMarkdown: "Исходный текст",
		ProposedMarkdown: "Исправленный текст",
	}
	if err := repo.Create(ctx, s); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := pool.Exec(ctx,
		`UPDATE pages SET content_markdown = $1 WHERE id = $2`,
		"Текст после прогона машинной вычитки", pageID); err != nil {
		t.Fatalf("правка полосы: %v", err)
	}

	d, err := repo.GetDetail(ctx, s.ID)
	if err != nil {
		t.Fatalf("GetDetail: %v", err)
	}
	if !d.Stale {
		t.Fatal("полоса изменилась — предложение обязано считаться устаревшим")
	}
	if d.CurrentMarkdown != "Текст после прогона машинной вычитки" {
		t.Fatalf("текущий текст не тот: %q", d.CurrentMarkdown)
	}

	// Возврат к прежнему тексту гасит признак: он вычисляется, а не хранится.
	if _, err := pool.Exec(ctx,
		`UPDATE pages SET content_markdown = $1 WHERE id = $2`,
		"Исходный текст", pageID); err != nil {
		t.Fatalf("возврат текста: %v", err)
	}
	d, err = repo.GetDetail(ctx, s.ID)
	if err != nil {
		t.Fatalf("GetDetail после возврата: %v", err)
	}
	if d.Stale {
		t.Fatal("текст вернули — признак устаревания обязан погаснуть")
	}
}

// TestPageSuggestionStaleFalseAfterAccepted проверяет обратную сторону
// TestPageSuggestionStaleWhenPageChanged: принятие предложения само пишет
// текст модератора в полосу, поэтому base_markdown и content_markdown после
// этого расходятся всегда — и это не «устарело», а результат решения.
// Признак должен считаться только для неразобранных предложений.
func TestPageSuggestionStaleFalseAfterAccepted(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewPageSuggestionRepository(pool)

	_, pageID := seedWorkWithPage(t, pool, "Исходный текст")
	s := &models.PageSuggestion{
		PageID: pageID, BaseMarkdown: "Исходный текст",
		ProposedMarkdown: "Исправленный текст",
	}
	if err := repo.Create(ctx, s); err != nil {
		t.Fatalf("Create: %v", err)
	}

	moderator := int64(1)
	if err := repo.Resolve(ctx, s.ID, models.SuggestionAccepted, nil, moderator); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	// Принятие пишет итоговый текст модератора в полосу — эмулируем шаг,
	// который в обработчике делает accept.
	if _, err := pool.Exec(ctx,
		`UPDATE pages SET content_markdown = $1 WHERE id = $2`,
		"Исправленный текст", pageID); err != nil {
		t.Fatalf("правка полосы после принятия: %v", err)
	}

	d, err := repo.GetDetail(ctx, s.ID)
	if err != nil {
		t.Fatalf("GetDetail: %v", err)
	}
	if d.BaseMarkdown == d.CurrentMarkdown {
		t.Fatalf("тест не проверяет то, что заявлено: base и current совпали")
	}
	if d.Stale {
		t.Fatal("предложение разобрано (принято) — устаревшим оно быть не должно, даже когда base и current разошлись")
	}
}

func TestPageSuggestionResolveOnce(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewPageSuggestionRepository(pool)

	_, pageID := seedWorkWithPage(t, pool, "Текст")
	s := &models.PageSuggestion{
		PageID: pageID, BaseMarkdown: "Текст", ProposedMarkdown: "Текст исправленный",
	}
	if err := repo.Create(ctx, s); err != nil {
		t.Fatalf("Create: %v", err)
	}

	moderator := int64(1)
	if err := repo.Resolve(ctx, s.ID, models.SuggestionAccepted, nil, moderator); err != nil {
		t.Fatalf("первое решение: %v", err)
	}
	err := repo.Resolve(ctx, s.ID, models.SuggestionAccepted, nil, moderator)
	if err != ErrSuggestionResolved {
		t.Fatalf("повторное решение должно давать ErrSuggestionResolved, получено %v", err)
	}
}

// TestPageSuggestionCreateWithinLimitWindow проверяет то же, что раньше
// проверял TestPageSuggestionCountRecentByIP (окно считается по времени, а не
// по всей истории отметки, чужая отметка в счёт не идёт), но через новый
// атомарный метод: счёт и запись теперь одно действие, отдельного метода
// счёта больше нет.
func TestPageSuggestionCreateWithinLimitWindow(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewPageSuggestionRepository(pool)

	_, pageID := seedWorkWithPage(t, pool, "Текст")
	mine := "отметка-окна-" + t.Name()
	other := "чужая-отметка-" + t.Name()

	for i := 0; i < 3; i++ {
		s := &models.PageSuggestion{
			PageID: pageID, BaseMarkdown: "Текст",
			ProposedMarkdown: "Текст правка " + string(rune('а'+i)),
			IPHash:           mine,
		}
		ok, err := repo.CreateWithinLimit(ctx, s, 10, time.Now().Add(-time.Hour))
		if err != nil {
			t.Fatalf("CreateWithinLimit %d: %v", i, err)
		}
		if !ok {
			t.Fatalf("предложение %d отбито пределом, хотя предел 10", i+1)
		}
		if s.ID == 0 || s.Status != models.SuggestionNew || s.CreatedAt.IsZero() {
			t.Fatalf("CreateWithinLimit не заполнил id/статус/время: %+v", s)
		}
	}

	// Чужая отметка в счёт не идёт: при том же пределе 3 она проходит.
	foreign := &models.PageSuggestion{
		PageID: pageID, BaseMarkdown: "Текст", ProposedMarkdown: "Текст чужой правки",
		IPHash: other,
	}
	if ok, err := repo.CreateWithinLimit(ctx, foreign, 3, time.Now().Add(-time.Hour)); err != nil || !ok {
		t.Fatalf("CreateWithinLimit(чужая отметка): ok=%v err=%v", ok, err)
	}

	// Предел 3 своими же тремя исчерпан.
	fourth := &models.PageSuggestion{
		PageID: pageID, BaseMarkdown: "Текст", ProposedMarkdown: "Текст четвёртой правки",
		IPHash: mine,
	}
	ok, err := repo.CreateWithinLimit(ctx, fourth, 3, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("CreateWithinLimit: %v", err)
	}
	if ok {
		t.Error("предел (3) исчерпан своими же предложениями, четвёртое не должно приниматься")
	}

	// Окно приходит готовым временем, поэтому «час вперёд» проверяется без
	// ожидания: граница именно по времени, а не «всё, что было с этой отметки».
	fifth := &models.PageSuggestion{
		PageID: pageID, BaseMarkdown: "Текст", ProposedMarkdown: "Текст пятой правки",
		IPHash: mine,
	}
	ok, err = repo.CreateWithinLimit(ctx, fifth, 3, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("CreateWithinLimit (будущее окно): %v", err)
	}
	if !ok {
		t.Error("окно в будущем должно давать 0 совпадений и пропускать предложение")
	}
}

// TestPageSuggestionCreateWithinLimitRace — находка финальной проверки ветки:
// счёт и вставка шли двумя раздельными обращениями к пулу (CountRecentByIP,
// затем Create), без транзакции и без блокировки. Двадцать одновременных
// подач читали recent = 0 и вставлялись все двадцать — «5 в час»
// превращалось в «сколько угодно, залпами», каждая строка до 200 000 знаков.
// Ловушка-honeypot этому не мешает: бот её просто не заполняет.
//
// Подставным складом такое не показать: гонка живёт в двух round-trip'ах к
// Postgres. Тест идёт против одноразовой базы и требует РОВНО предела.
func TestPageSuggestionCreateWithinLimitRace(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewPageSuggestionRepository(pool)

	_, pageID := seedWorkWithPage(t, pool, "Текст полосы")

	const (
		goroutines = 20
		limit      = 5
	)
	// Отметка своя у каждого прогона: строки живут до конца теста (уходят
	// каскадом вместе с работой), и чужие остатки не должны считаться.
	ipHash := "гонка-" + time.Now().Format("150405.000000000")

	var wg sync.WaitGroup
	var accepted int32
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s := &models.PageSuggestion{
				PageID:           pageID,
				BaseMarkdown:     "Текст полосы",
				ProposedMarkdown: fmt.Sprintf("Текст полосы, правка %d", i),
				IPHash:           ipHash,
			}
			ok, err := repo.CreateWithinLimit(ctx, s, limit, time.Now().Add(-time.Hour))
			if err != nil {
				t.Errorf("CreateWithinLimit: %v", err)
				return
			}
			if ok {
				atomic.AddInt32(&accepted, 1)
			}
		}(i)
	}
	wg.Wait()

	if accepted != limit {
		t.Fatalf("из %d одновременных подач принято %d, ожидалось ровно %d",
			goroutines, accepted, limit)
	}

	// Ответ метода мало что значит сам по себе: считаем строки в базе.
	var stored int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM page_suggestions WHERE ip_hash = $1`, ipHash).Scan(&stored); err != nil {
		t.Fatalf("подсчёт записанного: %v", err)
	}
	if stored != limit {
		t.Fatalf("в базе %d строк с этой отметкой, ожидалось ровно %d", stored, limit)
	}
}
