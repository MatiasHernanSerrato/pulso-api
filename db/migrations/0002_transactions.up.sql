-- Modulo plata: transactions + categories.
-- La idea es que cada expense/income del user tenga una category, y las
-- categorias las puede definir el user (user_id not null) o venir como
-- defaults globales que sembramos abajo (user_id null).
--
-- No metemos accounts todavia porque Pulso arranca con un solo "wallet"
-- implicito por user. Cuando aparezcan tarjetas y cuentas separadas se
-- agrega una tabla accounts y la FK va aca.

CREATE TABLE categories (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NULL REFERENCES users(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    kind        TEXT NOT NULL CHECK (kind IN ('expense','income','transfer')),
    icon        TEXT NULL,
    color       TEXT NULL,
    archived    BOOLEAN NOT NULL DEFAULT false,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Un user no puede tener dos categorias activas con el mismo nombre
-- (comparacion case-insensitive). Las archivadas no cuentan asi se puede
-- renombrar reusando un nombre viejo.
CREATE UNIQUE INDEX idx_categories_user_name_active
    ON categories (user_id, lower(name))
    WHERE archived = false AND user_id IS NOT NULL;

-- Y los defaults globales (user_id null) tampoco pueden duplicarse entre si.
CREATE UNIQUE INDEX idx_categories_global_name_active
    ON categories (lower(name))
    WHERE archived = false AND user_id IS NULL;

CREATE INDEX idx_categories_user_kind
    ON categories (user_id, kind)
    WHERE archived = false;

-- Transactions. Hypertable sobre occurred_at: las queries del dashboard
-- casi siempre son "ultimo mes" o "ultimos N dias" y ahi chunking pega bien.
-- Chunk de 7 dias porque pensamos ~<100 trx por user por semana, no tiene
-- sentido chunk de 1 dia como weights.
CREATE TABLE transactions (
    id              UUID NOT NULL DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    category_id     UUID NULL REFERENCES categories(id) ON DELETE SET NULL,
    occurred_at     TIMESTAMPTZ NOT NULL,
    amount          NUMERIC(14,2) NOT NULL CHECK (amount <> 0),
    currency        TEXT NOT NULL DEFAULT 'ARS' CHECK (char_length(currency) = 3),
    kind            TEXT NOT NULL CHECK (kind IN ('expense','income','transfer')),
    note            TEXT NULL,
    external_source TEXT NULL,
    external_ref    TEXT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (occurred_at, id)
);

SELECT create_hypertable('transactions', 'occurred_at', chunk_time_interval => INTERVAL '7 days');

CREATE INDEX idx_transactions_user_time
    ON transactions (user_id, occurred_at DESC);

CREATE INDEX idx_transactions_user_category
    ON transactions (user_id, category_id)
    WHERE category_id IS NOT NULL;

-- Para deduplicar webhooks (mismo mp payment id no se inserta dos veces
-- para el mismo user). Partial index: solo cuando el ref vino, no querenos
-- que "null,null" colisione.
CREATE UNIQUE INDEX idx_transactions_external_ref
    ON transactions (user_id, external_source, external_ref)
    WHERE external_ref IS NOT NULL;

-- Defaults globales: los copiamos al user con un Fork en el service la
-- primera vez que carga el modulo plata. Mientras tanto quedan como
-- fallback listable.
INSERT INTO categories (user_id, name, kind, icon, color) VALUES
    (NULL, 'Comida',        'expense', 'utensils', '#f97316'),
    (NULL, 'Super',         'expense', 'shopping-cart', '#ef4444'),
    (NULL, 'Transporte',    'expense', 'bus', '#3b82f6'),
    (NULL, 'Casa',          'expense', 'home', '#8b5cf6'),
    (NULL, 'Salud',         'expense', 'heart-pulse', '#ec4899'),
    (NULL, 'Ocio',          'expense', 'music', '#10b981'),
    (NULL, 'Educacion',     'expense', 'book', '#06b6d4'),
    (NULL, 'Otros',         'expense', 'more-horizontal', '#6b7280'),
    (NULL, 'Sueldo',        'income',  'wallet', '#22c55e'),
    (NULL, 'Freelance',     'income',  'briefcase', '#84cc16'),
    (NULL, 'Reintegro',     'income',  'refresh-cw', '#14b8a6'),
    (NULL, 'Transferencia', 'transfer','arrow-left-right', '#64748b');
