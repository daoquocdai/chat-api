-- name: CreateUser :one
INSERT INTO users (username)
VALUES ($1)
RETURNING id, external_id, username, created_at;

-- name: GetUserByExternalID :one
SELECT id, external_id, username, created_at
FROM users
WHERE external_id = $1;