-- Enable UUID extension
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Create user_role enum type
CREATE TYPE user_role AS ENUM ('administrator', 'editor', 'proofreader');

-- Create page_status enum type
CREATE TYPE page_status AS ENUM ('не_вычитана', 'вычитывается', 'вычитана', 'есть_проблемы', 'пустая_страница');

-- Create work_status enum type
CREATE TYPE work_status AS ENUM ('draft', 'in_progress', 'completed', 'archived');

-- Create users table
CREATE TABLE users (
    id BIGSERIAL PRIMARY KEY,
    email VARCHAR(255) NOT NULL UNIQUE,
    password_hash VARCHAR(255) NOT NULL,
    role user_role NOT NULL DEFAULT 'proofreader',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- Create index on email for faster lookups
CREATE INDEX idx_users_email ON users(email);

-- Create works table
CREATE TABLE works (
    id BIGSERIAL PRIMARY KEY,
    title VARCHAR(500) NOT NULL,
    author VARCHAR(255),
    publication_date DATE,
    language VARCHAR(50),
    country VARCHAR(100),
    file_path VARCHAR(500) NOT NULL,
    status work_status NOT NULL DEFAULT 'draft',
    owner_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- Create indexes on works
CREATE INDEX idx_works_owner ON works(owner_id);
CREATE INDEX idx_works_status ON works(status);
CREATE INDEX idx_works_title ON works USING gin(to_tsvector('english', title));
CREATE INDEX idx_works_author ON works USING gin(to_tsvector('english', author));

-- Create categories table
CREATE TABLE categories (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL UNIQUE,
    slug VARCHAR(255) NOT NULL UNIQUE,
    description TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- Create index on slug
CREATE INDEX idx_categories_slug ON categories(slug);

-- Create work_categories junction table
CREATE TABLE work_categories (
    work_id BIGINT NOT NULL REFERENCES works(id) ON DELETE CASCADE,
    category_id BIGINT NOT NULL REFERENCES categories(id) ON DELETE CASCADE,
    PRIMARY KEY (work_id, category_id)
);

-- Create indexes on work_categories
CREATE INDEX idx_work_categories_work ON work_categories(work_id);
CREATE INDEX idx_work_categories_category ON work_categories(category_id);

-- Create chapters table
CREATE TABLE chapters (
    id BIGSERIAL PRIMARY KEY,
    work_id BIGINT NOT NULL REFERENCES works(id) ON DELETE CASCADE,
    title VARCHAR(500) NOT NULL,
    type VARCHAR(50) NOT NULL, -- 'chapter' or 'part'
    order_number INT NOT NULL,
    start_page INT NOT NULL,
    end_page INT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    UNIQUE (work_id, order_number)
);

-- Create indexes on chapters
CREATE INDEX idx_chapters_work ON chapters(work_id);
CREATE INDEX idx_chapters_order ON chapters(work_id, order_number);

-- Create pages table
CREATE TABLE pages (
    id BIGSERIAL PRIMARY KEY,
    work_id BIGINT NOT NULL REFERENCES works(id) ON DELETE CASCADE,
    page_number INT NOT NULL,
    preview_path VARCHAR(500),
    content_markdown TEXT,
    status page_status NOT NULL DEFAULT 'не_вычитана',
    chapter_id BIGINT REFERENCES chapters(id) ON DELETE SET NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    UNIQUE (work_id, page_number)
);

-- Create indexes on pages
CREATE INDEX idx_pages_work ON pages(work_id);
CREATE INDEX idx_pages_status ON pages(status);
CREATE INDEX idx_pages_chapter ON pages(chapter_id);
CREATE INDEX idx_pages_content ON pages USING gin(to_tsvector('english', content_markdown));

-- Create page_versions table
CREATE TABLE page_versions (
    id BIGSERIAL PRIMARY KEY,
    page_id BIGINT NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
    content_markdown TEXT NOT NULL,
    version_number INT NOT NULL,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    comment TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    UNIQUE (page_id, version_number)
);

-- Create indexes on page_versions
CREATE INDEX idx_page_versions_page ON page_versions(page_id);
CREATE INDEX idx_page_versions_user ON page_versions(user_id);
CREATE INDEX idx_page_versions_created ON page_versions(created_at DESC);

-- Create footnotes table
CREATE TABLE footnotes (
    id BIGSERIAL PRIMARY KEY,
    page_id BIGINT NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
    content_markdown TEXT NOT NULL,
    order_number INT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- Create indexes on footnotes
CREATE INDEX idx_footnotes_page ON footnotes(page_id);
CREATE INDEX idx_footnotes_order ON footnotes(page_id, order_number);

-- Create composite_pages table
CREATE TABLE composite_pages (
    id BIGSERIAL PRIMARY KEY,
    title VARCHAR(500) NOT NULL,
    slug VARCHAR(255) NOT NULL UNIQUE,
    description TEXT,
    owner_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- Create index on slug
CREATE INDEX idx_composite_pages_slug ON composite_pages(slug);
CREATE INDEX idx_composite_pages_owner ON composite_pages(owner_id);

-- Create page_ranges table
CREATE TABLE page_ranges (
    id BIGSERIAL PRIMARY KEY,
    composite_page_id BIGINT NOT NULL REFERENCES composite_pages(id) ON DELETE CASCADE,
    work_id BIGINT NOT NULL REFERENCES works(id) ON DELETE CASCADE,
    start_page INT NOT NULL,
    end_page INT NOT NULL,
    order_number INT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- Create indexes on page_ranges
CREATE INDEX idx_page_ranges_composite ON page_ranges(composite_page_id);
CREATE INDEX idx_page_ranges_work ON page_ranges(work_id);
CREATE INDEX idx_page_ranges_order ON page_ranges(composite_page_id, order_number);

-- Create status_history table
CREATE TABLE status_history (
    id BIGSERIAL PRIMARY KEY,
    entity_type VARCHAR(50) NOT NULL, -- 'work' or 'page'
    entity_id BIGINT NOT NULL,
    status VARCHAR(50) NOT NULL,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- Create indexes on status_history
CREATE INDEX idx_status_history_entity ON status_history(entity_type, entity_id);
CREATE INDEX idx_status_history_user ON status_history(user_id);
CREATE INDEX idx_status_history_created ON status_history(created_at DESC);

-- Create function to update updated_at timestamp
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ language 'plpgsql';

-- Create triggers for updated_at columns
CREATE TRIGGER update_users_updated_at BEFORE UPDATE ON users FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
CREATE TRIGGER update_works_updated_at BEFORE UPDATE ON works FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
CREATE TRIGGER update_categories_updated_at BEFORE UPDATE ON categories FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
CREATE TRIGGER update_chapters_updated_at BEFORE UPDATE ON chapters FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
CREATE TRIGGER update_pages_updated_at BEFORE UPDATE ON pages FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
CREATE TRIGGER update_footnotes_updated_at BEFORE UPDATE ON footnotes FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
CREATE TRIGGER update_composite_pages_updated_at BEFORE UPDATE ON composite_pages FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
CREATE TRIGGER update_page_ranges_updated_at BEFORE UPDATE ON page_ranges FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

