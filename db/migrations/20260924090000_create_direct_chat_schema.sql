-- +goose Up
-- Existing users do not have password hashes and must not be assigned
-- synthetic passwords. This first statement intentionally fails when legacy
-- users exist. Explicitly reset only the disposable local database before
-- applying this migration; see README.md.
ALTER TABLE users
    ADD COLUMN password_hash TEXT NOT NULL,
    ADD COLUMN identity_public_key BYTEA,
    ADD COLUMN last_prekey_id BIGINT NOT NULL DEFAULT 0,
    ADD CONSTRAINT users_password_hash_not_empty CHECK (password_hash <> ''),
    ADD CONSTRAINT users_last_prekey_id_nonnegative CHECK (last_prekey_id >= 0);

DROP TABLE messages;

CREATE TABLE threads (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    external_id UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    kind TEXT NOT NULL CHECK (kind IN ('direct', 'group')),
    name TEXT,
    created_by BIGINT NOT NULL REFERENCES users(id),
    direct_user_low_id BIGINT REFERENCES users(id),
    direct_user_high_id BIGINT REFERENCES users(id),
    encryption_mode TEXT NOT NULL DEFAULT 'plaintext'
        CHECK (encryption_mode IN ('plaintext', 'e2ee')),
    last_seq BIGINT NOT NULL DEFAULT 0 CHECK (last_seq >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (
        (kind = 'direct' AND name IS NULL
            AND direct_user_low_id IS NOT NULL
            AND direct_user_high_id IS NOT NULL
            AND direct_user_low_id < direct_user_high_id
            AND created_by IN (direct_user_low_id, direct_user_high_id))
        OR
        (kind = 'group' AND name IS NOT NULL AND BTRIM(name) <> ''
            AND direct_user_low_id IS NULL
            AND direct_user_high_id IS NULL)
    ),
    CHECK (kind <> 'group' OR encryption_mode = 'plaintext')
);

CREATE UNIQUE INDEX threads_direct_pair_key
    ON threads (direct_user_low_id, direct_user_high_id)
    WHERE kind = 'direct';

CREATE TABLE participants (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    thread_id BIGINT NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id),
    role TEXT NOT NULL DEFAULT 'member' CHECK (role IN ('admin', 'member')),
    joined_seq BIGINT NOT NULL CHECK (joined_seq >= 1),
    left_seq BIGINT,
    last_read_seq BIGINT NOT NULL,
    joined_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    left_at TIMESTAMPTZ,
    UNIQUE (thread_id, user_id, joined_seq),
    CHECK (last_read_seq >= joined_seq - 1),
    CHECK (left_seq IS NULL OR left_seq >= joined_seq - 1),
    CHECK ((left_seq IS NULL) = (left_at IS NULL)),
    CHECK (left_seq IS NULL OR last_read_seq <= left_seq)
);

CREATE UNIQUE INDEX participants_active_membership_key
    ON participants (thread_id, user_id)
    WHERE left_seq IS NULL;
CREATE INDEX participants_user_threads_idx
    ON participants (user_id, thread_id)
    WHERE left_seq IS NULL;

CREATE TABLE messages (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    external_id UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    thread_id BIGINT NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    sender_id BIGINT NOT NULL REFERENCES users(id),
    seq BIGINT NOT NULL CHECK (seq >= 1),
    client_msg_id UUID,
    kind TEXT NOT NULL DEFAULT 'text' CHECK (kind IN ('text', 'system')),
    content_format TEXT NOT NULL DEFAULT 'plaintext'
        CHECK (content_format IN ('plaintext', 'e2ee_v1')),
    content TEXT NOT NULL CHECK (content <> ''),
    metadata JSONB NOT NULL DEFAULT '{}'::JSONB
        CHECK (jsonb_typeof(metadata) = 'object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (thread_id, seq),
    CHECK (
        (kind = 'text' AND client_msg_id IS NOT NULL)
        OR (kind = 'system' AND client_msg_id IS NULL)
    )
);

CREATE UNIQUE INDEX messages_sender_client_id_key
    ON messages (thread_id, sender_id, client_msg_id)
    WHERE client_msg_id IS NOT NULL;
CREATE INDEX messages_thread_history_idx ON messages (thread_id, seq DESC);

CREATE TABLE prekeys (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    key_id BIGINT NOT NULL CHECK (key_id > 0),
    kind TEXT NOT NULL CHECK (kind IN ('signed', 'one_time')),
    public_key BYTEA NOT NULL CHECK (OCTET_LENGTH(public_key) > 0),
    signature BYTEA,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    retired_at TIMESTAMPTZ,
    UNIQUE (user_id, key_id),
    CHECK (
        (kind = 'signed' AND signature IS NOT NULL AND OCTET_LENGTH(signature) > 0)
        OR (kind = 'one_time' AND signature IS NULL AND retired_at IS NULL)
    )
);

CREATE INDEX prekeys_available_idx ON prekeys (user_id, kind, key_id)
    WHERE retired_at IS NULL;

-- +goose Down
DROP TABLE prekeys;
DROP TABLE messages;
DROP TABLE participants;
DROP TABLE threads;

ALTER TABLE users
    DROP CONSTRAINT users_last_prekey_id_nonnegative,
    DROP CONSTRAINT users_password_hash_not_empty,
    DROP COLUMN last_prekey_id,
    DROP COLUMN identity_public_key,
    DROP COLUMN password_hash;

CREATE TABLE messages (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    external_id UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    sender_id BIGINT NOT NULL REFERENCES users(id),
    receiver_id BIGINT NOT NULL REFERENCES users(id),
    content TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (sender_id <> receiver_id)
);
