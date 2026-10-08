-- Сколько томов в собрании по плану издания. Полка на главной пишет «45 из 55»,
-- и второе число брать больше неоткуда: у собраний Ленина и Маркса—Энгельса
-- плановый объём не записан нигде, а у остальных стоит внутри заголовка
-- словами («Сочинения в 24 томах»), откуда его пришлось бы угадывать разбором
-- строки — и угадывать неверно на первом же собрании, названном иначе.
ALTER TABLE editions ADD COLUMN volumes_planned integer;

COMMENT ON COLUMN editions.volumes_planned IS
    'Число томов в собрании по плану издания. Пусто — план неизвестен, полка называет только залитые тома.';

-- Ноль и отрицательное — не «неизвестно», а мусор: неизвестное выражается
-- пустотой, и подпись полки различает эти случаи.
ALTER TABLE editions ADD CONSTRAINT editions_volumes_planned_positive
    CHECK (volumes_planned IS NULL OR volumes_planned > 0);

-- Пять собраний корпуса. Плановый объём — с титульных листов изданий; у Ленина
-- он в самом названии пятого издания (55 томов), у Маркса—Энгельса — объём
-- второго издания (50 томов, включая 25-й и 26-й в двух книгах каждый).
UPDATE editions SET volumes_planned = 55 WHERE title LIKE 'В. И. Ленин.%';
UPDATE editions SET volumes_planned = 50 WHERE title LIKE 'К. Маркс и Ф. Энгельс.%';
UPDATE editions SET volumes_planned = 24 WHERE title LIKE 'Г. В. Плеханов.%';
UPDATE editions SET volumes_planned = 6 WHERE title LIKE 'Л. С. Выготский.%';
UPDATE editions SET volumes_planned = 15 WHERE title LIKE 'Н. Г. Чернышевский.%';
