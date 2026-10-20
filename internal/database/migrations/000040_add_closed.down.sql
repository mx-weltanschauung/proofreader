-- Шаг вниз ОТКАЗЫВАЕТ, пока закрыта хоть одна работа, издание или журнал:
-- снос столбцов открыл бы их всем разом. Отказ — первым оператором; файл
-- выполняется одним неявным блоком, поэтому упавший откат не меняет ничего.
-- golang-migrate помечает версию грязной ДО выполнения файла: после отказа
-- schema_migrations читается «предыдущая версия, dirty=true», хотя схема на 40, — чинится
-- `DB_NAME=<база> go run cmd/migrate/main.go force 40`.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM works WHERE closed)
       OR EXISTS (SELECT 1 FROM editions WHERE closed)
       OR EXISTS (SELECT 1 FROM journals WHERE closed) THEN
        RAISE EXCEPTION 'в базе есть закрытые работы, издания или журналы: шаг вниз 000040 открыл бы их всем — сначала откройте их';
    END IF;
END $$;

DROP VIEW closed_works;
DROP FUNCTION work_closed_by(bigint);
ALTER TABLE journals DROP COLUMN closed;
ALTER TABLE editions DROP COLUMN closed;
ALTER TABLE works DROP COLUMN closed;
