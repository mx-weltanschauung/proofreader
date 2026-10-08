package repository

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestReserveAttemptStopsAtLimit(t *testing.T) {
	pool := testPool(t) // тот же помощник, что у остальных тестов пакета
	repo := NewAuthAttemptRepository(pool)
	ctx := context.Background()
	since := time.Now().Add(-time.Hour)

	for i := 0; i < 3; i++ {
		ok, err := repo.ReserveAttempt(ctx, "отметка-А", "ник-А", 3, since)
		if err != nil {
			t.Fatalf("попытка %d: %v", i, err)
		}
		if !ok {
			t.Fatalf("попытка %d отвергнута, а предел ещё не выбран", i)
		}
	}

	ok, err := repo.ReserveAttempt(ctx, "отметка-А", "ник-А", 3, since)
	if err != nil {
		t.Fatalf("четвёртая попытка: %v", err)
	}
	if ok {
		t.Error("четвёртая попытка прошла при пределе три")
	}
}

// Залп параллельных попыток обязан упереться в предел целиком. Проверка и
// запись двумя шагами уже пропускали восемнадцать подач из двадцати при
// пределе пять — это тот же дефект на том же рельсе.
func TestReserveAttemptRace(t *testing.T) {
	pool := testPool(t)
	repo := NewAuthAttemptRepository(pool)
	ctx := context.Background()
	since := time.Now().Add(-time.Hour)

	const parallel = 20
	const limit = 5

	var wg sync.WaitGroup
	var mu sync.Mutex
	passed := 0
	var errs []error

	for i := 0; i < parallel; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := repo.ReserveAttempt(ctx, "отметка-Б", "ник-Б", limit, since)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				// Ошибку нельзя просто проглатывать: на сломанном рельсе
				// часть попыток может падать ошибкой вместо ok=true, и тогда
				// счёт принятых случайно совпадёт с пределом на зелёном
				// тесте при настоящей поломке.
				errs = append(errs, err)
				return
			}
			if ok {
				passed++
			}
		}()
	}
	wg.Wait()

	if len(errs) > 0 {
		t.Fatalf("%d попыток упали ошибкой, первая: %v", len(errs), errs[0])
	}
	if passed != limit {
		t.Errorf("прошло %d попыток при пределе %d", passed, limit)
	}
}

func TestClearForgetsAttempts(t *testing.T) {
	pool := testPool(t)
	repo := NewAuthAttemptRepository(pool)
	ctx := context.Background()
	since := time.Now().Add(-time.Hour)

	for i := 0; i < 3; i++ {
		if _, err := repo.ReserveAttempt(ctx, "отметка-В", "ник-В", 3, since); err != nil {
			t.Fatalf("попытка %d: %v", i, err)
		}
	}
	if err := repo.Clear(ctx, "отметка-В", "ник-В"); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	ok, err := repo.ReserveAttempt(ctx, "отметка-В", "ник-В", 3, since)
	if err != nil {
		t.Fatalf("после Clear: %v", err)
	}
	if !ok {
		t.Error("после успешного входа попытки не забыты")
	}
}

// TestClearDoesNotResetOtherNickname — единственный тест, ловящий исходный
// дефект: подбирающий девять раз пробует чужой ник (Г), десятым входит в
// свою учётку (Д) и рассчитывает, что Clear своей пары откроет ему новый
// круг подбора чужой. Clear обязан стирать счёт СВОЕЙ пары и не трогать
// счёт чужой — иначе предел на нике Г обходится бесплатно через вход в
// собственную учётку с того же адреса.
func TestClearDoesNotResetOtherNickname(t *testing.T) {
	pool := testPool(t)
	repo := NewAuthAttemptRepository(pool)
	ctx := context.Background()
	since := time.Now().Add(-time.Hour)

	const limit = 3
	for i := 0; i < limit; i++ {
		ok, err := repo.ReserveAttempt(ctx, "отметка-Г", "ник-Г", limit, since)
		if err != nil {
			t.Fatalf("попытка %d к нику-Г: %v", i, err)
		}
		if !ok {
			t.Fatalf("попытка %d к нику-Г отвергнута, а предел ещё не выбран", i)
		}
	}
	ok, err := repo.ReserveAttempt(ctx, "отметка-Г", "ник-Г", limit, since)
	if err != nil {
		t.Fatalf("попытка сверх предела к нику-Г: %v", err)
	}
	if ok {
		t.Fatalf("предел ника-Г уже должен быть исчерпан")
	}

	// Успешный вход в СВОЮ учётку с того же адреса.
	if err := repo.Clear(ctx, "отметка-Г", "ник-Д"); err != nil {
		t.Fatalf("Clear своей пары: %v", err)
	}

	ok, err = repo.ReserveAttempt(ctx, "отметка-Г", "ник-Г", limit, since)
	if err != nil {
		t.Fatalf("попытка к нику-Г после чужого Clear: %v", err)
	}
	if ok {
		t.Error("Clear своей пары открыл новые попытки к чужому нику — предел обойдён")
	}
}

// TestReserveAttemptCountsPerNickname проверяет вторую, независимую половину
// довода за составной ключ: общий NAT сотовых операторов. С одного адреса
// исчерпывается предел для ника Е, и попытка к НИКУ Ж с того же адреса
// обязана пройти — счёт ведётся по паре (адрес, ник), а не по голому адресу,
// иначе читатели одной соты делили бы один бюджет попыток.
//
// Clear здесь намеренно не вызывается ни разу: TestClearDoesNotResetOtherNickname
// уже покрывает изоляцию счёта через Clear, а этот тест обязан ловить регресс
// именно в ReserveAttempt — если бы Clear остался верным (по паре), а
// ReserveAttempt откатился к счёту по голому ip_hash, тот тест не заметил бы
// ничего: он проверяет только один ник и не трогает поведение подсчёта для
// второго.
func TestReserveAttemptCountsPerNickname(t *testing.T) {
	pool := testPool(t)
	repo := NewAuthAttemptRepository(pool)
	ctx := context.Background()
	since := time.Now().Add(-time.Hour)

	const limit = 3
	for i := 0; i < limit; i++ {
		ok, err := repo.ReserveAttempt(ctx, "отметка-Е", "ник-Е", limit, since)
		if err != nil {
			t.Fatalf("попытка %d к нику-Е: %v", i, err)
		}
		if !ok {
			t.Fatalf("попытка %d к нику-Е отвергнута, а предел ещё не выбран", i)
		}
	}
	ok, err := repo.ReserveAttempt(ctx, "отметка-Е", "ник-Е", limit, since)
	if err != nil {
		t.Fatalf("попытка сверх предела к нику-Е: %v", err)
	}
	if ok {
		t.Fatalf("предел ника-Е уже должен быть исчерпан")
	}

	ok, err = repo.ReserveAttempt(ctx, "отметка-Е", "ник-Ж", limit, since)
	if err != nil {
		t.Fatalf("попытка к нику-Ж с того же адреса: %v", err)
	}
	if !ok {
		t.Error("попытка к другому нику с того же адреса отвергнута — счёт ведётся по голому адресу")
	}
}
