DROP TABLE IF EXISTS index_concept_links;
DROP TABLE IF EXISTS index_references;
DROP TABLE IF EXISTS index_concepts;

DROP INDEX IF EXISTS idx_works_edition_volume;
DROP INDEX IF EXISTS idx_works_edition;

ALTER TABLE works
    DROP COLUMN IF EXISTS page_offset,
    DROP COLUMN IF EXISTS volume_part,
    DROP COLUMN IF EXISTS volume_number,
    DROP COLUMN IF EXISTS edition_id;

DROP TABLE IF EXISTS editions;
