-- Шаг вниз ОТКАЗЫВАЕТ, пока закрыта хоть одна глава: снос столбца открыл бы
-- её полосы всем разом. Отказ — первым оператором; файл выполняется одним
-- неявным блоком, поэтому упавший откат не меняет ничего. golang-migrate
-- помечает версию грязной ДО выполнения файла: после отказа schema_migrations
-- читается «41, dirty=true», хотя схема на 42, — чинится
-- `DB_NAME=<база> go run cmd/migrate/main.go force 42`.
-- Журнал closures при откате сносится вместе с таблицей.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM chapters WHERE closed) THEN
        RAISE EXCEPTION 'в базе есть закрытые главы: шаг вниз 000042 открыл бы их всем — сначала откройте их';
    END IF;
END $$;

DROP VIEW closed_pages;
DROP VIEW closed_chapters;
DROP TABLE closures;
DROP INDEX chapters_closed_idx;
ALTER TABLE chapters DROP COLUMN closed;
