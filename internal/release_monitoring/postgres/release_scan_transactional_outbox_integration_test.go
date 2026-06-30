//go:build integration

package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/contracts"
	contractevents "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/contracts/events"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/events"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/logger"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/outbox"
	releasemonitoringapp "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/release_monitoring/app"
)

func TestIntegration_ReleaseScanTransactionalOutbox(t *testing.T) {
	ctx, cancel := integrationContext(t)
	defer cancel()

	releaseScanRepo, pool := newReleaseScanTestDB(t, ctx)

	t.Run("commits last_seen_tag and outbox event together", func(t *testing.T) {
		truncateReleaseScanDeliveryState(t, ctx, pool)
		subscriptionID := insertReleaseScanSubscription(t, ctx, pool, releaseScanSubscriptionSeed{
			Email:            "user@example.com",
			Repo:             "owner/repo",
			Confirmed:        true,
			ConfirmToken:     "confirm-token",
			UnsubscribeToken: "unsub-token",
		})

		service := releasemonitoringapp.NewScannerService(&releasemonitoringapp.ScannerDeps{
			Repo:      releaseScanRepo,
			GitHub:    stubReleaseClient{release: &contracts.Release{TagName: "v1.2.3", HTMLURL: "https://github.com/owner/repo/releases/v1.2.3", Name: "Release 1.2.3"}},
			TxManager: releaseScanRepo,
			Publisher: outbox.NewPublisher(outbox.NewRepository(pool)),
			Log:       logger.NoopLogger{},
		})

		service.Scan(ctx)

		assertReleaseScanLastSeenTag(t, ctx, pool, subscriptionID, "v1.2.3")
		assertReleaseScanOutboxCount(t, ctx, pool, "release_monitoring.release_detected", 1)

		event := loadReleaseDetectedEvent(t, ctx, pool)
		if event.Repo != "owner/repo" || event.Release.TagName != "v1.2.3" {
			t.Fatalf("event = %#v, want repo owner/repo and tag v1.2.3", event)
		}
		if len(event.Subscribers) != 1 || event.Subscribers[0].SubscriptionID != subscriptionID {
			t.Fatalf("subscribers = %#v, want seeded subscriber only", event.Subscribers)
		}
	})

	t.Run("does not advance last_seen_tag when publish fails", func(t *testing.T) {
		truncateReleaseScanDeliveryState(t, ctx, pool)
		subscriptionID := insertReleaseScanSubscription(t, ctx, pool, releaseScanSubscriptionSeed{
			Email:            "user@example.com",
			Repo:             "owner/repo",
			Confirmed:        true,
			ConfirmToken:     "confirm-token",
			UnsubscribeToken: "unsub-token",
		})

		service := releasemonitoringapp.NewScannerService(&releasemonitoringapp.ScannerDeps{
			Repo:      releaseScanRepo,
			GitHub:    stubReleaseClient{release: &contracts.Release{TagName: "v1.2.3", HTMLURL: "https://github.com/owner/repo/releases/v1.2.3"}},
			TxManager: releaseScanRepo,
			Publisher: failAfterPublish(t, outbox.NewPublisher(outbox.NewRepository(pool)), errors.New("boom")),
			Log:       logger.NoopLogger{},
		})

		service.Scan(ctx)

		assertReleaseScanNullLastSeenTag(t, ctx, pool, subscriptionID)
		assertReleaseScanOutboxCount(t, ctx, pool, "release_monitoring.release_detected", 0)
	})

	t.Run("does not create outbox row when there is no new release", func(t *testing.T) {
		truncateReleaseScanDeliveryState(t, ctx, pool)
		tag := "v1.2.3"
		subscriptionID := insertReleaseScanSubscription(t, ctx, pool, releaseScanSubscriptionSeed{
			Email:            "user@example.com",
			Repo:             "owner/repo",
			Confirmed:        true,
			ConfirmToken:     "confirm-token",
			UnsubscribeToken: "unsub-token",
			LastSeenTag:      &tag,
		})

		service := releasemonitoringapp.NewScannerService(&releasemonitoringapp.ScannerDeps{
			Repo:      releaseScanRepo,
			GitHub:    stubReleaseClient{release: &contracts.Release{TagName: "v1.2.3", HTMLURL: "https://github.com/owner/repo/releases/v1.2.3"}},
			TxManager: releaseScanRepo,
			Publisher: outbox.NewPublisher(outbox.NewRepository(pool)),
			Log:       logger.NoopLogger{},
		})

		service.Scan(ctx)

		assertReleaseScanLastSeenTag(t, ctx, pool, subscriptionID, "v1.2.3")
		assertReleaseScanOutboxCount(t, ctx, pool, "release_monitoring.release_detected", 0)
	})
}

type stubReleaseClient struct {
	release *contracts.Release
	err     error
}

func (c stubReleaseClient) GetLatestRelease(context.Context, string) (*contracts.Release, error) {
	return c.release, c.err
}

type releaseScanSubscriptionSeed struct {
	Email            string
	Repo             string
	Confirmed        bool
	ConfirmToken     string
	UnsubscribeToken string
	LastSeenTag      *string
}

func insertReleaseScanSubscription(t *testing.T, ctx context.Context, pool *pgxpool.Pool, seed releaseScanSubscriptionSeed) string { //nolint:gocritic
	t.Helper()

	var id string
	err := pool.QueryRow(ctx, `
		INSERT INTO subscriptions (
			email, repo, confirmed, confirm_token, unsubscribe_token, last_seen_tag
		)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id
	`, seed.Email, seed.Repo, seed.Confirmed, seed.ConfirmToken, seed.UnsubscribeToken, seed.LastSeenTag).Scan(&id)
	if err != nil {
		t.Fatalf("insert subscription seed: %v", err)
	}
	return id
}

func truncateReleaseScanDeliveryState(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()

	if _, err := pool.Exec(ctx, `TRUNCATE notification_jobs, event_deliveries, outbox_events, subscriptions RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("truncate async delivery tables: %v", err)
	}
}

func assertReleaseScanLastSeenTag(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id, want string) {
	t.Helper()

	var got *string
	if err := pool.QueryRow(ctx, `SELECT last_seen_tag FROM subscriptions WHERE id=$1`, id).Scan(&got); err != nil {
		t.Fatalf("query last_seen_tag: %v", err)
	}
	if got == nil || *got != want {
		t.Fatalf("last_seen_tag = %#v, want %q", got, want)
	}
}

func assertReleaseScanNullLastSeenTag(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id string) {
	t.Helper()

	var got *string
	if err := pool.QueryRow(ctx, `SELECT last_seen_tag FROM subscriptions WHERE id=$1`, id).Scan(&got); err != nil {
		t.Fatalf("query last_seen_tag: %v", err)
	}
	if got != nil {
		t.Fatalf("last_seen_tag = %q, want NULL", *got)
	}
}

func assertReleaseScanOutboxCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, eventType string, want int) {
	t.Helper()

	var got int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM outbox_events WHERE event_type=$1`, eventType).Scan(&got); err != nil {
		t.Fatalf("count outbox rows: %v", err)
	}
	if got != want {
		t.Fatalf("outbox count = %d, want %d", got, want)
	}
}

func loadReleaseDetectedEvent(t *testing.T, ctx context.Context, pool *pgxpool.Pool) contractevents.ReleaseDetected {
	t.Helper()

	var payload []byte
	if err := pool.QueryRow(ctx, `SELECT payload_json FROM outbox_events WHERE event_type=$1`, "release_monitoring.release_detected").Scan(&payload); err != nil {
		t.Fatalf("select release payload: %v", err)
	}

	var event contractevents.ReleaseDetected
	if err := json.Unmarshal(payload, &event); err != nil {
		t.Fatalf("unmarshal release payload: %v", err)
	}
	return event
}

type failAfterPublishPublisher struct {
	t    *testing.T
	next events.Publisher
	err  error
}

func failAfterPublish(t *testing.T, next events.Publisher, err error) failAfterPublishPublisher {
	t.Helper()
	return failAfterPublishPublisher{t: t, next: next, err: err}
}

func (p failAfterPublishPublisher) Publish(ctx context.Context, event events.Event) error {
	p.t.Helper()
	if err := p.next.Publish(ctx, event); err != nil {
		return err
	}
	return p.err
}
