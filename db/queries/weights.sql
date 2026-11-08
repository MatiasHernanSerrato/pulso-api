-- name: InsertWeight :one
INSERT INTO weights (
    user_id, measured_at, weight_kg, body_fat, source, note
) VALUES (
    $1, $2, $3, $4, $5, $6
)
RETURNING id, user_id, measured_at, weight_kg, body_fat, source, note, created_at;

-- name: GetWeight :one
SELECT id, user_id, measured_at, weight_kg, body_fat, source, note, created_at
FROM weights
WHERE id = $1 AND user_id = $2
LIMIT 1;

-- name: ListWeightsByUser :many
SELECT id, user_id, measured_at, weight_kg, body_fat, source, note, created_at
FROM weights
WHERE user_id = $1
  AND measured_at >= sqlc.arg(from_ts)
  AND measured_at <= sqlc.arg(to_ts)
ORDER BY measured_at DESC
LIMIT sqlc.arg(max_rows);

-- name: LatestWeight :one
SELECT id, user_id, measured_at, weight_kg, body_fat, source, note, created_at
FROM weights
WHERE user_id = $1
ORDER BY measured_at DESC
LIMIT 1;

-- name: WeightsCountByUser :one
SELECT COUNT(*) FROM weights WHERE user_id = $1;

-- name: DeleteWeight :exec
DELETE FROM weights WHERE id = $1 AND user_id = $2;

-- name: WeightsAvgLastNDays :one
SELECT AVG(weight_kg)::NUMERIC(6,3) AS avg_kg
FROM weights
WHERE user_id = $1
  AND measured_at >= now() - make_interval(days => sqlc.arg(days)::int);
