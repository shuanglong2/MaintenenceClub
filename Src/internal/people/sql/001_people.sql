CREATE SCHEMA people;

CREATE TABLE people.people (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name text NOT NULL,
    login_name text NOT NULL,
    password_hash text NOT NULL,
    role text NOT NULL,
    finance boolean NOT NULL DEFAULT false,
    student_id text NOT NULL DEFAULT '',
    contact text NOT NULL DEFAULT '',
    joined_at timestamptz NOT NULL DEFAULT now(),
    status text NOT NULL,
    note text NOT NULL DEFAULT '',
    must_change_password boolean NOT NULL DEFAULT true,
    CONSTRAINT people_login_name_unique UNIQUE (login_name),
    CONSTRAINT people_role_check CHECK (role IN ('leader', 'deputy', 'member')),
    CONSTRAINT people_status_check CHECK (status IN ('active', 'exited')),
    CONSTRAINT people_name_check CHECK (char_length(name) BETWEEN 1 AND 40),
    CONSTRAINT people_login_name_check CHECK (login_name ~ '^[a-z][a-z0-9_]{1,31}$')
);

CREATE TABLE people.sessions (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    person_id bigint NOT NULL REFERENCES people.people (id),
    token_hash bytea NOT NULL,
    csrf_token text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    absolute_expires_at timestamptz NOT NULL,
    CONSTRAINT people_sessions_token_unique UNIQUE (token_hash)
);

CREATE INDEX people_sessions_person ON people.sessions (person_id);
