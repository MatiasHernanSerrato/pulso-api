-- name: InsertAlert :one
-- Inserta una alerta nueva. Si choca contra el unique partial de dedupe,
-- devuelve ErrNoRows: el caller decide que hacer (loggear + seguir).
INSERT INTO alerts (
    user_id, kind, severity, payload, dedupe_key, status, triggered_at
) VALUES (
    $1, $2, $3, $4, $5, COALESCE(sqlc.narg(status)::text, 'open'), $6
)
ON CONFLICT DO NOTHING
RETURNING id, user_id, kind, severity, payload, dedupe_key, status, triggered_at, resolved_at;

-- name: GetAlert :one
SELECT id, user_id, kind, severity, payload, dedupe_key, status, triggered_at, resolved_at
FROM alerts
WHERE id = $1 AND user_id = $2
LIMIT 1;

-- name: ListAlertsByUser :many
-- Listado paginado por tiempo. El filtro por status es opcional (array vacio
-- = todos). Lo usan el dashboard y el endpoint /api/alerts.
SELECT id, user_id, kind, severity, payload, dedupe_key, status, triggered_at, resolved_at
FROM alerts
WHERE user_id = $1
  AND triggered_at >= sqlc.arg(from_ts)
  AND triggered_at <= sqlc.arg(to_ts)
  AND (cardinality(sqlc.arg(statuses)::text[]) = 0 OR status = ANY(sqlc.arg(statuses)::text[]))
ORDER BY triggered_at DESC
LIMIT sqlc.arg(max_rows);

-- name: OpenAlertsCount :one
SELECT COUNT(*) FROM alerts WHERE user_id = $1 AND status = 'open';

-- name: AcknowledgeAlert :exec
UPDATE alerts SET status = 'acknowledged'
WHERE id = $1 AND user_id = $2 AND status = 'open';

-- name: ResolveAlert :exec
UPDATE alerts SET status = 'resolved', resolved_at = now()
WHERE id = $1 AND user_id = $2 AND status IN ('open', 'acknowledged');

-- name: DismissAlert :exec
UPDATE alerts SET status = 'dismissed'
WHERE id = $1 AND user_id = $2 AND status IN ('open', 'acknowledged');
