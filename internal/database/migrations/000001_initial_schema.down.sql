-- Drop triggers
DROP TRIGGER IF EXISTS update_users_updated_at ON users;
DROP TRIGGER IF EXISTS update_works_updated_at ON works;
DROP TRIGGER IF EXISTS update_categories_updated_at ON categories;
DROP TRIGGER IF EXISTS update_chapters_updated_at ON chapters;
DROP TRIGGER IF EXISTS update_pages_updated_at ON pages;
DROP TRIGGER IF EXISTS update_footnotes_updated_at ON footnotes;
DROP TRIGGER IF EXISTS update_composite_pages_updated_at ON composite_pages;
DROP TRIGGER IF EXISTS update_page_ranges_updated_at ON page_ranges;

-- Drop function
DROP FUNCTION IF EXISTS update_updated_at_column();

-- Drop tables
DROP TABLE IF EXISTS status_history;
DROP TABLE IF EXISTS page_ranges;
DROP TABLE IF EXISTS composite_pages;
DROP TABLE IF EXISTS footnotes;
DROP TABLE IF EXISTS page_versions;
DROP TABLE IF EXISTS pages;
DROP TABLE IF EXISTS chapters;
DROP TABLE IF EXISTS work_categories;
DROP TABLE IF EXISTS categories;
DROP TABLE IF EXISTS works;
DROP TABLE IF EXISTS users;

-- Drop enum types
DROP TYPE IF EXISTS work_status;
DROP TYPE IF EXISTS page_status;
DROP TYPE IF EXISTS user_role;

