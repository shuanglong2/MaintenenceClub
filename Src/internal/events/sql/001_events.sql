CREATE SCHEMA events;

CREATE TABLE events.events (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    title text NOT NULL,
    kind text NOT NULL,
    starts_at timestamptz NOT NULL,
    ends_at timestamptz NOT NULL,
    all_day boolean NOT NULL DEFAULT false,
    place text NOT NULL DEFAULT '',
    body text NOT NULL DEFAULT '',
    summary text NOT NULL DEFAULT '',
    owner_id bigint NOT NULL REFERENCES people.people (id),
    owner_name text NOT NULL,
    status text NOT NULL DEFAULT 'planned',
    signup_until timestamptz,
    created_by bigint REFERENCES people.people (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT events_title_check CHECK (char_length(title) BETWEEN 1 AND 80),
    CONSTRAINT events_kind_check CHECK (kind IN ('科普', '竞赛', '例会', '保养日', '其他')),
    CONSTRAINT events_place_check CHECK (char_length(place) <= 80),
    CONSTRAINT events_body_check CHECK (char_length(body) <= 8000),
    CONSTRAINT events_summary_check CHECK (char_length(summary) <= 20000),
    CONSTRAINT events_status_check CHECK (status IN ('planned', 'done', 'cancelled')),
    CONSTRAINT events_span_check CHECK (ends_at > starts_at)
);

CREATE INDEX events_span_idx ON events.events (starts_at, ends_at);

CREATE TABLE events.attendees (
    event_id bigint NOT NULL REFERENCES events.events (id),
    person_id bigint NOT NULL REFERENCES people.people (id),
    person_name text NOT NULL,
    signed_up_at timestamptz NOT NULL DEFAULT now(),
    checked_in boolean NOT NULL DEFAULT false,
    checked_by bigint REFERENCES people.people (id),
    checked_name text NOT NULL DEFAULT '',
    checked_at timestamptz,
    PRIMARY KEY (event_id, person_id)
);

CREATE INDEX events_attendee_person_idx ON events.attendees (person_id);

CREATE TABLE events.event_files (
    event_id bigint NOT NULL REFERENCES events.events (id),
    file_id bigint NOT NULL REFERENCES files.files (id),
    PRIMARY KEY (event_id, file_id)
);
