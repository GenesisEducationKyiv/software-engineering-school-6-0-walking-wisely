//go:build integration

package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/outbox"
	subscriptionapp "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions/app"
	subscriptionsdomain "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions/domain"
)

func TestIntegration_SubscribeTransactionalOutbox(t *testing.T) {
	ctx, cancel := integrationContext(t)
	defer cancel()

	repos := newTestRepos(t, ctx)

	t.Run("commits subscription row and outbox event together", func(t *testing.T) {
		truncateAsyncDeliveryState(t, ctx, repos.pool)

		service := subscriptionapp.NewSubscribeService(&subscriptionapp.SubscribeDeps{
			Repo:           repos.token,
			TxManager:      repos.token,
			Github:         testGithubValidator{},
			Publisher:      outbox.NewPublisher(outbox.NewRepository(repos.pool)),
			EmailSecretKey: "test-secret",
		})

		result, err := service.Subscribe(ctx, subscriptionapp.SubscribeCommand{
			Email: "user@example.com",
			Repo:  "owner/repo",
		})
		if err != nil {
			t.Fatalf("Subscribe returned error: %v", err)
		}
		if result.Action != subscriptionsdomain.SubscribeActionCreated {
			t.Fatalf("action = %q, want %q", result.Action, subscriptionsdomain.SubscribeActionCreated)
		}

		assertSubscriptionCount(t, ctx, repos.pool, "user@example.com", "owner/repo", 1)
		assertOutboxCount(t, ctx, repos.pool, "subscriptions.subscription_requested", 1)

		event := loadSubscriptionRequestedEvent(t, ctx, repos.pool)
		if event.SubscriptionID != result.SubscriptionID {
			t.Fatalf("event subscription_id = %q, want %q", event.SubscriptionID, result.SubscriptionID)
		}
		if event.Email != "user@example.com" || event.Repo != "owner/repo" {
			t.Fatalf("event = %#v, want normalized email/repo payload", event)
		}
		if event.ConfirmToken == "" || event.UnsubToken == "" {
			t.Fatalf("event tokens should be populated: %#v", event)
		}
	})

	t.Run("rolls back subscription when publish fails inside transaction", func(t *testing.T) {
		truncateAsyncDeliveryState(t, ctx, repos.pool)

		service := subscriptionapp.NewSubscribeService(&subscriptionapp.SubscribeDeps{
			Repo:           repos.token,
			TxManager:      repos.token,
			Github:         testGithubValidator{},
			Publisher:      failAfterPublish(t, outbox.NewPublisher(outbox.NewRepository(repos.pool)), errors.New("boom")),
			EmailSecretKey: "test-secret",
		})

		_, err := service.Subscribe(ctx, subscriptionapp.SubscribeCommand{
			Email: "user@example.com",
			Repo:  "owner/repo",
		})
		if err == nil || !strings.Contains(err.Error(), "publish subscription requested") {
			t.Fatalf("Subscribe error = %v, want publish failure", err)
		}

		assertSubscriptionCount(t, ctx, repos.pool, "user@example.com", "owner/repo", 0)
		assertOutboxCount(t, ctx, repos.pool, "subscriptions.subscription_requested", 0)
	})

	t.Run("retrying same unconfirmed subscription keeps one logical row and one outbox event", func(t *testing.T) {
		truncateAsyncDeliveryState(t, ctx, repos.pool)

		service := subscriptionapp.NewSubscribeService(&subscriptionapp.SubscribeDeps{
			Repo:           repos.token,
			TxManager:      repos.token,
			Github:         testGithubValidator{},
			Publisher:      outbox.NewPublisher(outbox.NewRepository(repos.pool)),
			EmailSecretKey: "test-secret",
		})

		first, err := service.Subscribe(ctx, subscriptionapp.SubscribeCommand{
			Email: "user@example.com",
			Repo:  "owner/repo",
		})
		if err != nil {
			t.Fatalf("first Subscribe returned error: %v", err)
		}
		second, err := service.Subscribe(ctx, subscriptionapp.SubscribeCommand{
			Email: "user@example.com",
			Repo:  "owner/repo",
		})
		if err != nil {
			t.Fatalf("second Subscribe returned error: %v", err)
		}

		if second.Action != subscriptionsdomain.SubscribeActionConfirmationRefreshed {
			t.Fatalf("second action = %q, want %q", second.Action, subscriptionsdomain.SubscribeActionConfirmationRefreshed)
		}
		if second.SubscriptionID != first.SubscriptionID {
			t.Fatalf("subscription_id = %q, want %q", second.SubscriptionID, first.SubscriptionID)
		}

		assertSubscriptionCount(t, ctx, repos.pool, "user@example.com", "owner/repo", 1)
		assertOutboxCount(t, ctx, repos.pool, "subscriptions.subscription_requested", 1)
	})
}

type testGithubValidator struct {
	err error
}

func (v testGithubValidator) ValidateRepo(context.Context, string) error {
	return v.err
}
