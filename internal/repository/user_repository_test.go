package repository

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"proofreader/internal/models"
)

// Пустую базу готовит testPool — см. main_test.go. Пул общий на пакет и
// живёт до конца прогона, поэтому уборка строки ниже не уходит в закрытый пул.
func TestCountByRole(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)

	repo := NewUserRepository(pool)
	u := &models.User{Email: "countbyrole-probe@example.test", PasswordHash: "h", Role: models.RoleAdministrator}
	if err := repo.Create(ctx, u); err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _ = repo.Delete(ctx, u.ID) }) // clean up only the row we created, by ID

	n, err := repo.CountByRole(ctx, models.RoleAdministrator)
	if err != nil {
		t.Fatalf("CountByRole: %v", err)
	}
	if n < 1 {
		t.Fatalf("CountByRole(admin) = %d, want >= 1", n)
	}
}

// seedReader заводит читателя прямо запросом: CreateReaderWithinLimit тянет за
// собой предел частоты и отметку адреса, к списку отношения не имеющие.
func seedReader(t *testing.T, pool *pgxpool.Pool, nickname string) int64 {
	t.Helper()
	var id int64
	err := pool.QueryRow(context.Background(), `
		INSERT INTO users (email, password_hash, role, nickname)
		VALUES (NULL, 'h', 'reader', $1) RETURNING id`, nickname).Scan(&id)
	if err != nil {
		t.Fatalf("seed reader %q: %v", nickname, err)
	}
	return id
}

// seedCollection кладёт подборку под снимком ника. Черновик — published = false.
func seedCollection(t *testing.T, pool *pgxpool.Pool, authorNickname, slug string, published bool) {
	t.Helper()
	var publishedAt *time.Time
	if published {
		now := time.Now()
		publishedAt = &now
	}
	_, err := pool.Exec(context.Background(), `
		INSERT INTO collections (title, slug, description, author_nickname, published_at)
		VALUES ($1, $2, '', $3, $4)`, "подборка "+slug, slug, authorNickname, publishedAt)
	if err != nil {
		t.Fatalf("seed collection %q: %v", slug, err)
	}
}

// Администратор ищет читателя по нику из жалобы. Список обязан показывать
// только читателей — сотрудник в него не попадает, иначе управление
// сотрудниками и читательский список смешались бы обратно, — и считать
// подборки по НОРМАЛИЗОВАННОМУ снимку ника: подписался «Чтец», ищут «чтец».
func TestListReadersReturnsReadersWithCollectionCount(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	repo := NewUserRepository(pool)

	seedReader(t, pool, "Чтец")
	seedReader(t, pool, "книгочей")
	// Сотрудник: в фикстуре уже есть администратор id = 1, добавим редактора.
	if err := repo.Create(ctx, &models.User{
		Email: "editor@test.local", PasswordHash: "h", Role: models.RoleEditor,
	}); err != nil {
		t.Fatalf("create editor: %v", err)
	}
	// Одна опубликованная и один черновик — администратор обязан видеть оба.
	seedCollection(t, pool, "Чтец", "ranniy-marks", true)
	seedCollection(t, pool, "чтец", "chernovik", false)

	rows, err := repo.ListReaders(ctx, "", 100)
	if err != nil {
		t.Fatalf("ListReaders: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("ListReaders вернул %d строк, ждём 2 (только читатели): %+v", len(rows), rows)
	}
	byNick := map[string]ReaderRow{}
	for _, row := range rows {
		byNick[row.Nickname] = row
	}
	reader, ok := byNick["Чтец"]
	if !ok {
		t.Fatalf("читателя «Чтец» нет в списке: %+v", rows)
	}
	if reader.CollectionCount != 2 {
		t.Errorf("подборок у «Чтец» = %d, ждём 2 (опубликованная и черновик)", reader.CollectionCount)
	}
	if byNick["книгочей"].CollectionCount != 0 {
		t.Errorf("подборок у «книгочей» = %d, ждём 0", byNick["книгочей"].CollectionCount)
	}
	if reader.ID == 0 || reader.CreatedAt.IsZero() {
		t.Errorf("строка читателя пришла без id или даты: %+v", reader)
	}
}

// Ник из письма приходит как придётся — куском и в другом регистре. Поиск
// обязан сравнивать нормализованно, той же формой, что держит уникальность
// ника, иначе администратор не найдёт того, на кого жалуются.
//
// Регистр и NFKC — всё, что нормализация сводит. «Ё» с «е» она НЕ сравнивает:
// в отличие от полнотекстового поиска, где ё→е делает unaccent перед стеммером
// (конфиг ru), у ников такого шага нет и быть не может — ключ уникальности
// один и тот же столбец, и сведение ё изменило бы, какие ники считаются
// занятыми.
func TestListReadersFiltersByNicknameFragment(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	repo := NewUserRepository(pool)

	seedReader(t, pool, "Чтец")
	seedReader(t, pool, "книгочей")

	rows, err := repo.ListReaders(ctx, "ТЕЦ", 100)
	if err != nil {
		t.Fatalf("ListReaders: %v", err)
	}
	if len(rows) != 1 || rows[0].Nickname != "Чтец" {
		t.Fatalf("поиск «ТЕЦ» дал %+v, ждём одного «Чтец»", rows)
	}
}

// Знаки шаблона LIKE в поле поиска — не шаблон, а буквы. Без экранирования
// запрос «%» показал бы всех читателей разом, то есть выдал бы личные данные
// на пустом месте, а «_» тихо расширял бы выборку.
func TestListReadersTreatsLikeWildcardsAsLiterals(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	repo := NewUserRepository(pool)

	seedReader(t, pool, "Чтец")
	seedReader(t, pool, "книгочей")

	// Полноширинные «％» и «＿» здесь не экзотика ради экзотики: NFKC
	// превращает их в обычные % и _, поэтому экранирование ДО нормализации
	// их пропускает — знак уезжает в запрос буквой, а возвращается шаблоном.
	// Проверено на Postgres 15 проекта: normalize(U&'\FF05', NFKC) = '%'.
	for _, q := range []string{"%", "_", "ч%й", "％", "＿"} {
		rows, err := repo.ListReaders(ctx, q, 100)
		if err != nil {
			t.Fatalf("ListReaders(%q): %v", q, err)
		}
		if len(rows) != 0 {
			t.Errorf("поиск %q дал %d строк, ждём 0: знаки шаблона должны искаться буквально", q, len(rows))
		}
	}
}

// Уход читателя не уносит его труд: подборки, правки и разборы остаются,
// теряя владельца (ON DELETE SET NULL). Подпись при этом жива снимком ника,
// и осиротевшая вещь перестаёт быть редактируемой ВОВСЕ — ни бывшим
// владельцем, ни редактором «по доброте» (mayEditDocument/mayEdit).
func TestDeleteReaderOrphansButKeepsWork(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	users := NewUserRepository(pool)
	docs := NewDocumentRepository(pool)

	nick := "уходящий"
	reader := &models.User{Email: "", Role: models.RoleReader, Nickname: &nick}
	if err := users.Create(ctx, reader); err != nil {
		t.Fatalf("завести читателя: %v", err)
	}
	doc := &models.Document{Title: "Разбор", MarkdownContent: "тело",
		OwnerID: &reader.ID, AuthorNickname: nick, Slug: "razbor-uhodyaschego"}
	if err := docs.Create(ctx, doc); err != nil {
		t.Fatalf("создать разбор: %v", err)
	}

	if err := users.DeleteReader(ctx, reader.ID); err != nil {
		t.Fatalf("удалить читателя: %v", err)
	}

	after, err := docs.GetByID(ctx, doc.ID)
	if err != nil {
		t.Fatalf("разбор исчез вместе с автором: %v", err)
	}
	if after.OwnerID != nil {
		t.Fatal("владелец не обнулён")
	}
	if after.AuthorNickname != nick {
		t.Fatalf("подпись потеряна: %q", after.AuthorNickname)
	}
}

// Дверь читателя не годится сотруднику: администратор, снявший сам себя,
// прошёл бы мимо охраны последнего администратора, которая живёт в /users.
func TestDeleteReaderRefusesStaffRow(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	users := NewUserRepository(pool)

	staff := &models.User{Email: "editor-delete-probe@example.org", Role: models.RoleEditor}
	if err := users.Create(ctx, staff); err != nil {
		t.Fatalf("завести редактора: %v", err)
	}
	if err := users.DeleteReader(ctx, staff.ID); err == nil {
		t.Fatal("сотрудник удалён читательской дверью")
	}
	if _, err := users.GetByID(ctx, staff.ID); err != nil {
		t.Fatalf("строка сотрудника всё же исчезла: %v", err)
	}
}
