package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/logger"
	platformpostgres "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/postgres"
	releasemonitoringdomain "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/release_monitoring/domain"
)

type ReleaseScanRepo struct {
	db  *pgxpool.Pool
	log logger.Logger
}

func NewReleaseScanRepo(db *pgxpool.Pool, log logger.Logger) *ReleaseScanRepo {
	if log == nil {
		log = logger.NoopLogger{}
	}
	return &ReleaseScanRepo{db: db, log: log}
}

func (r *ReleaseScanRepo) WithinTransaction(ctx context.Context, fn func(context.Context) error) error {
	return platformpostgres.WithinTransaction(ctx, r.db, fn)
}

func (r *ReleaseScanRepo) ListDistinctConfirmedRepos(ctx context.Context) ([]string, error) {
	exec := platformpostgres.ExecutorFromContext(ctx, r.db)
	rows, err := exec.Query(ctx, `SELECT DISTINCT repo FROM subscriptions WHERE confirmed=TRUE`)
	if err != nil {
		return nil, fmt.Errorf("list distinct repos: %w", err)
	}
	defer rows.Close()

	var repos []string
	for rows.Next() {
		var repo string
		if err := rows.Scan(&repo); err != nil {
			return nil, fmt.Errorf("scan repo: %w", err)
		}
		repos = append(repos, repo)
	}
	return repos, rows.Err()
}

func (r *ReleaseScanRepo) ListConfirmedSubscribersForRepo(ctx context.Context, repo string) ([]releasemonitoringdomain.Subscriber, error) {
	exec := platformpostgres.ExecutorFromContext(ctx, r.db)
	rows, err := exec.Query(
		ctx,
		`SELECT id, email, repo, last_seen_tag, unsubscribe_token
		 FROM subscriptions WHERE repo=$1 AND confirmed=TRUE`,
		repo,
	)
	if err != nil {
		return nil, fmt.Errorf("list subscribers for repo: %w", err)
	}
	defer rows.Close()

	var subscribers []releasemonitoringdomain.Subscriber
	for rows.Next() {
		var subscriber releasemonitoringdomain.Subscriber
		if err := rows.Scan(
			&subscriber.SubscriptionID,
			&subscriber.Email,
			&subscriber.Repo,
			&subscriber.LastSeenTag,
			&subscriber.UnsubscribeToken,
		); err != nil {
			return nil, fmt.Errorf("scan subscriber: %w", err)
		}
		subscribers = append(subscribers, subscriber)
	}
	return subscribers, rows.Err()
}

func (r *ReleaseScanRepo) UpdateLastSeenTag(ctx context.Context, repo, tag string) error {
	exec := platformpostgres.ExecutorFromContext(ctx, r.db)
	if _, err := exec.Exec(
		ctx,
		`UPDATE subscriptions SET last_seen_tag=$2, updated_at=NOW()
		 WHERE repo=$1 AND confirmed=TRUE`,
		repo, tag,
	); err != nil {
		return fmt.Errorf("update last seen tag: %w", err)
	}
	return nil
}
