-- name: ThreadExistsByExternalID :one
SELECT EXISTS (
    SELECT 1 FROM threads WHERE external_id = $1
);

-- name: GetActiveThreadAccess :one
SELECT t.id
FROM threads AS t
JOIN participants AS p
  ON p.thread_id = t.id
 AND p.user_id = sqlc.arg(user_id)
 AND p.left_seq IS NULL
WHERE t.external_id = sqlc.arg(thread_external_id);

-- name: LockThreadForParticipant :one
SELECT t.id
FROM threads AS t
JOIN participants AS p
  ON p.thread_id = t.id
 AND p.user_id = sqlc.arg(user_id)
 AND p.left_seq IS NULL
WHERE t.external_id = sqlc.arg(thread_external_id)
FOR UPDATE OF t;

-- name: GetMessageByClientID :one
SELECT
    m.id,
    m.external_id,
    t.external_id AS thread_external_id,
    sender.external_id AS sender_external_id,
    m.seq,
    m.client_msg_id,
    m.kind,
    m.content_format,
    m.content,
    m.created_at
FROM messages AS m
JOIN threads AS t ON t.id = m.thread_id
JOIN users AS sender ON sender.id = m.sender_id
WHERE m.thread_id = sqlc.arg(thread_id)
  AND m.sender_id = sqlc.arg(sender_id)
  AND m.client_msg_id = sqlc.arg(client_msg_id);

-- name: IncrementThreadSequence :one
UPDATE threads
SET last_seq = last_seq + 1
WHERE id = $1
RETURNING last_seq;

-- name: CreateThreadMessage :one
WITH created AS (
    INSERT INTO messages (
        thread_id,
        sender_id,
        seq,
        client_msg_id,
        kind,
        content_format,
        content
    )
    VALUES (
        sqlc.arg(thread_id),
        sqlc.arg(sender_id),
        sqlc.arg(seq),
        sqlc.arg(client_msg_id),
        'text',
        'plaintext',
        sqlc.arg(content)
    )
    RETURNING id, external_id, thread_id, sender_id, seq,
              client_msg_id, kind, content_format, content, created_at
)
SELECT
    created.id,
    created.external_id,
    thread.external_id AS thread_external_id,
    sender.external_id AS sender_external_id,
    created.seq,
    created.client_msg_id,
    created.kind,
    created.content_format,
    created.content,
    created.created_at
FROM created
JOIN threads AS thread ON thread.id = created.thread_id
JOIN users AS sender ON sender.id = created.sender_id;

-- name: ListThreadMessagesPage :many
SELECT
    m.id,
    m.external_id,
    t.external_id AS thread_external_id,
    sender.external_id AS sender_external_id,
    m.seq,
    m.client_msg_id,
    m.kind,
    m.content_format,
    m.content,
    m.created_at
FROM messages AS m
JOIN threads AS t ON t.id = m.thread_id
JOIN users AS sender ON sender.id = m.sender_id
JOIN participants AS participant
  ON participant.thread_id = t.id
 AND participant.user_id = sqlc.arg(user_id)
 AND participant.left_seq IS NULL
WHERE t.external_id = sqlc.arg(thread_external_id)
  AND m.seq >= participant.joined_seq
  AND (
    sqlc.narg(before_seq)::BIGINT IS NULL
    OR m.seq < sqlc.narg(before_seq)
  )
ORDER BY m.seq DESC
LIMIT sqlc.arg(page_size)::INTEGER + 1;
