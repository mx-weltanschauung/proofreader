-- Сырые появления полос, 90 дней. Нет внешних ключей: событие не должно
-- мешать чтению при удалении тома или полосы. При свёртке проверяем pages.
CREATE TABLE visible_page_views (
    day date NOT NULL,
    ts timestamptz NOT NULL,
    work_id bigint NOT NULL,
    page_number integer NOT NULL,
    visitor text NOT NULL
);
CREATE INDEX visible_page_views_day_work_page_idx ON visible_page_views (day, work_id, page_number);

CREATE TABLE visible_page_views_daily (
    day date NOT NULL,
    work_id bigint NOT NULL,
    page_number integer NOT NULL,
    visitors integer NOT NULL,
    PRIMARY KEY (day, work_id, page_number)
);
CREATE INDEX visible_page_views_daily_work_page_day_idx ON visible_page_views_daily (work_id, page_number, day);
