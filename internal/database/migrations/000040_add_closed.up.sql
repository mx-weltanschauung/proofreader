-- Закрытые работы: признак «закрыт» у работы, издания и журнала. Закрытое
-- видят только сотрудники; действующую закрытость работы считает
-- work_closed_by (собственный признак, закрытый том-родитель служебной
-- работы или его издание, издание, журнал номера), а не колонка: хранимая
-- копия разошлась бы с признаком издания молча.
ALTER TABLE works    ADD COLUMN closed boolean NOT NULL DEFAULT false;
ALTER TABLE editions ADD COLUMN closed boolean NOT NULL DEFAULT false;
ALTER TABLE journals ADD COLUMN closed boolean NOT NULL DEFAULT false;

-- Причина закрытости работы: 'work' | 'parent' | 'edition' | 'journal';
-- пустая строка — открыта (или работы нет). Порядок причин — порядок проверки:
-- закрытая сама и в закрытом издании называется 'work'.
CREATE FUNCTION work_closed_by(work_id bigint) RETURNS text
LANGUAGE sql STABLE AS $$
    SELECT CASE
        WHEN w.closed THEN 'work'
        WHEN p.closed
          OR EXISTS (SELECT 1 FROM editions pe WHERE pe.id = p.edition_id AND pe.closed) THEN 'parent'
        WHEN EXISTS (SELECT 1 FROM editions e WHERE e.id = w.edition_id AND e.closed) THEN 'edition'
        WHEN EXISTS (SELECT 1 FROM journal_issues ji
                     JOIN journals j ON j.id = ji.journal_id
                     WHERE ji.work_id = w.id AND j.closed) THEN 'journal'
        ELSE ''
    END
    FROM works w
    LEFT JOIN works p ON p.id = w.parent_work_id
    WHERE w.id = work_id
$$;

CREATE VIEW closed_works AS
    SELECT id FROM works WHERE work_closed_by(id) <> '';
