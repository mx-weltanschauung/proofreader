-- Общее правило для этого отката (относится ко всем случаям ниже,
-- помеченным по месту): откат возвращает СТРУКТУРУ на состояние до
-- миграции, но не ограничения, которым живые данные могли перестать
-- удовлетворять за время её действия. Наложить ограничение обратно вслепую
-- значит либо уронить откат на честных данных, либо (хуже) молча исказить
-- их, чтобы ограничение прошло. Ни то ни другое не дело автоматической
-- миграции — вернуть ограничение, когда оно снова верно, задача оператора,
-- знающего своих данных, а не down.sql. Из того же правила следует и
-- обратная сторона: откат теряет не только ограничения, но и саму
-- дропнутую колонку целиком — с данными в ней. Если эти данные различали
-- иначе неразличимые строки, повторный накат наткнётся на новое
-- ограничение и упадёт (пример — author_nickname ниже); развести такие
-- строки вручную — снова работа оператора, а не миграции.

DROP TABLE IF EXISTS auth_attempts;

ALTER TABLE collections DROP CONSTRAINT IF EXISTS collections_author_slug_key;
-- UNIQUE (slug) обратно не возвращаем: подборки разных авторов с одним
-- слагом — штатное состояние, ради которого миграция и затевалась;
-- восстановление ограничения падает ровно на нём (см. правило выше).
ALTER TABLE collections DROP COLUMN IF EXISTS publish_ip_hash;
-- author_nickname дропается вместе с данными в ней — это не упущение, а
-- следствие того же правила (см. абзац выше): откат теряет данные, а не
-- только ограничения. Если на момент отката было две и более подборки,
-- различавшиеся только автором и делившие один slug, они станут
-- неразличимы, и повторный up упрётся в UNIQUE (author_nickname, slug) —
-- разводить такие строки (переименовывать slug вручную) придётся
-- оператору, который видит исходные данные; миграция за него не гадает.
ALTER TABLE collections DROP COLUMN IF EXISTS author_nickname;
ALTER TABLE collections DROP COLUMN IF EXISTS published_at;

ALTER TABLE documents DROP CONSTRAINT IF EXISTS documents_owner_id_fkey;
ALTER TABLE documents
    ADD CONSTRAINT documents_owner_id_fkey
    FOREIGN KEY (owner_id) REFERENCES users(id) ON DELETE CASCADE;
-- NOT NULL обратно не возвращаем: после SET NULL в колонке могут лежать NULL,
-- и откат упал бы на живых данных (см. правило выше).

DROP INDEX IF EXISTS idx_page_suggestions_user;
ALTER TABLE page_suggestions DROP COLUMN IF EXISTS user_id;
-- reader_key обратно в NOT NULL не возвращаем по той же причине.

DROP INDEX IF EXISTS idx_users_nickname_key;
ALTER TABLE users DROP COLUMN IF EXISTS nickname_key;
ALTER TABLE users DROP COLUMN IF EXISTS signup_ip_hash;
ALTER TABLE users DROP COLUMN IF EXISTS nickname;

-- Читателей на откате удаляем, а не переводим в editor: повышение отдало бы
-- каждому читателю права правки полос, очередь предложений и раздел
-- пользователей — согласия на это читатель не давал, а нажатие
-- `make migrate-down` не должно быть источником прав редактора. Удалять не
-- жалко: у читателя нет ни почты, ни чего-либо ещё, что стоило бы пережить
-- откату, а его подборки и предложения уже переживают его сами —
-- collections.owner_id заведён ON DELETE SET NULL (000009), а колонка
-- page_suggestions.user_id к этой строке уже дропнута (см. выше). Прецедент
-- 000005 (там откат тоже понижал роль — proofreader) сюда не переносится:
-- там роль была пустой, живых обладателей не было; здесь читатели живые, и
-- у них есть подборки, которые повышение молча утащило бы в редакторские.
DELETE FROM users WHERE role = 'reader';
-- email обратно в NOT NULL не возвращаем: общее правило файла (см. шапку) —
-- откат не восстанавливает ограничения, которым данные могли перестать
-- удовлетворять, даже если ни одной нарушающей строки в этот раз не
-- осталось.

ALTER TABLE users ALTER COLUMN role DROP DEFAULT;
ALTER TYPE user_role RENAME TO user_role_old;
CREATE TYPE user_role AS ENUM ('administrator', 'editor');
ALTER TABLE users
    ALTER COLUMN role TYPE user_role
    USING role::text::user_role;
ALTER TABLE users ALTER COLUMN role SET DEFAULT 'editor';
DROP TYPE user_role_old;
