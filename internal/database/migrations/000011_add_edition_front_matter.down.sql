-- Возврат ровно к тому виду, в каком ограничения стояли после 000007
-- (сверено с файлом 000007_add_work_roles.up.sql, а не по памяти).
--
-- Если в базе есть работы роли edition_front_matter, восстановление
-- works_role_check упадёт на проверке — и это правильно: молча переписать
-- роль живой работе или снести её откат не должен. Такие работы надо
-- разобрать руками до отката.
DROP INDEX IF EXISTS idx_works_edition_front_matter;

ALTER TABLE works
    DROP CONSTRAINT works_role_check,
    DROP CONSTRAINT works_parent_role_check;

ALTER TABLE works
    ADD CONSTRAINT works_role_check
        CHECK (role IN ('volume', 'front_matter')),
    ADD CONSTRAINT works_parent_role_check
        CHECK ((role =  'volume' AND parent_work_id IS NULL)
            OR (role <> 'volume' AND parent_work_id IS NOT NULL));

ALTER TABLE works
    DROP COLUMN precedes_volume;
