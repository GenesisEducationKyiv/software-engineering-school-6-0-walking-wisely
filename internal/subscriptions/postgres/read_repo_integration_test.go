//go:build integration

package postgres

import (
	"context"
	"testing"
	"time"
)

func testReadRepoListByEmail(t *testing.T, ctx context.Context, repos testRepos) {
	t.Helper()

	t.Run("returns empty list when email has no subscriptions", func(t *testing.T) {
		truncateSubscriptions(t, ctx, repos.pool)

		got, err := repos.read.ListByEmail(ctx, "missing@example.com")
		if err != nil {
			t.Fatalf("ListByEmail returned error: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("ListByEmail returned %#v, want empty list", got)
		}
	})

	t.Run("lists matching subscriptions ordered by newest first", func(t *testing.T) {
		truncateSubscriptions(t, ctx, repos.pool)

		oldTag := "v1.0.0"
		oldCreatedAt := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
		middleCreatedAt := time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)
		newCreatedAt := time.Date(2026, 1, 3, 10, 0, 0, 0, time.UTC)
		mustInsertSubscription(t, ctx, repos.pool, subscriptionSeed{
			Email:            "user@example.com",
			Repo:             "owner/old",
			Confirmed:        true,
			ConfirmToken:     "confirm-old",
			UnsubscribeToken: "unsub-old",
			LastSeenTag:      &oldTag,
			CreatedAt:        oldCreatedAt,
		})
		mustInsertSubscription(t, ctx, repos.pool, subscriptionSeed{
			Email:            "other@example.com",
			Repo:             "owner/other",
			Confirmed:        true,
			ConfirmToken:     "confirm-other",
			UnsubscribeToken: "unsub-other",
			CreatedAt:        middleCreatedAt,
		})
		mustInsertSubscription(t, ctx, repos.pool, subscriptionSeed{
			Email:            "user@example.com",
			Repo:             "owner/middle",
			Confirmed:        true,
			ConfirmToken:     "confirm-middle",
			UnsubscribeToken: "unsub-middle",
			CreatedAt:        middleCreatedAt,
		})
		mustInsertSubscription(t, ctx, repos.pool, subscriptionSeed{
			Email:            "user@example.com",
			Repo:             "owner/new",
			ConfirmToken:     "confirm-new",
			UnsubscribeToken: "unsub-new",
			CreatedAt:        newCreatedAt,
		})

		got, err := repos.read.ListByEmail(ctx, "user@example.com")
		if err != nil {
			t.Fatalf("ListByEmail returned error: %v", err)
		}
		if len(got) != 3 {
			t.Fatalf("ListByEmail returned %d subscriptions, want 3", len(got))
		}
		if got[0].Repo != "owner/new" || got[1].Repo != "owner/middle" || got[2].Repo != "owner/old" {
			t.Fatalf("repos = [%q, %q, %q], want newest first", got[0].Repo, got[1].Repo, got[2].Repo)
		}
		if got[0].Email != "user@example.com" || got[0].Confirmed {
			t.Fatalf("new subscription = %#v, want unconfirmed user@example.com", got[0])
		}
		if !got[0].CreatedAt.Equal(newCreatedAt) || !got[0].UpdatedAt.Equal(newCreatedAt) {
			t.Fatalf("new subscription timestamps = (%s, %s), want %s", got[0].CreatedAt, got[0].UpdatedAt, newCreatedAt)
		}
		if got[1].Email != "user@example.com" || !got[1].Confirmed {
			t.Fatalf("middle subscription = %#v, want confirmed user@example.com", got[1])
		}
		if !got[1].CreatedAt.Equal(middleCreatedAt) || !got[1].UpdatedAt.Equal(middleCreatedAt) {
			t.Fatalf("middle subscription timestamps = (%s, %s), want %s", got[1].CreatedAt, got[1].UpdatedAt, middleCreatedAt)
		}
		if got[2].Email != "user@example.com" || !got[2].Confirmed {
			t.Fatalf("old subscription = %#v, want confirmed user@example.com", got[2])
		}
		if !got[2].CreatedAt.Equal(oldCreatedAt) || !got[2].UpdatedAt.Equal(oldCreatedAt) {
			t.Fatalf("old subscription timestamps = (%s, %s), want %s", got[2].CreatedAt, got[2].UpdatedAt, oldCreatedAt)
		}
		if got[2].LastSeenTag == nil || *got[2].LastSeenTag != oldTag {
			t.Fatalf("old subscription last_seen_tag = %#v, want %q", got[2].LastSeenTag, oldTag)
		}
	})
}
