-- name: GetEpochThreadForParticipant :one
SELECT t.id, t.kind, t.current_epoch_id
FROM threads AS t JOIN participants AS p ON p.thread_id = t.id
WHERE t.external_id = sqlc.arg(thread_external_id)
  AND p.user_id = sqlc.arg(user_id) AND p.left_seq IS NULL;

-- name: LockEpochThreadForParticipant :one
SELECT t.id, t.kind, t.current_epoch_id
FROM threads AS t JOIN participants AS p ON p.thread_id = t.id
WHERE t.external_id = sqlc.arg(thread_external_id)
  AND p.user_id = sqlc.arg(user_id) AND p.left_seq IS NULL
FOR UPDATE OF t;

-- name: GetEpochParticipantProfiles :many
SELECT u.id, u.external_id, u.public_bundle
FROM participants AS p JOIN users AS u ON u.id = p.user_id
WHERE p.thread_id = $1 AND p.left_seq IS NULL ORDER BY u.id;

-- name: ListEpochsForUser :many
SELECT e.id, t.external_id AS thread_external_id,
       sender.external_id AS sender_external_id, recipient.external_id AS recipient_external_id,
       e.bootstrap, b.key_backup
FROM e2ee_epochs AS e
JOIN threads AS t ON t.id = e.thread_id
JOIN users AS sender ON sender.id = e.sender_id
JOIN users AS recipient ON recipient.id = e.recipient_id
LEFT JOIN e2ee_epoch_backups AS b ON b.epoch_id = e.id AND b.user_id = sqlc.arg(user_id)
WHERE e.thread_id = sqlc.arg(thread_id) ORDER BY e.created_at, e.id;

-- name: GetEpochForUser :one
SELECT e.id, t.external_id AS thread_external_id,
       sender.external_id AS sender_external_id, recipient.external_id AS recipient_external_id,
       e.bootstrap, b.key_backup
FROM e2ee_epochs AS e
JOIN threads AS t ON t.id = e.thread_id
JOIN users AS sender ON sender.id = e.sender_id
JOIN users AS recipient ON recipient.id = e.recipient_id
LEFT JOIN e2ee_epoch_backups AS b ON b.epoch_id = e.id AND b.user_id = sqlc.arg(user_id)
WHERE e.id = sqlc.arg(epoch_id) AND e.thread_id = sqlc.arg(thread_id);

-- name: CreateEpoch :exec
INSERT INTO e2ee_epochs (id, thread_id, sender_id, recipient_id, bootstrap)
VALUES (sqlc.arg(id), sqlc.arg(thread_id), sqlc.arg(sender_id), sqlc.arg(recipient_id), sqlc.arg(bootstrap));

-- name: SetCurrentEpoch :exec
UPDATE threads SET current_epoch_id = sqlc.arg(epoch_id) WHERE id = sqlc.arg(thread_id);

-- name: StoreEpochBackup :exec
INSERT INTO e2ee_epoch_backups (epoch_id, user_id, key_backup)
VALUES (sqlc.arg(epoch_id), sqlc.arg(user_id), sqlc.arg(key_backup))
ON CONFLICT (epoch_id, user_id) DO NOTHING;

-- name: GetEpochIdentity :one
SELECT id FROM e2ee_epochs WHERE id = sqlc.arg(epoch_id) AND thread_id = sqlc.arg(thread_id);
