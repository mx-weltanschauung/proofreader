-- Класс ширины окна читателя (зимняя карта, направление 1): narrow < 600,
-- medium < 1024, wide — остальное; '' — не прислан (старый клиент, серверные
-- каналы). Точная ширина и User-Agent не хранятся: нужна доля телефонов, а не
-- отпечаток. Корзину проверяет сервер по закрытому списку (stats.DeviceClass).
ALTER TABLE page_views ADD COLUMN device text NOT NULL DEFAULT '';

-- Дневная свёртка устройств, только канал spa. Отдельно от
-- page_views_daily_totals по той же причине, что и они от page_views_daily:
-- уникальных нельзя сложить из групп.
CREATE TABLE page_views_daily_devices (
    day      date    NOT NULL,
    device   text    NOT NULL,
    views    integer NOT NULL,
    visitors integer NOT NULL,
    PRIMARY KEY (day, device)
);
