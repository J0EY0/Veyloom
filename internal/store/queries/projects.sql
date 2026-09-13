-- name: CreateProject :one
INSERT INTO projects (name, repo_url, default_branch)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetProject :one
SELECT * FROM projects WHERE id = $1;

-- name: ListProjects :many
SELECT * FROM projects ORDER BY created_at, name;
