ALTER TABLE people.people DROP CONSTRAINT people_login_name_check;

ALTER TABLE people.people ADD CONSTRAINT people_login_name_check CHECK (login_name ~ '^[a-z0-9][a-z0-9_]{1,31}$');
