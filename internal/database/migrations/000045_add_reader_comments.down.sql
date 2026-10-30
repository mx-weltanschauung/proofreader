-- golang-migrate после отказа помечает версию dirty: проверив сохранность
-- схемы, force 45 возвращает номер без повторного применения up.
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM reader_comments) THEN
  RAISE EXCEPTION 'в базе есть комментарии читателей: шаг вниз 000045 снёс бы их';
 END IF;
END $$;
DROP TABLE reader_comment_submissions;
DROP TABLE reader_comment_revisions;
DROP TABLE reader_comments;
ALTER TABLE users DROP COLUMN comments_banned;
ALTER TABLE users DROP COLUMN comments_rejections;
