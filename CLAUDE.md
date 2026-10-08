# CLAUDE.md

Руководство для Claude Code (и любого нового разработчика) по коду этой
платформы. Язык кода и текстов — русский.

## Что это

Proofreader — платформа «читальни»: сканы книг (PDF/DJVU) превращаются в
постраничный текст в Markdown, который вычитывают и читают онлайн. Go-бэкенд
отдаёт JSON API, React-SPA — интерфейс читателя и редактора.

Модель: **работа** (том) состоит из **полос** (превью скана +
`content_markdown`), полосы сгруппированы в **главы**; **издания**
собирают тома в собрания; **разборы** — авторский текст читателя с
вклейками из корпуса; **подборки** — свой порядок глав и работ;
**предметный указатель** — понятия с адресами на полосы. У тома бывают
служебные работы (`parent_work_id`, `role = front_matter`) — титул,
содержание, предисловие; в каталоге их нет, они видны с карточки тома.

## Команды

Бэкенд (из корня, `make help` — все цели):

```bash
make docker-up          # Postgres и SeaweedFS (нужны до запуска)
make migrate-up         # миграции (golang-migrate)
make migrate-down       # откат ОДНОЙ миграции — сперва прочти её .down.sql:
                        # некоторые сносят данные целиком
make run                # API на :8080
make build              # -> bin/server
make test               # go test ./...
make lint               # golangci-lint
make test-scripts       # тесты scripts/ (bash)
make leak-guard         # сторож утечек (см. ниже)
```

Фронтенд (из `frontend/`): `npm install`, `npm run dev` (:3100),
`npm run build`, `npm run lint` (без предупреждений), `npm test` (vitest).
`npm install` ставит `core.hooksPath=frontend/.husky` на весь репозиторий:
pre-commit гоняет eslint и prettier по изменённым файлам фронта. Пакет
`husky` не используется — он ищет `.git` строго рядом с `package.json`.

Весь стек в Docker: `docker compose up --build` — Postgres, SeaweedFS,
миграции, бэкенд и фронт под nginx; читальня на http://localhost:3100,
вход `admin@proofreader.local` / `admin`.

Тесты `internal/repository` идут только с одноразовой базой в
`PROOFREADER_TEST_DB_URL`; без неё они молча пропускаются.

## Экземпляр

Всё, чем одна читальня отличается от другой, задаётся снаружи кода; образы у
всех одни (подробно — `docs/SELF_HOSTING.md`):

- **окружение** (`internal/config.SiteConfig`, `env.example`): `SITE_NAME`,
  `SITE_DESCRIPTION`,
  `SITE_SUPPORT_URL`, `SITE_CHANNEL_URL`, `SITE_AGE_RATING`, `SITE_TAGLINE`,
  `RESERVED_NICKNAMES`, `PUBLIC_BASE_URL`. Имя и описание сервер держит в
  `internal/site` (ставятся один раз на старте), фронт получает их через
  `GET /api/site` (`frontend/src/services/site.ts`, умолчание до ответа);
- **каталог `instance/`** монтируется во фронт: nginx берёт файл отсюда
  раньше файла сборки (иконки, `site.webmanifest`, ключи поисковиков).
  `legal.html` — страница `/legal`; файла нет — страницы нет.

Ни одного имени, домена, адреса или контакта конкретной читальни в коде
платформы быть не должно — это сторожит `scripts/leak-guard.sh` (шаблоны
ключей, токенов, публичных адресов; `LEAK_DENYLIST=файл` добавляет слова
экземпляра).

## Окружение и сервисы

- `cmd/server` и `cmd/migrate` читают `.env`, если он есть, иначе —
  переменные окружения (так настроены контейнеры). Локально:
  `cp env.example .env`.
- Порты Docker нестандартные: Postgres `5433`, S3 SeaweedFS `8333`.
- Администратор (`ADMIN_EMAIL`/`ADMIN_PASSWORD`) заводится на старте, если
  его нет.
- Файлы (оригиналы и превью полос) лежат в S3 (`pkg/storage`): локально
  SeaweedFS, в проде можно внешний S3 (режим решает `S3_ENDPOINT`,
  `endpoint_is_external` в `scripts/lib.sh`; `deploy.sh --external-s3`,
  переменные `EXTERNAL_S3_*`). API отдаёт presigned-ссылки, а не файлы.
  Запасное хранилище только на чтение — `S3_FALLBACK_*`.
- Превью и текст из PDF/DJVU — внешние утилиты `poppler-utils` и
  `djvulibre-bin`; их отсутствие ломает создание полос во время работы, не
  сборку.

## Архитектура

Слоистый бэкенд, зависимости собираются руками в `cmd/server/main.go`:

```
internal/api/        → обработчики + router.go (gorilla/mux) — источник правды о маршрутах и ролях
internal/repository/ → доступ к данным поверх pgxpool, файл на агрегат
internal/middleware/ → CORS, журнал, AuthMiddleware, RequireRole
internal/auth/       → JWT, bcrypt
internal/models/     → доменные структуры и константы статусов
internal/database/   → пул pgx и migrations/
internal/seo, opds, mcp, staticsite → краулеры, каталог OPDS, MCP-сервер, статическая копия
pkg/markdown/        → markdown→HTML, сноски
pkg/book/            → выгрузки EPUB/FB2/MD/HTML
pkg/fileprocessor/   → PDF/DJVU → изображения полос и текст
```

Инварианты — нарушать нельзя:

- **Роли**: `administrator | editor | reader`. Чтение публично; запись — под
  `RequireRole`. Каждый маршрут обязан стоять в таблице `routeAccess`
  (`internal/api/router_roles_test.go`) — новый без неё роняет тест.
- **Статус полосы — русские строки**: `не_вычитана | вычитывается |
  вычитана | есть_проблемы | пустая_страница | вычитано_машиной |
  требует_внимания`. Сохраняй их дословно.
- **Текст полосы пишется только через `applyPageEdit`**
  (`internal/api/page_edit.go`): снимок версии, запись, переякоривание
  вырезок указателя и вклеек разборов. Четвёртого пути записи не заводить.
- **Разбор рендерится при чтении** (`assembleDocument`), авторский текст —
  через `markdown.Renderer.ForUntrustedAuthor` (без сырого HTML, атрибутов,
  опасных ссылок и картинок); корпус — прежним рендерером.
- **Кэш глав** (`internal/pagecache`) адресуется диапазоном полос, живёт
  час; инвалидации на запись нет, сброс — руками
  (`DELETE /works/{id}/chapters/{id}/cache`, `/admin/cache`).
- **Поиск** — Postgres FTS (конфиг `ru`, без стоп-слов), запрос короче двух
  знаков — 400, одновременность ограничена слотами.

`router.go` — источник правды: обработчик без маршрута не работает.
Когда документы и код расходятся, верь коду и миграциям.

## Документация

- `docs/SELF_HOSTING.md` — своя читальня от пустого сервера до домена.
- `docs/CORPUS.md` — как наполнять читальню.
- Устройство — сам код: маршруты и роли в `internal/api/router.go`, схема
  в `internal/database/migrations/`.
- `CONTRIBUTING.md` — как вносить правки.
