-- name: CreateGroupThread :one
INSERT INTO threads (kind, name, created_by)
VALUES ('group', sqlc.arg(name), sqlc.arg(created_by))
RETURNING id, external_id;

-- name: LockThreadByExternalID :one
SELECT id, external_id, kind, last_seq
FROM threads
WHERE external_id = sqlc.arg(thread_external_id)
FOR UPDATE;

-- name: GetActiveParticipant :one
SELECT id, role, joined_seq, last_read_seq
FROM participants
WHERE thread_id = sqlc.arg(thread_id)
  AND user_id = sqlc.arg(user_id) AND left_seq IS NULL;

-- name: GetLatestMembership :one
SELECT id, joined_seq, left_seq
FROM participants
WHERE thread_id = sqlc.arg(thread_id) AND user_id = sqlc.arg(user_id)
ORDER BY joined_seq DESC
LIMIT 1;

-- name: CreateGroupParticipant :exec
INSERT INTO participants (thread_id, user_id, role, joined_seq, last_read_seq)
VALUES (sqlc.arg(thread_id), sqlc.arg(user_id), sqlc.arg(role),
        sqlc.arg(joined_seq), sqlc.arg(joined_seq)::BIGINT - 1);

-- name: EndParticipant :exec
UPDATE participants
SET left_seq = sqlc.arg(left_seq), left_at = NOW()
WHERE id = sqlc.arg(id) AND left_seq IS NULL;

-- name: CountActiveGroupMembers :one
SELECT COUNT(*) AS members,
       COUNT(*) FILTER (WHERE role = 'admin') AS admins
FROM participants
WHERE thread_id = sqlc.arg(thread_id) AND left_seq IS NULL;

-- name: ListGroupMembers :many
SELECT member.external_id, member.username, participant.role,
       participant.joined_seq, participant.last_read_seq
FROM participants AS participant
JOIN users AS member ON member.id = participant.user_id
WHERE participant.thread_id = sqlc.arg(thread_id)
  AND participant.left_seq IS NULL
  AND EXISTS (SELECT 1 FROM participants AS actor
              WHERE actor.thread_id = participant.thread_id
                AND actor.user_id = sqlc.arg(actor_id) AND actor.left_seq IS NULL)
ORDER BY participant.role, member.username, member.id;

-- name: PromoteOldestGroupMember :one
WITH promoted AS (
    UPDATE participants SET role = 'admin'
    WHERE id = (
        SELECT member.id FROM participants AS member
        WHERE member.thread_id = sqlc.arg(thread_id) AND member.left_seq IS NULL
        ORDER BY member.joined_seq, member.user_id LIMIT 1
    )
    RETURNING user_id
)
SELECT username FROM users JOIN promoted ON promoted.user_id = users.id;

-- name: CreateSystemMessage :one
WITH created AS (
    INSERT INTO messages (thread_id, sender_id, seq, kind, content_format, content)
    VALUES (sqlc.arg(thread_id), sqlc.arg(sender_id), sqlc.arg(seq),
            'system', 'plaintext', sqlc.arg(content))
    RETURNING id, external_id, thread_id, sender_id, seq, kind,
              content_format, content, created_at
)
SELECT created.id, created.external_id, thread.external_id AS thread_external_id, thread.kind AS thread_kind,
       sender.external_id AS sender_external_id, created.seq, created.kind,
       created.content_format, created.content, created.created_at
FROM created
JOIN threads AS thread ON thread.id = created.thread_id
JOIN users AS sender ON sender.id = created.sender_id;

-- name: GetThreadMessageAtSequence :one
SELECT m.id, m.external_id, t.external_id AS thread_external_id, t.kind AS thread_kind,
       sender.external_id AS sender_external_id, m.seq, m.kind,
       m.content_format, m.content, m.created_at
FROM messages AS m
JOIN threads AS t ON t.id = m.thread_id
JOIN users AS sender ON sender.id = m.sender_id
WHERE m.thread_id = sqlc.arg(thread_id) AND m.seq = sqlc.arg(seq);
