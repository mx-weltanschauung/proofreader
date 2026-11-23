ALTER TABLE reader_comments
    ADD COLUMN thread_id BIGINT REFERENCES reader_comments(id) ON DELETE CASCADE,
    ADD COLUMN reply_to_nickname TEXT,
    ADD COLUMN recipient_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN reply_read_at TIMESTAMPTZ,
    ADD COLUMN removed TEXT NOT NULL DEFAULT '' CHECK (removed IN ('', 'author', 'moderator')),
    ADD CONSTRAINT reader_comments_reply_fields CHECK (
        (thread_id IS NULL AND recipient_id IS NULL AND reply_to_nickname IS NULL AND reply_read_at IS NULL)
        OR (thread_id IS NOT NULL AND thread_id <> id AND removed = '')
    );

CREATE INDEX reader_comments_thread_idx ON reader_comments(thread_id, id) WHERE thread_id IS NOT NULL;
CREATE INDEX reader_comments_recipient_idx ON reader_comments(recipient_id, id DESC)
    WHERE recipient_id IS NOT NULL AND published_revision > 0;
