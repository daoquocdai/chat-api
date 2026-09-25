-- name: CreateDirectThread :one
INSERT INTO threads (
    kind, created_by, direct_user_low_id, direct_user_high_id, encryption_mode
)
VALUES (
    'direct', sqlc.arg(created_by), sqlc.arg(direct_user_low_id),
    sqlc.arg(direct_user_high_id), 'plaintext'
)
ON CONFLICT (direct_user_low_id, direct_user_high_id) WHERE kind = 'direct'
DO NOTHING
RETURNING id, external_id, kind, last_seq, created_at;

-- name: GetDirectThreadByPair :one
SELECT id, external_id, kind, last_seq, created_at
FROM threads
WHERE kind = 'direct'
  AND direct_user_low_id = sqlc.arg(direct_user_low_id)
  AND direct_user_high_id = sqlc.arg(direct_user_high_id);

-- name: CreateParticipant :exec
INSERT INTO participants (thread_id, user_id, role, joined_seq, last_read_seq)
VALUES (sqlc.arg(thread_id), sqlc.arg(user_id), 'member', 1, 0);

-- name: GetThreadSummaryForUser :one
SELECT
    t.id,
    t.external_id,
    t.kind,
    peer.external_id AS peer_external_id,
    peer.username AS peer_username,
    t.last_seq,
    last_message.seq AS last_message_seq,
    last_sender.external_id AS last_message_sender_external_id,
    last_message.content AS last_message_content,
    last_message.created_at AS last_message_created_at,
    t.created_at
FROM threads AS t
JOIN participants AS mine
  ON mine.thread_id = t.id
 AND mine.user_id = sqlc.arg(user_id)
 AND mine.left_seq IS NULL
JOIN participants AS other
  ON other.thread_id = t.id
 AND other.user_id <> mine.user_id
 AND other.left_seq IS NULL
JOIN users AS peer ON peer.id = other.user_id
LEFT JOIN messages AS last_message
  ON last_message.thread_id = t.id
 AND last_message.seq = t.last_seq
LEFT JOIN users AS last_sender ON last_sender.id = last_message.sender_id
WHERE t.external_id = sqlc.arg(thread_external_id)
  AND t.kind = 'direct';

-- name: ListThreadsForUser :many
SELECT
    t.id,
    t.external_id,
    t.kind,
    peer.external_id AS peer_external_id,
    peer.username AS peer_username,
    t.last_seq,
    last_message.seq AS last_message_seq,
    last_sender.external_id AS last_message_sender_external_id,
    last_message.content AS last_message_content,
    last_message.created_at AS last_message_created_at,
    t.created_at
FROM participants AS mine
JOIN threads AS t ON t.id = mine.thread_id
JOIN participants AS other
  ON other.thread_id = t.id
 AND other.user_id <> mine.user_id
 AND other.left_seq IS NULL
JOIN users AS peer ON peer.id = other.user_id
LEFT JOIN messages AS last_message
  ON last_message.thread_id = t.id
 AND last_message.seq = t.last_seq
LEFT JOIN users AS last_sender ON last_sender.id = last_message.sender_id
WHERE mine.user_id = sqlc.arg(user_id)
  AND mine.left_seq IS NULL
  AND t.kind = 'direct'
ORDER BY COALESCE(last_message.created_at, t.created_at) DESC, t.id DESC;
