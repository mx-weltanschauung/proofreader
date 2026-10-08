ALTER TABLE users ALTER COLUMN role DROP DEFAULT;

ALTER TYPE user_role RENAME TO user_role_old;
CREATE TYPE user_role AS ENUM ('administrator', 'editor', 'proofreader');

ALTER TABLE users
    ALTER COLUMN role TYPE user_role
    USING role::text::user_role;

ALTER TABLE users ALTER COLUMN role SET DEFAULT 'proofreader';

DROP TYPE user_role_old;
