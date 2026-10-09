-- Шаг вниз ОТКАЗЫВАЕТ, если залит хоть один номер журнала: снос таблиц унёс
-- бы номера с главами и подписями (работы номеров остались бы без роли в
-- каталоге). Отказ — первым оператором; файл выполняется одним неявным
-- блоком, поэтому упавший откат не меняет ничего. golang-migrate помечает
-- версию грязной ДО выполнения файла: после отказа schema_migrations читается
-- «37, dirty=true», хотя схема на 38, — чинится
-- `DB_NAME=<база> go run cmd/migrate/main.go force 38`.
-- Снять журналы осознанно: удалить работы номеров (DELETE /api/works/{id}),
-- затем повторить шаг вниз.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM journal_issues)
       OR EXISTS (SELECT 1 FROM works WHERE role = 'journal_issue') THEN
        RAISE EXCEPTION 'в базе есть номера журналов: шаг вниз 000038 снёс бы их — сначала удалите работы номеров';
    END IF;
END $$;

ALTER TABLE works
    DROP CONSTRAINT works_role_check,
    DROP CONSTRAINT works_parent_role_check;
ALTER TABLE works
    ADD CONSTRAINT works_role_check
        CHECK (role IN ('volume', 'front_matter', 'edition_front_matter')),
    ADD CONSTRAINT works_parent_role_check
        CHECK ((role = 'volume' AND parent_work_id IS NULL)
            OR (role = 'front_matter' AND parent_work_id IS NOT NULL)
            OR (role = 'edition_front_matter'
                AND parent_work_id IS NULL AND edition_id IS NOT NULL));

ALTER TABLE chapters DROP CONSTRAINT IF EXISTS chapters_article_kind_check;
ALTER TABLE chapters DROP COLUMN IF EXISTS article_kind;
DROP TABLE IF EXISTS article_credits;
DROP TABLE IF EXISTS persons;
DROP TABLE IF EXISTS journal_issues;
DROP TABLE IF EXISTS journals;
