DROP INDEX IF EXISTS idx_weights_source;
DROP INDEX IF EXISTS idx_weights_user_time;
DROP TABLE IF EXISTS weights;

DROP INDEX IF EXISTS idx_users_email;
DROP TABLE IF EXISTS users;

-- No bajamos timescaledb por las dudas haya otras tablas usandolo.
