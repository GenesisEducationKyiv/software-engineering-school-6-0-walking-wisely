//go:build integration

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/logger"
	platformmigrations "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/postgres/migrations"
)

type testRepos struct {
	token       *TokenRepo
	read        *ReadRepo
	releaseScan *ReleaseScanRepo
	pool        *pgxpool.Pool
}

func TestIntegration_SubscriptionRepos(t *testing.T) {
	ctx, cancel := integrationContext(t)
	defer cancel()

	repos := newTestRepos(t, ctx)

	testTokenRepoSubscribe(t, ctx, repos)
	testTokenRepoConfirmByToken(t, ctx, repos)
	testTokenRepoUnsubscribeByToken(t, ctx, repos)
	testReadRepoListByEmail(t, ctx, repos)
	testReleaseScanRepoListDistinctConfirmedRepos(t, ctx, repos)
	testReleaseScanRepoListConfirmedSubscribersForRepo(t, ctx, repos)
	testReleaseScanRepoUpdateLastSeenTag(t, ctx, repos)
}

func newTestRepos(t *testing.T, ctx context.Context) testRepos {
	t.Helper()

	testcontainers.SkipIfProviderIsNotHealthy(t)

	container, err := tcpostgres.Run(
		ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("app"),
		tcpostgres.WithUsername("app"),
		tcpostgres.WithPassword("secret"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}
	t.Cleanup(func() {
		if err := container.Terminate(context.Background()); err != nil {
			t.Logf("terminate postgres container: %v", err)
		}
	})

	databaseURL, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("build postgres connection string: %v", err)
	}
	if err := platformmigrations.Run(databaseURL, logger.NoopLogger{}); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect to postgres: %v", err)
	}
	t.Cleanup(pool.Close)

	return testRepos{
		token:       NewTokenRepo(pool, logger.NoopLogger{}),
		read:        NewReadRepo(pool, logger.NoopLogger{}),
		releaseScan: NewReleaseScanRepo(pool, logger.NoopLogger{}),
		pool:        pool,
	}
}

func integrationContext(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), 2*time.Minute)
}

func truncateSubscriptions(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()

	truncateAsyncDeliveryState(t, ctx, pool)
}

type subscriptionSeed struct {
	Email            string
	Repo             string
	Confirmed        bool
	ConfirmToken     string
	UnsubscribeToken string
	LastSeenTag      *string
	CreatedAt        time.Time
}

func mustInsertSubscription(t *testing.T, ctx context.Context, pool *pgxpool.Pool, seed subscriptionSeed) string {
	t.Helper()

	createdAt := seed.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	var id string
	err := pool.QueryRow(ctx, `
		INSERT INTO subscriptions (
			email, repo, confirmed, confirm_token, unsubscribe_token, last_seen_tag, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $7)
		RETURNING id
	`, seed.Email, seed.Repo, seed.Confirmed, seed.ConfirmToken, seed.UnsubscribeToken, seed.LastSeenTag, createdAt).Scan(&id)
	if err != nil {
		t.Fatalf("insert subscription seed: %v", err)
	}
	return id
}

func assertConfirmed(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id string, want bool) {
	t.Helper()

	var got bool
	if err := pool.QueryRow(ctx, `SELECT confirmed FROM subscriptions WHERE id=$1`, id).Scan(&got); err != nil {
		t.Fatalf("query confirmed: %v", err)
	}
	if got != want {
		t.Fatalf("confirmed = %v, want %v", got, want)
	}
}

func assertLastSeenTag(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id, want string) {
	t.Helper()

	var got *string
	if err := pool.QueryRow(ctx, `SELECT last_seen_tag FROM subscriptions WHERE id=$1`, id).Scan(&got); err != nil {
		t.Fatalf("query last_seen_tag: %v", err)
	}
	if got == nil || *got != want {
		t.Fatalf("last_seen_tag = %#v, want %q", got, want)
	}
}

func assertNullLastSeenTag(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id string) {
	t.Helper()

	var got *string
	if err := pool.QueryRow(ctx, `SELECT last_seen_tag FROM subscriptions WHERE id=$1`, id).Scan(&got); err != nil {
		t.Fatalf("query last_seen_tag: %v", err)
	}
	if got != nil {
		t.Fatalf("last_seen_tag = %q, want NULL", *got)
	}
}

func assertUpdatedAfter(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id string, cutoff time.Time) {
	t.Helper()

	var got time.Time
	if err := pool.QueryRow(ctx, `SELECT updated_at FROM subscriptions WHERE id=$1`, id).Scan(&got); err != nil {
		t.Fatalf("query updated_at: %v", err)
	}
	if !got.After(cutoff) {
		t.Fatalf("updated_at = %s, want after %s", got.Format(time.RFC3339Nano), cutoff.Format(time.RFC3339Nano))
	}
}

func assertUniqueViolation(t *testing.T, err error, wantConstraint string) {
	t.Helper()

	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("error = %v, want postgres unique violation", err)
	}
	if pgErr.Code != "23505" {
		t.Fatalf("postgres error code = %s, want 23505", pgErr.Code)
	}
	if pgErr.ConstraintName != wantConstraint {
		t.Fatalf("constraint = %q, want %q", pgErr.ConstraintName, wantConstraint)
	}
}
