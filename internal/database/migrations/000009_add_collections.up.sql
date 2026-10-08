-- Подборка: главы и работы разных авторов, собранные составителем.
-- Публичен на чтение, как и всё остальное в читальне.
CREATE TABLE collections (
    id BIGSERIAL PRIMARY KEY,
    title VARCHAR(500) NOT NULL,
    slug VARCHAR(255) NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    -- SET NULL, а не CASCADE: подборка публична, и удаление аккаунта
    -- составителя не должно уносить читаемый материал.
    owner_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TRIGGER update_collections_updated_at BEFORE UPDATE ON collections
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- Элемент состава. Ссылка, а не копия текста: правка страницы доезжает до
-- подборки сама.
--
-- chapter_id — SET NULL, потому что главы в этом проекте не вечны: toc-chapters
-- пересоздаёт их при повторном прогоне тома. CASCADE тихо выел бы состав
-- опубликованной подборки. Снимок заголовка остаётся, чтобы битую строку
-- было чем подписать.
--
-- work_id заполнен у обоих видов элемента, в том числе у элемента-главы:
-- иначе обнуление chapter_id унесло бы и связь с томом.
CREATE TABLE collection_items (
    id BIGSERIAL PRIMARY KEY,
    collection_id BIGINT NOT NULL REFERENCES collections(id) ON DELETE CASCADE,
    kind VARCHAR(16) NOT NULL,
    chapter_id BIGINT REFERENCES chapters(id) ON DELETE SET NULL,
    work_id    BIGINT REFERENCES works(id)    ON DELETE SET NULL,
    snapshot_title  VARCHAR(500) NOT NULL,
    snapshot_author VARCHAR(500) NOT NULL,
    -- Своего автора у главы нет: он на уровне работы. Без переопределения
    -- глава Энгельса внутри тома МиЭ подписывается авторством тома целиком.
    author_override VARCHAR(500) NOT NULL DEFAULT '',
    order_number INT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    CONSTRAINT collection_items_kind_check
        CHECK (kind IN ('chapter','work')),
    CONSTRAINT collection_items_target_check
        CHECK (kind <> 'work' OR chapter_id IS NULL)
);

CREATE TRIGGER update_collection_items_updated_at BEFORE UPDATE ON collection_items
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE INDEX idx_collection_items_collection ON collection_items(collection_id, order_number);
CREATE INDEX idx_collection_items_chapter ON collection_items(chapter_id);
CREATE INDEX idx_collection_items_work ON collection_items(work_id);
