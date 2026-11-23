DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM reader_comments WHERE thread_id IS NOT NULL OR removed <> '') THEN
        RAISE EXCEPTION 'в базе есть ответы или снятые комментарии: откат 000055 раскрыл бы снятый текст и потерял связи ответов';
    END IF;
END $$;

DROP INDEX reader_comments_thread_idx;
DROP INDEX reader_comments_recipient_idx;
ALTER TABLE reader_comments
    DROP CONSTRAINT reader_comments_reply_fields,
    DROP COLUMN removed,
    DROP COLUMN reply_read_at,
    DROP COLUMN recipient_id,
    DROP COLUMN reply_to_nickname,
    DROP COLUMN thread_id;
