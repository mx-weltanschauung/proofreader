ALTER TABLE collections ADD COLUMN program_revision BIGINT NOT NULL DEFAULT 0;

CREATE TABLE collection_meetings (
    collection_id BIGINT NOT NULL REFERENCES collections(id) ON DELETE CASCADE,
    position INT NOT NULL CHECK (position > 0),
    title TEXT NOT NULL CHECK (btrim(title) <> ''),
    question TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (collection_id, position)
);

ALTER TABLE collection_items
    ADD COLUMN meeting_position INT,
    ADD CONSTRAINT collection_items_meeting_fkey
        FOREIGN KEY (collection_id, meeting_position)
        REFERENCES collection_meetings (collection_id, position);

CREATE INDEX idx_collection_items_meeting ON collection_items (collection_id, meeting_position);
