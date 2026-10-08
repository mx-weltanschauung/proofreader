-- Обратный порядок: столбец и индексы по выражению зависят от конфига ru.
DROP INDEX IF EXISTS idx_index_concepts_title_search;
DROP INDEX IF EXISTS idx_chapters_title_search;
DROP INDEX IF EXISTS idx_pages_search;
ALTER TABLE pages DROP COLUMN IF EXISTS search_vector;
DROP TEXT SEARCH CONFIGURATION IF EXISTS ru;
DROP TEXT SEARCH DICTIONARY IF EXISTS ru_stem_nostop;
-- Расширение unaccent остаётся: им мог воспользоваться кто-то ещё.
CREATE INDEX idx_pages_content ON pages USING gin (to_tsvector('english', content_markdown));
