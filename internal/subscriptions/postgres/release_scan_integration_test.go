//go:build integration

package postgres

import (
	"context"
	"testing"
	"time"
)

func testReleaseScanRepoListDistinctConfirmedRepos(t *testing.T, ctx context.Context, repos testRepos) {
	t.Helper()

	t.Run("returns empty list when there are no confirmed repos", func(t *testing.T) {
		truncateSubscriptions(t, ctx, repos.pool)
		mustInsertSubscription(t, ctx, repos.pool, subscriptionSeed{
			Email:            "unconfirmed@example.com",
			Repo:             "owner/repo",
			ConfirmToken:     "confirm-token",
			UnsubscribeToken: "unsub-token",
		})

		got, err := repos.releaseScan.ListDistinctConfirmedRepos(ctx)
		if err != nil {
			t.Fatalf("ListDistinctConfirmedRepos returned error: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("repos = %#v, want empty list", got)
		}
	})

	t.Run("lists unique confirmed repos", func(t *testing.T) {
		truncateSubscriptions(t, ctx, repos.pool)
		mustInsertSubscription(t, ctx, repos.pool, subscriptionSeed{
			Email:            "one@example.com",
			Repo:             "owner/repo",
			Confirmed:        true,
			ConfirmToken:     "confirm-one",
			UnsubscribeToken: "unsub-one",
		})
		mustInsertSubscription(t, ctx, repos.pool, subscriptionSeed{
			Email:            "two@example.com",
			Repo:             "owner/repo",
			Confirmed:        true,
			ConfirmToken:     "confirm-two",
			UnsubscribeToken: "unsub-two",
		})
		mustInsertSubscription(t, ctx, repos.pool, subscriptionSeed{
			Email:            "three@example.com",
			Repo:             "owner/unconfirmed",
			ConfirmToken:     "confirm-three",
			UnsubscribeToken: "unsub-three",
		})

		got, err := repos.releaseScan.ListDistinctConfirmedRepos(ctx)
		if err != nil {
			t.Fatalf("ListDistinctConfirmedRepos returned error: %v", err)
		}
		if len(got) != 1 || got[0] != "owner/repo" {
			t.Fatalf("repos = %#v, want only owner/repo", got)
		}
	})
}

func testReleaseScanRepoListConfirmedSubscribersForRepo(t *testing.T, ctx context.Context, repos testRepos) {
	t.Helper()

	t.Run("returns empty list when repo has no confirmed subscribers", func(t *testing.T) {
		truncateSubscriptions(t, ctx, repos.pool)
		mustInsertSubscription(t, ctx, repos.pool, subscriptionSeed{
			Email:            "unconfirmed@example.com",
			Repo:             "owner/repo",
			ConfirmToken:     "confirm-token",
			UnsubscribeToken: "unsub-token",
		})

		got, err := repos.releaseScan.ListConfirmedSubscribersForRepo(ctx, "owner/repo")
		if err != nil {
			t.Fatalf("ListConfirmedSubscribersForRepo returned error: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("subscribers = %#v, want empty list", got)
		}
	})

	t.Run("lists confirmed subscribers with unsubscribe tokens", func(t *testing.T) {
		truncateSubscriptions(t, ctx, repos.pool)
		tag := "v1.2.3"
		mustInsertSubscription(t, ctx, repos.pool, subscriptionSeed{
			Email:            "confirmed@example.com",
			Repo:             "owner/repo",
			Confirmed:        true,
			ConfirmToken:     "confirm-one",
			UnsubscribeToken: "unsub-one",
			LastSeenTag:      &tag,
		})
		mustInsertSubscription(t, ctx, repos.pool, subscriptionSeed{
			Email:            "unconfirmed@example.com",
			Repo:             "owner/repo",
			ConfirmToken:     "confirm-two",
			UnsubscribeToken: "unsub-two",
		})
		mustInsertSubscription(t, ctx, repos.pool, subscriptionSeed{
			Email:            "other@example.com",
			Repo:             "owner/other",
			Confirmed:        true,
			ConfirmToken:     "confirm-three",
			UnsubscribeToken: "unsub-three",
		})

		got, err := repos.releaseScan.ListConfirmedSubscribersForRepo(ctx, "owner/repo")
		if err != nil {
			t.Fatalf("ListConfirmedSubscribersForRepo returned error: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("subscribers = %#v, want 1 confirmed subscriber", got)
		}
		if got[0].Email != "confirmed@example.com" || got[0].UnsubscribeToken != "unsub-one" {
			t.Fatalf("subscriber = %#v, want confirmed subscriber with unsubscribe token", got[0])
		}
		if got[0].LastSeenTag == nil || *got[0].LastSeenTag != tag {
			t.Fatalf("last_seen_tag = %#v, want %q", got[0].LastSeenTag, tag)
		}
	})

	t.Run("filters subscribers by exact repo and confirmation state", func(t *testing.T) {
		truncateSubscriptions(t, ctx, repos.pool)
		mustInsertSubscription(t, ctx, repos.pool, subscriptionSeed{
			Email:            "match@example.com",
			Repo:             "owner/repo",
			Confirmed:        true,
			ConfirmToken:     "confirm-match",
			UnsubscribeToken: "unsub-match",
		})
		mustInsertSubscription(t, ctx, repos.pool, subscriptionSeed{
			Email:            "pending@example.com",
			Repo:             "owner/repo",
			ConfirmToken:     "confirm-pending",
			UnsubscribeToken: "unsub-pending",
		})
		mustInsertSubscription(t, ctx, repos.pool, subscriptionSeed{
			Email:            "other@example.com",
			Repo:             "owner/repo-extra",
			Confirmed:        true,
			ConfirmToken:     "confirm-other",
			UnsubscribeToken: "unsub-other",
		})

		got, err := repos.releaseScan.ListConfirmedSubscribersForRepo(ctx, "owner/repo")
		if err != nil {
			t.Fatalf("ListConfirmedSubscribersForRepo returned error: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("subscribers = %#v, want only exact confirmed repo match", got)
		}
		if got[0].Email != "match@example.com" || got[0].Repo != "owner/repo" {
			t.Fatalf("subscriber = %#v, want exact confirmed repo match", got[0])
		}
	})
}

func testReleaseScanRepoUpdateLastSeenTag(t *testing.T, ctx context.Context, repos testRepos) {
	t.Helper()

	t.Run("updates last seen tag only for confirmed subscriptions in repo", func(t *testing.T) {
		truncateSubscriptions(t, ctx, repos.pool)
		createdAt := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
		confirmedID := mustInsertSubscription(t, ctx, repos.pool, subscriptionSeed{
			Email:            "confirmed@example.com",
			Repo:             "owner/repo",
			Confirmed:        true,
			ConfirmToken:     "confirm-one",
			UnsubscribeToken: "unsub-one",
			CreatedAt:        createdAt,
		})
		unconfirmedID := mustInsertSubscription(t, ctx, repos.pool, subscriptionSeed{
			Email:            "unconfirmed@example.com",
			Repo:             "owner/repo",
			ConfirmToken:     "confirm-two",
			UnsubscribeToken: "unsub-two",
			CreatedAt:        createdAt,
		})
		otherRepoID := mustInsertSubscription(t, ctx, repos.pool, subscriptionSeed{
			Email:            "other@example.com",
			Repo:             "owner/other",
			Confirmed:        true,
			ConfirmToken:     "confirm-three",
			UnsubscribeToken: "unsub-three",
			CreatedAt:        createdAt,
		})
		prefixRepoID := mustInsertSubscription(t, ctx, repos.pool, subscriptionSeed{
			Email:            "prefix@example.com",
			Repo:             "owner/repo-extra",
			Confirmed:        true,
			ConfirmToken:     "confirm-four",
			UnsubscribeToken: "unsub-four",
			CreatedAt:        createdAt,
		})

		if err := repos.releaseScan.UpdateLastSeenTag(ctx, "owner/repo", "v1.2.3"); err != nil {
			t.Fatalf("UpdateLastSeenTag returned error: %v", err)
		}
		assertLastSeenTag(t, ctx, repos.pool, confirmedID, "v1.2.3")
		assertUpdatedAfter(t, ctx, repos.pool, confirmedID, createdAt)
		assertNullLastSeenTag(t, ctx, repos.pool, unconfirmedID)
		assertNullLastSeenTag(t, ctx, repos.pool, otherRepoID)
		assertNullLastSeenTag(t, ctx, repos.pool, prefixRepoID)

		if err := repos.releaseScan.UpdateLastSeenTag(ctx, "owner/repo", "v1.2.4"); err != nil {
			t.Fatalf("UpdateLastSeenTag overwrite returned error: %v", err)
		}
		assertLastSeenTag(t, ctx, repos.pool, confirmedID, "v1.2.4")
	})
}
