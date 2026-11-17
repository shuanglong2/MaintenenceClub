CREATE SCHEMA finance;

CREATE TABLE finance.categories (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name text NOT NULL UNIQUE,
    CONSTRAINT finance_category_name_check CHECK (char_length(name) BETWEEN 1 AND 40)
);

INSERT INTO finance.categories (name) VALUES
    ('期初'),
    ('报销'),
    ('学校拨款'),
    ('活动经费'),
    ('器材'),
    ('耗材'),
    ('其他');

CREATE TABLE finance.entries (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    kind text NOT NULL,
    amount_cents bigint NOT NULL,
    happened_on date NOT NULL,
    category_id bigint NOT NULL REFERENCES finance.categories (id),
    note text NOT NULL DEFAULT '',
    actor_id bigint NOT NULL REFERENCES people.people (id),
    actor_name text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    voided_at timestamptz,
    voided_by bigint REFERENCES people.people (id),
    void_reason text NOT NULL DEFAULT '',
    CONSTRAINT finance_kind_check CHECK (kind IN ('in', 'out', 'save', 'draw')),
    CONSTRAINT finance_amount_check CHECK (amount_cents > 0),
    CONSTRAINT finance_note_check CHECK (char_length(note) <= 2000),
    CONSTRAINT finance_void_reason_check CHECK (char_length(void_reason) <= 200)
);

CREATE INDEX finance_entries_day_idx ON finance.entries (happened_on DESC, id DESC);
CREATE INDEX finance_entries_kind_idx ON finance.entries (kind, happened_on DESC);
CREATE INDEX finance_entries_category_idx ON finance.entries (category_id);
CREATE INDEX finance_entries_actor_idx ON finance.entries (actor_id);

CREATE FUNCTION finance.reject_entry_change()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION '财务流水不能删除';
    END IF;
    IF OLD.voided_at IS NOT NULL THEN
        RAISE EXCEPTION '已作废的流水不能再改';
    END IF;
    IF NEW.id IS DISTINCT FROM OLD.id
        OR NEW.kind IS DISTINCT FROM OLD.kind
        OR NEW.amount_cents IS DISTINCT FROM OLD.amount_cents
        OR NEW.happened_on IS DISTINCT FROM OLD.happened_on
        OR NEW.category_id IS DISTINCT FROM OLD.category_id
        OR NEW.note IS DISTINCT FROM OLD.note
        OR NEW.actor_id IS DISTINCT FROM OLD.actor_id
        OR NEW.actor_name IS DISTINCT FROM OLD.actor_name
        OR NEW.created_at IS DISTINCT FROM OLD.created_at
    THEN
        RAISE EXCEPTION '财务流水不能修改';
    END IF;
    IF NEW.voided_at IS NULL OR NEW.voided_by IS NULL OR btrim(NEW.void_reason) = '' THEN
        RAISE EXCEPTION '作废必须填原因';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER finance_entries_no_update
    BEFORE UPDATE ON finance.entries
    FOR EACH ROW
    EXECUTE FUNCTION finance.reject_entry_change();

CREATE TRIGGER finance_entries_no_delete
    BEFORE DELETE ON finance.entries
    FOR EACH ROW
    EXECUTE FUNCTION finance.reject_entry_change();

CREATE TABLE finance.entry_files (
    entry_id bigint NOT NULL REFERENCES finance.entries (id),
    file_id bigint NOT NULL REFERENCES files.files (id),
    PRIMARY KEY (entry_id, file_id)
);

CREATE INDEX finance_entry_files_file_idx ON finance.entry_files (file_id);
