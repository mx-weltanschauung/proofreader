-- Откат умеет вернуть только то, что представимо в прежней форме: разбор,
-- одобренный и стоящий на людях. Всё прочее — черновик, заявка на
-- рассмотрении, отклонённое, снятое с публикации — в прежней схеме не
-- выражается никак, и молчаливая потеря тут хуже отказа: после отката такая
-- строка неотличима от старого сотруднического разбора, а следующий накат
-- засевом без WHERE проставит ей «одобрено» и «на людях». То есть цикл
-- вниз-вверх опубликовал бы непроверенный читательский текст — ровно то,
-- против чего заведена вся эта работа.
--
-- Отказ ниже оставляет golang-migrate в состоянии «version 27, dirty», хотя
-- схема ЦЕЛА и стоит на 28: проверка идёт до всякой записи, колонки, индексы
-- и данные остаются на месте. Дальнейшие up/down не работают, пока не пройдёт
-- `migrate force 28` — именно 28, а не 27, которую называет сообщение самого
-- golang-migrate. Это сказано и в тексте самого отказа: комментарий здесь
-- читают заранее или вовсе не читают, а исключение — ровно в ту минуту, когда
-- оператор смотрит на два сообщения подряд и второе называет неверное число.
DO $$
DECLARE unsafe bigint;
BEGIN
    SELECT count(*) INTO unsafe FROM documents
    WHERE published_at IS NULL OR review_status <> 'одобрено';
    IF unsafe > 0 THEN
        RAISE EXCEPTION 'Откат 000028 невозможен: % разбор(ов) не одобрены или сняты с публикации, и прежняя схема их состояния не хранит. Разберитесь с ними вручную (одобрить, снять или удалить), затем повторите откат. Схема при этом НЕ тронута и осталась на версии 28: колонки, индексы и данные на месте, чинить в ней нечего. Поправить нужно только отметку версии — golang-migrate сейчас пометит базу грязной и назовёт версию 27, но форсить надо 28: «migrate force 28». После «force 27» накат пойдёт заводить уже существующие колонки и упадёт на 42701.', unsafe;
    END IF;
END $$;

-- Одобренная редакция при откате НЕ теряется — она переносится обратно в
-- title/markdown_content, иначе откат подменил бы показываемый текст
-- неодобренным черновиком. История модерации теряется: возвращаемая форма её
-- просто не знает, но после проверки выше терять больше нечего.
UPDATE documents
SET title = published_title,
    markdown_content = published_markdown
WHERE published_at IS NOT NULL;

-- Снимаются только свои индексы: idx_documents_owner принадлежит 000002 и
-- переживает этот откат.
DROP INDEX IF EXISTS idx_documents_review;
DROP INDEX IF EXISTS idx_documents_published;

-- Явно, хотя DROP COLUMN ниже унёс бы это ограничение и сам: в откате видно,
-- что снимается, без знания того, какие связи Postgres рвёт молча.
ALTER TABLE documents DROP CONSTRAINT IF EXISTS documents_reject_reason_matches_status;

-- Колонка markdown_content откат переживает, а её комментарий ссылается на
-- published_markdown, которой после отката не будет: незанятый комментарий
-- остался бы в pg_description и врал бы всякому, кто смотрит \d+ documents.
-- Комментарий к author_nickname снимать не надо — он уходит вместе с самой
-- колонкой ниже (проверено прогоном).
COMMENT ON COLUMN documents.markdown_content IS NULL;

ALTER TABLE documents
    DROP COLUMN submit_ip_hash,
    DROP COLUMN submitted_at,
    DROP COLUMN reviewed_at,
    DROP COLUMN moderator_id,
    DROP COLUMN reject_reason,
    DROP COLUMN review_status,
    DROP COLUMN was_published,
    DROP COLUMN published_at,
    DROP COLUMN published_markdown,
    DROP COLUMN published_title,
    DROP COLUMN author_nickname;
