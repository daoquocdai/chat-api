-- name: LockE2EEUser :one
SELECT id, external_id, identity_public_key, last_prekey_id
FROM users
WHERE id = $1
FOR UPDATE;

-- name: GetE2EESignedPrekeys :many
SELECT key_id, public_key, signature
FROM prekeys
WHERE user_id = $1 AND kind = 'signed' AND retired_at IS NULL
ORDER BY key_id;

-- name: GetE2EEPrekey :one
SELECT key_id, kind, public_key
FROM prekeys
WHERE user_id = sqlc.arg(user_id) AND key_id = sqlc.arg(key_id);

-- name: InsertE2EEPrekey :exec
INSERT INTO prekeys (user_id, key_id, kind, public_key, signature)
VALUES (sqlc.arg(user_id), sqlc.arg(key_id), sqlc.arg(kind),
        sqlc.arg(public_key), sqlc.narg(signature));

-- name: SetE2EEUserKeys :exec
UPDATE users
SET identity_public_key = sqlc.arg(identity_public_key),
    last_prekey_id = sqlc.arg(last_prekey_id)
WHERE id = sqlc.arg(user_id);

-- name: CountE2EEOneTimePrekeys :one
SELECT COUNT(*) FROM prekeys
WHERE user_id = $1 AND kind = 'one_time' AND retired_at IS NULL;

-- name: GetE2EEClaimThread :one
SELECT id, kind, encryption_mode
FROM threads
WHERE external_id = $1
FOR SHARE;

-- name: GetE2EEClaimParticipants :many
SELECT user_id FROM participants
WHERE thread_id = $1 AND left_seq IS NULL
ORDER BY user_id
FOR SHARE;

-- name: ConsumeE2EEOneTimePrekey :one
DELETE FROM prekeys AS consumed
WHERE consumed.id = (
    SELECT candidate.id FROM prekeys AS candidate
    WHERE candidate.user_id = sqlc.arg(user_id)
      AND candidate.kind = 'one_time' AND candidate.retired_at IS NULL
    ORDER BY candidate.key_id
    LIMIT 1
)
RETURNING consumed.key_id, consumed.public_key;
