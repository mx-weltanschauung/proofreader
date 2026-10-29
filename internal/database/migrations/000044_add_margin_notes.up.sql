-- Заметки читателя на полях: личные, видны только владельцу. Заметка
-- привязана к тому (work_id), а её цель — весь том, глава или цитата.
-- Закрытость проверяется по work_id при чтении, своего признака у заметок нет.
CREATE TABLE margin_notes (
    id                BIGSERIAL PRIMARY KEY,
    owner_id          BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    work_id           BIGINT NOT NULL REFERENCES works(id) ON DELETE CASCADE,
    target            TEXT NOT NULL CHECK (target IN ('work', 'chapter', 'quote')),
    chapter_id        BIGINT REFERENCES chapters(id) ON DELETE SET NULL,
    chapter_title     TEXT NOT NULL DEFAULT '',
    start_page_id     BIGINT REFERENCES pages(id) ON DELETE SET NULL,
    end_page_id       BIGINT REFERENCES pages(id) ON DELETE SET NULL,
    start_page_number INT,
    end_page_number   INT,
    quote_key         TEXT NOT NULL DEFAULT '',
    quote_tail        TEXT NOT NULL DEFAULT '',
    quote_text        TEXT NOT NULL DEFAULT '',
    body              TEXT NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT margin_notes_quote_shape CHECK (
        target <> 'quote' OR (
            start_page_number IS NOT NULL
            AND end_page_number >= start_page_number
            AND quote_key <> ''
        )
    ),
    CONSTRAINT margin_notes_chapter_shape CHECK (
        target <> 'chapter' OR chapter_title <> ''
    )
);

CREATE INDEX margin_notes_owner_work_idx ON margin_notes (owner_id, work_id);
CREATE INDEX margin_notes_owner_created_idx ON margin_notes (owner_id, created_at DESC);
