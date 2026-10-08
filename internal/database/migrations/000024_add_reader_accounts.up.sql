-- Роль читателя. Enum перестраивается целиком, а не ALTER TYPE ... ADD VALUE:
-- значение, добавленное через ADD VALUE, нельзя использовать в той же
-- транзакции, а golang-migrate оборачивает шаг в транзакцию. Приём взят у
-- 000005_drop_proofreader_role, где enum так же пересобирался.
ALTER TABLE users ALTER COLUMN role DROP DEFAULT;

ALTER TYPE user_role RENAME TO user_role_old;
CREATE TYPE user_role AS ENUM ('administrator', 'editor', 'reader');

ALTER TABLE users
    ALTER COLUMN role TYPE user_role
    USING role::text::user_role;

ALTER TABLE users ALTER COLUMN role SET DEFAULT 'editor';

DROP TYPE user_role_old;

-- Читатель входит ником; почты у него нет вовсе.
ALTER TABLE users ALTER COLUMN email DROP NOT NULL;

ALTER TABLE users ADD COLUMN nickname varchar(64);
ALTER TABLE users ADD COLUMN signup_ip_hash text NOT NULL DEFAULT '';

-- Уникальность ника держит Postgres, а не код: нормализация (регистр + NFKC)
-- живёт вычисляемым столбцом, как search_vector у полос. Синхронизировать ключ
-- из Go не надо — Go только проверяет форму и возвращает отказ. NormalizeNickname
-- в Go существует только для проверки по закрытому списку занятых имён.
-- NULL в nickname даёт NULL в ключе, а UNIQUE пропускает любое число NULL,
-- поэтому сотрудники без ника индексу не мешают.
-- Проверено на Postgres 15 (образ проекта): normalize(text, NFKC) принят
-- как IMMUTABLE, миграция накатывается без ошибки "generation expression
-- is not immutable" — запасной путь через Go-функцию NormalizeNickname не
-- понадобился.
ALTER TABLE users ADD COLUMN nickname_key text
    GENERATED ALWAYS AS (lower(normalize(nickname, NFKC))) STORED;
CREATE UNIQUE INDEX idx_users_nickname_key ON users (nickname_key);

-- Владение предложением правки переезжает с предъявительского билета на
-- учётную запись. reader_key остаётся исторической колонкой: выданные билеты
-- не переносим (на боевом их два, оба проверочные).
ALTER TABLE page_suggestions ALTER COLUMN reader_key DROP NOT NULL;
ALTER TABLE page_suggestions
    ADD COLUMN user_id BIGINT REFERENCES users(id) ON DELETE SET NULL;
CREATE INDEX idx_page_suggestions_user ON page_suggestions (user_id, created_at DESC);

-- CASCADE у читательского тела означает, что одна кнопка сносит разбор вместе
-- с чужими ссылками на него.
ALTER TABLE documents ALTER COLUMN owner_id DROP NOT NULL;
ALTER TABLE documents DROP CONSTRAINT documents_owner_id_fkey;
ALTER TABLE documents
    ADD CONSTRAINT documents_owner_id_fkey
    FOREIGN KEY (owner_id) REFERENCES users(id) ON DELETE SET NULL;

-- Подборка: черновик и снимок подписи.
-- author_nickname заполняется ПРИ СОЗДАНИИ, а не при публикации, иначе
-- черновик читателя жил бы в сотрудническом пространстве имён. У сотрудника
-- он пустой, и тогда одна пара (author_nickname, slug) закрывает оба вида
-- адреса: /collections/{слаг} при пустом нике и /collections/{ник}/{слаг} при
-- непустом. Ключ — снимок, а не ссылка на владельца, чтобы адрес пережил
-- удаление учётной записи.
ALTER TABLE collections ADD COLUMN published_at timestamptz;
ALTER TABLE collections ADD COLUMN author_nickname varchar(64) NOT NULL DEFAULT '';
-- Отметка адреса, с которого подборку опубликовали: предел 3 в сутки считается
-- по ней тем же рельсом, что у писем и правок. Сам адрес не хранится.
ALTER TABLE collections ADD COLUMN publish_ip_hash text NOT NULL DEFAULT '';

-- Уже существующие подборки — сотруднические и опубликованные: до этой
-- миграции черновиков не существовало как понятия.
UPDATE collections SET published_at = created_at WHERE published_at IS NULL;

-- IF EXISTS: на первом прогоне ограничение есть (заведено 000009), и его
-- нужно снести перед заменой на составное. После отката эта же миграция
-- его уже не восстанавливает (см. down.sql — восстановление UNIQUE (slug)
-- убрано как ломающее откат на живых данных), поэтому повторный up на
-- цикле up→down→up находит колонку уже без этого ограничения; без
-- IF EXISTS такой повторный накат падал бы на несуществующем имени.
ALTER TABLE collections DROP CONSTRAINT IF EXISTS collections_slug_key;
ALTER TABLE collections ADD CONSTRAINT collections_author_slug_key
    UNIQUE (author_nickname, slug);

-- Рельс предела попыток входа. Строки старше суток удаляются той же
-- транзакцией, что считает предел, — отдельного уборщика не заводим.
--
-- Ключ — пара (ip_hash, nickname_key), а не голый ip_hash. Причина: заводить
-- учётку ограничено тремя в сутки, но подбирающему нужна ОДНА своя учётка —
-- дальше он девять раз пробует чужой ник, десятым входит в свою и стирает
-- счёт (Clear), и цикл повторяется сколько угодно раз в час, если счёт не
-- различает, к какому нику шла попытка. Второй, независимый довод — сотовые
-- операторы держат абонентов за общим NAT, и предел на голый адрес был бы
-- бюджетом, который читатели одной соты делят между собой.
CREATE TABLE auth_attempts (
    ip_hash       text        NOT NULL,
    nickname_key  text        NOT NULL DEFAULT '',
    created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_auth_attempts_ip ON auth_attempts (ip_hash, nickname_key, created_at DESC);
