CREATE INDEX events_upcoming_idx ON events.events (starts_at) WHERE status = 'planned';
