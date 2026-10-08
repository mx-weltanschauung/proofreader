-- Полнотекстовый поиск по читальне: конфиг, вычисляемый столбец, индексы.
-- Спека: docs/superpowers/specs/2026-09-03-full-text-search-design.md

-- Trusted с PG13: суперпользователь не нужен, хватает владельца базы.
CREATE EXTENSION IF NOT EXISTS unaccent;

-- Тот же стеммер snowball, что у штатного russian, но без списка стоп-слов:
-- иначе «Что делать?» превращается в одинокое 'дела', а «Кто виноват?» — в
-- 'виноват', и ни главу, ни фразу в тексте найти нельзя.
CREATE TEXT SEARCH DICTIONARY ru_stem_nostop (TEMPLATE = snowball, Language = russian);
CREATE TEXT SEARCH CONFIGURATION ru (COPY = russian);
-- unaccent впереди стеммера сводит ё→е (и é→e в иноязычных цитатах) ещё до
-- лемматизации, одинаково в тексте и в запросе. й, ъ, ь, щ он не трогает.
ALTER TEXT SEARCH CONFIGURATION ru
  ALTER MAPPING FOR word, asciiword, hword, hword_part, hword_asciipart, asciihword
  WITH unaccent, ru_stem_nostop;

-- Пересчитывает сам Postgres при любой записи, откуда бы она ни пришла:
-- правка, возврат версии, принятие предложения, заливка тома, restore.sh.
-- pg_dump значения GENERATED … STORED не пишет — снапшот не растёт.
ALTER TABLE pages ADD COLUMN search_vector tsvector
  GENERATED ALWAYS AS (to_tsvector('ru', coalesce(content_markdown, ''))) STORED;
CREATE INDEX idx_pages_search ON pages USING gin (search_vector);

CREATE INDEX idx_chapters_title_search
  ON chapters USING gin (to_tsvector('ru', title));
CREATE INDEX idx_index_concepts_title_search
  ON index_concepts USING gin (to_tsvector('ru', title));

-- GIN по английскому конфигу из начальной схемы: 70 МБ, никем не читается,
-- английский стеммер кириллицу не трогает.
DROP INDEX idx_pages_content;
