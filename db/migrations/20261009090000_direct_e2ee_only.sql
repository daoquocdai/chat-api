-- +goose Up
-- Direct threads always use E2EE; groups always use plaintext.
-- The thread kind determines the format, so a separate mode is redundant.
ALTER TABLE threads DROP COLUMN encryption_mode;

-- +goose Down
ALTER TABLE threads
    ADD COLUMN encryption_mode TEXT NOT NULL DEFAULT 'plaintext'
        CHECK (encryption_mode IN ('plaintext', 'e2ee'));

UPDATE threads SET encryption_mode = 'e2ee' WHERE kind = 'direct';

ALTER TABLE threads
    ADD CONSTRAINT threads_group_plaintext_check
        CHECK (kind <> 'group' OR encryption_mode = 'plaintext');
