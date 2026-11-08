package db

import (
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

// MigrateUp aplica todas las migrations pendientes.
// sourceURL tipico: file://db/migrations
// dsn el mismo que usamos para el pool pero en formato pgx://... o postgres://...
func MigrateUp(sourceURL, dsn string) error {
	m, err := migrate.New(sourceURL, dsn)
	if err != nil {
		return fmt.Errorf("new migrator: %w", err)
	}
	defer m.Close()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate up: %w", err)
	}
	return nil
}

// MigrateDown rollback de una sola step. Lo uso en tests y scripts locales.
func MigrateDown(sourceURL, dsn string) error {
	m, err := migrate.New(sourceURL, dsn)
	if err != nil {
		return fmt.Errorf("new migrator: %w", err)
	}
	defer m.Close()

	if err := m.Steps(-1); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate down: %w", err)
	}
	return nil
}

// MigrateVersion devuelve la version actual de la db, util para /healthz detallado.
func MigrateVersion(sourceURL, dsn string) (uint, bool, error) {
	m, err := migrate.New(sourceURL, dsn)
	if err != nil {
		return 0, false, fmt.Errorf("new migrator: %w", err)
	}
	defer m.Close()

	v, dirty, err := m.Version()
	if err != nil && !errors.Is(err, migrate.ErrNilVersion) {
		return 0, false, fmt.Errorf("version: %w", err)
	}
	return v, dirty, nil
}
