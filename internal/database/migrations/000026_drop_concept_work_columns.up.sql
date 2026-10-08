-- Адрес без статьи и отсылка без статьи — мусор переходного периода.
DELETE FROM index_references WHERE article_id IS NULL;
DELETE FROM index_concept_links WHERE from_article_id IS NULL;

ALTER TABLE index_references
    ALTER COLUMN article_id SET NOT NULL,
    DROP COLUMN concept_id,
    DROP COLUMN rubric;

ALTER TABLE index_concept_links
    ALTER COLUMN from_article_id SET NOT NULL,
    DROP COLUMN from_concept_id;

ALTER TABLE index_concepts
    DROP COLUMN work_id,
    DROP COLUMN article_markdown,
    DROP COLUMN source_page_start,
    DROP COLUMN source_page_end,
    DROP COLUMN kind;
