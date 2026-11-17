CREATE TABLE docs.notices (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    body text NOT NULL,
    author_id bigint NOT NULL,
    author_name text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    voided_at timestamptz,
    CONSTRAINT docs_notices_body_check CHECK (char_length(body) BETWEEN 1 AND 400)
);

CREATE INDEX docs_notices_open_idx ON docs.notices (created_at DESC, id DESC) WHERE voided_at IS NULL;
