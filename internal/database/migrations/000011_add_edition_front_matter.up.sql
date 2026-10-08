-- Предисловие, относящееся не к тому, а к группе томов: в томе 1 напечатано
-- предисловие ко всему изданию, в томе 27 — ко всем томам писем. Такая работа
-- не принадлежит ни одному тому, поэтому родителя у неё нет, но она обязана
-- принадлежать изданию — иначе её негде показать.
--
-- volume_number у неё остаётся NULL намеренно: предметный указатель резолвит
-- ссылку «том N, стр. M» через пару (edition_id, volume_number), и работа с
-- номером тома увела бы ссылку из указателя в предисловие.
ALTER TABLE works
    ADD COLUMN precedes_volume INT;

COMMENT ON COLUMN works.precedes_volume IS
    'Том, перед которым работа стоит на полке издания; только у edition_front_matter';

ALTER TABLE works
    DROP CONSTRAINT works_role_check,
    DROP CONSTRAINT works_parent_role_check;

ALTER TABLE works
    ADD CONSTRAINT works_role_check
        CHECK (role IN ('volume', 'front_matter', 'edition_front_matter')),
    ADD CONSTRAINT works_parent_role_check
        CHECK ((role = 'volume' AND parent_work_id IS NULL)
            OR (role = 'front_matter' AND parent_work_id IS NOT NULL)
            OR (role = 'edition_front_matter'
                AND parent_work_id IS NULL AND edition_id IS NOT NULL));

-- Родителя у edition_front_matter нет, поэтому idx_works_parent_role её не
-- защищает. Без своего индекса забытый идентификатор при повторном прогоне
-- ingest завёл бы второе «Предисловие ко второму изданию» рядом с первым.
CREATE UNIQUE INDEX idx_works_edition_front_matter
    ON works(edition_id, precedes_volume) WHERE role = 'edition_front_matter';
