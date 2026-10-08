# Proofreader — платформа читальни

Сканы книг (PDF, DJVU) превращаются в постраничный текст, который
вычитывают и читают онлайн. Печатная пагинация сохраняется: страница на
экране — та же страница бумажного тома, и рядом лежит её скан.

Что умеет читальня:

- полки изданий и томов, главы с подстрочными сносками, чтение потоком;
- вычитка в Markdown с историей версий и статусами полос, предложения
  правок от читателей с модерацией;
- полнотекстовый поиск (Postgres FTS), предметный указатель;
- подборки и разборы читателей, ссылки на цитату с точностью до полосы;
- выгрузки EPUB, FB2, Markdown, HTML; каталог OPDS для читалок;
- страницы для поисковиков и превью ссылок, текст для нейросетей (`.md`,
  `llms.txt`), MCP-сервер;
- статическая копия всей читальни одним архивом, открывается без сервера.

Go-бэкенд отдаёт JSON API, React-SPA — интерфейс; файлы лежат в любом
S3-совместимом хранилище (локально — SeaweedFS). Своя читальня поднимается
на своём сервере и под своим именем без правки кода — см.
[docs/SELF_HOSTING.md](docs/SELF_HOSTING.md). English summary:
[README.en.md](README.en.md).

---

## Запуск всего стека одной командой (Docker)

**Требуется:** Docker + Docker Compose.

```bash
cp env.example .env
docker compose up --build
```

Поднимаются PostgreSQL, SeaweedFS, миграции (одноразовый сервис `migrate`),
Go-бэкенд и фронт под nginx.

| Что | URL |
|-----|-----|
| Читальня | http://localhost:3100 |
| API | http://localhost:8080/api |
| Проверка здоровья API | http://localhost:8080/api/health |

Вход по умолчанию: **`admin@proofreader.local`** / **`admin`**. Как
наполнить читальню книгами — [docs/CORPUS.md](docs/CORPUS.md).

```bash
docker compose down          # остановить, данные в томах сохраняются
docker compose down -v       # остановить и удалить тома (полный сброс)
```

## Локальная разработка (без контейнеризации приложения)

Режим для активной разработки: инфраструктура — в Docker, а бэкенд и фронтенд
запускаются напрямую (удобно для hot-reload фронтенда).

**Требуется:**
- Go 1.24+, Node.js 18+ (для сборки использовался Node 22), Docker Compose.
- Системные утилиты для извлечения страниц: `poppler-utils`
  (`pdfinfo`, `pdftoppm`, `pdftotext`) и `djvulibre-bin`
  (`djvused`, `ddjvu`, `djvutxt`). Без них создание страниц падает в рантайме.

### 1. Настроить окружение

```bash
cp env.example .env
```

`.env` уже содержит рабочие значения для локального запуска. Оставь
`S3_PUBLIC_ENDPOINT=` пустым — код сам подставит `S3_ENDPOINT`
(`http://localhost:8333`).

### 2. Поднять инфраструктуру

```bash
make docker-up      # PostgreSQL (5433), SeaweedFS (8333)
make migrate-up     # применить миграции БД
```

> Хостовый порт Postgres нестандартный (`5433`), чтобы не
> конфликтовать с локальными установками. Внутри Docker-сети сервисы слушают
> дефолтные порты.

### 3. Запустить бэкенд

```bash
make run            # go run cmd/server/main.go, слушает :8080
```

Админ-пользователь (`admin@proofreader.local` / `admin`) создаётся на старте,
если его нет.

### 4. Запустить фронтенд

```bash
cd frontend
npm install
npm run dev         # Vite dev-сервер на :3100 (проксирует /api на :8080)
```

Открыть http://localhost:3100.

---

## Полезные команды

Бэкенд (из корня репозитория, `make help` покажет все цели):

```bash
make build          # собрать bin/server
make test           # go test -v ./...
make test-coverage  # отчёт покрытия -> coverage.html
make test-race      # тесты с детектором гонок
make lint           # golangci-lint
make fmt            # go fmt ./...
make docker-down    # остановить инфраструктуру
```

Фронтенд (из `frontend/`):

```bash
npm run build       # tsc && vite build
npm run lint        # eslint
```

Пересобрать только один контейнер в Docker-стеке:

```bash
docker compose up -d --build backend    # или frontend
```

---

## Переменные окружения

Все настройки — через переменные окружения (см. `env.example`). Ключевые для
хранилища:

| Переменная | Локально (`.env`) | В Docker (compose) | Назначение |
|-----------|-------------------|--------------------|-----------|
| `S3_ENDPOINT` | `http://localhost:8333` | `http://seaweedfs:8333` | адрес S3 для операций бэкенда |
| `S3_PUBLIC_ENDPOINT` | пусто (= `S3_ENDPOINT`) | `http://localhost:8333` | адрес для presigned-ссылок (должен быть доступен из браузера) |
| `S3_BUCKET` | `proofreader` | `proofreader` | бакет |
| `S3_ACCESS_KEY` / `S3_SECRET_KEY` | dev-значения | dev-значения | креды S3 |
| `S3_PRESIGN_TTL` | `30m` | `30m` | срок жизни presigned-ссылки |

> **Продакшен.** Значения `S3_ACCESS_KEY`/`S3_SECRET_KEY`, `JWT_SECRET` и
> админ-пароль в репозитории — dev-заглушки. Для продакшена подставь свои через
> переменные окружения и замени `docker/seaweedfs/s3_config.json` на свою
> S3-идентичность.

---


## Своя читальня на сервере

```bash
./scripts/deploy.sh root@203.0.113.4 --domain lib.example.org --acme-email you@example.org
```

С чистой Ubuntu и root-доступом по SSH за один прогон получается читальня на
своём домене с TLS (Let's Encrypt через Caddy). Имя, описание, контакты,
иконки и правовая страница — настройки экземпляра, а не код. Пошагово, с
внешним S3, бэкапом и обновлением — [docs/SELF_HOSTING.md](docs/SELF_HOSTING.md).

---

## Документация

- [docs/SELF_HOSTING.md](docs/SELF_HOSTING.md) — своя читальня от пустого сервера до домена.
- [docs/CORPUS.md](docs/CORPUS.md) — как наполнять читальню.
- Маршруты и роли — `internal/api/router.go`, схема базы — `internal/database/migrations/`.
- [CONTRIBUTING.md](CONTRIBUTING.md) — как вносить правки; [SECURITY.md](SECURITY.md) — как сообщить об уязвимости.
- `CLAUDE.md` — заметки по архитектуре и инвариантам для работы с кодом.

## Лицензия

[GNU AGPL-3.0](LICENSE). Кто запускает изменённую платформу как сетевой
сервис, обязан открыть свои изменения пользователям этого сервиса.

Образ бэкенда содержит `poppler-utils` и `djvulibre-bin` (GPL-2.0/GPL-3.0):
распространяя образ, предлагайте получателю и их исходники (пакеты
дистрибутива, из которых собран образ).
