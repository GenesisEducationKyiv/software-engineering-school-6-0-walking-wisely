//go:build integration

package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/contracts"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/logger"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/outbox"
	releasemonitoringapp "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/release_monitoring/app"
	releasemonitoringpostgres "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/release_monitoring/postgres"
)

func TestIntegration_ReleaseScanTransactionalOutbox(t *testing.T) {
	ctx, cancel := integrationContext(t)
	defer cancel()

	repos := newTestRepos(t, ctx)

	t.Run("commits last_seen_tag and outbox event together", func(t *testing.T) {
		truncateAsyncDeliveryState(t, ctx, repos.pool)
		subscriptionID := mustInsertSubscription(t, ctx, repos.pool, subscriptionSeed{
			Email:            "user@example.com",
			Repo:             "owner/repo",
			Confirmed:        true,
			ConfirmToken:     "confirm-token",
			UnsubscribeToken: "unsub-token",
		})
		releaseScanRepo := releasemonitoringpostgres.NewReleaseScanRepo(repos.pool, logger.NoopLogger{})

		service := releasemonitoringapp.NewScannerService(&releasemonitoringapp.ScannerDeps{
			Repo:      releaseScanRepo,
			GitHub:    stubReleaseClient{release: &contracts.Release{TagName: "v1.2.3", HTMLURL: "https://github.com/owner/repo/releases/v1.2.3", Name: "Release 1.2.3"}},
			TxManager: releaseScanRepo,
			Publisher: outbox.NewPublisher(outbox.NewRepository(repos.pool)),
		})

		service.Scan(ctx)

		assertLastSeenTag(t, ctx, repos.pool, subscriptionID, "v1.2.3")
		assertOutboxCount(t, ctx, repos.pool, "release_monitoring.release_detected", 1)

		event := loadReleaseDetectedEvent(t, ctx, repos.pool)
		if event.Repo != "owner/repo" || event.Release.TagName != "v1.2.3" {
			t.Fatalf("event = %#v, want repo owner/repo and tag v1.2.3", event)
		}
		if len(event.Subscribers) != 1 || event.Subscribers[0].SubscriptionID != subscriptionID {
			t.Fatalf("subscribers = %#v, want seeded subscriber only", event.Subscribers)
		}
	})

	t.Run("does not advance last_seen_tag when publish fails", func(t *testing.T) {
		truncateAsyncDeliveryState(t, ctx, repos.pool)
		subscriptionID := mustInsertSubscription(t, ctx, repos.pool, subscriptionSeed{
			Email:            "user@example.com",
			Repo:             "owner/repo",
			Confirmed:        true,
			ConfirmToken:     "confirm-token",
			UnsubscribeToken: "unsub-token",
		})
		releaseScanRepo := releasemonitoringpostgres.NewReleaseScanRepo(repos.pool, logger.NoopLogger{})

		service := releasemonitoringapp.NewScannerService(&releasemonitoringapp.ScannerDeps{
			Repo:      releaseScanRepo,
			GitHub:    stubReleaseClient{release: &contracts.Release{TagName: "v1.2.3", HTMLURL: "https://github.com/owner/repo/releases/v1.2.3"}},
			TxManager: releaseScanRepo,
			Publisher: failAfterPublish(t, outbox.NewPublisher(outbox.NewRepository(repos.pool)), errors.New("boom")),
		})

		service.Scan(ctx)

		assertNullLastSeenTag(t, ctx, repos.pool, subscriptionID)
		assertOutboxCount(t, ctx, repos.pool, "release_monitoring.release_detected", 0)
	})

	t.Run("does not create outbox row when there is no new release", func(t *testing.T) {
		truncateAsyncDeliveryState(t, ctx, repos.pool)
		tag := "v1.2.3"
		subscriptionID := mustInsertSubscription(t, ctx, repos.pool, subscriptionSeed{
			Email:            "user@example.com",
			Repo:             "owner/repo",
			Confirmed:        true,
			ConfirmToken:     "confirm-token",
			UnsubscribeToken: "unsub-token",
			LastSeenTag:      &tag,
		})
		releaseScanRepo := releasemonitoringpostgres.NewReleaseScanRepo(repos.pool, logger.NoopLogger{})

		service := releasemonitoringapp.NewScannerService(&releasemonitoringapp.ScannerDeps{
			Repo:      releaseScanRepo,
			GitHub:    stubReleaseClient{release: &contracts.Release{TagName: "v1.2.3", HTMLURL: "https://github.com/owner/repo/releases/v1.2.3"}},
			TxManager: releaseScanRepo,
			Publisher: outbox.NewPublisher(outbox.NewRepository(repos.pool)),
		})

		service.Scan(ctx)

		assertLastSeenTag(t, ctx, repos.pool, subscriptionID, "v1.2.3")
		assertOutboxCount(t, ctx, repos.pool, "release_monitoring.release_detected", 0)
	})
}

type stubReleaseClient struct {
	release *contracts.Release
	err     error
}

func (c stubReleaseClient) GetLatestRelease(context.Context, string) (*contracts.Release, error) {
	return c.release, c.err
}
