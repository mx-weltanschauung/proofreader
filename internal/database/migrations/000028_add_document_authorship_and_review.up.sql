-- Вторая редакция разбора и модерация до публикации (ветка 2 направления
-- «Разбор со вклейкой»).
--
-- title/markdown_content с этой миграции — ЧЕРНОВИК автора; на людях лежит
-- published_title/published_markdown. Правка опубликованного создаёт новую
-- редакцию, которая ждёт модерации, а показываемая остаётся прежней.
--
-- Номер: 000027 занят вклейкой (ветка 1), 000025/000026 — перестройкой
-- предметного указателя; все три в master. На documents указательные не
-- смотрят вовсе, только на свои таблицы, так что 000028 от них независима.
-- Цепочка 1→28 прогнана целиком, затем ступень вниз и снова вверх.
ALTER TABLE documents
    ADD COLUMN author_nickname TEXT NOT NULL DEFAULT '',
    -- VARCHAR(500), а не TEXT: колонка зеркалит title, значит зеркалит и его
    -- предел. Одобрение пишет published_title напрямую, и заглавие длиннее
    -- 500 знаков уронило бы ОТКАТ этой миграции на `title = published_title`
    -- — не на пробе, где таких заглавий нет, а однажды на боевом.
    ADD COLUMN published_title VARCHAR(500) NOT NULL DEFAULT '',
    ADD COLUMN published_markdown TEXT NOT NULL DEFAULT '',
    ADD COLUMN published_at TIMESTAMPTZ,
    ADD COLUMN was_published BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN review_status VARCHAR(32) NOT NULL DEFAULT 'черновик'
        CHECK (review_status IN ('черновик', 'на_рассмотрении', 'одобрено', 'отклонено')),
    ADD COLUMN reject_reason TEXT
        CHECK (reject_reason IS NULL OR reject_reason IN
            ('не_по_теме', 'текст_без_разбора', 'брань_или_оскорбления', 'нарушает_закон')),
    ADD COLUMN moderator_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN reviewed_at TIMESTAMPTZ,
    ADD COLUMN submitted_at TIMESTAMPTZ,
    ADD COLUMN submit_ip_hash TEXT NOT NULL DEFAULT '';

-- Всё, что лежало до этой миграции, писал сотрудник, и оно было публичным с
-- момента создания: без переноса в одобренную редакцию такой разбор исчез бы
-- с людей молча.
UPDATE documents
SET published_title = title,
    published_markdown = markdown_content,
    published_at = created_at,
    was_published = true,
    review_status = 'одобрено';

-- Причина отказа и отказ — одно и то же событие, и держать их порознь значит
-- позволить «одобрено» с причиной отказа в придачу. Ограничение ставится
-- ПОСЛЕ засева выше: засев переводит старое в «одобрено» с пустой причиной и
-- ему не противоречит, но порядок тут не вкусовой — ограничение,
-- поставленное раньше, проверялось бы против ещё не засеянных строк.
ALTER TABLE documents
    ADD CONSTRAINT documents_reject_reason_matches_status
    CHECK ((review_status = 'отклонено') = (reject_reason IS NOT NULL));

-- Витрина берёт только опубликованные; очередь — только ждущие.
CREATE INDEX idx_documents_published ON documents(published_at DESC)
    WHERE published_at IS NOT NULL;
CREATE INDEX idx_documents_review ON documents(submitted_at)
    WHERE review_status = 'на_рассмотрении';
-- Индекса по owner_id здесь нет намеренно: idx_documents_owner заводит ещё
-- 000002 вместе с самой таблицей. Повторное CREATE роняло эту миграцию
-- (42P07), а парный DROP в откате снёс бы ЧУЖОЙ индекс — обещанный 000002 и
-- ею же не восстанавливаемый.

COMMENT ON COLUMN documents.markdown_content IS
    'Черновик автора; на людях — published_markdown';
COMMENT ON COLUMN documents.author_nickname IS
    'Снимок подписи читателя; пусто — разбор собрал сотрудник';
