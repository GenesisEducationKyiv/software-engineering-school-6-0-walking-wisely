//go:build integration

package postgres

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	subscriptioncmds "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/contracts/commands"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/events"
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

func loadSendConfirmationEmailCommand(t *testing.T, ctx context.Context, pool *pgxpool.Pool) subscriptioncmds.SendConfirmationEmail {
	t.Helper()

	var payload []byte
	if err := pool.QueryRow(ctx, `SELECT payload_json FROM outbox_events WHERE event_type=$1`, "subscriptions.send_confirmation_email").Scan(&payload); err != nil {
		t.Fatalf("select send_confirmation_email payload: %v", err)
	}

	var cmd subscriptioncmds.SendConfirmationEmail
	if err := json.Unmarshal(payload, &cmd); err != nil {
		t.Fatalf("unmarshal send_confirmation_email payload: %v", err)
	}
	return cmd
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
