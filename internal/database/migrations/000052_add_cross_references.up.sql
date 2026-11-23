CREATE TABLE cross_references (
    id bigserial PRIMARY KEY,
    edition_id bigint NOT NULL REFERENCES editions(id) ON DELETE CASCADE,
    edition_designation integer NOT NULL CHECK (edition_designation >= 0),
    source_page_id bigint NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
    source_digest bytea NOT NULL CHECK (octet_length(source_digest) = 32),
    note text NOT NULL,
    ordinal integer NOT NULL CHECK (ordinal > 0),
    quote text NOT NULL,
    context text NOT NULL,
    volume integer NOT NULL CHECK (volume > 0),
    printed_page integer NOT NULL CHECK (printed_page > 0),
    edition_number integer NOT NULL CHECK (edition_number >= 0),
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'confirmed', 'rejected')),
    target_page_id bigint REFERENCES pages(id) ON DELETE SET NULL,
    CHECK (target_page_id IS NULL OR status = 'confirmed'),
    UNIQUE (source_page_id, edition_id, edition_designation, source_digest, note, ordinal)
);
CREATE INDEX cross_references_edition_status_id_idx ON cross_references (edition_id, status, id);
CREATE INDEX cross_references_target_page_id_idx ON cross_references (target_page_id) WHERE target_page_id IS NOT NULL;
