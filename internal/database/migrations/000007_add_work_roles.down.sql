-- Строк НЕ удаляем. Снести служебные работы при откате заманчиво — иначе они
-- всплывут в каталоге как обычные, — но откат одной ступени не должен уносить
-- данные: make migrate-down уже однажды стёр базу целиком. Разбирает оператор.
DROP INDEX IF EXISTS idx_works_parent_role;
DROP INDEX IF EXISTS idx_works_parent;

ALTER TABLE works
    DROP CONSTRAINT IF EXISTS works_parent_role_check,
    DROP CONSTRAINT IF EXISTS works_numbering_style_check,
    DROP CONSTRAINT IF EXISTS works_role_check;

ALTER TABLE works
    DROP COLUMN IF EXISTS numbering_style,
    DROP COLUMN IF EXISTS role,
    DROP COLUMN IF EXISTS parent_work_id;
