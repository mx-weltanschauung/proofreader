-- Воссоздание ровно в том виде, в каком таблицы стояли в 000001_initial_schema.up.sql
-- (сверено с файлом, а не по памяти — там же для composite_pages/page_ranges
-- заведены оба триггера updated_at, а не один).
CREATE TABLE composite_pages (
    id BIGSERIAL PRIMARY KEY,
    title VARCHAR(500) NOT NULL,
    slug VARCHAR(255) NOT NULL UNIQUE,
    description TEXT,
    owner_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_composite_pages_slug ON composite_pages(slug);
CREATE INDEX idx_composite_pages_owner ON composite_pages(owner_id);

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

CREATE INDEX idx_page_ranges_composite ON page_ranges(composite_page_id);
CREATE INDEX idx_page_ranges_work ON page_ranges(work_id);
CREATE INDEX idx_page_ranges_order ON page_ranges(composite_page_id, order_number);

CREATE TRIGGER update_composite_pages_updated_at BEFORE UPDATE ON composite_pages
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
CREATE TRIGGER update_page_ranges_updated_at BEFORE UPDATE ON page_ranges
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
