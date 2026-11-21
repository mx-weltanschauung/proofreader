-- Joining two sets lets PostgreSQL restrict each by work_id before matching
-- ranges. The old OR/EXISTS evaluated closed_chapters for every page and
-- could trigger expensive JIT compilation even for one work.
CREATE OR REPLACE VIEW closed_pages AS
    SELECT p.id, p.work_id, p.page_number
    FROM pages p JOIN closed_works cw ON cw.id = p.work_id
    UNION
    SELECT p.id, p.work_id, p.page_number
    FROM closed_chapters cc
    JOIN pages p ON p.work_id = cc.work_id
                AND p.page_number BETWEEN cc.start_page AND cc.end_page;

-- Seed from explicit restrictions and propagate to all descendant works.
-- This is the set equivalent of work_scans_hidden_by(), which remains useful
-- for asking why one work is restricted, but is costly once per corpus row.
CREATE OR REPLACE VIEW scans_hidden_works AS
    WITH RECURSIVE hidden(id) AS (
        SELECT w.id FROM works w
        WHERE w.scans_hidden
           OR EXISTS (SELECT 1 FROM editions e
                      WHERE e.id = w.edition_id AND e.scans_hidden)
           OR EXISTS (SELECT 1 FROM journal_issues ji
                      JOIN journals j ON j.id = ji.journal_id
                      WHERE ji.work_id = w.id AND j.scans_hidden)
        UNION
        SELECT w.id FROM works w JOIN hidden h ON w.parent_work_id = h.id
    )
    SELECT id FROM hidden;

-- The recursive chapter views use a partial-index seed. After large corpus
-- imports without statistics PostgreSQL estimates half the chapters as closed
-- and may JIT-compile ordinary reads. Refresh the estimate on deployment.
ANALYZE chapters;
