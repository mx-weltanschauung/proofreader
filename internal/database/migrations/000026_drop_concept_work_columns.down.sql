ALTER TABLE index_concepts
    -- Nullable и без внешнего ключа: у статьи внешнего источника работы нет,
    -- и восстановить её неоткуда. NOT NULL или FK тут уронили бы откат.
    -- Решение прошлого раунда, трогать не надо. Индекс по колонке — другой
    -- вопрос: он ничего не проверяет и не запрещает, только ускоряет чтение,
    -- поэтому у него нет причины оставаться отсутствующим — восстановлен
    -- ниже, вместе с двумя другими (раунд правок 2, находка 1: прежняя
    -- формулировка «nullable и без внешнего ключа» ошибочно читалась и как
    -- «без индекса», хотя индекс DROP COLUMN снёс неявно вместе с колонкой,
    -- и его возврат ничем не запрещён).
    ADD COLUMN work_id BIGINT,
    ADD COLUMN article_markdown TEXT NOT NULL DEFAULT '',
    ADD COLUMN source_page_start INT NOT NULL DEFAULT 0,
    ADD COLUMN source_page_end INT NOT NULL DEFAULT 0,
    ADD COLUMN kind VARCHAR(16) NOT NULL DEFAULT 'article';

-- concept_id и from_concept_id ниже — НЕ work_id: у каждой статьи concept_id
-- всегда есть (index_concept_articles.concept_id NOT NULL), поэтому, в
-- отличие от work_id, здесь после отката нет случая, законно остающегося
-- пустым. Раунд правок 1, находка 5: восстанавливаем внешний ключ и индекс
-- ровно в форме, какую эти колонки носили до задачи 9 (миграция
-- 000006_add_editions_and_index_concepts) — RESTRICT/CASCADE поведение то
-- же, ON DELETE CASCADE от index_concepts.
ALTER TABLE index_references
    ADD COLUMN concept_id BIGINT REFERENCES index_concepts(id) ON DELETE CASCADE,
    ADD COLUMN rubric TEXT NOT NULL DEFAULT '';

ALTER TABLE index_concept_links
    ADD COLUMN from_concept_id BIGINT REFERENCES index_concepts(id) ON DELETE CASCADE;

-- Раунд правок 1, находка 6: article_id/from_article_id стали NOT NULL
-- ТОЛЬКО в 000026_up (задача 11) — до неё, со времён 000025, они были
-- nullable. make migrate-down — контрактно одна ступень (см. CLAUDE.md), и
-- откат обязан сам вернуть это состояние, а не полагаться на то, что вслед
-- за ним прогонят и down у 000025: код, знавший схему только до этой
-- ступени, не обязан заполнять article_id/from_article_id при вставке, и
-- голое NOT NULL после отката превращало бы такую вставку в ошибку схемы,
-- которую откат обязан был устранить сам.
ALTER TABLE index_references ALTER COLUMN article_id DROP NOT NULL;
ALTER TABLE index_concept_links ALTER COLUMN from_article_id DROP NOT NULL;

-- Вторая и далее статья понятия получает СОБСТВЕННОЕ понятие: старая схема
-- держит одну статью на понятие, и склейка, сделанная куратором, откатом
-- теряется. Данные при этом не теряются — теряется только сведение.
--
-- Слаг разобранного понятия — slug || '-a' || article_id, НЕ slug || '-' || n:
-- index_concepts.slug уникален глобально, и вид «-2» может быть уже занят
-- живым понятием (откат упал бы на уникальном индексе); id статьи уникален по
-- построению и коллизии не даст.
INSERT INTO index_concepts
    (work_id, title, slug, sort_key, title_key, article_markdown,
     source_page_start, source_page_end, kind)
SELECT a.work_id, a.title, c.slug || '-a' || a.id, c.sort_key, a.title_key,
       a.article_markdown, a.source_page_start, a.source_page_end, a.kind
FROM index_concept_articles a
JOIN index_concepts c ON c.id = a.concept_id
WHERE a.id <> (
    SELECT min(a2.id) FROM index_concept_articles a2 WHERE a2.concept_id = a.concept_id
);

-- Первая (наименьший id) статья понятия остаётся на исходной строке
-- index_concepts — её данные переносятся туда напрямую.
UPDATE index_concepts c
SET work_id = a.work_id,
    article_markdown = a.article_markdown,
    source_page_start = a.source_page_start,
    source_page_end = a.source_page_end,
    kind = a.kind
FROM index_concept_articles a
WHERE a.concept_id = c.id
  AND a.id = (SELECT min(a2.id) FROM index_concept_articles a2 WHERE a2.concept_id = c.id);

-- Подрубрика — отдельным UPDATE: планировщик отверг форму с двумя LEFT JOIN
-- в одном FROM (ON ru.id = r.rubric_id ссылался на цель UPDATE внутри
-- FROM-списка — "invalid reference to FROM-clause entry for table \"r\"").
-- Ровно тот случай, о котором предупреждает задание: делим на два UPDATE, а
-- не подгоняем форму под планировщик.
UPDATE index_references r
SET rubric = COALESCE(ru.title, '')
FROM index_rubrics ru
WHERE ru.id = r.rubric_id;

-- Раунд правок 1, находка 4: адрес и отсылка находят СВОЙ (возможно
-- разобранный) концепт по точному слагу, а не гадают лейтеральным джойном по
-- (title_key, article_markdown). У прежней формы понятие с двумя статьями,
-- делящими title_key И пустой текст статьи (обычное дело в предметном
-- указателе — короткая статья без текста, только адреса), совпадало под
-- условие сразу дважды, и порядок строк без опоры на что-либо, кроме ORDER
-- BY id DESC LIMIT 1, отдавал адреса первой статьи НЕ той статье. Слаг же
-- построен из article_id и коллизии не даёт по конструкции: split-строка с
-- slug = c.slug || '-a' || a.id существует тогда и только тогда, когда a —
-- НЕ первая статья своего понятия (см. INSERT выше), а для первой статьи
-- такого split_c нет вовсе, и COALESCE возвращает исходный concept_id.
UPDATE index_references r
SET concept_id = COALESCE(split_c.id, a.concept_id)
FROM index_concept_articles a
JOIN index_concepts orig_c ON orig_c.id = a.concept_id
LEFT JOIN index_concepts split_c ON split_c.slug = orig_c.slug || '-a' || a.id
WHERE a.id = r.article_id;

-- Находка 4, вторая половина: раньше отсылки безусловно уезжали на
-- ИСХОДНОЕ понятие (from_concept_id = a.concept_id), даже когда сама статья
-- была разобрана в отдельный split-концепт выше, — адреса и отсылки одной и
-- той же статьи после отката оказывались на РАЗНЫХ понятиях. Тот же
-- адресный способ, что и у index_references.
UPDATE index_concept_links l
SET from_concept_id = COALESCE(split_c.id, a.concept_id)
FROM index_concept_articles a
JOIN index_concepts orig_c ON orig_c.id = a.concept_id
LEFT JOIN index_concepts split_c ON split_c.slug = orig_c.slug || '-a' || a.id
WHERE a.id = l.from_article_id;

DELETE FROM index_references WHERE concept_id IS NULL;
DELETE FROM index_concept_links WHERE from_concept_id IS NULL;

-- concept_id/from_concept_id всегда резолвятся (см. комментарий у ADD COLUMN
-- выше) — в отличие от index_concepts.work_id, NOT NULL здесь безусловен, не
-- обёрнут в проверку на пустоту.
ALTER TABLE index_references ALTER COLUMN concept_id SET NOT NULL;
ALTER TABLE index_concept_links ALTER COLUMN from_concept_id SET NOT NULL;

-- Индексы, снесённые неявно вместе со своими колонками при DROP COLUMN в
-- 000026_up (Postgres роняет зависящий от колонки индекс без отдельного
-- DROP INDEX) — находка 5, вторая половина: без них чтение по concept_id/
-- from_concept_id идёт последовательным сканом. idx_index_concepts_work —
-- туда же (раунд правок 2, находка 1): пропущен в прошлом раунде, хотя
-- ADD COLUMN work_id выше был.
CREATE INDEX idx_index_references_concept ON index_references(concept_id, order_number);
CREATE INDEX idx_index_concept_links_from ON index_concept_links(from_concept_id, order_number);
CREATE INDEX idx_index_concepts_work ON index_concepts(work_id);
