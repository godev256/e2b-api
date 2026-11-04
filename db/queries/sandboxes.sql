-- name: CountRunningSandboxes :one
SELECT count(*) FROM sandboxes WHERE project_id = $1 AND status = 'running';

-- name: CreateSandbox :one
INSERT INTO sandboxes (id, project_id, created_by, template)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetSandbox :one
SELECT * FROM sandboxes WHERE id = $1;

-- name: ListSandboxesByProject :many
-- Keyset pagination: pass the last seen id (or the nil UUID) as the cursor.
SELECT * FROM sandboxes
WHERE project_id = $1 AND id > $2
ORDER BY id
LIMIT $3;

-- name: StopSandbox :one
-- Only a running sandbox changes. Stopping a stopped one returns no row.
UPDATE sandboxes
SET status = 'stopped', stopped_at = now()
WHERE id = $1 AND status = 'running'
RETURNING *;
