-- Шаг вниз ОТКАЗЫВАЕТ, пока в events есть строки: снос таблиц унёс бы
-- раскладку повесток. Отказ — первым оператором; файл выполняется одним
-- неявным блоком, поэтому упавший откат не меняет ничего. golang-migrate
-- помечает версию грязной ДО выполнения файла: после отказа
-- schema_migrations читается «40, dirty=true», хотя схема на 41, —
-- чинится `DB_NAME=<база> go run cmd/migrate/main.go force 41`.
-- Снять события осознанно: удалить их (DELETE /api/events/{id}), затем
-- повторить шаг вниз.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM events) THEN
        RAISE EXCEPTION 'в базе есть события: шаг вниз 000041 снёс бы раскладку повесток — сначала удалите события';
    END IF;
END $$;

DROP TABLE IF EXISTS event_targets;
DROP TABLE IF EXISTS event_entries;
DROP TABLE IF EXISTS event_sections;
DROP TABLE IF EXISTS events;
DROP TABLE IF EXISTS event_kinds;
DROP TABLE IF EXISTS event_organizations;
