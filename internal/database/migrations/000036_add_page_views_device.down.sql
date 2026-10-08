-- Шаг вниз теряет классы устройств целиком — и сырые, и свёрнутые. Прочая
-- посещаемость остаётся.
DROP TABLE page_views_daily_devices;
ALTER TABLE page_views DROP COLUMN device;
