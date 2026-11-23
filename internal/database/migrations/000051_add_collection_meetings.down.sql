-- Сначала очистите программы через редактор, чтобы откат не удалил
-- вопросы и названия занятий. Плоский порядок чтения сохранится.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM collection_meetings) THEN
        RAISE EXCEPTION 'шаг вниз 000051 удалит программы встреч: сначала очистите программы';
    END IF;
END $$;

ALTER TABLE collection_items DROP COLUMN meeting_position;
DROP TABLE collection_meetings;
ALTER TABLE collections DROP COLUMN program_revision;
