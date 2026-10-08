DROP INDEX IF EXISTS idx_index_concept_links_from_article;
ALTER TABLE index_concept_links DROP COLUMN IF EXISTS from_article_id;

DROP INDEX IF EXISTS idx_index_references_article;
ALTER TABLE index_references
    DROP COLUMN IF EXISTS rubric_id,
    DROP COLUMN IF EXISTS article_id;

DROP TABLE IF EXISTS index_rubrics;
DROP TABLE IF EXISTS index_concept_articles;

DROP INDEX IF EXISTS idx_index_concepts_title_key;
ALTER TABLE index_concepts DROP COLUMN IF EXISTS title_key;

-- NOT NULL возвращается, только если возвращать его есть куда. Понятие без
-- работы-указателя старая схема выразить не может, и молча снести его откат
-- не вправе: колонка остаётся nullable, а оператор получает громкое
-- предупреждение. Падение отката тут недопустимо.
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM index_concepts WHERE work_id IS NULL) THEN
        ALTER TABLE index_concepts ALTER COLUMN work_id SET NOT NULL;
    ELSE
        RAISE NOTICE 'index_concepts.work_id оставлен nullable: % понятий без работы-указателя', (SELECT count(*) FROM index_concepts WHERE work_id IS NULL);
    END IF;
END $$;
