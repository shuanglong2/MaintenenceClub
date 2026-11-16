CREATE SCHEMA audit;

CREATE TABLE audit.events (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    at timestamptz NOT NULL DEFAULT now(),
    actor_id bigint NOT NULL,
    actor_name text NOT NULL,
    ip text NOT NULL DEFAULT '',
    action text NOT NULL,
    entity text NOT NULL,
    entity_id bigint,
    summary text NOT NULL,
    detail jsonb NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX audit_events_at ON audit.events (at DESC, id DESC);
CREATE INDEX audit_events_actor ON audit.events (actor_id, at DESC);

CREATE FUNCTION audit.reject_change()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION '审计记录不能修改或删除';
END;
$$;

CREATE TRIGGER audit_events_no_update
    BEFORE UPDATE ON audit.events
    FOR EACH ROW
    EXECUTE FUNCTION audit.reject_change();

CREATE TRIGGER audit_events_no_delete
    BEFORE DELETE ON audit.events
    FOR EACH ROW
    EXECUTE FUNCTION audit.reject_change();
