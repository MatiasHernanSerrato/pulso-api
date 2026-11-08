-- Habilitamos timescaledb a nivel db. Si no esta el extension, esto revienta
-- a proposito, mejor que descubrirlo mas tarde.
CREATE EXTENSION IF NOT EXISTS timescaledb;

-- Users. Mas adelante agregamos oauth providers, por ahora solo email + hash.
CREATE TABLE users (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email       TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_users_email ON users (lower(email));

-- Weights. Hypertable sobre measured_at asi las queries por rango pegan
-- a chunks concretos en vez de scanear todo.
CREATE TABLE weights (
    id          UUID NOT NULL DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    measured_at TIMESTAMPTZ NOT NULL,
    weight_kg   NUMERIC(6,3) NOT NULL CHECK (weight_kg > 0 AND weight_kg < 500),
    body_fat    NUMERIC(5,2) NULL CHECK (body_fat IS NULL OR (body_fat >= 0 AND body_fat <= 100)),
    source      TEXT NOT NULL DEFAULT 'manual' CHECK (source IN ('manual','ble','healthkit','health_connect')),
    note        TEXT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (measured_at, id)
);

-- 1 dia por chunk va fino para la cantidad de registros que esperamos
-- (varias mediciones por usuario por dia, no millones).
SELECT create_hypertable('weights', 'measured_at', chunk_time_interval => INTERVAL '1 day');

CREATE INDEX idx_weights_user_time ON weights (user_id, measured_at DESC);
CREATE INDEX idx_weights_source ON weights (source);
