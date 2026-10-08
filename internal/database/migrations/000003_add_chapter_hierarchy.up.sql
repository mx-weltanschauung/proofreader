-- Add parent_id column for chapter hierarchy
ALTER TABLE chapters ADD COLUMN parent_id BIGINT REFERENCES chapters(id) ON DELETE CASCADE;

-- Add index on parent_id for faster lookups
CREATE INDEX idx_chapters_parent ON chapters(parent_id);

-- Drop the old unique constraint on (work_id, order_number)
ALTER TABLE chapters DROP CONSTRAINT IF EXISTS chapters_work_id_order_number_key;

-- Add new unique constraint for ordering within siblings (parent)
-- Use COALESCE to handle NULL parent_id (root chapters)
CREATE UNIQUE INDEX idx_chapters_sibling_order ON chapters(work_id, COALESCE(parent_id, 0), order_number);

