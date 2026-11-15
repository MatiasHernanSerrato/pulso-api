-- Alerts es la tabla donde dejamos registro de cada vez que algo salta en
-- Pulso: un outlier de peso (via detector z-score), un gasto fuera del
-- presupuesto, un pago de mercadopago que no pudimos conciliar, etc.
--
-- La hacemos hypertable sobre triggered_at porque esperamos muchas por
-- usuario con el tiempo y queries casi siempre por rango.

CREATE TABLE alerts (
    id            UUID NOT NULL DEFAULT gen_random_uuid(),
    user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind          TEXT NOT NULL CHECK (kind IN (
        'weight_anomaly',
        'budget_exceeded',
        'mp_reconcile_failed',
        'ble_drift'
    )),
    severity      TEXT NOT NULL CHECK (severity IN ('low', 'medium', 'high', 'critical')),

    -- Payload libre con el detalle de la alerta. Ej, para weight_anomaly:
    --   { "z_score": 3.1, "value_kg": 95.0, "mean_kg": 82.4, "stddev": 4.1,
    --     "window_days": 28 }
    -- Lo ponemos en JSONB asi despues podemos indexar campos puntuales.
    payload       JSONB NOT NULL DEFAULT '{}'::jsonb,

    -- Dedupe key es un hash o combinacion de campos que define "la misma
    -- alerta". Para outliers de peso lo usamos como "no re-alertar la
    -- misma medicion dos veces aunque re-corras el worker". Para budget
    -- es "mes + categoria". Nullable porque no todas las alerts son dedupeables.
    dedupe_key    TEXT NULL,

    -- Estado del lado del usuario, lo mueve el cliente via mutation.
    status        TEXT NOT NULL DEFAULT 'open'
                    CHECK (status IN ('open', 'acknowledged', 'dismissed', 'resolved')),

    triggered_at  TIMESTAMPTZ NOT NULL,
    resolved_at   TIMESTAMPTZ NULL,

    PRIMARY KEY (triggered_at, id)
);

-- 7 dias por chunk es un balance razonable: queries del dashboard miran
-- ultimo mes, con 7 dias tocan 4-5 chunks y los antiguos duermen.
SELECT create_hypertable('alerts', 'triggered_at', chunk_time_interval => INTERVAL '7 days');

-- El dashboard filtra por user y por status con frecuencia.
CREATE INDEX idx_alerts_user_time ON alerts (user_id, triggered_at DESC);
CREATE INDEX idx_alerts_user_status ON alerts (user_id, status) WHERE status = 'open';

-- Unique parcial para dedupear: si una row ya existe para (user_id,
-- dedupe_key) con status open o acknowledged, el insert tiene que fallar
-- y el caller debe ignorar el conflicto.
CREATE UNIQUE INDEX uniq_alerts_user_dedupe_open
    ON alerts (user_id, dedupe_key)
    WHERE dedupe_key IS NOT NULL AND status IN ('open', 'acknowledged');
