ALTER TABLE docs.pages
    ADD COLUMN tone text NOT NULL DEFAULT 'ink',
    ADD COLUMN mark text NOT NULL DEFAULT 'note',
    ADD CONSTRAINT docs_page_tone_check CHECK (tone IN ('ink', 'copper', 'blue', 'red', 'gold', 'plum')),
    ADD CONSTRAINT docs_page_mark_check CHECK (mark IN ('note', 'book', 'wrench', 'flask', 'chip', 'folder'));
