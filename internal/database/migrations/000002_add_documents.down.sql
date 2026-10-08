-- Drop trigger
DROP TRIGGER IF EXISTS update_documents_updated_at ON documents;

-- Drop indexes
DROP INDEX IF EXISTS idx_documents_created;
DROP INDEX IF EXISTS idx_documents_owner;

-- Drop documents table
DROP TABLE IF EXISTS documents;


