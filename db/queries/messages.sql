-- name: CreateMessage :one
WITH created AS (
    INSERT INTO messages (sender_id, receiver_id, content)
    VALUES (sqlc.arg(sender_id), sqlc.arg(receiver_id), sqlc.arg(content))
    RETURNING id, external_id, sender_id, receiver_id, content, created_at
)
SELECT
    created.id,
    created.external_id,
    sender.external_id AS sender_external_id,
    receiver.external_id AS receiver_external_id,
    created.content,
    created.created_at
FROM created
JOIN users AS sender ON sender.id = created.sender_id
JOIN users AS receiver ON receiver.id = created.receiver_id;

-- name: ListMessagesBetween :many
SELECT
    recent.id,
    recent.external_id,
    sender.external_id AS sender_external_id,
    receiver.external_id AS receiver_external_id,
    recent.content,
    recent.created_at
FROM (
    SELECT id, external_id, sender_id, receiver_id, content, created_at
    FROM messages
    WHERE
        (sender_id = sqlc.arg(user_one_id) AND receiver_id = sqlc.arg(user_two_id))
        OR
        (sender_id = sqlc.arg(user_two_id) AND receiver_id = sqlc.arg(user_one_id))
    ORDER BY id DESC
    LIMIT 100
) AS recent
JOIN users AS sender ON sender.id = recent.sender_id
JOIN users AS receiver ON receiver.id = recent.receiver_id
ORDER BY recent.id;
