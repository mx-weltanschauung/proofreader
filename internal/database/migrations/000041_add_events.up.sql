-- События: организации -> виды -> события -> пункты повестки -> строки ->
-- цели (глава или том). Цель держит живую ссылку (ON DELETE SET NULL) и
-- снимок заглавия: перепрогон тома удаляет главы, и строка не должна
-- молча стать ссылкой на весь том или исчезнуть.
CREATE TABLE event_organizations (
    id          BIGSERIAL PRIMARY KEY,
    slug        TEXT NOT NULL UNIQUE,
    title       TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    position    INT NOT NULL DEFAULT 0,
    created_at  TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE event_kinds (
    id              BIGSERIAL PRIMARY KEY,
    organization_id BIGINT NOT NULL REFERENCES event_organizations(id) ON DELETE RESTRICT,
    title           TEXT NOT NULL,
    position        INT NOT NULL DEFAULT 0
);
CREATE INDEX idx_event_kinds_org ON event_kinds(organization_id);

CREATE TABLE events (
    id          BIGSERIAL PRIMARY KEY,
    kind_id     BIGINT NOT NULL REFERENCES event_kinds(id) ON DELETE RESTRICT,
    slug        TEXT NOT NULL UNIQUE,
    title       TEXT NOT NULL,
    dates       TEXT NOT NULL DEFAULT '',
    place       TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    position    INT NOT NULL DEFAULT 0,
    created_at  TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMP NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_events_kind ON events(kind_id);

CREATE TABLE event_sections (
    id       BIGSERIAL PRIMARY KEY,
    event_id BIGINT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    position INT NOT NULL,
    title    TEXT NOT NULL,
    UNIQUE (event_id, position)
);

CREATE TABLE event_entries (
    id         BIGSERIAL PRIMARY KEY,
    section_id BIGINT NOT NULL REFERENCES event_sections(id) ON DELETE CASCADE,
    position   INT NOT NULL,
    label      TEXT NOT NULL,
    UNIQUE (section_id, position)
);

CREATE TABLE event_targets (
    id             BIGSERIAL PRIMARY KEY,
    entry_id       BIGINT NOT NULL REFERENCES event_entries(id) ON DELETE CASCADE,
    position       INT NOT NULL,
    kind           TEXT NOT NULL CHECK (kind IN ('chapter', 'work')),
    work_id        BIGINT REFERENCES works(id) ON DELETE SET NULL,
    chapter_id     BIGINT REFERENCES chapters(id) ON DELETE SET NULL,
    label          TEXT NOT NULL DEFAULT '',
    snapshot_title TEXT NOT NULL,
    UNIQUE (entry_id, position),
    CHECK (kind = 'chapter' OR chapter_id IS NULL)
);
CREATE INDEX idx_event_targets_chapter ON event_targets(chapter_id);
CREATE INDEX idx_event_targets_work ON event_targets(work_id);
