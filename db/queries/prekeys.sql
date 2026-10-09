-- name: LockE2EEUser :one
SELECT id, external_id, public_bundle FROM users WHERE id = $1 FOR UPDATE;

-- name: InsertE2EEOneTimePrekey :exec
INSERT INTO prekeys (user_id, key_id, public_key)
VALUES (sqlc.arg(user_id), sqlc.arg(key_id), sqlc.arg(public_key));

-- name: GetE2EEClaimThread :one
SELECT id, kind FROM threads WHERE external_id = $1 FOR SHARE;

-- name: GetE2EEClaimParticipants :many
SELECT user_id FROM participants
WHERE thread_id = $1 AND left_seq IS NULL ORDER BY user_id FOR SHARE;

-- name: ConsumeE2EEOneTimePrekey :one
DELETE FROM prekeys AS consumed
WHERE consumed.id = (
    SELECT candidate.id FROM prekeys AS candidate
    WHERE candidate.user_id = sqlc.arg(user_id)
    ORDER BY candidate.key_id LIMIT 1
)
RETURNING consumed.key_id, consumed.public_key;
