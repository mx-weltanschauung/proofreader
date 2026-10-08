package repository

import (
	"context"
	"testing"
	"time"

	"proofreader/internal/models"
)

// createReader заводит читателя ТЕМ ЖЕ путём, что и живая дверь
// (CreateReaderWithinLimit): UserRepository.Create ник не пишет вовсе, и
// тест на нём проверял бы строку без nickname_key — то есть не то.
func createReader(t *testing.T, users *UserRepository, nickname, ipHash string) *models.User {
	t.Helper()
	nick := nickname
	user := &models.User{Role: models.RoleReader, Nickname: &nick, SignupIPHash: ipHash}
	ok, err := users.CreateReaderWithinLimit(context.Background(), user, 100, time.Now().Add(-24*time.Hour))
	if err != nil || !ok {
		t.Fatalf("завести читателя %q: ok=%v err=%v", nickname, ok, err)
	}
	return user
}

// I4 против настоящего Postgres: уход читателя отставляет его ник ТОЙ ЖЕ
// транзакцией, что сносит строку, и отставка держится тем же ключом
// (lower(normalize(…, NFKC))), которым база держит уникальность ника.
func TestDeleteReaderRetiresNicknameInOneTransaction(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	users := NewUserRepository(pool)

	nick := "Уходящий"
	reader := createReader(t, users, nick, "отметка-ухода")

	if retired, err := users.IsNicknameRetired(ctx, nick); err != nil || retired {
		t.Fatalf("живой ник числится отставленным: retired=%v err=%v", retired, err)
	}

	if err := users.DeleteReader(ctx, reader.ID); err != nil {
		t.Fatalf("уход читателя: %v", err)
	}

	// Строка снесена И имя отставлено — обе половины одного действия.
	if _, err := users.GetByNickname(ctx, nick); err == nil {
		t.Error("строка читателя пережила уход")
	}
	for _, form := range []string{"Уходящий", "уходящий", "УХОДЯЩИЙ", "  уходящий  "} {
		retired, err := users.IsNicknameRetired(ctx, trimForKey(form))
		if err != nil {
			t.Fatalf("проверка отставки %q: %v", form, err)
		}
		if !retired {
			t.Errorf("форма %q не накрыта отставкой — ключ разошёлся с users.nickname_key", form)
		}
	}

	// Посторонний ник свободен: отставка накрывает имя, а не дверь целиком.
	if retired, err := users.IsNicknameRetired(ctx, "новичок"); err != nil || retired {
		t.Fatalf("посторонний ник числится отставленным: retired=%v err=%v", retired, err)
	}
}

// Повторный уход по тому же id не роняется на уже отставленном ключе
// (ON CONFLICT DO NOTHING) и честно отвечает «не найдено» — строки уже нет.
func TestDeleteReaderTwiceIsNotFoundNotDuplicateKey(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	users := NewUserRepository(pool)

	reader := createReader(t, users, "дважды", "отметка-дважды")
	if err := users.DeleteReader(ctx, reader.ID); err != nil {
		t.Fatalf("первый уход: %v", err)
	}
	err := users.DeleteReader(ctx, reader.ID)
	if err == nil {
		t.Fatal("повторный уход прошёл успешно")
	}
	if err.Error() != "reader not found" {
		t.Fatalf("повторный уход: получено %v, ожидалось \"reader not found\"", err)
	}
}

// trimForKey повторяет то, что делает вызывающий из обработчика: SQL
// normalize() пробелы не режет, поэтому строку тримит Go — см. докстроку
// models.NormalizeNickname.
func trimForKey(s string) string {
	return models.NormalizeNickname(s)
}
