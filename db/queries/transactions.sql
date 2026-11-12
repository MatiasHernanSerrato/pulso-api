-- name: InsertTransaction :one
INSERT INTO transactions (
    user_id, category_id, occurred_at, amount, currency, kind, note,
    external_source, external_ref
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9
)
RETURNING id, user_id, category_id, occurred_at, amount, currency, kind, note,
          external_source, external_ref, created_at;

-- name: GetTransaction :one
SELECT id, user_id, category_id, occurred_at, amount, currency, kind, note,
       external_source, external_ref, created_at
FROM transactions
WHERE id = $1 AND user_id = $2
LIMIT 1;

-- name: ListTransactionsByUser :many
SELECT id, user_id, category_id, occurred_at, amount, currency, kind, note,
       external_source, external_ref, created_at
FROM transactions
WHERE user_id = $1
  AND occurred_at >= sqlc.arg(from_ts)
  AND occurred_at <= sqlc.arg(to_ts)
ORDER BY occurred_at DESC
LIMIT sqlc.arg(max_rows);

-- name: ListTransactionsByCategory :many
SELECT id, user_id, category_id, occurred_at, amount, currency, kind, note,
       external_source, external_ref, created_at
FROM transactions
WHERE user_id = $1
  AND category_id = $2
  AND occurred_at >= sqlc.arg(from_ts)
  AND occurred_at <= sqlc.arg(to_ts)
ORDER BY occurred_at DESC
LIMIT sqlc.arg(max_rows);

-- name: DeleteTransaction :exec
DELETE FROM transactions
WHERE id = $1 AND user_id = $2;

-- name: SumByCategory :many
SELECT category_id, kind, SUM(amount)::NUMERIC(14,2) AS total
FROM transactions
WHERE user_id = $1
  AND occurred_at >= sqlc.arg(from_ts)
  AND occurred_at <= sqlc.arg(to_ts)
GROUP BY category_id, kind;

-- name: TransactionByExternalRef :one
SELECT id, user_id, category_id, occurred_at, amount, currency, kind, note,
       external_source, external_ref, created_at
FROM transactions
WHERE user_id = $1
  AND external_source = $2
  AND external_ref = $3
LIMIT 1;
