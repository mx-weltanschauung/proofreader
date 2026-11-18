-- Скан можно скрыть, оставив текст открытым. Свой признак на каждом из
-- четырёх уровней независим от полного закрытия.
ALTER TABLE works ADD COLUMN scans_hidden boolean NOT NULL DEFAULT false;
ALTER TABLE editions ADD COLUMN scans_hidden boolean NOT NULL DEFAULT false;
ALTER TABLE journals ADD COLUMN scans_hidden boolean NOT NULL DEFAULT false;
ALTER TABLE chapters ADD COLUMN scans_hidden boolean NOT NULL DEFAULT false;
CREATE INDEX chapters_scans_hidden_idx ON chapters (id) WHERE scans_hidden;

-- Существующие записи относятся к полному закрытию; новые различаются
-- областью, чтобы последнее открытие сканов не подменило историю текста.
ALTER TABLE closures ADD COLUMN scope text NOT NULL DEFAULT 'full'
    CHECK (scope IN ('full', 'scans'));

-- Причина действующего ограничения работы, по порядку приоритета.
CREATE FUNCTION work_scans_hidden_by(work_id bigint) RETURNS text
LANGUAGE sql STABLE AS $$
    SELECT CASE
        WHEN w.scans_hidden THEN 'work'
        WHEN p.scans_hidden
          OR EXISTS (SELECT 1 FROM editions pe WHERE pe.id = p.edition_id AND pe.scans_hidden) THEN 'parent'
        WHEN EXISTS (SELECT 1 FROM editions e WHERE e.id = w.edition_id AND e.scans_hidden) THEN 'edition'
        WHEN EXISTS (SELECT 1 FROM journal_issues ji
                     JOIN journals j ON j.id = ji.journal_id
                     WHERE ji.work_id = w.id AND j.scans_hidden) THEN 'journal'
        ELSE ''
    END
    FROM works w
    LEFT JOIN works p ON p.id = w.parent_work_id
    WHERE w.id = work_id
$$;

CREATE VIEW scans_hidden_works AS
    SELECT w.id
    FROM works w
    LEFT JOIN works p ON p.id = w.parent_work_id
    WHERE w.scans_hidden
       OR p.scans_hidden
       OR EXISTS (SELECT 1 FROM editions pe WHERE pe.id = p.edition_id AND pe.scans_hidden)
       OR EXISTS (SELECT 1 FROM editions e WHERE e.id = w.edition_id AND e.scans_hidden)
       OR EXISTS (SELECT 1 FROM journal_issues ji
                  JOIN journals j ON j.id = ji.journal_id
                  WHERE ji.work_id = w.id AND j.scans_hidden);

CREATE VIEW scans_hidden_chapters AS
    WITH RECURSIVE t AS (
        SELECT c.id, c.work_id, c.start_page, c.end_page
        FROM chapters c WHERE c.scans_hidden
        UNION
        SELECT c.id, c.work_id, c.start_page, c.end_page
        FROM chapters c JOIN t ON c.parent_id = t.id
    )
    SELECT id, work_id, start_page, end_page FROM t;
