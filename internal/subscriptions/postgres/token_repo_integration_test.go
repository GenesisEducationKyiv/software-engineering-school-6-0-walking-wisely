//go:build integration

package postgres

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	subscriptionsdomain "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions/domain"
)

func testTokenRepoSubscribe(t *testing.T, ctx context.Context, repos testRepos) {
	t.Helper()
	t.Run("inserts unconfirmed subscription", func(t *testing.T) {
		truncateSubscriptions(t, ctx, repos.pool)

		result, err := repos.token.Subscribe(ctx, "user@example.com", "owner/repo", "confirm-token", "unsub-token")
		if err != nil {
			t.Fatalf("Subscribe returned error: %v", err)
		}
		if result.Action != subscriptionsdomain.SubscribeActionCreated || result.SubscriptionID == "" {
			t.Fatalf("Subscribe result = %+v, want created subscription id", result)
		}

		var got subscriptionsdomain.Subscription
		err = repos.pool.QueryRow(ctx, `
			SELECT id, email, repo, confirmed, confirm_token, unsubscribe_token
			FROM subscriptions
		`).Scan(&got.ID, &got.Email, &got.Repo, &got.Confirmed, &got.ConfirmToken, &got.UnsubscribeToken)
		if err != nil {
			t.Fatalf("query subscription: %v", err)
		}
		if got.ID != result.SubscriptionID {
			t.Fatalf("subscription id = %q, want result id %q", got.ID, result.SubscriptionID)
		}
		if got.Email != "user@example.com" || got.Repo != "owner/repo" || got.Confirmed {
			t.Fatalf("subscription = %#v, want unconfirmed user@example.com owner/repo", got)
		}
		if got.ConfirmToken != "confirm-token" || got.UnsubscribeToken != "unsub-token" {
			t.Fatalf("tokens = (%q, %q), want inserted tokens", got.ConfirmToken, got.UnsubscribeToken)
		}
	})

	t.Run("refreshes token for unconfirmed subscription", func(t *testing.T) {
		truncateSubscriptions(t, ctx, repos.pool)
		createdAt := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
		id := mustInsertSubscription(t, ctx, repos.pool, subscriptionSeed{
			Email:            "user@example.com",
			Repo:             "owner/repo",
			ConfirmToken:     "old-confirm-token",
			UnsubscribeToken: "stable-unsub-token",
			CreatedAt:        createdAt,
		})

		result, err := repos.token.Subscribe(ctx, "user@example.com", "owner/repo", "new-confirm-token", "new-unsub-token")
		if err != nil {
			t.Fatalf("Subscribe returned error: %v", err)
		}
		if result.SubscriptionID != id || result.Action != subscriptionsdomain.SubscribeActionConfirmationRefreshed {
			t.Fatalf("Subscribe result = %+v, want refreshed id %q", result, id)
		}

		var count int
		var confirmToken, unsubscribeToken string
		err = repos.pool.QueryRow(ctx, `
			SELECT COUNT(*), MAX(confirm_token), MAX(unsubscribe_token)
			FROM subscriptions
			WHERE email=$1 AND repo=$2
		`, "user@example.com", "owner/repo").Scan(&count, &confirmToken, &unsubscribeToken)
		if err != nil {
			t.Fatalf("query refreshed subscription: %v", err)
		}
		if count != 1 {
			t.Fatalf("subscription count = %d, want 1", count)
		}
		if confirmToken != "new-confirm-token" {
			t.Fatalf("confirm token = %q, want refreshed token", confirmToken)
		}
		if unsubscribeToken != "stable-unsub-token" {
			t.Fatalf("unsubscribe token = %q, want existing token preserved", unsubscribeToken)
		}
		assertUpdatedAfter(t, ctx, repos.pool, id, createdAt)
	})

	t.Run("returns already subscribed for confirmed subscription", func(t *testing.T) {
		truncateSubscriptions(t, ctx, repos.pool)
		createdAt := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
		id := mustInsertSubscription(t, ctx, repos.pool, subscriptionSeed{
			Email:            "user@example.com",
			Repo:             "owner/repo",
			Confirmed:        true,
			ConfirmToken:     "confirm-token",
			UnsubscribeToken: "unsub-token",
			CreatedAt:        createdAt,
		})

		_, err := repos.token.Subscribe(ctx, "user@example.com", "owner/repo", "new-confirm-token", "new-unsub-token")
		if !errors.Is(err, subscriptionsdomain.ErrAlreadySubscribed) {
			t.Fatalf("Subscribe error = %v, want ErrAlreadySubscribed", err)
		}

		var confirmed bool
		var confirmToken, unsubscribeToken string
		var updatedAt time.Time
		err = repos.pool.QueryRow(ctx, `
			SELECT confirmed, confirm_token, unsubscribe_token, updated_at
			FROM subscriptions
			WHERE id=$1
		`, id).Scan(&confirmed, &confirmToken, &unsubscribeToken, &updatedAt)
		if err != nil {
			t.Fatalf("query confirmed subscription: %v", err)
		}
		if !confirmed {
			t.Fatal("confirmed = false, want true")
		}
		if confirmToken != "confirm-token" {
			t.Fatalf("confirm token = %q, want original token", confirmToken)
		}
		if unsubscribeToken != "unsub-token" {
			t.Fatalf("unsubscribe token = %q, want original token", unsubscribeToken)
		}
		if !updatedAt.Equal(createdAt) {
			t.Fatalf("updated_at = %s, want unchanged %s", updatedAt.Format(time.RFC3339Nano), createdAt.Format(time.RFC3339Nano))
		}
	})

	t.Run("rolls back insert when confirm token is not unique", func(t *testing.T) {
		truncateSubscriptions(t, ctx, repos.pool)
		mustInsertSubscription(t, ctx, repos.pool, subscriptionSeed{
			Email:            "existing@example.com",
			Repo:             "owner/existing",
			ConfirmToken:     "duplicate-confirm-token",
			UnsubscribeToken: "existing-unsub-token",
		})

		_, err := repos.token.Subscribe(
			ctx,
			"new@example.com",
			"owner/new",
			"duplicate-confirm-token",
			"new-unsub-token",
		)
		assertUniqueViolation(t, err, "idx_subscriptions_confirm_token")

		var count int
		if err := repos.pool.QueryRow(ctx, `
			SELECT COUNT(*) FROM subscriptions WHERE email=$1 AND repo=$2
		`, "new@example.com", "owner/new").Scan(&count); err != nil {
			t.Fatalf("count rolled back subscription: %v", err)
		}
		if count != 0 {
			t.Fatalf("subscription count = %d, want 0", count)
		}
	})

	t.Run("rolls back insert when unsubscribe token is not unique", func(t *testing.T) {
		truncateSubscriptions(t, ctx, repos.pool)
		mustInsertSubscription(t, ctx, repos.pool, subscriptionSeed{
			Email:            "existing@example.com",
			Repo:             "owner/existing",
			ConfirmToken:     "existing-confirm-token",
			UnsubscribeToken: "duplicate-unsub-token",
		})

		_, err := repos.token.Subscribe(
			ctx,
			"new@example.com",
			"owner/new",
			"new-confirm-token",
			"duplicate-unsub-token",
		)
		assertUniqueViolation(t, err, "idx_subscriptions_unsubscribe_tok")

		var count int
		if err := repos.pool.QueryRow(ctx, `
			SELECT COUNT(*) FROM subscriptions WHERE email=$1 AND repo=$2
		`, "new@example.com", "owner/new").Scan(&count); err != nil {
			t.Fatalf("count rolled back subscription: %v", err)
		}
		if count != 0 {
			t.Fatalf("subscription count = %d, want 0", count)
		}
	})

	t.Run("rolls back refresh when confirm token is not unique", func(t *testing.T) {
		truncateSubscriptions(t, ctx, repos.pool)
		createdAt := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
		id := mustInsertSubscription(t, ctx, repos.pool, subscriptionSeed{
			Email:            "user@example.com",
			Repo:             "owner/repo",
			ConfirmToken:     "old-confirm-token",
			UnsubscribeToken: "stable-unsub-token",
			CreatedAt:        createdAt,
		})
		mustInsertSubscription(t, ctx, repos.pool, subscriptionSeed{
			Email:            "other@example.com",
			Repo:             "owner/other",
			ConfirmToken:     "duplicate-confirm-token",
			UnsubscribeToken: "other-unsub-token",
		})

		_, err := repos.token.Subscribe(
			ctx,
			"user@example.com",
			"owner/repo",
			"duplicate-confirm-token",
			"ignored-unsub-token",
		)
		assertUniqueViolation(t, err, "idx_subscriptions_confirm_token")

		var confirmToken, unsubscribeToken string
		var updatedAt time.Time
		err = repos.pool.QueryRow(ctx, `
			SELECT confirm_token, unsubscribe_token, updated_at
			FROM subscriptions
			WHERE id=$1
		`, id).Scan(&confirmToken, &unsubscribeToken, &updatedAt)
		if err != nil {
			t.Fatalf("query rolled back subscription: %v", err)
		}
		if confirmToken != "old-confirm-token" {
			t.Fatalf("confirm token = %q, want original token", confirmToken)
		}
		if unsubscribeToken != "stable-unsub-token" {
			t.Fatalf("unsubscribe token = %q, want original token", unsubscribeToken)
		}
		if !updatedAt.Equal(createdAt) {
			t.Fatalf("updated_at = %s, want unchanged %s", updatedAt.Format(time.RFC3339Nano), createdAt.Format(time.RFC3339Nano))
		}
	})

	t.Run("serializes concurrent requests for same email and repo", func(t *testing.T) {
		truncateSubscriptions(t, ctx, repos.pool)

		const workers = 16
		var wg sync.WaitGroup
		errs := make(chan error, workers)

		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				_, err := repos.token.Subscribe(
					ctx,
					"concurrent@example.com",
					"owner/repo",
					fmt.Sprintf("confirm-token-%d", i),
					fmt.Sprintf("unsub-token-%d", i),
				)
				errs <- err
			}(i)
		}
		wg.Wait()
		close(errs)

		for err := range errs {
			if err != nil {
				t.Fatalf("concurrent Subscribe returned error: %v", err)
			}
		}

		var count int
		if err := repos.pool.QueryRow(ctx, `SELECT COUNT(*) FROM subscriptions`).Scan(&count); err != nil {
			t.Fatalf("count subscriptions: %v", err)
		}
		if count != 1 {
			t.Fatalf("subscription count = %d, want 1", count)
		}
	})
}

func testTokenRepoConfirmByToken(t *testing.T, ctx context.Context, repos testRepos) {
	t.Helper()
	t.Run("confirms matching subscription", func(t *testing.T) {
		truncateSubscriptions(t, ctx, repos.pool)
		createdAt := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
		wantID := mustInsertSubscription(t, ctx, repos.pool, subscriptionSeed{
			Email:            "user@example.com",
			Repo:             "owner/repo",
			ConfirmToken:     "confirm-token",
			UnsubscribeToken: "unsub-token",
			CreatedAt:        createdAt,
		})

		gotID, err := repos.token.ConfirmByToken(ctx, "confirm-token")
		if err != nil {
			t.Fatalf("ConfirmByToken returned error: %v", err)
		}
		if gotID != wantID {
			t.Fatalf("ConfirmByToken id = %q, want %q", gotID, wantID)
		}
		assertConfirmed(t, ctx, repos.pool, wantID, true)
		assertUpdatedAfter(t, ctx, repos.pool, wantID, createdAt)
	})

	t.Run("returns token not found for missing token", func(t *testing.T) {
		truncateSubscriptions(t, ctx, repos.pool)

		_, err := repos.token.ConfirmByToken(ctx, "missing-token")
		if !errors.Is(err, subscriptionsdomain.ErrTokenNotFound) {
			t.Fatalf("ConfirmByToken error = %v, want ErrTokenNotFound", err)
		}
	})

	t.Run("accepts already confirmed token", func(t *testing.T) {
		truncateSubscriptions(t, ctx, repos.pool)
		wantID := mustInsertSubscription(t, ctx, repos.pool, subscriptionSeed{
			Email:            "user@example.com",
			Repo:             "owner/repo",
			Confirmed:        true,
			ConfirmToken:     "confirm-token",
			UnsubscribeToken: "unsub-token",
		})

		gotID, err := repos.token.ConfirmByToken(ctx, "confirm-token")
		if err != nil {
			t.Fatalf("ConfirmByToken returned error: %v", err)
		}
		if gotID != wantID {
			t.Fatalf("ConfirmByToken id = %q, want %q", gotID, wantID)
		}
		assertConfirmed(t, ctx, repos.pool, wantID, true)
	})
}

func testTokenRepoUnsubscribeByToken(t *testing.T, ctx context.Context, repos testRepos) {
	t.Helper()
	t.Run("deletes matching subscription", func(t *testing.T) {
		truncateSubscriptions(t, ctx, repos.pool)
		wantID := mustInsertSubscription(t, ctx, repos.pool, subscriptionSeed{
			Email:            "user@example.com",
			Repo:             "owner/repo",
			Confirmed:        true,
			ConfirmToken:     "confirm-token",
			UnsubscribeToken: "unsub-token",
		})

		gotID, err := repos.token.UnsubscribeByToken(ctx, "unsub-token")
		if err != nil {
			t.Fatalf("UnsubscribeByToken returned error: %v", err)
		}
		if gotID != wantID {
			t.Fatalf("UnsubscribeByToken id = %q, want %q", gotID, wantID)
		}

		var count int
		if err := repos.pool.QueryRow(ctx, `SELECT COUNT(*) FROM subscriptions`).Scan(&count); err != nil {
			t.Fatalf("count subscriptions: %v", err)
		}
		if count != 0 {
			t.Fatalf("subscription count = %d, want 0", count)
		}
	})

	t.Run("leaves non matching subscriptions untouched", func(t *testing.T) {
		truncateSubscriptions(t, ctx, repos.pool)
		targetID := mustInsertSubscription(t, ctx, repos.pool, subscriptionSeed{
			Email:            "target@example.com",
			Repo:             "owner/target",
			Confirmed:        true,
			ConfirmToken:     "confirm-target",
			UnsubscribeToken: "unsub-target",
		})
		otherID := mustInsertSubscription(t, ctx, repos.pool, subscriptionSeed{
			Email:            "other@example.com",
			Repo:             "owner/other",
			Confirmed:        true,
			ConfirmToken:     "confirm-other",
			UnsubscribeToken: "unsub-other",
		})
		pendingID := mustInsertSubscription(t, ctx, repos.pool, subscriptionSeed{
			Email:            "pending@example.com",
			Repo:             "owner/pending",
			ConfirmToken:     "confirm-pending",
			UnsubscribeToken: "unsub-pending",
		})

		gotID, err := repos.token.UnsubscribeByToken(ctx, "unsub-target")
		if err != nil {
			t.Fatalf("UnsubscribeByToken returned error: %v", err)
		}
		if gotID != targetID {
			t.Fatalf("UnsubscribeByToken id = %q, want %q", gotID, targetID)
		}

		var targetCount, otherCount int
		err = repos.pool.QueryRow(ctx, `
			SELECT
				COUNT(*) FILTER (WHERE id=$1),
				COUNT(*) FILTER (WHERE id IN ($2, $3))
			FROM subscriptions
		`, targetID, otherID, pendingID).Scan(&targetCount, &otherCount)
		if err != nil {
			t.Fatalf("count subscriptions after unsubscribe: %v", err)
		}
		if targetCount != 0 {
			t.Fatalf("target subscription count = %d, want 0", targetCount)
		}
		if otherCount != 2 {
			t.Fatalf("non matching subscription count = %d, want 2", otherCount)
		}
	})

	t.Run("returns token not found for missing token", func(t *testing.T) {
		truncateSubscriptions(t, ctx, repos.pool)

		_, err := repos.token.UnsubscribeByToken(ctx, "missing-token")
		if !errors.Is(err, subscriptionsdomain.ErrTokenNotFound) {
			t.Fatalf("UnsubscribeByToken error = %v, want ErrTokenNotFound", err)
		}
	})

	t.Run("deletes unconfirmed subscription", func(t *testing.T) {
		truncateSubscriptions(t, ctx, repos.pool)
		wantID := mustInsertSubscription(t, ctx, repos.pool, subscriptionSeed{
			Email:            "user@example.com",
			Repo:             "owner/repo",
			ConfirmToken:     "confirm-token",
			UnsubscribeToken: "unsub-token",
		})

		gotID, err := repos.token.UnsubscribeByToken(ctx, "unsub-token")
		if err != nil {
			t.Fatalf("UnsubscribeByToken returned error: %v", err)
		}
		if gotID != wantID {
			t.Fatalf("UnsubscribeByToken id = %q, want %q", gotID, wantID)
		}
	})
}
