-- Откат снимает колонку. Код, откатываемый ВМЕСТЕ с ней, обязан вернуть в
-- шаг 7 ReplaceForEdition прежнее выражение: без этого после отката отсылки
-- перестанут связываться вовсе, а не вернутся к прежней неточности.
-- Данные при откате не теряются — ключ производный, он считается заново.
DROP INDEX IF EXISTS idx_index_concept_links_target_key;

ALTER TABLE index_concept_links
    DROP COLUMN target_title_key;
