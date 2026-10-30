ALTER TABLE users ADD COLUMN comments_banned BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE users ADD COLUMN comments_rejections INTEGER NOT NULL DEFAULT 0 CHECK (comments_rejections >= 0);

CREATE TABLE reader_comments (
 id BIGSERIAL PRIMARY KEY,
 owner_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
 author_nickname TEXT NOT NULL DEFAULT '',
 work_id BIGINT NOT NULL REFERENCES works(id) ON DELETE CASCADE,
 target TEXT NOT NULL CHECK (target IN ('work','chapter','quote')),
 chapter_id BIGINT REFERENCES chapters(id) ON DELETE SET NULL,
 chapter_title TEXT NOT NULL DEFAULT '',
 start_page_id BIGINT REFERENCES pages(id) ON DELETE SET NULL,
 end_page_id BIGINT REFERENCES pages(id) ON DELETE SET NULL,
 start_page_number INT,
 end_page_number INT,
 quote_key TEXT NOT NULL DEFAULT '', quote_tail TEXT NOT NULL DEFAULT '', quote_text TEXT NOT NULL DEFAULT '',
 body TEXT NOT NULL CHECK (char_length(body) BETWEEN 1 AND 2000),
 status TEXT NOT NULL CHECK (status IN ('на_рассмотрении','одобрено','отклонено')),
 revision INT NOT NULL DEFAULT 1,
 published_body TEXT NOT NULL DEFAULT '', published_revision INT NOT NULL DEFAULT 0,
 reject_reason TEXT NOT NULL DEFAULT '',
 edited_at TIMESTAMPTZ,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 CHECK (target <> 'quote' OR (start_page_number IS NOT NULL AND end_page_number >= start_page_number AND quote_key <> '')),
 CHECK (target <> 'chapter' OR chapter_title <> '')
);
CREATE INDEX reader_comments_work_idx ON reader_comments(work_id, id);
CREATE INDEX reader_comments_owner_idx ON reader_comments(owner_id, id);
CREATE INDEX reader_comments_queue_idx ON reader_comments(created_at, id) WHERE status='на_рассмотрении';

CREATE TABLE reader_comment_revisions (
 comment_id BIGINT NOT NULL REFERENCES reader_comments(id) ON DELETE CASCADE,
 revision INT NOT NULL,
 body TEXT NOT NULL,
 status TEXT NOT NULL,
 reason TEXT NOT NULL DEFAULT '',
 moderator_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 PRIMARY KEY (comment_id, revision)
);
-- Отдельный журнал не позволяет обойти частоту удалением комментариев.
CREATE TABLE reader_comment_submissions (
 owner_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
 ip_hash TEXT NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX reader_comment_submissions_owner_idx ON reader_comment_submissions(owner_id, created_at);
CREATE INDEX reader_comment_submissions_ip_idx ON reader_comment_submissions(ip_hash, created_at);
