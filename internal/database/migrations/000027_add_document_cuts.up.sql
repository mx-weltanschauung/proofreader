-- Вклейка: кусок корпуса внутри разбора. Форма якоря — дословно от вырезки
-- предметного указателя (index_fragments), потому что переякоривает обе одна
-- и та же функция. Диапазон полос целиком — частный случай: смещение начала
-- 0, смещение конца — длина последней полосы.
--
-- Смещения БАЙТОВЫЕ, по content_markdown.
--
-- ВНИМАНИЕ ПРИ СЛИЯНИИ: номер 000027 занят и в соседней ветке
-- worktree-concept-catalog-schema (000025_split_concept_articles, 26 там
-- ещё не заведён на момент записи этой заметки) — golang-migrate применяет
-- только версии выше текущей, поэтому база, доехавшая до 27 с этой ветки,
-- молча пропустит их 25/26 без единой ошибки. Сливать эту ветку нужно либо
-- после соседней, либо перенумеровав один из наборов миграций — решение за
-- владельцем слияния, эта заметка только фиксирует конфликт номеров.
CREATE TABLE document_cuts (
    id BIGSERIAL PRIMARY KEY,
    document_id BIGINT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,

    -- Работа нужна ссылке «читать в томе» и печатной колонцифре
    -- (works.page_offset). SET NULL, а не CASCADE.
    work_id BIGINT REFERENCES works(id) ON DELETE SET NULL,

    -- SET NULL по решению спеки. У вырезки здесь CASCADE, и там он верен:
    -- вырезка без полосы бессмысленна. Вклейка без полосы — битая вклейка,
    -- которую читатель обязан увидеть: иначе разбор молча укорачивается и
    -- оставляет рассуждение автора без опоры.
    start_page_id BIGINT REFERENCES pages(id) ON DELETE SET NULL,
    start_offset INT NOT NULL,
    end_page_id BIGINT REFERENCES pages(id) ON DELETE SET NULL,
    end_offset INT NOT NULL,

    -- Единственное, чем вклейка держится за живой текст: полосы правит
    -- машинная вычитка каждый день, и смещения от правки уезжают.
    head_quote TEXT NOT NULL,
    tail_quote TEXT NOT NULL,
    start_hash CHAR(64) NOT NULL,
    end_hash CHAR(64) NOT NULL,

    -- Свой словарь, а не словарь вырезки: machine|confirmed там означают
    -- «нарезала машина» и «подтвердил человек», а границы вклейки человек
    -- ставит с самого начала — подтверждать нечего.
    status VARCHAR(16) NOT NULL DEFAULT 'ok'
        CHECK (status IN ('ok', 'stale')),

    -- Снимок подписи на случай исчезновения источника. Пишется при создании
    -- и НЕ обновляется: живая вклейка печатает подпись из живых данных, а
    -- хранимая копия разошлась бы с ними молча.
    source_title TEXT NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_document_cuts_document ON document_cuts(document_id);
-- Этими двумя ходит переякоривание при каждой записи в полосу.
CREATE INDEX idx_document_cuts_start_page ON document_cuts(start_page_id);
CREATE INDEX idx_document_cuts_end_page ON document_cuts(end_page_id);

COMMENT ON TABLE document_cuts IS
    'Вклейка: кусок корпуса внутри разбора, переживающий правку полосы';
COMMENT ON COLUMN document_cuts.source_title IS
    'Снимок подписи источника; показывается, когда полос уже нет';
