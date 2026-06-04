package postgres

import (
	"context"
	"fmt"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions"
)

// ListByEmail returns all subscriptions (confirmed and unconfirmed) for an email.
func (r *ReadRepo) ListByEmail(ctx context.Context, email string) ([]subscriptions.Subscription, error) {
	rows, err := r.db.Query(
		ctx,
		`SELECT id, email, repo, confirmed, last_seen_tag, created_at, updated_at
		 FROM subscriptions WHERE email=$1 ORDER BY created_at DESC`,
		email,
	)
	if err != nil {
		return nil, fmt.Errorf("list by email: %w", err)
	}
	defer rows.Close()

	var subs []subscriptions.Subscription
	for rows.Next() {
		var s subscriptions.Subscription
		if err := rows.Scan(
			&s.ID, &s.Email, &s.Repo, &s.Confirmed, &s.LastSeenTag, &s.CreatedAt, &s.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan subscription: %w", err)
		}
		subs = append(subs, s)
	}
	return subs, rows.Err()
}
