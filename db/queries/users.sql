-- name: CreateUserWithPassword :one
INSERT INTO users (username, password_hash)
VALUES (sqlc.arg(username), sqlc.arg(password_hash))
RETURNING id, external_id, username, created_at;

-- name: GetUserCredentialsByUsername :one
SELECT id, external_id, username, password_hash, created_at
FROM users
WHERE username = $1;

-- name: GetUserByExternalID :one
SELECT id, external_id, username, created_at
FROM users
WHERE external_id = $1;

-- name: ListUsers :many
SELECT id, external_id, username, created_at
FROM users
ORDER BY username, id;
