-- Понятие каталога получает нормализованный ключ заголовка: по нему сводятся
-- статьи разных указателей об одном понятии и по нему же резолвятся отсылки.
-- Выражение ключа обязано совпадать с models.NormalizeIndexTitle: схлопнуть
-- внутренние пробелы, срезать края, привести регистр, свести ё→е. sort_key,
-- стоявший тут раньше, считается разборщиком (tools/ocr_ingest/index_parser.py)
-- и внутренние пробелы НЕ схлопывает: у заголовка с двойным пробелом или
-- табуляцией — обычный мусор распознанного указателя — перенесённый ключ не
-- совпал бы с тем, что вычислит первый же ввоз. Статья не нашлась бы по паре
-- (издание, ключ), была бы снесена и заведена заново (вырезки погибли бы
-- каскадом), понятие осиротело бы, а новое получило бы другой слаг — то есть
-- публичный адрес понятия сменился бы молча.
ALTER TABLE index_concepts ADD COLUMN title_key VARCHAR(500) NOT NULL DEFAULT '';
UPDATE index_concepts
SET title_key = replace(lower(btrim(regexp_replace(title, '\s+', ' ', 'g'))), 'ё', 'е');
CREATE INDEX idx_index_concepts_title_key ON index_concepts(title_key);

-- work_id у понятия — NOT NULL с внешним ключом на works (проверено \d
-- index_concepts). Новый ввоз заводит понятия, у которых работы-указателя нет
-- вовсе, и подставить туда ноль нельзя — внешний ключ не пустит. Колонка
-- доживает до 000026 только ради читающих запросов переходного периода.
ALTER TABLE index_concepts ALTER COLUMN work_id DROP NOT NULL;

-- Статья без издания — не статья: резолв адресов идёт через VolumeMap(edition).
-- Молча выбросить такое понятие нельзя, поэтому миграция падает вслух.
DO $$
DECLARE orphans INT;
BEGIN
    SELECT count(*) INTO orphans
    FROM index_concepts c
    JOIN works w ON w.id = c.work_id
    WHERE w.edition_id IS NULL;
    IF orphans > 0 THEN
        RAISE EXCEPTION 'у % понятий работа-указатель не входит ни в одно собрание: статье не к чему привязаться. Припишите работу к изданию (works.edition_id) и повторите миграцию', orphans;
    END IF;
END $$;

CREATE TABLE index_concept_articles (
    id BIGSERIAL PRIMARY KEY,
    concept_id BIGINT NOT NULL REFERENCES index_concepts(id) ON DELETE CASCADE,
    edition_id BIGINT NOT NULL REFERENCES editions(id) ON DELETE CASCADE,
    -- Работа-указатель, если она есть в корпусе. У внешнего источника её нет.
    work_id BIGINT REFERENCES works(id) ON DELETE SET NULL,
    source_url TEXT NOT NULL DEFAULT '',
    title VARCHAR(500) NOT NULL,
    title_key VARCHAR(500) NOT NULL,
    article_markdown TEXT NOT NULL DEFAULT '',
    kind VARCHAR(16) NOT NULL DEFAULT 'article',
    source_page_start INT NOT NULL DEFAULT 0,
    source_page_end INT NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    -- Рабочий ключ переимпорта: по нему статья узнаётся при повторном ввозе,
    -- и потому concept_id, проставленный куратором, переживает прогон.
    UNIQUE (edition_id, title_key),
    -- Печатный указатель не даёт двух статей об одном понятии; склейка,
    -- которая к этому привела бы, — ошибка, и упасть на ней лучше.
    UNIQUE (concept_id, edition_id)
);

CREATE TRIGGER update_index_concept_articles_updated_at
    BEFORE UPDATE ON index_concept_articles
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE INDEX idx_index_concept_articles_concept ON index_concept_articles(concept_id);
CREATE INDEX idx_index_concept_articles_edition ON index_concept_articles(edition_id);
-- Снятие аппарата тома ходит по work_id дважды (счёт статей в плане и снос —
-- apparatus_repository.go), и в старой схеме индекс по работе был
-- (idx_index_concepts_work, миграция 000006). Без него якорь снятия
-- обходится перебором всей таблицы.
CREATE INDEX idx_index_concept_articles_work ON index_concept_articles(work_id);

-- Подрубрика внутри статьи. Порядок ПЕЧАТНЫЙ, не алфавитный: издание ставит
-- «сущность» и «общая характеристика» первыми вне алфавита.
CREATE TABLE index_rubrics (
    id BIGSERIAL PRIMARY KEY,
    article_id BIGINT NOT NULL REFERENCES index_concept_articles(id) ON DELETE CASCADE,
    title VARCHAR(500) NOT NULL,
    title_key VARCHAR(500) NOT NULL,
    order_number INT NOT NULL,
    UNIQUE (article_id, title_key)
);

CREATE INDEX idx_index_rubrics_article ON index_rubrics(article_id, order_number);

-- Nullable намеренно: до задачи 10 строки пишет и старый код, который про эти
-- колонки не знает. NOT NULL ставит миграция 000026.
ALTER TABLE index_references
    ADD COLUMN article_id BIGINT REFERENCES index_concept_articles(id) ON DELETE CASCADE,
    ADD COLUMN rubric_id BIGINT REFERENCES index_rubrics(id) ON DELETE CASCADE;

CREATE INDEX idx_index_references_article ON index_references(article_id, order_number);

ALTER TABLE index_concept_links
    ADD COLUMN from_article_id BIGINT REFERENCES index_concept_articles(id) ON DELETE CASCADE;

CREATE INDEX idx_index_concept_links_from_article ON index_concept_links(from_article_id, order_number);

-- Перенос: каждое существующее понятие даёт ровно одну статью.
INSERT INTO index_concept_articles
    (concept_id, edition_id, work_id, title, title_key, article_markdown,
     kind, source_page_start, source_page_end, created_at, updated_at)
SELECT c.id, w.edition_id, c.work_id, c.title, c.title_key, c.article_markdown,
       c.kind, c.source_page_start, c.source_page_end, c.created_at, c.updated_at
FROM index_concepts c
JOIN works w ON w.id = c.work_id;

UPDATE index_references r
SET article_id = a.id
FROM index_concept_articles a
WHERE a.concept_id = r.concept_id;

-- Две подрубрики одной статьи, сошедшиеся по ключу при РАЗНОМ написании, —
-- отказ, а не склейка: адреса второй ушли бы под название первой. То же
-- правило и теми же словами держит ввоз (ReplaceForEdition, шаг 3). Уникальный
-- индекс (article_id, title_key) уронил бы миграцию и сам, но сообщением о
-- нарушении ограничения, по которому не видно ни статьи, ни написаний.
DO $$
DECLARE clashes TEXT;
BEGIN
    SELECT string_agg(format('статья %s: %L и %L', article_id, first_title, other_title), '; ')
      INTO clashes
    FROM (
        SELECT k.article_id,
               min(k.rubric) AS first_title,
               max(k.rubric) AS other_title
        FROM (
            SELECT r.article_id, r.rubric,
                   replace(lower(btrim(regexp_replace(r.rubric, '\s+', ' ', 'g'))), 'ё', 'е') AS title_key
            FROM index_references r
            WHERE r.article_id IS NOT NULL
        ) k
        WHERE k.title_key <> ''
        GROUP BY k.article_id, k.title_key
        HAVING count(DISTINCT k.rubric) > 1
    ) c;
    IF clashes IS NOT NULL THEN
        RAISE EXCEPTION 'две подрубрики одной статьи сходятся по нормализованному ключу при разном написании — склеить их значит увести адреса второй под название первой: %', clashes;
    END IF;
END $$;

-- Подрубрики: различные непустые КЛЮЧИ текстовой колонки, в порядке ПЕРВОГО
-- появления в статье (минимальный order_number адреса), с написанием того
-- адреса, где ключ встретился первым, — ровно так же сводит их ввоз
-- (ReplaceForEdition, шаг 3: дедупликация по ключу, первое написание
-- побеждает). Группировка по сырому тексту (как было до сквозной рецензии
-- ветки) считала бы ключ, но выводила различие по тексту: подрубрика,
-- записанная на одном адресе с двойным пробелом, а на другом с одинарным,
-- давала две строки с одним ключом — то есть падение на UNIQUE
-- (article_id, title_key) вместо внятного отказа выше, а переживи она его —
-- неоднозначный JOIN в проставлении rubric_id ниже. Фильтр тоже идёт по
-- ключу, а не по сырому тексту: подрубрика из одних пробелов — это отсутствие
-- подрубрики (так её читает и models.NormalizeIndexTitle), и строка с пустым
-- ключом собрала бы на себя все адреса БЕЗ подрубрики.
INSERT INTO index_rubrics (article_id, title, title_key, order_number)
SELECT g.article_id, g.title, g.title_key,
       row_number() OVER (PARTITION BY g.article_id ORDER BY g.first_order)
FROM (
    SELECT k.article_id, k.title_key,
           MIN(k.order_number) AS first_order,
           (array_agg(k.rubric ORDER BY k.order_number))[1] AS title
    FROM (
        SELECT r.article_id, r.rubric, r.order_number,
               replace(lower(btrim(regexp_replace(r.rubric, '\s+', ' ', 'g'))), 'ё', 'е') AS title_key
        FROM index_references r
        WHERE r.article_id IS NOT NULL
    ) k
    WHERE k.title_key <> ''
    GROUP BY k.article_id, k.title_key
) g;

UPDATE index_references r
SET rubric_id = ru.id
FROM index_rubrics ru
WHERE ru.article_id = r.article_id
  AND ru.title_key = replace(lower(btrim(regexp_replace(r.rubric, '\s+', ' ', 'g'))), 'ё', 'е');

UPDATE index_concept_links l
SET from_article_id = a.id
FROM index_concept_articles a
WHERE a.concept_id = l.from_concept_id;
