-- Вырезка: кусок страницы, на котором понятие действительно раскрывается.
-- Указатель адресует страницу целиком, но раскрытие — обычно абзац-другой.
CREATE TABLE index_fragments (
    id BIGSERIAL PRIMARY KEY,
    reference_id BIGINT NOT NULL REFERENCES index_references(id) ON DELETE CASCADE,
    order_number INT NOT NULL,
    -- Границы — БАЙТОВЫЕ смещения в content_markdown. Страницы совпадают в
    -- обычном случае и различаются, когда мысль переходит с 730-й на 731-ю.
    start_page_id BIGINT NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
    start_offset INT NOT NULL,
    end_page_id BIGINT NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
    end_offset INT NOT NULL,
    -- Единственное, чем вырезка держится за живой текст: страницы правят и
    -- после нарезки, а смещения от правки уезжают.
    head_quote TEXT NOT NULL,
    tail_quote TEXT NOT NULL,
    start_hash CHAR(64) NOT NULL,
    end_hash CHAR(64) NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'machine'
        CHECK (status IN ('machine', 'confirmed', 'stale'))
);

CREATE INDEX idx_index_fragments_reference ON index_fragments(reference_id, order_number);
-- Этими двумя ходит переякоривание при каждой записи в страницу.
CREATE INDEX idx_index_fragments_start_page ON index_fragments(start_page_id);
CREATE INDEX idx_index_fragments_end_page ON index_fragments(end_page_id);
