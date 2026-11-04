-- name: GetPlan :one
SELECT * FROM plans WHERE code = $1;
