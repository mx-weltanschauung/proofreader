-- Migrate any proofreader users to editor, then rebuild the enum without 'proofreader'.
ALTER TABLE users ALTER COLUMN role DROP DEFAULT;

UPDATE users SET role = 'editor' WHERE role = 'proofreader';

ALTER TYPE user_role RENAME TO user_role_old;
CREATE TYPE user_role AS ENUM ('administrator', 'editor');

ALTER TABLE users
    ALTER COLUMN role TYPE user_role
    USING role::text::user_role;

ALTER TABLE users ALTER COLUMN role SET DEFAULT 'editor';

DROP TYPE user_role_old;
