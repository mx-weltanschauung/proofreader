-- Откат возвращает ключам прежнее правило: NFC, края, схлопнутые пробелы,
-- регистр, ё→е. Обратно от ключа не пройти — снятые пробелы и вид тире не
-- восстановить, — поэтому ключ считается заново от заглавия. Выражение то же,
-- что засевало ключи миграциями 000025 и 000029, плюс NFC и неразрывный
-- пробел, которых у тех не было; с Go-правилом оно расходится разве что на
-- экзотических пробелах (Go режет всё, что unicode.IsSpace), и точность, как и
-- тогда, покупается повторным ввозом. lower() понижает кириллицу только при
-- ctype с Юникодом — та же проверка, что у подъёма.
--
-- Отсылки, связанные подъёмом, связанными и остаются: понятие они нашли
-- верное, а следующий ввоз издания всё равно пересоберёт его отсылки. Данные
-- откат не теряет — ключ производный.
DO $$
BEGIN
    IF lower('Ж') <> 'ж' THEN
        RAISE EXCEPTION 'ctype базы не понижает кириллицу — откат 000033 посчитал бы ключи не в том регистре';
    END IF;
END $$;

CREATE OR REPLACE FUNCTION pg_temp.index_title_key_v32(t TEXT) RETURNS TEXT AS $$
    SELECT replace(lower(btrim(regexp_replace(normalize(t, NFC), '[\s ]+', ' ', 'g'))), 'ё', 'е')
$$ LANGUAGE sql IMMUTABLE;

UPDATE index_concepts SET title_key = pg_temp.index_title_key_v32(title);
UPDATE index_concept_articles SET title_key = pg_temp.index_title_key_v32(title);
UPDATE index_rubrics SET title_key = pg_temp.index_title_key_v32(title);
UPDATE index_concept_links SET target_title_key = pg_temp.index_title_key_v32(target_title);

DROP FUNCTION pg_temp.index_title_key_v32(TEXT);
