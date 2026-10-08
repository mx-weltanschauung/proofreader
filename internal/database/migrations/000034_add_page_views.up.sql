-- Посещаемость читальни (docs/superpowers/specs/2026-09-29-traffic-and-health-metrics-design.md).
-- Внешних ключей на works/chapters нет намеренно: снятие тома не должно ни
-- падать на статистике, ни стирать её историю.

-- Соль дневной отметки посетителя. Строка прошлых суток удаляется — после
-- этого связать отметку с адресом не можем и мы сами.
CREATE TABLE visit_salts (
    day  date PRIMARY KEY,
    salt bytea NOT NULL
);

-- Сырые просмотры, 90 дней. day — московские сутки, посчитанные в Go.
CREATE TABLE page_views (
    day       date        NOT NULL,
    ts        timestamptz NOT NULL,
    channel   text        NOT NULL,
    kind      text        NOT NULL,
    work_id   bigint      NOT NULL DEFAULT 0,
    entity_id bigint      NOT NULL DEFAULT 0,
    slug_key  text        NOT NULL DEFAULT '',
    agent     text        NOT NULL DEFAULT '',
    visitor   text        NOT NULL DEFAULT '',
    ref_host  text        NOT NULL DEFAULT '',
    query     text        NOT NULL DEFAULT '',
    hits      integer
);
CREATE INDEX page_views_day_idx ON page_views (day);

-- Дневная свёртка по сущностям. Ключ без NULL: ON CONFLICT по NULL ненадёжен.
CREATE TABLE page_views_daily (
    day       date   NOT NULL,
    channel   text   NOT NULL,
    kind      text   NOT NULL,
    work_id   bigint NOT NULL DEFAULT 0,
    entity_id bigint NOT NULL DEFAULT 0,
    slug_key  text   NOT NULL DEFAULT '',
    agent     text   NOT NULL DEFAULT '',
    ref_host  text   NOT NULL DEFAULT '',
    query     text   NOT NULL DEFAULT '',
    views     integer NOT NULL,
    visitors  integer NOT NULL,
    zero_hits integer NOT NULL DEFAULT 0,
    PRIMARY KEY (day, channel, kind, work_id, entity_id, slug_key, agent, ref_host, query)
);

-- Дневные итоги канала. Отдельно, потому что уникальных нельзя сложить из
-- групп: один читатель двух глав дал бы двоих.
CREATE TABLE page_views_daily_totals (
    day      date    NOT NULL,
    channel  text    NOT NULL,
    views    integer NOT NULL,
    visitors integer NOT NULL,
    PRIMARY KEY (day, channel)
);
