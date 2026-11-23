ALTER TABLE collections
    ADD COLUMN program_review_status TEXT NOT NULL DEFAULT ''
        CHECK (program_review_status IN ('', 'pending', 'rejected')),
    ADD COLUMN program_reject_reason TEXT NOT NULL DEFAULT '',
    ADD COLUMN program_submitted_at TIMESTAMP,
    ADD COLUMN program_submit_ip_hash TEXT NOT NULL DEFAULT '';

CREATE INDEX idx_collections_program_queue ON collections (program_submitted_at, id)
    WHERE program_review_status = 'pending';
-- Запись подачи переживает отзыв и удаление подборки.
CREATE TABLE collection_program_submissions (
    ip_hash TEXT NOT NULL,
    submitted_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_collection_program_submissions_ip ON collection_program_submissions (ip_hash, submitted_at);
