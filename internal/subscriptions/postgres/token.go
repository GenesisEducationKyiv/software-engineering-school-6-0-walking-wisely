package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	platformpostgres "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/postgres"
	subscriptionsdomain "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions/domain"
)

// Subscribe creates a new subscription or refreshes the confirm token for an
// existing unconfirmed one. A SELECT FOR UPDATE inside a transaction serializes
// concurrent requests for the same (email, repo) pair, preventing duplicate inserts.
// Returns ErrAlreadySubscribed if the subscription is already confirmed.
func (r *TokenRepo) Subscribe(
	ctx context.Context,
	email, repo, confirmToken, unsubToken string,
) (result subscriptionsdomain.SubscribeResult, err error) {
	err = platformpostgres.WithinTransaction(ctx, r.db, func(ctx context.Context) error {
		exec := platformpostgres.ExecutorFromContext(ctx, r.db)

		var id string
		var confirmed bool
		err := exec.QueryRow(
			ctx,
			`SELECT id, confirmed FROM subscriptions WHERE email=$1 AND repo=$2 FOR UPDATE`,
			email, repo,
		).Scan(&id, &confirmed)

		switch {
		case err == nil && confirmed:
			return subscriptionsdomain.ErrAlreadySubscribed

		case err == nil && !confirmed:
			if _, err = exec.Exec(
				ctx,
				`UPDATE subscriptions SET confirm_token=$1, updated_at=NOW() WHERE id=$2`,
				confirmToken, id,
			); err != nil {
				return fmt.Errorf("refresh confirm token: %w", err)
			}
			result = subscriptionsdomain.SubscribeResult{
				SubscriptionID: id,
				Action:         subscriptionsdomain.SubscribeActionConfirmationRefreshed,
			}

		case errors.Is(err, pgx.ErrNoRows):
			err = exec.QueryRow(
				ctx,
				`INSERT INTO subscriptions (email, repo, confirm_token, unsubscribe_token)
				 VALUES ($1, $2, $3, $4)
				 RETURNING id`,
				email, repo, confirmToken, unsubToken,
			).Scan(&id)
			if err != nil {
				return fmt.Errorf("insert subscription: %w", err)
			}
			result = subscriptionsdomain.SubscribeResult{
				SubscriptionID: id,
				Action:         subscriptionsdomain.SubscribeActionCreated,
			}

		default:
			return fmt.Errorf("lock subscription row: %w", err)
		}
		return nil
	})
	return result, err
}

// ConfirmByToken marks a subscription as confirmed using the token from the
// confirmation email. Uses SELECT FOR UPDATE to guard against concurrent calls.
// Returns the subscription ID on success for logging (never the email).
func (r *TokenRepo) ConfirmByToken(ctx context.Context, token string) (id string, err error) {
	exec := platformpostgres.ExecutorFromContext(ctx, r.db)

	err = exec.QueryRow(
		ctx,
		`SELECT id FROM subscriptions WHERE confirm_token=$1 FOR UPDATE`,
		token,
	).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", subscriptionsdomain.ErrTokenNotFound
	}
	if err != nil {
		return "", fmt.Errorf("lock confirm token row: %w", err)
	}

	if _, err = exec.Exec(
		ctx,
		`UPDATE subscriptions SET confirmed=TRUE, updated_at=NOW() WHERE id=$1`, id,
	); err != nil {
		return "", fmt.Errorf("confirm subscription: %w", err)
	}

	return id, nil
}

// UnsubscribeByToken deletes a subscription using the token embedded in every
// notification email. Returns the subscription ID on success for logging.
func (r *TokenRepo) UnsubscribeByToken(ctx context.Context, token string) (string, error) {
	var id string
	err := r.db.QueryRow(
		ctx,
		`DELETE FROM subscriptions WHERE unsubscribe_token=$1 RETURNING id`,
		token,
	).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", subscriptionsdomain.ErrTokenNotFound
	}
	if err != nil {
		return "", fmt.Errorf("delete subscription: %w", err)
	}
	return id, nil
}
