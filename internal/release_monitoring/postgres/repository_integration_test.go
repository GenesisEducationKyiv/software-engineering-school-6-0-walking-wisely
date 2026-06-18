//go:build integration

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/logger"
)

// ── shared setup ──────────────────────────────────────────────────────────────

func newReleaseScanTestDB(t *testing.T, _ context.Context) (*ReleaseScanRepo, *pgxpool.Pool) {
	t.Helper()
	if sharedPool == nil {
		t.Fatal("sharedPool not initialized — TestMain must set it up")
	}
	return NewReleaseScanRepo(sharedPool, logger.NoopLogger{}), sharedPool
}

func integrationContext(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), 2*time.Minute)
}

type subSeed struct {
	email            string
	repo             string
	confirmed        bool
	lastSeenTag      *string
	unsubscribeToken string
}

func insertSub(t *testing.T, ctx context.Context, pool *pgxpool.Pool, s subSeed) string {
	t.Helper()
	if s.unsubscribeToken == "" {
		s.unsubscribeToken = uuid.NewString()
	}
	var id string
	err := pool.QueryRow(
		ctx, `
		INSERT INTO subscriptions (email, repo, confirmed, confirm_token, unsubscribe_token, last_seen_tag)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id`,
		s.email, s.repo, s.confirmed, uuid.NewString(), s.unsubscribeToken, s.lastSeenTag,
	).Scan(&id)
	if err != nil {
		t.Fatalf("insert subscription: %v", err)
	}
	return id
}

func truncateSubs(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `TRUNCATE subscriptions CASCADE`); err != nil {
		t.Fatalf("truncate subscriptions: %v", err)
	}
}

// readLastSeenTag queries last_seen_tag directly from the DB for a given subscription ID.
func readLastSeenTag(t *testing.T, ctx context.Context, pool *pgxpool.Pool, subID string) *string {
	t.Helper()
	var tag *string
	if err := pool.QueryRow(ctx, `SELECT last_seen_tag FROM subscriptions WHERE id=$1`, subID).Scan(&tag); err != nil {
		t.Fatalf("query last_seen_tag for %q: %v", subID, err)
	}
	return tag
}

// ── ListDistinctConfirmedRepos ────────────────────────────────────────────────

func TestIntegration_ListDistinctConfirmedReposEmptyTable(t *testing.T) {
	ctx, cancel := integrationContext(t)
	defer cancel()

	// Arrange
	repo, pool := newReleaseScanTestDB(t, ctx)
	truncateSubs(t, ctx, pool)

	// Act
	repos, err := repo.ListDistinctConfirmedRepos(ctx)
	// Assert
	if err != nil {
		t.Fatalf("ListDistinctConfirmedRepos: %v", err)
	}
	if len(repos) != 0 {
		t.Fatalf("expected empty result, got %v", repos)
	}
}

func TestIntegration_ListDistinctConfirmedRepos(t *testing.T) {
	ctx, cancel := integrationContext(t)
	defer cancel()

	// Arrange
	repo, pool := newReleaseScanTestDB(t, ctx)
	truncateSubs(t, ctx, pool)
	insertSub(t, ctx, pool, subSeed{email: "a@x.com", repo: "owner/repoA", confirmed: true})
	insertSub(t, ctx, pool, subSeed{email: "b@x.com", repo: "owner/repoA", confirmed: true}) // same repo, different subscriber
	insertSub(t, ctx, pool, subSeed{email: "c@x.com", repo: "owner/repoB", confirmed: true})
	insertSub(t, ctx, pool, subSeed{email: "d@x.com", repo: "owner/repoC", confirmed: false}) // unconfirmed — must be excluded

	// Act
	repos, err := repo.ListDistinctConfirmedRepos(ctx)
	// Assert
	if err != nil {
		t.Fatalf("ListDistinctConfirmedRepos: %v", err)
	}
	repoSet := make(map[string]bool, len(repos))
	for _, r := range repos {
		repoSet[r] = true
	}
	if !repoSet["owner/repoA"] {
		t.Error("expected owner/repoA in result")
	}
	if !repoSet["owner/repoB"] {
		t.Error("expected owner/repoB in result")
	}
	if repoSet["owner/repoC"] {
		t.Error("owner/repoC (unconfirmed) must not appear in result")
	}
	if len(repos) != 2 {
		t.Errorf("result count = %d, want 2 (deduplicated confirmed repos)", len(repos))
	}
}

// ── ListConfirmedSubscribersForRepo ───────────────────────────────────────────

func TestIntegration_ListConfirmedSubscribersForRepoFieldsRoundTrip(t *testing.T) {
	ctx, cancel := integrationContext(t)
	defer cancel()

	// Arrange
	repo, pool := newReleaseScanTestDB(t, ctx)
	truncateSubs(t, ctx, pool)
	tag := "v1.0.0"
	unsubToken := uuid.NewString()
	id := insertSub(t, ctx, pool, subSeed{
		email: "a@x.com", repo: "owner/repoA", confirmed: true,
		lastSeenTag: &tag, unsubscribeToken: unsubToken,
	})
	insertSub(t, ctx, pool, subSeed{email: "b@x.com", repo: "owner/repoA", confirmed: false}) // unconfirmed — excluded
	insertSub(t, ctx, pool, subSeed{email: "c@x.com", repo: "owner/repoB", confirmed: true})  // different repo — excluded

	// Act
	subs, err := repo.ListConfirmedSubscribersForRepo(ctx, "owner/repoA")
	// Assert
	if err != nil {
		t.Fatalf("ListConfirmedSubscribersForRepo: %v", err)
	}
	if len(subs) != 1 {
		t.Fatalf("got %d subscribers, want 1", len(subs))
	}
	sub := subs[0]
	if sub.SubscriptionID != id {
		t.Errorf("SubscriptionID = %q, want %q", sub.SubscriptionID, id)
	}
	if sub.Email != "a@x.com" {
		t.Errorf("Email = %q, want a@x.com", sub.Email)
	}
	if sub.Repo != "owner/repoA" {
		t.Errorf("Repo = %q, want owner/repoA", sub.Repo)
	}
	if sub.UnsubscribeToken != unsubToken {
		t.Errorf("UnsubscribeToken = %q, want %q", sub.UnsubscribeToken, unsubToken)
	}
	if sub.LastSeenTag == nil || *sub.LastSeenTag != "v1.0.0" {
		t.Errorf("LastSeenTag = %v, want v1.0.0", sub.LastSeenTag)
	}
}

func TestIntegration_ListConfirmedSubscribersExcludesUnconfirmed(t *testing.T) {
	ctx, cancel := integrationContext(t)
	defer cancel()

	// Arrange
	repo, pool := newReleaseScanTestDB(t, ctx)
	truncateSubs(t, ctx, pool)
	insertSub(t, ctx, pool, subSeed{email: "a@x.com", repo: "owner/repo", confirmed: false})

	// Act
	subs, err := repo.ListConfirmedSubscribersForRepo(ctx, "owner/repo")
	// Assert
	if err != nil {
		t.Fatalf("ListConfirmedSubscribersForRepo: %v", err)
	}
	if len(subs) != 0 {
		t.Fatalf("got %d subscribers, want 0 (unconfirmed must be excluded)", len(subs))
	}
}

// ── UpdateLastSeenTag ─────────────────────────────────────────────────────────

func TestIntegration_UpdateLastSeenTag(t *testing.T) {
	ctx, cancel := integrationContext(t)
	defer cancel()

	// Arrange
	repo, pool := newReleaseScanTestDB(t, ctx)
	truncateSubs(t, ctx, pool)
	confirmedID := insertSub(t, ctx, pool, subSeed{email: "a@x.com", repo: "owner/repo", confirmed: true})

	// Act
	err := repo.UpdateLastSeenTag(ctx, "owner/repo", "v2.0.0")
	// Assert
	if err != nil {
		t.Fatalf("UpdateLastSeenTag: %v", err)
	}
	tag := readLastSeenTag(t, ctx, pool, confirmedID)
	if tag == nil || *tag != "v2.0.0" {
		t.Errorf("last_seen_tag = %v, want v2.0.0", tag)
	}
}

func TestIntegration_UpdateLastSeenTagSkipsUnconfirmedRows(t *testing.T) {
	ctx, cancel := integrationContext(t)
	defer cancel()

	// Arrange — one confirmed, one unconfirmed, both for the same repo.
	repo, pool := newReleaseScanTestDB(t, ctx)
	truncateSubs(t, ctx, pool)
	confirmedID := insertSub(t, ctx, pool, subSeed{email: "a@x.com", repo: "owner/repo", confirmed: true})
	unconfirmedID := insertSub(t, ctx, pool, subSeed{email: "b@x.com", repo: "owner/repo", confirmed: false})

	// Act
	if err := repo.UpdateLastSeenTag(ctx, "owner/repo", "v3.0.0"); err != nil {
		t.Fatalf("UpdateLastSeenTag: %v", err)
	}

	// Assert — confirmed row updated, unconfirmed row unchanged.
	confirmedTag := readLastSeenTag(t, ctx, pool, confirmedID)
	if confirmedTag == nil || *confirmedTag != "v3.0.0" {
		t.Errorf("confirmed last_seen_tag = %v, want v3.0.0", confirmedTag)
	}
	unconfirmedTag := readLastSeenTag(t, ctx, pool, unconfirmedID)
	if unconfirmedTag != nil {
		t.Errorf("unconfirmed last_seen_tag = %v, want nil (must not be updated)", unconfirmedTag)
	}
}

func TestIntegration_UpdateLastSeenTagInsideTransactionRollback(t *testing.T) {
	ctx, cancel := integrationContext(t)
	defer cancel()

	// Arrange
	repo, pool := newReleaseScanTestDB(t, ctx)
	truncateSubs(t, ctx, pool)
	subID := insertSub(t, ctx, pool, subSeed{email: "a@x.com", repo: "owner/repo", confirmed: true})

	// Act — update inside a transaction that is rolled back.
	errDeliberate := errors.New("deliberate rollback")
	err := repo.WithinTransaction(ctx, func(txCtx context.Context) error {
		if err := repo.UpdateLastSeenTag(txCtx, "owner/repo", "v3.0.0"); err != nil {
			t.Fatalf("UpdateLastSeenTag inside tx: %v", err)
		}
		return errDeliberate
	})
	if !errors.Is(err, errDeliberate) {
		t.Fatalf("WithinTransaction returned %v, want %v", err, errDeliberate)
	}

	// Assert — tag must be unchanged because the transaction was rolled back.
	tag := readLastSeenTag(t, ctx, pool, subID)
	if tag != nil {
		t.Errorf("last_seen_tag = %v, want nil (rollback must revert the update)", tag)
	}
}
