UPDATE people.people SET student_id = login_name WHERE btrim(student_id) = '';

ALTER TABLE people.people DROP CONSTRAINT people_role_check;
ALTER TABLE people.people ADD CONSTRAINT people_role_check CHECK (role IN ('leader', 'deputy', 'member', 'guest', 'blacklist'));
ALTER TABLE people.people ADD CONSTRAINT people_student_id_unique UNIQUE (student_id);

ALTER TABLE people.people
    ADD COLUMN qq text NOT NULL DEFAULT '',
    ADD COLUMN phone text NOT NULL DEFAULT '',
    ADD COLUMN bio text NOT NULL DEFAULT '',
    ADD COLUMN birthday date,
    ADD COLUMN website text NOT NULL DEFAULT '',
    ADD COLUMN department text NOT NULL DEFAULT '',
    ADD CONSTRAINT people_qq_check CHECK (char_length(qq) <= 20),
    ADD CONSTRAINT people_phone_check CHECK (char_length(phone) <= 20),
    ADD CONSTRAINT people_bio_check CHECK (char_length(bio) <= 500),
    ADD CONSTRAINT people_website_check CHECK (char_length(website) <= 200),
    ADD CONSTRAINT people_department_check CHECK (department IN ('', '维修部', '竞赛部', '科普部', '创新部', '管理部'));

CREATE SCHEMA app;

CREATE TABLE app.settings (
    key text PRIMARY KEY,
    value text NOT NULL
);

INSERT INTO app.settings (key, value) VALUES
    ('docs_quota', '10737418240'),
    ('shared_quota', '10737418240');
