-- Адресный слаг собрания: короткий кусок для человекочитаемых адресов
-- (/works/49-lenin-t06, /editions/4-lenin).
--
-- Отдельной колонкой, а не переименованием editions.slug: тот слаг —
-- ключ журнала публикации (tools/ocr_ingest/published/lenin-pss-5-10.json,
-- десятки файлов в git). Переименование увело бы журнал в дрейф, а дрейф
-- журнала роняет прогон тома целиком, включая честные полосы.
ALTER TABLE editions ADD COLUMN url_slug text NOT NULL DEFAULT '';

COMMENT ON COLUMN editions.url_slug IS
    'Короткий слаг для адресов читальни (lenin, mae). Пусто — адрес откатывается на slug.';

-- Пустое значение законно (собрание завели, слаг не задали), поэтому индекс
-- частичный: иначе второе незаполненное собрание упало бы на уникальности.
CREATE UNIQUE INDEX editions_url_slug_key ON editions (url_slug) WHERE url_slug <> '';

-- Пять собраний корпуса. Без номера издания и без «pss»: аббревиатура
-- «полного собрания сочинений» в адресе читателю ничего не сообщает.
UPDATE editions SET url_slug = 'lenin'         WHERE title LIKE 'В. И. Ленин.%';
UPDATE editions SET url_slug = 'mae'           WHERE title LIKE 'К. Маркс и Ф. Энгельс.%';
UPDATE editions SET url_slug = 'plekhanov'     WHERE title LIKE 'Г. В. Плеханов.%';
UPDATE editions SET url_slug = 'vygotsky'      WHERE title LIKE 'Л. С. Выготский.%';
UPDATE editions SET url_slug = 'chernyshevsky' WHERE title LIKE 'Н. Г. Чернышевский.%';
