-- Откат недопустим после любого ограничения или записи о нём: пропали бы
-- права доступа либо история того, кто открыл сканы.
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM works WHERE scans_hidden)
       OR EXISTS (SELECT 1 FROM editions WHERE scans_hidden)
       OR EXISTS (SELECT 1 FROM journals WHERE scans_hidden)
       OR EXISTS (SELECT 1 FROM chapters WHERE scans_hidden)
       OR EXISTS (SELECT 1 FROM closures WHERE scope = 'scans') THEN
        RAISE EXCEPTION 'сначала снимите ограничения сканов и сохраните историю доступа';
    END IF;
END $$;

DROP VIEW scans_hidden_chapters;
DROP VIEW scans_hidden_works;
DROP FUNCTION work_scans_hidden_by(bigint);
ALTER TABLE closures DROP COLUMN scope;
DROP INDEX chapters_scans_hidden_idx;
ALTER TABLE chapters DROP COLUMN scans_hidden;
ALTER TABLE journals DROP COLUMN scans_hidden;
ALTER TABLE editions DROP COLUMN scans_hidden;
ALTER TABLE works DROP COLUMN scans_hidden;
