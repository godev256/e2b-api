-- name: CreateProject :one
INSERT INTO projects (id, name, plan_code, created_by)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: AddProjectMember :execrows
-- Adding an existing member is a no-op, which makes the endpoint idempotent.
INSERT INTO project_members (project_id, user_id, role, added_by)
VALUES ($1, $2, $3, $4)
ON CONFLICT (project_id, user_id) DO NOTHING;

-- name: ListProjectsForUser :many
-- Keyset pagination: pass the last seen id (or the nil UUID) as the cursor.
SELECT p.*
FROM projects p
JOIN project_members m ON m.project_id = p.id
WHERE m.user_id = $1 AND p.id > $2
ORDER BY p.id
LIMIT $3;

-- name: GetProjectAccess :one
-- Authorization in one indexed lookup: returns a row only for members.
SELECT p.id AS project_id, p.plan_code, m.role
FROM projects p
JOIN project_members m ON m.project_id = p.id
WHERE p.id = $1 AND m.user_id = $2;

-- name: LockProject :one
-- Serializes quota decisions for one project. Other projects are not blocked.
-- NO KEY UPDATE is enough and, unlike FOR UPDATE, does not block inserts into
-- tables that reference the project (they only take FOR KEY SHARE).
SELECT id FROM projects WHERE id = $1 FOR NO KEY UPDATE;

-- name: CountOwnedProjects :one
SELECT count(*) FROM project_members WHERE user_id = $1 AND role = 'owner';

-- name: GetProjectMember :one
SELECT m.user_id, u.email, m.role, m.added_at
FROM project_members m
JOIN users u ON u.id = m.user_id
WHERE m.project_id = $1 AND m.user_id = $2;

-- name: ListProjectMembers :many
-- Keyset pagination over user ids.
SELECT m.user_id, u.email, m.role, m.added_at
FROM project_members m
JOIN users u ON u.id = m.user_id
WHERE m.project_id = $1 AND m.user_id > $2
ORDER BY m.user_id
LIMIT $3;
