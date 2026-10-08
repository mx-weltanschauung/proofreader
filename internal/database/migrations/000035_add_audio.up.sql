-- Озвучка: заявки, синтезированный звук и записи человека
-- (docs/superpowers/specs/2026-09-29-audiobooks-delivery-design.md, шаг 2 и
-- «Ручные записи»). Объекты звука лежат в отдельном бакете S3_AUDIO_BUCKET;
-- каскад Postgres про него не знает — чистят обработчики снятия.

CREATE TABLE audio_queue (
    id            bigserial PRIMARY KEY,
    work_id       bigint NOT NULL REFERENCES works(id) ON DELETE CASCADE,
    -- NULL — весь том.
    chapter_id    bigint REFERENCES chapters(id) ON DELETE CASCADE,
    status        text NOT NULL DEFAULT 'в_очереди'
                  CHECK (status IN ('в_очереди', 'синтезируется', 'готово', 'ошибка')),
    error         text NOT NULL DEFAULT '',
    -- Счёт невычитанных полос диапазона, присылает worker.
    status_counts jsonb NOT NULL DEFAULT '{}'::jsonb,
    requested_by  bigint REFERENCES users(id) ON DELETE SET NULL,
    requested_at  timestamptz NOT NULL DEFAULT now(),
    claimed_at    timestamptz,
    finished_at   timestamptz
);
-- Незавершённая заявка на ту же главу (или том) одна: постановка отдаёт
-- существующую. coalesce — потому что NULL в уникальном индексе не равен
-- NULL, и две заявки «весь том» прошли бы обе.
CREATE UNIQUE INDEX audio_queue_open_key ON audio_queue (work_id, (coalesce(chapter_id, 0)))
    WHERE status IN ('в_очереди', 'синтезируется');
CREATE INDEX audio_queue_status_idx ON audio_queue (status, requested_at, id);

CREATE TABLE audio_tracks (
    id            bigserial PRIMARY KEY,
    work_id       bigint NOT NULL REFERENCES works(id) ON DELETE CASCADE,
    title         text NOT NULL,
    start_page    integer NOT NULL,
    end_page      integer NOT NULL CHECK (end_page >= start_page),
    s3_key        text NOT NULL UNIQUE,
    duration_ms   bigint NOT NULL,
    bytes         bigint NOT NULL,
    md5           text NOT NULL,
    recipe_sha256 text NOT NULL,
    -- sha256 сырого текста полос диапазона (internal/audio.PagesSHA256).
    pages_sha256  text NOT NULL,
    stale         boolean NOT NULL DEFAULT false,
    created_at    timestamptz NOT NULL DEFAULT now()
);
-- Порядка дорожек не храним: глава находит свои пересечением диапазонов,
-- порядок — по start_page (номер дорожки нестабилен, начало — стабильно).
CREATE INDEX audio_tracks_work_start_idx ON audio_tracks (work_id, start_page);

CREATE TABLE audio_recordings (
    id           bigserial PRIMARY KEY,
    work_id      bigint NOT NULL REFERENCES works(id) ON DELETE CASCADE,
    chapter_id   bigint NOT NULL REFERENCES chapters(id) ON DELETE CASCADE,
    position     integer NOT NULL CHECK (position >= 1),
    reader       text NOT NULL DEFAULT '',
    s3_key       text NOT NULL UNIQUE,
    content_type text NOT NULL
                 CHECK (content_type IN ('audio/mpeg', 'audio/mp4', 'audio/ogg', 'audio/opus', 'audio/flac')),
    bytes        bigint NOT NULL,
    duration_ms  bigint NOT NULL,
    uploaded_by  bigint REFERENCES users(id) ON DELETE SET NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    -- Отложенная: перестановка сдвигает соседей одной транзакцией, и
    -- посередине две строки на миг делят одну позицию.
    CONSTRAINT audio_recordings_position_key UNIQUE (chapter_id, position)
        DEFERRABLE INITIALLY DEFERRED
);
CREATE INDEX audio_recordings_work_idx ON audio_recordings (work_id);
