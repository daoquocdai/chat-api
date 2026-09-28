-- +goose Up
DROP INDEX threads_direct_pair_key;

ALTER TABLE threads
    DROP CONSTRAINT threads_check,
    DROP COLUMN direct_user_low_id,
    DROP COLUMN direct_user_high_id,
    ADD CONSTRAINT threads_kind_shape_check CHECK (
        (kind = 'direct' AND name IS NULL)
        OR
        (kind = 'group' AND name IS NOT NULL AND BTRIM(name) <> '')
    );

DROP INDEX messages_sender_client_id_key;

ALTER TABLE messages
    DROP CONSTRAINT messages_check,
    DROP COLUMN client_msg_id;

-- +goose Down
ALTER TABLE messages
    ADD COLUMN client_msg_id UUID;

UPDATE messages
SET client_msg_id = external_id
WHERE kind = 'text';

ALTER TABLE messages
    ADD CONSTRAINT messages_client_id_kind_check CHECK (
        (kind = 'text' AND client_msg_id IS NOT NULL)
        OR (kind = 'system' AND client_msg_id IS NULL)
    );

CREATE UNIQUE INDEX messages_sender_client_id_key
    ON messages (thread_id, sender_id, client_msg_id)
    WHERE client_msg_id IS NOT NULL;

ALTER TABLE threads
    DROP CONSTRAINT threads_kind_shape_check,
    ADD COLUMN direct_user_low_id BIGINT REFERENCES users(id),
    ADD COLUMN direct_user_high_id BIGINT REFERENCES users(id);

WITH direct_pairs AS (
    SELECT
        thread_id,
        MIN(user_id) AS low_id,
        MAX(user_id) AS high_id
    FROM participants
    WHERE left_seq IS NULL
    GROUP BY thread_id
    HAVING COUNT(*) = 2
)
UPDATE threads AS thread
SET
    direct_user_low_id = pair.low_id,
    direct_user_high_id = pair.high_id
FROM direct_pairs AS pair
WHERE thread.id = pair.thread_id
  AND thread.kind = 'direct';

ALTER TABLE threads
    ADD CONSTRAINT threads_direct_shape_check CHECK (
        (kind = 'direct' AND name IS NULL
            AND direct_user_low_id IS NOT NULL
            AND direct_user_high_id IS NOT NULL
            AND direct_user_low_id < direct_user_high_id
            AND created_by IN (direct_user_low_id, direct_user_high_id))
        OR
        (kind = 'group' AND name IS NOT NULL AND BTRIM(name) <> ''
            AND direct_user_low_id IS NULL
            AND direct_user_high_id IS NULL)
    );

CREATE UNIQUE INDEX threads_direct_pair_key
    ON threads (direct_user_low_id, direct_user_high_id)
    WHERE kind = 'direct';
