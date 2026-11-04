-- name: UpsertUser :one
-- Creates the user on first sight and refreshes the email when the provider
-- supplies one. The identity is (issuer, subject).
INSERT INTO users (id, issuer, subject, email)
VALUES ($1, $2, $3, $4)
ON CONFLICT (issuer, subject) DO UPDATE
    SET email = COALESCE(EXCLUDED.email, users.email)
RETURNING *;

-- name: LockUser :one
-- Serializes per-user decisions such as the project limit.
SELECT id FROM users WHERE id = $1 FOR NO KEY UPDATE;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = $1;
