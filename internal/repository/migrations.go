// Package repository provides PostgreSQL-backed storage for subscriptions and applies embedded SQL migrations.
package repository

import (
	"embed"
	"errors"
	"fmt"
	"log/slog"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres" // registers the postgres driver with golang-migrate
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// RunMigrations applies all pending SQL migrations embedded in the migrations directory.
func RunMigrations(databaseURL string) error {
	sourceDriver, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("create migration source: %w", err)
	}

	m, err := migrate.NewWithSourceInstance("iofs", sourceDriver, databaseURL)
	if err != nil {
		return fmt.Errorf("initialize migrator: %w", err)
	}

	slog.Info("running database migrations")

	if err := m.Up(); err != nil {
		if errors.Is(err, migrate.ErrNoChange) {
			slog.Info("database already up to date, no migrations needed")
			return nil
		}
		return fmt.Errorf("run migrations: %w", err)
	}

	slog.Info("migrations applied successfully")
	return nil
}
