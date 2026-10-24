-- Закрытые главы: признак «закрыт» у главы и журнал закрытий.
-- Закрыта глава — закрыты её потомки (рекурсивно по parent_id) и полосы её
-- диапазона; действующую закрытость главы и полосы читают представления, а
-- не хранимые копии: копия разошлась бы с признаком молча.
ALTER TABLE chapters ADD COLUMN closed boolean NOT NULL DEFAULT false;

-- Затравка closed_chapters — главы с собственным признаком. Без индекса
-- каждое вычисление представления (а чтение главы зовёт его несколько раз)
-- шло последовательным просмотром всех глав корпуса, даже когда закрытых нет
-- ни одной. Частичный индекс держит только закрытые: он почти пуст и
-- сводит затравку к его просмотру.
CREATE INDEX chapters_closed_idx ON chapters (id) WHERE closed;

-- Журнал закрытий: кто, когда, по какой причине закрыл или открыл работу,
-- издание, журнал или главу. Причина обязательна — CHECK откатывает запись
-- признака вместе со строкой журнала (обе пишутся одной транзакцией).
CREATE TABLE closures (
    id             bigserial PRIMARY KEY,
    entity_kind    text        NOT NULL CHECK (entity_kind IN ('work', 'edition', 'journal', 'chapter')),
    entity_id      bigint      NOT NULL,
    title_snapshot text        NOT NULL,
    closed         boolean     NOT NULL,
    reason         text        NOT NULL CHECK (btrim(reason) <> ''),
    actor_id       bigint      REFERENCES users(id) ON DELETE SET NULL,
    actor_name     text        NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX closures_entity_idx ON closures (entity_kind, entity_id, created_at DESC);

-- Главы с собственным признаком и все их потомки.
CREATE VIEW closed_chapters AS
    WITH RECURSIVE t AS (
        SELECT c.id, c.work_id, c.start_page, c.end_page
        FROM chapters c
        WHERE c.closed
        UNION
        SELECT c.id, c.work_id, c.start_page, c.end_page
        FROM chapters c
        JOIN t ON c.parent_id = t.id
    )
    SELECT id, work_id, start_page, end_page FROM t;

-- Полосы закрытых работ и полосы, накрытые диапазоном хоть одной закрытой
-- главы той же работы.
CREATE VIEW closed_pages AS
    SELECT p.id, p.work_id, p.page_number
    FROM pages p
    WHERE p.work_id IN (SELECT id FROM closed_works)
       OR EXISTS (SELECT 1 FROM closed_chapters cc
                  WHERE cc.work_id = p.work_id
                    AND p.page_number BETWEEN cc.start_page AND cc.end_page);
