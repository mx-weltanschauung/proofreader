package repository

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SharedTestPool открывает общую тестовую базу пакета внешнему тестовому
// пакету repository_test (сквозной обход каталога OPDS через роутер API).
// Тот обязан жить здесь, а не в internal/api: тесты этого пакета усекают все
// таблицы перед каждым корневым тестом, и параллельно идущий пакет api
// терял бы фикстуру посреди обхода. Внутри одного пакета тесты идут по
// очереди.
func SharedTestPool(t *testing.T) *pgxpool.Pool { return testPool(t) }
