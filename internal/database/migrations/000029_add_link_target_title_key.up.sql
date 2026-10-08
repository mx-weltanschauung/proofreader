-- Ключ заголовка-цели отсылки, посчитанный в Go (models.NormalizeIndexTitle)
-- при ввозе.
--
-- До этой миграции резолв отсылок (шаг 7 ReplaceForEdition) считал ключ
-- выражением SQL, у которого нет ни NFC, ни резки по неразрывному пробелу, —
-- а у ключа понятия в Go есть и то и другое. Отсылка с типографским
-- неразрывным пробелом в заголовке не связывалась с понятием МОЛЧА.
-- В корпусе Маркса отсылок две, в ленинском указателе — 1133.
ALTER TABLE index_concept_links
    ADD COLUMN target_title_key TEXT NOT NULL DEFAULT '';

-- Засев — прежним выражением. Оно неточно ровно в тех случаях, ради которых
-- колонка и заводится, но живых строк здесь две, и обе без неразрывных
-- пробелов. Точность засева покупается повторным ввозом, а не усложнением
-- SQL, который эта же миграция и убирает.
UPDATE index_concept_links
SET target_title_key =
    replace(lower(btrim(regexp_replace(target_title, '\s+', ' ', 'g'))), 'ё', 'е');

CREATE INDEX idx_index_concept_links_target_key
    ON index_concept_links(target_title_key);
