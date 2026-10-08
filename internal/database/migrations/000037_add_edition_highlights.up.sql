-- Избранные работы собрания на главной: ссылка на главу и короткая подпись.
-- Каскад по главе: снятие тома и удаление главы уносят пункт сами, битых
-- ссылок на главной не бывает. Пустая подпись — клиент берёт первое
-- предложение заглавия главы (chapterLabel).
CREATE TABLE edition_highlights (
    edition_id bigint NOT NULL REFERENCES editions(id) ON DELETE CASCADE,
    chapter_id bigint NOT NULL REFERENCES chapters(id) ON DELETE CASCADE,
    position   int    NOT NULL,
    label      text   NOT NULL DEFAULT '',
    PRIMARY KEY (edition_id, chapter_id)
);
