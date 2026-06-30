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
			Repo:      repos.token,
			TxManager: repos.token,
			Github:    testGithubValidator{},
			Orchestrator: subscriptionapp.NewSagaOrchestrator(&subscriptionapp.SagaOrchestratorDeps{
				SagaRepo:  NewSagaRepository(repos.pool),
				SubRepo:   repos.token,
				TxManager: repos.token,
				Publisher: outbox.NewPublisher(outbox.NewRepository(repos.pool)),
			}),
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
		assertOutboxCount(t, ctx, repos.pool, "subscriptions.send_confirmation_email", 1)

		cmd := loadSendConfirmationEmailCommand(t, ctx, repos.pool)
		if cmd.SubscriptionID != result.SubscriptionID {
			t.Fatalf("cmd subscription_id = %q, want %q", cmd.SubscriptionID, result.SubscriptionID)
		}
		if cmd.Email != "user@example.com" || cmd.Repo != "owner/repo" {
			t.Fatalf("cmd = %#v, want normalized email/repo payload", cmd)
		}
		if cmd.ConfirmToken == "" || cmd.UnsubToken == "" {
			t.Fatalf("cmd tokens should be populated: %#v", cmd)
		}
	})

	t.Run("rolls back subscription when publish fails inside transaction", func(t *testing.T) {
		truncateAsyncDeliveryState(t, ctx, repos.pool)

		service := subscriptionapp.NewSubscribeService(&subscriptionapp.SubscribeDeps{
			Repo:      repos.token,
			TxManager: repos.token,
			Github:    testGithubValidator{},
			Orchestrator: subscriptionapp.NewSagaOrchestrator(&subscriptionapp.SagaOrchestratorDeps{
				SagaRepo:  NewSagaRepository(repos.pool),
				SubRepo:   repos.token,
				TxManager: repos.token,
				Publisher: failAfterPublish(t, outbox.NewPublisher(outbox.NewRepository(repos.pool)), errors.New("boom")),
			}),
			EmailSecretKey: "test-secret",
		})

		_, err := service.Subscribe(ctx, subscriptionapp.SubscribeCommand{
			Email: "user@example.com",
			Repo:  "owner/repo",
		})
		if err == nil || !strings.Contains(err.Error(), "enqueue saga") {
			t.Fatalf("Subscribe error = %v, want enqueue failure", err)
		}

		assertSubscriptionCount(t, ctx, repos.pool, "user@example.com", "owner/repo", 0)
		assertOutboxCount(t, ctx, repos.pool, "subscriptions.send_confirmation_email", 0)
	})

	t.Run("retrying same unconfirmed subscription keeps one logical row and records refreshed confirmation event", func(t *testing.T) {
		truncateAsyncDeliveryState(t, ctx, repos.pool)

		service := subscriptionapp.NewSubscribeService(&subscriptionapp.SubscribeDeps{
			Repo:      repos.token,
			TxManager: repos.token,
			Github:    testGithubValidator{},
			Orchestrator: subscriptionapp.NewSagaOrchestrator(&subscriptionapp.SagaOrchestratorDeps{
				SagaRepo:  NewSagaRepository(repos.pool),
				SubRepo:   repos.token,
				TxManager: repos.token,
				Publisher: outbox.NewPublisher(outbox.NewRepository(repos.pool)),
			}),
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
		assertOutboxCount(t, ctx, repos.pool, "subscriptions.send_confirmation_email", 2)
	})
}

type testGithubValidator struct {
	err error
}

func (v testGithubValidator) ValidateRepo(context.Context, string) error {
	return v.err
}
