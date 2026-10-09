-- +goose Up
-- Apply to the reset development database: legacy accounts do not have vaults.
ALTER TABLE users RENAME COLUMN password_hash TO auth_credential_hash;
ALTER TABLE users
    DROP COLUMN identity_public_key,
    DROP COLUMN last_prekey_id,
    ADD COLUMN kdf JSONB NOT NULL CHECK (jsonb_typeof(kdf) = 'object'),
    ADD COLUMN public_bundle JSONB NOT NULL CHECK (jsonb_typeof(public_bundle) = 'object'),
    ADD COLUMN account_vault JSONB NOT NULL CHECK (jsonb_typeof(account_vault) = 'object');

-- The immutable registration bundle includes all original public keys.
-- These rows are only the available public one-time prekeys.
ALTER TABLE prekeys DROP COLUMN kind, DROP COLUMN signature, DROP COLUMN retired_at;

CREATE TABLE e2ee_epochs (
    id UUID PRIMARY KEY,
    thread_id BIGINT NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    sender_id BIGINT NOT NULL REFERENCES users(id),
    recipient_id BIGINT NOT NULL REFERENCES users(id),
    bootstrap TEXT NOT NULL CHECK (OCTET_LENGTH(bootstrap) BETWEEN 1 AND 8192),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (sender_id <> recipient_id),
    UNIQUE (thread_id, id)
);
CREATE TABLE e2ee_epoch_backups (
    epoch_id UUID NOT NULL REFERENCES e2ee_epochs(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id),
    key_backup JSONB NOT NULL CHECK (jsonb_typeof(key_backup) = 'object'),
    PRIMARY KEY (epoch_id, user_id)
);
ALTER TABLE threads
    ADD COLUMN current_epoch_id UUID,
    ADD FOREIGN KEY (id, current_epoch_id) REFERENCES e2ee_epochs(thread_id, id);
ALTER TABLE messages
    DROP CONSTRAINT messages_content_format_check,
    ADD COLUMN epoch_id UUID,
    ADD FOREIGN KEY (thread_id, epoch_id) REFERENCES e2ee_epochs(thread_id, id),
    ADD CONSTRAINT messages_content_format_check CHECK (content_format IN ('plaintext', 'e2ee_v2')),
    ADD CONSTRAINT messages_epoch_format_check CHECK (
        (content_format = 'plaintext' AND epoch_id IS NULL)
        OR (content_format = 'e2ee_v2' AND epoch_id IS NOT NULL)
    );
CREATE INDEX e2ee_epochs_thread_idx ON e2ee_epochs(thread_id, created_at, id);

-- +goose Down
-- New encrypted messages have no equivalent v1 payload.
DELETE FROM messages WHERE content_format = 'e2ee_v2';
ALTER TABLE messages
    DROP CONSTRAINT messages_epoch_format_check,
    DROP CONSTRAINT messages_content_format_check,
    DROP COLUMN epoch_id,
    ADD CONSTRAINT messages_content_format_check CHECK (content_format IN ('plaintext', 'e2ee_v1'));
ALTER TABLE threads DROP COLUMN current_epoch_id;
DROP TABLE e2ee_epoch_backups;
DROP TABLE e2ee_epochs;
ALTER TABLE prekeys
    ADD COLUMN kind TEXT NOT NULL DEFAULT 'one_time' CHECK (kind IN ('signed', 'one_time')),
    ADD COLUMN signature BYTEA,
    ADD COLUMN retired_at TIMESTAMPTZ;
ALTER TABLE users
    DROP COLUMN kdf,
    DROP COLUMN public_bundle,
    DROP COLUMN account_vault,
    ADD COLUMN identity_public_key BYTEA,
    ADD COLUMN last_prekey_id BIGINT NOT NULL DEFAULT 0;
ALTER TABLE users RENAME COLUMN auth_credential_hash TO password_hash;
