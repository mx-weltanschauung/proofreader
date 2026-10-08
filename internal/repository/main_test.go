package repository

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Общая фикстура интеграционных тестов пакета.
//
// До неё каждый файл убирал за собой сам: одни делали свой TRUNCATE по своему
// списку таблиц, другие полагались на строки, оставленные соседом (владелец
// works.owner_id = 1 заводился в seo-фикстуре, а пользовались им все). Тесты
// идут в одном процессе и одной базе, а порядок их сдвигает любое добавление
// теста — и пакет целиком падал там, где каждый тест порознь проходил.
// Поэтому дверь одна: testPool приводит базу к известному пустому состоянию
// ровно один раз на тест, и ни один тест не видит чужого состояния.
const (
	// testDBEnv — одноразовая база, которую тесты вправе опустошать.
	testDBEnv = "PROOFREADER_TEST_DB_URL"
	// fixtureIDFloor отодвигает последовательности за полосу id, которые
	// фикстуры вставляют руками (works 10—40, chapters 100—301). Без этого
	// выданный последовательностью id рано или поздно упёрся бы в явный.
	fixtureIDFloor = 1000
)

var (
	poolOnce   sync.Once
	sharedPool *pgxpool.Pool
	poolErr    error

	// lastReset — имя теста, под который база уже очищена. Второй вызов
	// testPool внутри того же теста обязан отдать пул, а не смыть то, что
	// тест успел насеять.
	lastReset string
)

func TestMain(m *testing.M) {
	code := m.Run()
	if sharedPool != nil {
		sharedPool.Close()
	}
	os.Exit(code)
}

// testPool отдаёт пул к одноразовой тестовой базе, предварительно приведя её
// к пустому состоянию с единственным пользователем id = 1 (владелец работ).
// Без переменной окружения тест пропускается: рабочая база разработчика не
// должна пострадать никогда.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv(testDBEnv)
	if dsn == "" {
		t.Skip("set " + testDBEnv + " to a throwaway DB to run repo integration tests")
	}
	poolOnce.Do(func() {
		sharedPool, poolErr = pgxpool.New(context.Background(), dsn)
	})
	if poolErr != nil {
		t.Fatalf("не удалось подключиться к тестовой БД: %v", poolErr)
	}
	// Подтест не смывает данные родителя: очистка привязана к корневому тесту.
	root := t.Name()
	if i := strings.Index(root, "/"); i >= 0 {
		root = root[:i]
	}
	if root != lastReset {
		resetDB(t, sharedPool)
		lastReset = root
	}
	return sharedPool
}

// resetDB опустошает все таблицы данных и заводит владельца. Список таблиц и
// последовательностей берётся из самой базы, а не из рукописного перечня:
// иначе таблица, добавленная очередной миграцией, тихо осталась бы протекать
// между тестами — ровно тем способом, каким этот дефект и появился.
func resetDB(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
		DO $$
		DECLARE tables text;
		BEGIN
			SELECT string_agg(format('public.%I', tablename), ', ')
			  INTO tables
			  FROM pg_tables
			 WHERE schemaname = 'public' AND tablename <> 'schema_migrations';
			IF tables IS NOT NULL THEN
				EXECUTE 'TRUNCATE ' || tables || ' RESTART IDENTITY CASCADE';
			END IF;
		END $$;`); err != nil {
		t.Fatalf("не удалось очистить тестовую БД: %v", err)
	}
	// DO-блок параметров не принимает, поэтому пол подставляется в текст.
	// Значение — константа пакета, не пользовательский ввод.
	if _, err := pool.Exec(ctx, fmt.Sprintf(`
		DO $$
		DECLARE s text;
		BEGIN
			FOR s IN SELECT format('%%I.%%I', sequence_schema, sequence_name)
			           FROM information_schema.sequences
			          WHERE sequence_schema = 'public'
			LOOP
				EXECUTE format('SELECT setval(%%L, %d, true)', s);
			END LOOP;
		END $$;`, fixtureIDFloor)); err != nil {
		t.Fatalf("не удалось отодвинуть последовательности: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id, email, password_hash, role)
		VALUES (1, 'owner@test.local', 'x', 'administrator')`); err != nil {
		t.Fatalf("не удалось создать владельца работ: %v", err)
	}
}
