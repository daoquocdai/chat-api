-- name: ThreadExistsByExternalID :one
SELECT EXISTS (
    SELECT 1 FROM threads WHERE external_id = $1
);

-- name: GetThreadHistoryAccess :one
SELECT t.id
FROM threads AS t
JOIN participants AS p
  ON p.thread_id = t.id
 AND p.user_id = sqlc.arg(user_id)
WHERE t.external_id = sqlc.arg(thread_external_id)
LIMIT 1;

-- name: LockThreadForParticipant :one
SELECT t.id
FROM threads AS t
JOIN participants AS p
  ON p.thread_id = t.id
 AND p.user_id = sqlc.arg(user_id)
 AND p.left_seq IS NULL
WHERE t.external_id = sqlc.arg(thread_external_id)
FOR UPDATE OF t;

-- name: ListMemberIDsAtSequence :many
SELECT DISTINCT member.external_id
FROM participants AS participant
JOIN threads AS thread ON thread.id = participant.thread_id
JOIN users AS member ON member.id = participant.user_id
WHERE thread.external_id = sqlc.arg(thread_external_id)
  AND participant.joined_seq <= sqlc.arg(seq)::BIGINT
  AND (participant.left_seq IS NULL OR participant.left_seq >= sqlc.arg(seq)::BIGINT)
ORDER BY member.external_id;

-- name: GetMessageByExternalID :one
SELECT
    m.id,
    m.external_id,
    m.thread_id,
    m.sender_id,
    t.external_id AS thread_external_id,
    t.kind AS thread_kind,
    sender.external_id AS sender_external_id,
    m.seq,
    m.kind,
    m.content_format,
    m.content,
    m.created_at
FROM messages AS m
JOIN threads AS t ON t.id = m.thread_id
JOIN users AS sender ON sender.id = m.sender_id
WHERE m.external_id = sqlc.arg(message_external_id);

-- name: IncrementThreadSequence :one
UPDATE threads
SET last_seq = last_seq + 1
WHERE id = $1
RETURNING last_seq;

-- name: CreateThreadMessage :one
WITH created AS (
    INSERT INTO messages (
        external_id,
        thread_id,
        sender_id,
        seq,
        kind,
        content_format,
        content
    )
    VALUES (
        sqlc.arg(message_external_id),
        sqlc.arg(thread_id),
        sqlc.arg(sender_id),
        sqlc.arg(seq),
        'text',
        'plaintext',
        sqlc.arg(content)
    )
    RETURNING id, external_id, thread_id, sender_id, seq,
              kind, content_format, content, created_at
)
SELECT
    created.id,
    created.external_id,
    thread.external_id AS thread_external_id,
    thread.kind AS thread_kind,
    sender.external_id AS sender_external_id,
    created.seq,
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
    t.kind AS thread_kind,
    sender.external_id AS sender_external_id,
    m.seq,
    m.kind,
    m.content_format,
    m.content,
    m.created_at
FROM messages AS m
JOIN threads AS t ON t.id = m.thread_id
JOIN users AS sender ON sender.id = m.sender_id
WHERE t.external_id = sqlc.arg(thread_external_id)
  AND EXISTS (
    SELECT 1 FROM participants AS participant
    WHERE participant.thread_id = t.id
      AND participant.user_id = sqlc.arg(user_id)
      AND m.seq >= participant.joined_seq
      AND (participant.left_seq IS NULL OR m.seq <= participant.left_seq)
  )
  AND (
    sqlc.narg(before_seq)::BIGINT IS NULL
    OR m.seq < sqlc.narg(before_seq)
  )
ORDER BY m.seq DESC
LIMIT sqlc.arg(page_size)::INTEGER + 1;
