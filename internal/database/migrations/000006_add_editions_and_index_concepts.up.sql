-- Собрание сочинений: структурный родитель работ-томов. Не категория:
-- внутри собрания том 23 ровно один, и на этой однозначности держится
-- резолв ссылок предметного указателя.
CREATE TABLE editions (
    id BIGSERIAL PRIMARY KEY,
    title VARCHAR(500) NOT NULL,
    slug VARCHAR(255) NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TRIGGER update_editions_updated_at BEFORE UPDATE ON editions FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- Координаты тома у работы. page_offset — конвенция strip_headers.py:
-- печатная = page_number + page_offset.
ALTER TABLE works
    ADD COLUMN edition_id BIGINT REFERENCES editions(id) ON DELETE SET NULL,
    ADD COLUMN volume_number INT,
    ADD COLUMN volume_part VARCHAR(8),
    ADD COLUMN page_offset INT NOT NULL DEFAULT 0;

CREATE INDEX idx_works_edition ON works(edition_id);

-- Полутома 25 (I—II) и 26 (I—III) — физически отдельные книги, то есть
-- отдельные работы, поэтому в ключ входит и часть. COALESCE, потому что
-- NULL в уникальном индексе не сравниваются между собой.
CREATE UNIQUE INDEX idx_works_edition_volume
    ON works (edition_id, volume_number, COALESCE(volume_part, ''))
    WHERE volume_number IS NOT NULL;

-- Статья указателя. slug уникален глобально: чтение идёт по GET /api/concepts/{slug}.
CREATE TABLE index_concepts (
    id BIGSERIAL PRIMARY KEY,
    work_id BIGINT NOT NULL REFERENCES works(id) ON DELETE CASCADE,
    title VARCHAR(500) NOT NULL,
    slug VARCHAR(255) NOT NULL UNIQUE,
    sort_key VARCHAR(500) NOT NULL,
    article_markdown TEXT NOT NULL DEFAULT '',
    source_page_start INT NOT NULL DEFAULT 0,
    source_page_end INT NOT NULL DEFAULT 0,
    kind VARCHAR(16) NOT NULL DEFAULT 'article',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TRIGGER update_index_concepts_updated_at BEFORE UPDATE ON index_concepts FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE INDEX idx_index_concepts_work ON index_concepts(work_id);
CREATE INDEX idx_index_concepts_sort ON index_concepts(sort_key);

-- Адрес в томе. page_start/page_end — ПЕЧАТНЫЕ номера; у одиночной ссылки равны.
CREATE TABLE index_references (
    id BIGSERIAL PRIMARY KEY,
    concept_id BIGINT NOT NULL REFERENCES index_concepts(id) ON DELETE CASCADE,
    volume_number INT NOT NULL,
    volume_part VARCHAR(8),
    page_start INT NOT NULL,
    page_end INT NOT NULL,
    rubric TEXT NOT NULL DEFAULT '',
    order_number INT NOT NULL,
    is_uncertain BOOLEAN NOT NULL DEFAULT FALSE,
    note TEXT NOT NULL DEFAULT ''
);

CREATE INDEX idx_index_references_concept ON index_references(concept_id, order_number);
-- Этим индексом ходят обратные ссылки со страницы тома.
CREATE INDEX idx_index_references_volume
    ON index_references(volume_number, volume_part, page_start, page_end);

-- Отсылки «см.» / «см. также». to_concept_id NULL, пока цель не разобрана.
CREATE TABLE index_concept_links (
    id BIGSERIAL PRIMARY KEY,
    from_concept_id BIGINT NOT NULL REFERENCES index_concepts(id) ON DELETE CASCADE,
    to_concept_id BIGINT REFERENCES index_concepts(id) ON DELETE SET NULL,
    target_title VARCHAR(500) NOT NULL,
    kind VARCHAR(16) NOT NULL,
    order_number INT NOT NULL
);

CREATE INDEX idx_index_concept_links_from ON index_concept_links(from_concept_id, order_number);
CREATE INDEX idx_index_concept_links_to ON index_concept_links(to_concept_id);
