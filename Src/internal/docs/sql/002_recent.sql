CREATE INDEX docs_pages_recent_idx ON docs.pages (updated_at DESC) WHERE voided_at IS NULL;
