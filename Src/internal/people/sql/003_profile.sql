ALTER TABLE people.people
    ADD COLUMN class_name text NOT NULL DEFAULT '',
    ADD COLUMN avatar_id bigint,
    ADD CONSTRAINT people_class_check CHECK (char_length(class_name) <= 40);
