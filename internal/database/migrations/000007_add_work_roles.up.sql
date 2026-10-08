-- Том — это две независимые последовательности нумерации (римская у передних
-- листов, арабская у тела) плюс ненумерованная обложка. Одним page_offset они
-- не выражаются, поэтому передние листы становятся отдельной работой,
-- привязанной к тому.
ALTER TABLE works
    ADD COLUMN parent_work_id  BIGINT REFERENCES works(id) ON DELETE CASCADE,
    ADD COLUMN role            VARCHAR(32) NOT NULL DEFAULT 'volume',
    ADD COLUMN numbering_style VARCHAR(16) NOT NULL DEFAULT 'arabic';

ALTER TABLE works
    ADD CONSTRAINT works_role_check
        CHECK (role IN ('volume', 'front_matter')),
    ADD CONSTRAINT works_numbering_style_check
        CHECK (numbering_style IN ('arabic', 'roman')),
    -- служебная работа обязана иметь родителя, том — не иметь
    ADD CONSTRAINT works_parent_role_check
        CHECK ((role =  'volume' AND parent_work_id IS NULL)
            OR (role <> 'volume' AND parent_work_id IS NOT NULL));

CREATE INDEX idx_works_parent ON works(parent_work_id);

-- Один front matter на том.
CREATE UNIQUE INDEX idx_works_parent_role
    ON works(parent_work_id, role) WHERE parent_work_id IS NOT NULL;
