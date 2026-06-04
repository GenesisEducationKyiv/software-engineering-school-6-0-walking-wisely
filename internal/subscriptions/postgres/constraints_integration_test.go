//go:build integration

package postgres

import (
	"context"
	"testing"
)

func TestIntegration_SubscriptionConstraints(t *testing.T) {
	ctx, cancel := integrationContext(t)
	defer cancel()

	repos := newTestRepos(t, ctx)

	testSubscriptionUniqueConstraints(t, ctx, repos)
}

func testSubscriptionUniqueConstraints(t *testing.T, ctx context.Context, repos testRepos) {
	t.Helper()

	t.Run("enforces unique subscription tokens", func(t *testing.T) {
		truncateSubscriptions(t, ctx, repos.pool)
		mustInsertSubscription(t, ctx, repos.pool, subscriptionSeed{
			Email:            "one@example.com",
			Repo:             "owner/one",
			ConfirmToken:     "confirm-token",
			UnsubscribeToken: "unsub-one",
		})

		_, err := repos.pool.Exec(ctx, `
			INSERT INTO subscriptions (email, repo, confirm_token, unsubscribe_token)
			VALUES ($1, $2, $3, $4)
		`, "two@example.com", "owner/two", "confirm-token", "unsub-two")
		assertUniqueViolation(t, err, "idx_subscriptions_confirm_token")

		_, err = repos.pool.Exec(ctx, `
			INSERT INTO subscriptions (email, repo, confirm_token, unsubscribe_token)
			VALUES ($1, $2, $3, $4)
		`, "three@example.com", "owner/three", "confirm-three", "unsub-one")
		assertUniqueViolation(t, err, "idx_subscriptions_unsubscribe_tok")
	})
}
