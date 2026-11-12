-- name: CreateCategory :one
INSERT INTO categories (user_id, name, kind, icon, color)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, user_id, name, kind, icon, color, archived, created_at, updated_at;

-- name: GetCategory :one
SELECT id, user_id, name, kind, icon, color, archived, created_at, updated_at
FROM categories
WHERE id = $1
  AND (user_id = sqlc.arg(user_id)::uuid OR user_id IS NULL)
LIMIT 1;

-- name: ListCategoriesForUser :many
SELECT id, user_id, name, kind, icon, color, archived, created_at, updated_at
FROM categories
WHERE (user_id = $1 OR user_id IS NULL)
  AND archived = false
ORDER BY (user_id IS NULL), kind, lower(name);

-- name: ListCategoriesByKind :many
SELECT id, user_id, name, kind, icon, color, archived, created_at, updated_at
FROM categories
WHERE (user_id = $1 OR user_id IS NULL)
  AND kind = $2
  AND archived = false
ORDER BY (user_id IS NULL), lower(name);

-- name: UpdateCategory :one
UPDATE categories
SET name       = COALESCE(sqlc.narg(name), name),
    icon       = COALESCE(sqlc.narg(icon), icon),
    color      = COALESCE(sqlc.narg(color), color),
    updated_at = now()
WHERE id = $1 AND user_id = $2
RETURNING id, user_id, name, kind, icon, color, archived, created_at, updated_at;

-- name: ArchiveCategory :exec
UPDATE categories
SET archived = true, updated_at = now()
WHERE id = $1 AND user_id = $2;
