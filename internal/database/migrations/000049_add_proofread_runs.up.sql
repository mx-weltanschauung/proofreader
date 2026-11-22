ALTER TABLE chapters ADD CONSTRAINT chapters_id_work_id_key UNIQUE (id, work_id);

CREATE TABLE proofread_runs (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    work_id BIGINT NOT NULL,
    chapter_id BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    pair_code_hash BYTEA UNIQUE,
    pair_code_expires_at TIMESTAMPTZ,
    worker_token_hash BYTEA,
    worker_token_expires_at TIMESTAMPTZ,
    FOREIGN KEY (chapter_id, work_id) REFERENCES chapters(id, work_id) ON DELETE CASCADE,
    CHECK ((pair_code_hash IS NULL) = (pair_code_expires_at IS NULL)),
    CHECK (octet_length(pair_code_hash) = 32),
    CHECK ((worker_token_hash IS NULL) = (worker_token_expires_at IS NULL)),
    CHECK (octet_length(worker_token_hash) = 32)
);

CREATE INDEX idx_proofread_runs_user ON proofread_runs (user_id);
CREATE INDEX idx_proofread_runs_chapter ON proofread_runs (chapter_id, work_id);

CREATE TABLE proofread_run_pages (
    run_id BIGINT NOT NULL REFERENCES proofread_runs(id) ON DELETE CASCADE,
    page_id BIGINT NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
    page_number INTEGER NOT NULL,
    base_markdown TEXT NOT NULL,
    base_sha256 TEXT NOT NULL CHECK (base_sha256 ~ '^[a-f0-9]{64}$'),
    analysis_status TEXT NOT NULL DEFAULT 'pending' CHECK (analysis_status IN ('pending', 'analyzed', 'failed')),
    analysis_error TEXT NOT NULL DEFAULT '',
    suggestion_id BIGINT UNIQUE REFERENCES page_suggestions(id) ON DELETE SET NULL,
    submitted_at TIMESTAMPTZ,
    PRIMARY KEY (run_id, page_id),
    CHECK (suggestion_id IS NULL OR submitted_at IS NOT NULL)
);

CREATE INDEX idx_proofread_run_pages_page ON proofread_run_pages (page_id);

CREATE TABLE proofread_findings (
    id BIGSERIAL PRIMARY KEY,
    run_id BIGINT NOT NULL,
    page_id BIGINT NOT NULL,
    candidate_key VARCHAR(256) NOT NULL CHECK (candidate_key <> ''),
    start_rune INTEGER NOT NULL CHECK (start_rune >= 0),
    end_rune INTEGER NOT NULL CHECK (end_rune >= start_rune),
    old TEXT NOT NULL,
    new TEXT NOT NULL,
    source TEXT NOT NULL,
    reason TEXT NOT NULL,
    scan_bbox JSONB,
    decision TEXT NOT NULL DEFAULT 'pending' CHECK (decision IN ('pending', 'accepted', 'rejected')),
    replacement TEXT NOT NULL DEFAULT '',
    FOREIGN KEY (run_id, page_id) REFERENCES proofread_run_pages(run_id, page_id) ON DELETE CASCADE,
    UNIQUE (run_id, page_id, candidate_key),
    CHECK (decision = 'accepted' OR replacement = '')
);
