-- Remove unique constraint for sibling ordering
DROP INDEX IF EXISTS idx_chapters_sibling_order;

-- Remove index on parent_id
DROP INDEX IF EXISTS idx_chapters_parent;

-- Remove parent_id column
ALTER TABLE chapters DROP COLUMN IF EXISTS parent_id;

-- Restore original unique constraint
ALTER TABLE chapters ADD CONSTRAINT chapters_work_id_order_number_key UNIQUE (work_id, order_number);

