-- Сначала разберите очередь и сохраните решения модерации.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM collections WHERE program_review_status <> '') THEN
        RAISE EXCEPTION 'в базе есть программы на проверке или с отказом: шаг вниз 000053 удалил бы их состояние';
    END IF;
END $$;

DROP TABLE collection_program_submissions;
DROP INDEX idx_collections_program_queue;
ALTER TABLE collections
    DROP COLUMN program_submit_ip_hash,
    DROP COLUMN program_submitted_at,
    DROP COLUMN program_reject_reason,
    DROP COLUMN program_review_status;
