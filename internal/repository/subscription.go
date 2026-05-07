package repository

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/domain"
)

// SubscriptionRepo wraps a pgxpool.Pool and implements all subscription persistence operations.
type SubscriptionRepo struct {
	db *pgxpool.Pool
}

// NewSubscriptionRepo returns a SubscriptionRepo backed by the given connection pool.
func NewSubscriptionRepo(db *pgxpool.Pool) *SubscriptionRepo {
	return &SubscriptionRepo{db: db}
}

// Subscribe creates a new subscription or refreshes the confirm token for an
// existing unconfirmed one. A SELECT FOR UPDATE serializes concurrent requests
// for the same (email, repo) pair, preventing duplicate inserts.
// Returns ErrAlreadySubscribed if the subscription is already confirmed.
func (r *SubscriptionRepo) Subscribe(
	ctx context.Context,
	email, repo, confirmToken, unsubToken string,
) (err error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	defer func() {
		if err != nil {
			rollbackErr := tx.Rollback(ctx)
			if rollbackErr != nil {
				log.Printf("subscribe: failed to rollback transaction: %v", rollbackErr)
			}
		}
	}()

	var id string
	var confirmed bool
	err = tx.QueryRow(
		ctx,
		`SELECT id, confirmed FROM subscriptions WHERE email=$1 AND repo=$2 FOR UPDATE`,
		email, repo,
	).Scan(&id, &confirmed)

	switch {
	case err == nil && confirmed:
		return domain.ErrAlreadySubscribed

	case err == nil && !confirmed:
		// Unconfirmed - refresh the confirm token so the new email works.
		if _, err = tx.Exec(
			ctx,
			`UPDATE subscriptions SET confirm_token=$1, updated_at=NOW() WHERE id=$2`,
			confirmToken, id,
		); err != nil {
			return fmt.Errorf("refresh confirm token: %w", err)
		}

	case errors.Is(err, pgx.ErrNoRows):
		if _, err = tx.Exec(
			ctx,
			`INSERT INTO subscriptions (email, repo, confirm_token, unsubscribe_token)
			 VALUES ($1, $2, $3, $4)`,
			email, repo, confirmToken, unsubToken,
		); err != nil {
			return fmt.Errorf("insert subscription: %w", err)
		}

	default:
		return fmt.Errorf("lock subscription row: %w", err)
	}

	return tx.Commit(ctx)
}

// ConfirmByToken marks a subscription as confirmed using the token from the
// confirmation email. Uses SELECT FOR UPDATE to guard against concurrent calls.
// Returns the subscription ID on success for logging (never the email).
func (r *SubscriptionRepo) ConfirmByToken(ctx context.Context, token string) (id string, err error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		if err != nil {
			rollbackErr := tx.Rollback(ctx)
			if rollbackErr != nil {
				log.Printf("confirm: failed to rollback transaction: %v", rollbackErr)
			}
		}
	}()

	err = tx.QueryRow(
		ctx,
		`SELECT id FROM subscriptions WHERE confirm_token=$1 FOR UPDATE`,
		token,
	).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", domain.ErrTokenNotFound
	}
	if err != nil {
		return "", fmt.Errorf("lock confirm token row: %w", err)
	}

	if _, err = tx.Exec(
		ctx,
		`UPDATE subscriptions SET confirmed=TRUE, updated_at=NOW() WHERE id=$1`, id,
	); err != nil {
		return "", fmt.Errorf("confirm subscription: %w", err)
	}

	return id, tx.Commit(ctx)
}

// UnsubscribeByToken deletes a subscription using the token embedded in every
// notification email. Returns the subscription ID on success for logging.
func (r *SubscriptionRepo) UnsubscribeByToken(ctx context.Context, token string) (string, error) {
	var id string
	err := r.db.QueryRow(
		ctx,
		`DELETE FROM subscriptions WHERE unsubscribe_token=$1 RETURNING id`,
		token,
	).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", domain.ErrTokenNotFound
	}
	if err != nil {
		return "", fmt.Errorf("delete subscription: %w", err)
	}
	return id, nil
}

// ListByEmail returns all subscriptions (confirmed and unconfirmed) for an email.
func (r *SubscriptionRepo) ListByEmail(ctx context.Context, email string) ([]domain.Subscription, error) {
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

	var subs []domain.Subscription
	for rows.Next() {
		var s domain.Subscription
		if err := rows.Scan(
			&s.ID, &s.Email, &s.Repo, &s.Confirmed, &s.LastSeenTag, &s.CreatedAt, &s.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan subscription: %w", err)
		}
		subs = append(subs, s)
	}
	return subs, rows.Err()
}

// ListDistinctConfirmedRepos returns the unique set of repos that have at least
// one confirmed subscriber. Used by the scanner to know what to check.
func (r *SubscriptionRepo) ListDistinctConfirmedRepos(ctx context.Context) ([]string, error) {
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
func (r *SubscriptionRepo) ListConfirmedSubscribersForRepo(ctx context.Context, repo string) ([]domain.Subscription, error) {
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

	var subs []domain.Subscription
	for rows.Next() {
		var s domain.Subscription
		if err := rows.Scan(&s.ID, &s.Email, &s.Repo, &s.LastSeenTag, &s.UnsubscribeToken); err != nil {
			return nil, fmt.Errorf("scan subscriber: %w", err)
		}
		subs = append(subs, s)
	}
	return subs, rows.Err()
}

// UpdateLastSeenTag records the latest release tag for all confirmed subscribers
// of a repo so that already-notified releases are not sent again.
func (r *SubscriptionRepo) UpdateLastSeenTag(ctx context.Context, repo, tag string) error {
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
