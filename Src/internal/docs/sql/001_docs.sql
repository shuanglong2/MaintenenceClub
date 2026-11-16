CREATE SCHEMA docs;

CREATE TABLE docs.folders (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    parent_id bigint REFERENCES docs.folders (id),
    name text NOT NULL,
    sort_order integer NOT NULL DEFAULT 0,
    CONSTRAINT docs_folder_name_check CHECK (char_length(name) BETWEEN 1 AND 40)
);

CREATE TABLE docs.pages (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    folder_id bigint REFERENCES docs.folders (id),
    title text NOT NULL,
    body text NOT NULL,
    search_tokens text NOT NULL DEFAULT '',
    tsv tsvector GENERATED ALWAYS AS (to_tsvector('simple', search_tokens)) STORED,
    visibility text NOT NULL DEFAULT 'all',
    version integer NOT NULL DEFAULT 1,
    created_by bigint REFERENCES people.people (id),
    updated_by bigint REFERENCES people.people (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    voided_at timestamptz,
    void_reason text NOT NULL DEFAULT '',
    voided_by bigint REFERENCES people.people (id),
    CONSTRAINT docs_page_title_check CHECK (char_length(title) BETWEEN 1 AND 80),
    CONSTRAINT docs_page_visibility_check CHECK (visibility IN ('all', 'management')),
    CONSTRAINT docs_page_version_check CHECK (version >= 1)
);

CREATE INDEX docs_pages_tsv ON docs.pages USING gin (tsv);
CREATE INDEX docs_pages_folder ON docs.pages (folder_id);

CREATE TABLE docs.page_versions (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    page_id bigint NOT NULL REFERENCES docs.pages (id),
    version integer NOT NULL,
    title text NOT NULL,
    body text NOT NULL,
    editor_id bigint REFERENCES people.people (id),
    editor_name text NOT NULL DEFAULT '',
    saved_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT docs_page_versions_unique UNIQUE (page_id, version)
);

CREATE TABLE docs.tags (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name text NOT NULL,
    CONSTRAINT docs_tag_name_unique UNIQUE (name),
    CONSTRAINT docs_tag_name_check CHECK (char_length(name) BETWEEN 1 AND 20)
);

CREATE TABLE docs.page_tags (
    page_id bigint NOT NULL REFERENCES docs.pages (id),
    tag_id bigint NOT NULL REFERENCES docs.tags (id),
    PRIMARY KEY (page_id, tag_id)
);

CREATE TABLE docs.page_files (
    page_id bigint NOT NULL REFERENCES docs.pages (id),
    file_id bigint NOT NULL REFERENCES files.files (id),
    PRIMARY KEY (page_id, file_id)
);

CREATE FUNCTION docs.reject_delete() RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION '文档不能物理删除';
END;
$$;

CREATE TRIGGER docs_pages_no_delete
BEFORE DELETE ON docs.pages
FOR EACH ROW EXECUTE FUNCTION docs.reject_delete();

CREATE TRIGGER docs_versions_no_delete
BEFORE DELETE ON docs.page_versions
FOR EACH ROW EXECUTE FUNCTION docs.reject_delete();

CREATE TRIGGER docs_versions_no_update
BEFORE UPDATE ON docs.page_versions
FOR EACH ROW EXECUTE FUNCTION docs.reject_delete();
