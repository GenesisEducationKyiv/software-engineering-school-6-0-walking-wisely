//go:build integration

package postgres

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/events"
	releasemonitoringdomain "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/release_monitoring/domain"
	subscriptionapp "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions/app"
)

func truncateAsyncDeliveryState(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()

	if _, err := pool.Exec(ctx, `TRUNCATE notification_jobs, event_deliveries, outbox_events, subscriptions RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("truncate async delivery tables: %v", err)
	}
}

func assertSubscriptionCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, email, repo string, want int) {
	t.Helper()

	var got int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM subscriptions WHERE email=$1 AND repo=$2`, email, repo).Scan(&got); err != nil {
		t.Fatalf("count subscriptions: %v", err)
	}
	if got != want {
		t.Fatalf("subscription count = %d, want %d", got, want)
	}
}

func assertOutboxCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, eventType string, want int) {
	t.Helper()

	var got int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM outbox_events WHERE event_type=$1`, eventType).Scan(&got); err != nil {
		t.Fatalf("count outbox rows: %v", err)
	}
	if got != want {
		t.Fatalf("outbox count = %d, want %d", got, want)
	}
}

func loadSubscriptionRequestedEvent(t *testing.T, ctx context.Context, pool *pgxpool.Pool) subscriptionapp.SubscriptionRequested {
	t.Helper()

	var payload []byte
	if err := pool.QueryRow(ctx, `SELECT payload_json FROM outbox_events WHERE event_type=$1`, "subscriptions.subscription_requested").Scan(&payload); err != nil {
		t.Fatalf("select subscription payload: %v", err)
	}

	var event subscriptionapp.SubscriptionRequested
	if err := json.Unmarshal(payload, &event); err != nil {
		t.Fatalf("unmarshal subscription payload: %v", err)
	}
	return event
}

func loadReleaseDetectedEvent(t *testing.T, ctx context.Context, pool *pgxpool.Pool) releasemonitoringdomain.ReleaseDetected {
	t.Helper()

	var payload []byte
	if err := pool.QueryRow(ctx, `SELECT payload_json FROM outbox_events WHERE event_type=$1`, "release_monitoring.release_detected").Scan(&payload); err != nil {
		t.Fatalf("select release payload: %v", err)
	}

	var event releasemonitoringdomain.ReleaseDetected
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
