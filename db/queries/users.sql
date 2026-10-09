-- name: CreateUserAccount :one
INSERT INTO users (username, auth_credential_hash, kdf, public_bundle, account_vault)
VALUES (sqlc.arg(username), sqlc.arg(auth_credential_hash), sqlc.arg(kdf),
        sqlc.arg(public_bundle), sqlc.arg(account_vault))
RETURNING id, external_id, username, created_at;

-- name: GetUserCredentialsByUsername :one
SELECT id, external_id, username, auth_credential_hash, created_at
FROM users
WHERE username = $1;

-- name: GetAuthParamsByUsername :one
SELECT username, kdf FROM users WHERE username = $1;

-- name: GetUserAccount :one
SELECT id, external_id, username, kdf, public_bundle, account_vault
FROM users WHERE id = $1;

-- name: GetUserByExternalID :one
SELECT id, external_id, username, created_at
FROM users
WHERE external_id = $1;

-- name: ListUsers :many
SELECT id, external_id, username, created_at
FROM users
ORDER BY username, id;
