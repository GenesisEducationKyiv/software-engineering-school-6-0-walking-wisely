package postgres

import (
	"context"
	"fmt"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions"
)

// ListDistinctConfirmedRepos returns the unique set of repos that have at least
// one confirmed subscriber. Used by the scanner to know what to check.
func (r *ReleaseScanRepo) ListDistinctConfirmedRepos(ctx context.Context) ([]string, error) {
	rows, err := r.db.Query(
		ctx,
		`SELECT DISTINCT repo FROM subscriptions WHERE confirmed=TRUE`,
	)
	if err != nil {
		return nil, fmt.Errorf("list distinct repos: %w", err)
	}
	defer rows.Close()

	var repos []string
	for rows.Next() {
		var repo string
		if err := rows.Scan(&repo); err != nil {
			return nil, fmt.Errorf("scan repo: %w", err)
		}
		repos = append(repos, repo)
	}
	return repos, rows.Err()
}

// ListConfirmedSubscribersForRepo returns all confirmed subscribers for a repo,
// including the unsubscribe token needed to build the email footer link.
func (r *ReleaseScanRepo) ListConfirmedSubscribersForRepo(ctx context.Context, repo string) ([]subscriptions.Subscription, error) {
	rows, err := r.db.Query(
		ctx,
		`SELECT id, email, repo, last_seen_tag, unsubscribe_token
		 FROM subscriptions WHERE repo=$1 AND confirmed=TRUE`,
		repo,
	)
	if err != nil {
		return nil, fmt.Errorf("list subscribers for repo: %w", err)
	}
	defer rows.Close()

	var subs []subscriptions.Subscription
	for rows.Next() {
		var s subscriptions.Subscription
		if err := rows.Scan(&s.ID, &s.Email, &s.Repo, &s.LastSeenTag, &s.UnsubscribeToken); err != nil {
			return nil, fmt.Errorf("scan subscriber: %w", err)
		}
		subs = append(subs, s)
	}
	return subs, rows.Err()
}

// UpdateLastSeenTag records the latest release tag for all confirmed subscribers
// of a repo so that already-notified releases are not sent again.
func (r *ReleaseScanRepo) UpdateLastSeenTag(ctx context.Context, repo, tag string) error {
	if _, err := r.db.Exec(
		ctx,
		`UPDATE subscriptions SET last_seen_tag=$2, updated_at=NOW()
		 WHERE repo=$1 AND confirmed=TRUE`,
		repo, tag,
	); err != nil {
		return fmt.Errorf("update last seen tag: %w", err)
	}
	return nil
}
