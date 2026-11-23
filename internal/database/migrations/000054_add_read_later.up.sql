CREATE TABLE read_later (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    owner_id    bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    work_id     bigint REFERENCES works(id) ON DELETE CASCADE,
    chapter_id  bigint REFERENCES chapters(id) ON DELETE CASCADE,
    created_at  timestamptz NOT NULL DEFAULT now(),
    CHECK ((work_id IS NULL) <> (chapter_id IS NULL)),
    UNIQUE NULLS NOT DISTINCT (owner_id, work_id, chapter_id)
);

CREATE INDEX read_later_owner_recent_idx ON read_later (owner_id, created_at DESC, id DESC);
CREATE INDEX read_later_work_idx ON read_later (work_id) WHERE work_id IS NOT NULL;
CREATE INDEX read_later_chapter_idx ON read_later (chapter_id) WHERE chapter_id IS NOT NULL;
