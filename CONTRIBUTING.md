# Как вносить правки

## Сборка и тесты

Нужны Go (версия — в `go.mod`), Node.js 22, Docker с Compose, системные
`poppler-utils` и `djvulibre-bin`.

```bash
cp env.example .env
make docker-up && make migrate-up
make run                                   # API на :8080
cd frontend && npm install && npm run dev  # интерфейс на :3100
```

Перед отправкой правки:

```bash
make test           # Go
make lint           # golangci-lint
make test-scripts   # bash-тесты scripts/
make leak-guard     # сторож утечек
cd frontend && npm run lint && npm test && npm run build
```

`make lint` сейчас показывает около сотни старых находок (в основном
`errcheck` — непроверенные `Close`/`Write`); их разбор — отдельная работа.
CI на pull request гоняет линтер только по новому коду
(`only-new-issues`): правка не должна добавлять находок.

Тесты `internal/repository` требуют одноразовую базу
(`PROOFREADER_TEST_DB_URL=postgres://…/proofreader_test`), иначе молча
пропускаются: для правок в репозиториях гоняйте их с базой.

`npm install` включает pre-commit хук (`core.hooksPath=frontend/.husky`):
eslint и prettier по изменённым файлам фронта.

## Язык

Интерфейс, комментарии, документация и сообщения коммитов — по-русски.
Комментарий объясняет **почему**, а не что делает строка.

## Что нельзя сломать

- **Роли.** Каждый маршрут `internal/api/router.go` классифицирован в
  `routeAccess` (`router_roles_test.go`); новый маршрут без строки там
  роняет тест.
- **Статусы полос** — русские строки (`не_вычитана`, `вычитана`, …),
  сохраняйте их дословно.
- **Единственный путь записи текста полосы** — `applyPageEdit`
  (`internal/api/page_edit.go`). Новый путь, минуя его, ломает версии и
  ссылки указателя молча.
- **Текст читателя** рендерится только через
  `markdown.Renderer.ForUntrustedAuthor`.
- **Миграции** не переписываются задним числом: только новые файлы.
- **Ничего конкретной читальни в коде.** Имя, домен, контакты, ключи —
  настройки экземпляра (`docs/SELF_HOSTING.md`). `make leak-guard` ловит
  ключи, токены и публичные адреса.

## Правка

1. Задача или обсуждение — в Issues.
2. Ветка от `main`, правка с тестом: тест сначала падает без правки и
   проходит с ней.
3. Pull request с описанием, зачем правка и как она проверена.

Отправляя правку, вы соглашаетесь распространять её под лицензией
репозитория (AGPL-3.0).
