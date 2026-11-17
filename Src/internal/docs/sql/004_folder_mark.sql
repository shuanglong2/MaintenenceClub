ALTER TABLE docs.folders
    ADD COLUMN tone text NOT NULL DEFAULT 'ink',
    ADD COLUMN mark text NOT NULL DEFAULT 'folder',
    ADD CONSTRAINT docs_folder_tone_check CHECK (tone IN ('ink', 'copper', 'blue', 'red', 'gold', 'plum')),
    ADD CONSTRAINT docs_folder_mark_check CHECK (mark IN ('folder', 'book', 'wrench', 'flask', 'chip', 'note'));
