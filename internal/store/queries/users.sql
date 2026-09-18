-- name: CreateUser :one
INSERT INTO users (name) VALUES ($1) RETURNING *;

-- name: GetUser :one
SELECT * FROM users WHERE id = $1;

-- name: ListUsers :many
SELECT * FROM users ORDER BY created_at, name;

-- name: RenameUser :one
UPDATE users SET name = $2 WHERE id = $1 RETURNING *;
