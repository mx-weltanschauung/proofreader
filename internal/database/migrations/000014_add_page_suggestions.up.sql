CREATE TABLE page_suggestions (
    id                BIGSERIAL PRIMARY KEY,
    page_id           BIGINT NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
    base_markdown     TEXT NOT NULL,
    proposed_markdown TEXT NOT NULL,
    note              TEXT NOT NULL DEFAULT '',
    reader_key        UUID NOT NULL,
    ip_hash           TEXT NOT NULL DEFAULT '',
    status            TEXT NOT NULL DEFAULT 'новое',
    reject_reason     TEXT,
    moderator_id      BIGINT REFERENCES users(id) ON DELETE SET NULL,
    resolved_at       TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (status IN ('новое','принято','отклонено')),
    CHECK ((status = 'отклонено') = (reject_reason IS NOT NULL)),
    CHECK (reject_reason IS NULL OR reject_reason IN
           ('так_в_оригинале','уже_исправлено','не_по_теме')),
    CHECK (proposed_markdown <> base_markdown),
    CHECK (char_length(proposed_markdown) <= 200000),
    CHECK (char_length(note) <= 2000)
);

CREATE INDEX idx_page_suggestions_queue  ON page_suggestions(status, created_at DESC);
CREATE INDEX idx_page_suggestions_reader ON page_suggestions(reader_key, created_at DESC);
CREATE INDEX idx_page_suggestions_ip     ON page_suggestions(ip_hash, created_at DESC);
CREATE INDEX idx_page_suggestions_page   ON page_suggestions(page_id);
