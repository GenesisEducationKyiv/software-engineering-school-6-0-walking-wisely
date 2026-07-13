// Package migrations applies the shared PostgreSQL schema used by the service.
package migrations

import (
	"embed"
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres" // registers the postgres driver with golang-migrate
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/logger"
)

//go:embed *.sql
var migrationsFS embed.FS

// Run applies all pending SQL migrations embedded in this package.
func Run(databaseURL string, log logger.Logger) error {
	if log == nil {
		log = logger.NoopLogger{}
	}

	sourceDriver, err := iofs.New(migrationsFS, ".")
	if err != nil {
		return fmt.Errorf("create migration source: %w", err)
	}

	m, err := migrate.NewWithSourceInstance("iofs", sourceDriver, databaseURL)
	if err != nil {
		return fmt.Errorf("initialize migrator: %w", err)
	}

	log.Info("running database migrations")

	if err := m.Up(); err != nil {
		if errors.Is(err, migrate.ErrNoChange) {
			log.Info("database already up to date, no migrations needed")
			return nil
		}
		return fmt.Errorf("run migrations: %w", err)
	}

	log.Info("migrations applied successfully")
	return nil
}
