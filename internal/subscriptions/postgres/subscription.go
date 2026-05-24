package postgres

import (
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/logger"
)

type repository struct {
	db  *pgxpool.Pool
	log logger.Logger
}

func newRepository(db *pgxpool.Pool, log logger.Logger) repository {
	if log == nil {
		log = logger.NoopLogger{}
	}
	return repository{db: db, log: log}
}

// TokenRepo persists token-mediated subscription lifecycle changes.
type TokenRepo struct {
	repository
}

// NewTokenRepo returns a TokenRepo backed by the given connection pool.
func NewTokenRepo(db *pgxpool.Pool, log logger.Logger) *TokenRepo {
	return &TokenRepo{repository: newRepository(db, log)}
}

// ReadRepo reads subscription state for user-facing APIs.
type ReadRepo struct {
	repository
}

// NewReadRepo returns a ReadRepo backed by the given connection pool.
func NewReadRepo(db *pgxpool.Pool, log logger.Logger) *ReadRepo {
	return &ReadRepo{repository: newRepository(db, log)}
}

// ReleaseScanRepo provides subscription state needed by the release scanner.
type ReleaseScanRepo struct {
	repository
}

// NewReleaseScanRepo returns a ReleaseScanRepo backed by the given connection pool.
func NewReleaseScanRepo(db *pgxpool.Pool, log logger.Logger) *ReleaseScanRepo {
	return &ReleaseScanRepo{repository: newRepository(db, log)}
}
