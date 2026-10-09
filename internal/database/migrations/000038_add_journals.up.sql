-- Журналы в читальне (спека 2026-10-08-journals-in-reading-room-design.md).
-- Номер журнала — строка journal_issues поверх работы с ролью journal_issue:
-- полосы, правка, поиск и выгрузки остаются на работе. Статья — глава с
-- article_kind, её подпись — article_credits со ссылкой на человека.

CREATE TABLE journals (
    id          BIGSERIAL PRIMARY KEY,
    slug        VARCHAR(64) NOT NULL,
    title       TEXT NOT NULL CHECK (btrim(title) <> ''),
    subtitle    TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMP NOT NULL DEFAULT NOW(),
    CONSTRAINT journals_slug_key UNIQUE (slug)
);
CREATE TRIGGER update_journals_updated_at BEFORE UPDATE ON journals
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TABLE journal_issues (
    id          BIGSERIAL PRIMARY KEY,
    journal_id  BIGINT NOT NULL REFERENCES journals(id) ON DELETE RESTRICT,
    year        INT NOT NULL CHECK (year BETWEEN 1800 AND 2100),
    number_from INT NOT NULL CHECK (number_from >= 1),
    number_to   INT NOT NULL,
    label       VARCHAR(16) NOT NULL,
    months      TEXT NOT NULL DEFAULT '',
    -- Номер удаляется удалением его работы: строка уходит каскадом.
    work_id     BIGINT NOT NULL REFERENCES works(id) ON DELETE CASCADE,
    created_at  TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMP NOT NULL DEFAULT NOW(),
    CONSTRAINT journal_issues_numbers_check CHECK (number_to >= number_from),
    CONSTRAINT journal_issues_work_id_key UNIQUE (work_id),
    CONSTRAINT journal_issues_journal_year_number_key UNIQUE (journal_id, year, number_from)
);
CREATE TRIGGER update_journal_issues_updated_at BEFORE UPDATE ON journal_issues
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TABLE persons (
    id         BIGSERIAL PRIMARY KEY,
    name       TEXT NOT NULL CHECK (btrim(name) <> ''),
    sort_key   TEXT NOT NULL,
    slug       VARCHAR(80) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    CONSTRAINT persons_slug_key UNIQUE (slug)
);
CREATE INDEX idx_persons_sort_key ON persons (sort_key text_pattern_ops);
CREATE TRIGGER update_persons_updated_at BEFORE UPDATE ON persons
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TABLE article_credits (
    chapter_id BIGINT NOT NULL REFERENCES chapters(id) ON DELETE CASCADE,
    position   INT NOT NULL CHECK (position >= 1),
    role       VARCHAR(16) NOT NULL CHECK (role IN ('author', 'translator')),
    printed    TEXT NOT NULL CHECK (btrim(printed) <> ''),
    person_id  BIGINT,
    CONSTRAINT article_credits_pkey PRIMARY KEY (chapter_id, position),
    CONSTRAINT article_credits_person_id_fkey FOREIGN KEY (person_id)
        REFERENCES persons(id) ON DELETE SET NULL
);
CREATE INDEX idx_article_credits_person ON article_credits (person_id);

ALTER TABLE chapters ADD COLUMN article_kind VARCHAR(16);
ALTER TABLE chapters ADD CONSTRAINT chapters_article_kind_check CHECK (article_kind IN
    ('статья', 'рецензия', 'документ', 'от_редакции', 'выступление', 'прочее'));
COMMENT ON COLUMN chapters.article_kind IS
    'Вид статьи журнала; NULL — не статья (глава тома или рубрика номера)';

ALTER TABLE works
    DROP CONSTRAINT works_role_check,
    DROP CONSTRAINT works_parent_role_check;
ALTER TABLE works
    ADD CONSTRAINT works_role_check
        CHECK (role IN ('volume', 'front_matter', 'edition_front_matter', 'journal_issue')),
    ADD CONSTRAINT works_parent_role_check
        CHECK ((role = 'volume' AND parent_work_id IS NULL)
            OR (role = 'front_matter' AND parent_work_id IS NOT NULL)
            OR (role = 'edition_front_matter'
                AND parent_work_id IS NULL AND edition_id IS NOT NULL)
            OR (role = 'journal_issue'
                AND parent_work_id IS NULL AND edition_id IS NULL));
