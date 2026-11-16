CREATE SCHEMA files;

CREATE TABLE files.files (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    module text NOT NULL,
    rel_path text NOT NULL,
    original_name text NOT NULL,
    size_bytes bigint NOT NULL,
    content_type text NOT NULL,
    sha256 bytea NOT NULL,
    uploaded_by bigint NOT NULL REFERENCES people.people (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT files_module_check CHECK (module ~ '^[a-z][a-z0-9_]{0,31}$'),
    CONSTRAINT files_path_unique UNIQUE (rel_path),
    CONSTRAINT files_size_check CHECK (size_bytes >= 0)
);

CREATE INDEX files_uploaded_by ON files.files (uploaded_by);
