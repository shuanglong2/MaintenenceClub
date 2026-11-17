CREATE SCHEMA logs;

CREATE TABLE logs.entries (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    kind text NOT NULL,
    happened_on date NOT NULL,
    author_id bigint NOT NULL REFERENCES people.people (id),
    author_name text NOT NULL,
    body text NOT NULL DEFAULT '',
    minutes integer,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    voided_at timestamptz,
    void_reason text NOT NULL DEFAULT '',
    CONSTRAINT logs_kind_check CHECK (kind IN ('work', 'repair', 'maintain')),
    CONSTRAINT logs_body_check CHECK (char_length(body) <= 8000),
    CONSTRAINT logs_minutes_check CHECK (minutes IS NULL OR minutes BETWEEN 0 AND 1440),
    CONSTRAINT logs_void_reason_check CHECK (char_length(void_reason) <= 200)
);

CREATE INDEX logs_entries_day_idx ON logs.entries (happened_on DESC, id DESC);
CREATE INDEX logs_entries_author_idx ON logs.entries (author_id);
CREATE INDEX logs_entries_kind_idx ON logs.entries (kind, happened_on DESC);

CREATE TABLE logs.repairs (
    entry_id bigint PRIMARY KEY REFERENCES logs.entries (id),
    target text NOT NULL,
    item_id bigint REFERENCES inventory.items (id),
    symptom text NOT NULL DEFAULT '',
    action text NOT NULL DEFAULT '',
    result text NOT NULL DEFAULT '',
    status text NOT NULL DEFAULT 'accepted',
    source text NOT NULL DEFAULT '',
    contact text NOT NULL DEFAULT '',
    CONSTRAINT logs_target_check CHECK (target IN ('item', 'computer', 'other')),
    CONSTRAINT logs_symptom_check CHECK (char_length(symptom) BETWEEN 1 AND 2000),
    CONSTRAINT logs_action_check CHECK (char_length(action) <= 2000),
    CONSTRAINT logs_result_check CHECK (char_length(result) <= 2000),
    CONSTRAINT logs_status_check CHECK (status IN ('accepted', 'doing', 'ready', 'done')),
    CONSTRAINT logs_source_check CHECK (char_length(source) <= 80),
    CONSTRAINT logs_contact_check CHECK (char_length(contact) <= 80),
    CONSTRAINT logs_item_target_check CHECK (target <> 'item' OR item_id IS NOT NULL)
);

CREATE INDEX logs_repairs_item_idx ON logs.repairs (item_id);
CREATE INDEX logs_repairs_status_idx ON logs.repairs (status);

CREATE TABLE logs.repair_parts (
    entry_id bigint NOT NULL REFERENCES logs.entries (id),
    item_id bigint NOT NULL REFERENCES inventory.items (id),
    qty integer NOT NULL,
    PRIMARY KEY (entry_id, item_id),
    CONSTRAINT logs_part_qty_check CHECK (qty BETWEEN 1 AND 999)
);

CREATE INDEX logs_parts_item_idx ON logs.repair_parts (item_id);

CREATE TABLE logs.entry_files (
    entry_id bigint NOT NULL REFERENCES logs.entries (id),
    file_id bigint NOT NULL REFERENCES files.files (id),
    PRIMARY KEY (entry_id, file_id)
);

CREATE INDEX logs_entry_files_file_idx ON logs.entry_files (file_id);
