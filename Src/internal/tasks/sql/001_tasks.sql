CREATE SCHEMA tasks;

CREATE TABLE tasks.tasks (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    title text NOT NULL,
    body text NOT NULL DEFAULT '',
    kind text NOT NULL,
    status text NOT NULL DEFAULT 'todo',
    priority text NOT NULL DEFAULT 'normal',
    owner_id bigint NOT NULL REFERENCES people.people (id),
    owner_name text NOT NULL,
    due_on date,
    event_id bigint REFERENCES events.events (id),
    created_by bigint NOT NULL REFERENCES people.people (id),
    created_name text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT tasks_title_check CHECK (char_length(title) BETWEEN 1 AND 80),
    CONSTRAINT tasks_body_check CHECK (char_length(body) <= 8000),
    CONSTRAINT tasks_kind_check CHECK (kind IN ('科普活动', '创新竞赛', '实验室保养', '电脑软硬件维护', '其他')),
    CONSTRAINT tasks_status_check CHECK (status IN ('todo', 'doing', 'done', 'cancelled')),
    CONSTRAINT tasks_priority_check CHECK (priority IN ('low', 'normal', 'high'))
);

CREATE INDEX tasks_status_due_idx ON tasks.tasks (status, due_on);
CREATE INDEX tasks_owner_idx ON tasks.tasks (owner_id);
CREATE INDEX tasks_created_idx ON tasks.tasks (created_by);
CREATE INDEX tasks_event_idx ON tasks.tasks (event_id);

CREATE TABLE tasks.participants (
    task_id bigint NOT NULL REFERENCES tasks.tasks (id),
    person_id bigint NOT NULL REFERENCES people.people (id),
    person_name text NOT NULL,
    PRIMARY KEY (task_id, person_id)
);

CREATE TABLE tasks.comments (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    task_id bigint NOT NULL REFERENCES tasks.tasks (id),
    author_id bigint NOT NULL REFERENCES people.people (id),
    author_name text NOT NULL,
    body text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT tasks_comment_body_check CHECK (char_length(body) BETWEEN 1 AND 4000)
);

CREATE INDEX tasks_comments_task_idx ON tasks.comments (task_id, id);

CREATE TABLE tasks.task_items (
    task_id bigint NOT NULL REFERENCES tasks.tasks (id),
    item_id bigint NOT NULL REFERENCES inventory.items (id),
    PRIMARY KEY (task_id, item_id)
);
