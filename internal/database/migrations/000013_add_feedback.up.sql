-- Обращения читателей — письма в читальню. Единственная таблица, куда
-- пишет неавторизованный посетитель, поэтому пределы длин стоят прямо в
-- схеме: проверка в обработчике — первая линия обороны, а не единственная.
CREATE TABLE feedback (
    id BIGSERIAL PRIMARY KEY,
    -- char_length считает символы. length() в Postgres — тоже символы, а вот
    -- len() в Go считает байты и на кириллице даёт вдвое больше: пределы
    -- схемы и обработчика обязаны мериться одной линейкой.
    message TEXT NOT NULL
        CHECK (char_length(message) BETWEEN 1 AND 4000),
    -- Пустая строка, а не NULL: «контакта не оставили» должно записываться
    -- одним способом, иначе ветвление заводится в каждой проверке. Тот же
    -- выбор уже сделан для works.shelf_label, role и numbering_style.
    contact TEXT NOT NULL DEFAULT ''
        CHECK (char_length(contact) <= 200),
    -- Путь внутри читальни, откуда пришёл читатель. Значение приходит от
    -- отправителя и в админке становится ссылкой, по которой кликает
    -- администратор, — проверяется normalizeSourcePath в обработчике.
    source_path TEXT NOT NULL DEFAULT ''
        CHECK (char_length(source_path) <= 500),
    -- sha256(адрес + JWT_SECRET), первые 16 байт в hex. Сам адрес нигде не
    -- хранится: ни здесь, ни в журнале.
    ip_hash TEXT NOT NULL DEFAULT '',
    -- NULL — письмо не разобрано. Отметка времени, а не булев флаг:
    -- состояний по-прежнему два, но «когда разобрали» достаётся даром.
    handled_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Список в админке: новые сверху.
CREATE INDEX idx_feedback_created_at ON feedback(created_at DESC);
-- Предел частоты: сколько писем с этой отметки адреса за последний час.
CREATE INDEX idx_feedback_ip_hash_created ON feedback(ip_hash, created_at DESC);

COMMENT ON TABLE feedback IS 'Обращения читателей — письма в читальню';
COMMENT ON COLUMN feedback.ip_hash IS
    'sha256(адрес + JWT_SECRET), 16 байт; смена JWT_SECRET обнуляет счётчики частоты';
COMMENT ON COLUMN feedback.handled_at IS 'NULL — не разобрано';
