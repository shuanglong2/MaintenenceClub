CREATE SCHEMA boards;

CREATE TABLE boards.items (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    board text NOT NULL,
    title text NOT NULL,
    summary text NOT NULL DEFAULT '',
    contact text NOT NULL DEFAULT '',
    kind text NOT NULL,
    status text NOT NULL DEFAULT 'closed',
    place text NOT NULL DEFAULT '',
    starts_at timestamptz,
    ends_at timestamptz,
    created_by bigint NOT NULL REFERENCES people.people (id),
    created_name text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT boards_board_check CHECK (board IN ('activity', 'competition')),
    CONSTRAINT boards_title_check CHECK (char_length(title) BETWEEN 1 AND 80),
    CONSTRAINT boards_summary_check CHECK (char_length(summary) <= 4000),
    CONSTRAINT boards_contact_check CHECK (char_length(contact) <= 80),
    CONSTRAINT boards_place_check CHECK (char_length(place) <= 80),
    CONSTRAINT boards_status_check CHECK (status IN ('closed', 'open', 'running', 'done')),
    CONSTRAINT boards_span_check CHECK (ends_at IS NULL OR starts_at IS NULL OR ends_at > starts_at)
);

CREATE INDEX boards_items_board_idx ON boards.items (board, status, created_at);

CREATE TABLE boards.members (
    item_id bigint NOT NULL REFERENCES boards.items (id),
    person_id bigint NOT NULL REFERENCES people.people (id),
    person_name text NOT NULL,
    joined_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (item_id, person_id)
);
