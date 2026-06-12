//go:build integration

package postgres

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/logger"
	subscriptionpostgres "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions/postgres"
)

// ── shared setup ──────────────────────────────────────────────────────────────

func newNotificationTestDB(t *testing.T, ctx context.Context) (*Repository, *pgxpool.Pool) {
	t.Helper()

	testcontainers.SkipIfProviderIsNotHealthy(t)

	container, err := tcpostgres.Run(
		ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("app"),
		tcpostgres.WithUsername("app"),
		tcpostgres.WithPassword("secret"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}
	t.Cleanup(func() {
		if err := container.Terminate(context.Background()); err != nil {
			t.Logf("terminate postgres container: %v", err)
		}
	})

	databaseURL, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("build postgres connection string: %v", err)
	}
	if err := subscriptionpostgres.RunMigrations(databaseURL, logger.NoopLogger{}); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect to postgres: %v", err)
	}
	t.Cleanup(pool.Close)

	return NewRepository(pool), pool
}

func integrationContext(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), 2*time.Minute)
}

// insertSubscription inserts a minimal confirmed subscription and returns its ID.
func insertSubscription(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var id string
	err := pool.QueryRow(
		ctx, `
		INSERT INTO subscriptions (email, repo, confirmed, confirm_token, unsubscribe_token)
		VALUES ($1, $2, TRUE, $3, $4)
		RETURNING id`,
		uuid.NewString()+"@example.com",
		"owner/repo",
		uuid.NewString(),
		uuid.NewString(),
	).Scan(&id)
	if err != nil {
		t.Fatalf("insert subscription: %v", err)
	}
	return id
}

func truncateNotificationTables(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `TRUNCATE notification_jobs, event_deliveries, subscriptions CASCADE`); err != nil {
		t.Fatalf("truncate tables: %v", err)
	}
}

// ── RecordConfirmation ────────────────────────────────────────────────────────

func TestRecordConfirmationHappyPath(t *testing.T) {
	t.Parallel()
	ctx, cancel := integrationContext(t)
	defer cancel()

	// Arrange
	repo, pool := newNotificationTestDB(t, ctx)
	truncateNotificationTables(t, ctx, pool)
	subID := insertSubscription(t, ctx, pool)
	eventID := uuid.NewString()
	confirmToken := uuid.NewString()

	// Act
	err := repo.RecordConfirmation(ctx, "handler.test", eventID, subID, "to@example.com", "Subject", "<p>html</p>", confirmToken)
	// Assert
	if err != nil {
		t.Fatalf("RecordConfirmation returned error: %v", err)
	}

	var jobType, status, to, subject, html, token, gotEventID string
	err = pool.QueryRow(
		ctx,
		`SELECT job_type, status, to_email, subject, html_body, confirm_token, event_id::text
		 FROM notification_jobs WHERE subscription_id=$1::uuid`,
		subID,
	).Scan(&jobType, &status, &to, &subject, &html, &token, &gotEventID)
	if err != nil {
		t.Fatalf("query notification_jobs: %v", err)
	}
	if jobType != "confirmation" {
		t.Errorf("job_type = %q, want confirmation", jobType)
	}
	if status != StatusPending {
		t.Errorf("status = %q, want pending", status)
	}
	if to != "to@example.com" {
		t.Errorf("to_email = %q, want to@example.com", to)
	}
	if subject != "Subject" {
		t.Errorf("subject = %q, want Subject", subject)
	}
	if html != "<p>html</p>" {
		t.Errorf("html_body = %q, want <p>html</p>", html)
	}
	if token != confirmToken {
		t.Errorf("confirm_token = %q, want %q", token, confirmToken)
	}
	if gotEventID != eventID {
		t.Errorf("event_id = %q, want %q", gotEventID, eventID)
	}

	var deliveryCount int
	if err := pool.QueryRow(
		ctx,
		`SELECT COUNT(*) FROM event_deliveries WHERE handler_name='handler.test' AND event_id=$1::uuid`,
		eventID,
	).Scan(&deliveryCount); err != nil {
		t.Fatalf("query event_deliveries: %v", err)
	}
	if deliveryCount != 1 {
		t.Fatalf("event_deliveries count = %d, want 1", deliveryCount)
	}
}

func TestRecordConfirmationIdempotent(t *testing.T) {
	t.Parallel()
	ctx, cancel := integrationContext(t)
	defer cancel()

	// Arrange
	repo, pool := newNotificationTestDB(t, ctx)
	truncateNotificationTables(t, ctx, pool)
	subID := insertSubscription(t, ctx, pool)
	eventID := uuid.NewString()
	confirmToken := uuid.NewString()

	// Act — call twice with the same eventID
	for i := 0; i < 2; i++ {
		if err := repo.RecordConfirmation(ctx, "handler.test", eventID, subID, "to@example.com", "Subject", "<p>html</p>", confirmToken); err != nil {
			t.Fatalf("RecordConfirmation call %d returned error: %v", i+1, err)
		}
	}

	// Assert — exactly one job and one delivery row
	var jobCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM notification_jobs WHERE subscription_id=$1::uuid`, subID).Scan(&jobCount); err != nil {
		t.Fatalf("query notification_jobs: %v", err)
	}
	if jobCount != 1 {
		t.Fatalf("notification_jobs count = %d, want 1 (idempotent)", jobCount)
	}

	var deliveryCount int
	if err := pool.QueryRow(
		ctx,
		`SELECT COUNT(*) FROM event_deliveries WHERE handler_name='handler.test' AND event_id=$1::uuid`,
		eventID,
	).Scan(&deliveryCount); err != nil {
		t.Fatalf("query event_deliveries: %v", err)
	}
	if deliveryCount != 1 {
		t.Fatalf("event_deliveries count = %d, want 1 (idempotent)", deliveryCount)
	}
}

// ── RecordReleaseNotifications ────────────────────────────────────────────────

func TestRecordReleaseNotificationsHappyPath(t *testing.T) {
	t.Parallel()
	ctx, cancel := integrationContext(t)
	defer cancel()

	// Arrange
	repo, pool := newNotificationTestDB(t, ctx)
	truncateNotificationTables(t, ctx, pool)
	sub1 := insertSubscription(t, ctx, pool)
	sub2 := insertSubscription(t, ctx, pool)
	eventID := uuid.NewString()
	jobs := []ReleaseNotificationJob{
		{SubscriptionID: sub1, To: "a@example.com", Subject: "Release v1", HTML: "<p>v1</p>"},
		{SubscriptionID: sub2, To: "b@example.com", Subject: "Release v1", HTML: "<p>v1</p>"},
	}

	// Act
	err := repo.RecordReleaseNotifications(ctx, "handler.release", eventID, "v1.0.0", jobs)
	// Assert
	if err != nil {
		t.Fatalf("RecordReleaseNotifications returned error: %v", err)
	}

	var jobCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM notification_jobs WHERE job_type='release_notification'`).Scan(&jobCount); err != nil {
		t.Fatalf("query notification_jobs: %v", err)
	}
	if jobCount != 2 {
		t.Fatalf("notification_jobs count = %d, want 2", jobCount)
	}

	// Check the release_tag column is persisted
	var releaseTag string
	if err := pool.QueryRow(ctx, `SELECT release_tag FROM notification_jobs WHERE subscription_id=$1::uuid`, sub1).Scan(&releaseTag); err != nil {
		t.Fatalf("query release_tag: %v", err)
	}
	if releaseTag != "v1.0.0" {
		t.Errorf("release_tag = %q, want v1.0.0", releaseTag)
	}

	var deliveryCount int
	if err := pool.QueryRow(
		ctx,
		`SELECT COUNT(*) FROM event_deliveries WHERE handler_name='handler.release' AND event_id=$1::uuid`,
		eventID,
	).Scan(&deliveryCount); err != nil {
		t.Fatalf("query event_deliveries: %v", err)
	}
	if deliveryCount != 1 {
		t.Fatalf("event_deliveries count = %d, want 1", deliveryCount)
	}
}

func TestRecordReleaseNotificationsIdempotent(t *testing.T) {
	t.Parallel()
	ctx, cancel := integrationContext(t)
	defer cancel()

	// Arrange
	repo, pool := newNotificationTestDB(t, ctx)
	truncateNotificationTables(t, ctx, pool)
	subID := insertSubscription(t, ctx, pool)
	eventID := uuid.NewString()
	jobs := []ReleaseNotificationJob{
		{SubscriptionID: subID, To: "a@example.com", Subject: "Release", HTML: "<p>html</p>"},
	}

	// Act — call twice with the same eventID
	for i := 0; i < 2; i++ {
		if err := repo.RecordReleaseNotifications(ctx, "handler.release", eventID, "v1.0.0", jobs); err != nil {
			t.Fatalf("RecordReleaseNotifications call %d returned error: %v", i+1, err)
		}
	}

	// Assert
	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM notification_jobs WHERE job_type='release_notification'`).Scan(&count); err != nil {
		t.Fatalf("query notification_jobs: %v", err)
	}
	if count != 1 {
		t.Fatalf("notification_jobs count = %d, want 1 (idempotent)", count)
	}

	var deliveryCount int
	if err := pool.QueryRow(
		ctx,
		`SELECT COUNT(*) FROM event_deliveries WHERE handler_name='handler.release' AND event_id=$1::uuid`,
		eventID,
	).Scan(&deliveryCount); err != nil {
		t.Fatalf("query event_deliveries: %v", err)
	}
	if deliveryCount != 1 {
		t.Fatalf("event_deliveries count = %d, want 1 (idempotent)", deliveryCount)
	}
}

// ── ClaimPending ──────────────────────────────────────────────────────────────

func TestClaimPendingFieldsRoundTrip(t *testing.T) {
	t.Parallel()
	ctx, cancel := integrationContext(t)
	defer cancel()

	// Arrange
	repo, pool := newNotificationTestDB(t, ctx)
	truncateNotificationTables(t, ctx, pool)
	subID := insertSubscription(t, ctx, pool)
	eventID := uuid.NewString()
	confirmToken := uuid.NewString()
	if err := repo.RecordConfirmation(ctx, "handler.test", eventID, subID, "to@example.com", "My Subject", "<p>body</p>", confirmToken); err != nil {
		t.Fatalf("RecordConfirmation: %v", err)
	}

	// Act
	jobs, err := repo.ClaimPending(ctx, "worker-1", 10)
	// Assert
	if err != nil {
		t.Fatalf("ClaimPending returned error: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("ClaimPending returned %d jobs, want 1", len(jobs))
	}
	job := jobs[0]
	if job.To != "to@example.com" {
		t.Errorf("To = %q, want to@example.com", job.To)
	}
	if job.Subject != "My Subject" {
		t.Errorf("Subject = %q, want My Subject", job.Subject)
	}
	if job.HTML != "<p>body</p>" {
		t.Errorf("HTML = %q, want <p>body</p>", job.HTML)
	}
	if job.EventID != eventID {
		t.Errorf("EventID = %q, want %q", job.EventID, eventID)
	}
}

func TestClaimPendingSkipsInFlightRows(t *testing.T) {
	t.Parallel()
	ctx, cancel := integrationContext(t)
	defer cancel()

	// Arrange
	repo, pool := newNotificationTestDB(t, ctx)
	truncateNotificationTables(t, ctx, pool)
	subID := insertSubscription(t, ctx, pool)
	if err := repo.RecordConfirmation(ctx, "handler.test", uuid.NewString(), subID, "to@example.com", "Subject", "<p>body</p>", uuid.NewString()); err != nil {
		t.Fatalf("RecordConfirmation: %v", err)
	}
	first, err := repo.ClaimPending(ctx, "worker-1", 10)
	if err != nil || len(first) != 1 {
		t.Fatalf("first ClaimPending: err=%v, jobs=%d", err, len(first))
	}

	// Act — second worker tries to claim the same row (locked within 5 minutes)
	second, err := repo.ClaimPending(ctx, "worker-2", 10)
	// Assert
	if err != nil {
		t.Fatalf("second ClaimPending returned error: %v", err)
	}
	if len(second) != 0 {
		t.Fatalf("second claim returned %d jobs, want 0 (row is in-flight)", len(second))
	}
}

// ── MarkSent ──────────────────────────────────────────────────────────────────

func TestMarkSent(t *testing.T) {
	t.Parallel()
	ctx, cancel := integrationContext(t)
	defer cancel()

	// Arrange
	repo, pool := newNotificationTestDB(t, ctx)
	truncateNotificationTables(t, ctx, pool)
	subID := insertSubscription(t, ctx, pool)
	if err := repo.RecordConfirmation(ctx, "handler.test", uuid.NewString(), subID, "to@example.com", "Subject", "<p>body</p>", uuid.NewString()); err != nil {
		t.Fatalf("RecordConfirmation: %v", err)
	}
	jobs, err := repo.ClaimPending(ctx, "worker-1", 10)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("ClaimPending: err=%v, jobs=%d", err, len(jobs))
	}

	// Act
	err = repo.MarkSent(ctx, jobs)
	// Assert
	if err != nil {
		t.Fatalf("MarkSent returned error: %v", err)
	}
	var status string
	var sentAt *time.Time
	var lockedAt *time.Time
	var lockedBy *string
	var lastError *string
	if err := pool.QueryRow(
		ctx,
		`SELECT status, sent_at, locked_at, locked_by, last_error FROM notification_jobs WHERE id=$1::uuid`,
		jobs[0].ID,
	).Scan(&status, &sentAt, &lockedAt, &lockedBy, &lastError); err != nil {
		t.Fatalf("query notification_jobs: %v", err)
	}
	if status != StatusSent {
		t.Errorf("status = %q, want sent", status)
	}
	if sentAt == nil {
		t.Error("sent_at should be populated after MarkSent")
	}
	if lockedAt != nil {
		t.Errorf("locked_at should be NULL after MarkSent, got %v", lockedAt)
	}
	if lockedBy != nil {
		t.Errorf("locked_by should be NULL after MarkSent, got %v", lockedBy)
	}
	if lastError != nil {
		t.Errorf("last_error should be NULL after MarkSent, got %v", lastError)
	}
}

// ── MarkFailed ────────────────────────────────────────────────────────────────

func TestMarkFailedBelowMaxKeepsPending(t *testing.T) {
	t.Parallel()
	ctx, cancel := integrationContext(t)
	defer cancel()

	// Arrange
	repo, pool := newNotificationTestDB(t, ctx)
	truncateNotificationTables(t, ctx, pool)
	subID := insertSubscription(t, ctx, pool)
	if err := repo.RecordConfirmation(ctx, "handler.test", uuid.NewString(), subID, "to@example.com", "Subject", "<p>body</p>", uuid.NewString()); err != nil {
		t.Fatalf("RecordConfirmation: %v", err)
	}
	jobs, err := repo.ClaimPending(ctx, "worker-1", 10)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("ClaimPending: err=%v, jobs=%d", err, len(jobs))
	}
	before := time.Now().UTC()

	// Act
	err = repo.MarkFailed(ctx, jobs, 5, errors.New("transient error"))
	// Assert
	if err != nil {
		t.Fatalf("MarkFailed returned error: %v", err)
	}
	var status string
	var attemptCount int
	var availableAt time.Time
	var lastError *string
	var lockedAt *time.Time
	var lockedBy *string
	if err := pool.QueryRow(
		ctx,
		`SELECT status, attempt_count, available_at, last_error, locked_at, locked_by FROM notification_jobs WHERE id=$1::uuid`,
		jobs[0].ID,
	).Scan(&status, &attemptCount, &availableAt, &lastError, &lockedAt, &lockedBy); err != nil {
		t.Fatalf("query notification_jobs: %v", err)
	}
	if status != StatusPending {
		t.Errorf("status = %q, want pending", status)
	}
	if attemptCount != 1 {
		t.Errorf("attempt_count = %d, want 1", attemptCount)
	}
	if !availableAt.After(before) {
		t.Errorf("available_at = %v should be after %v (backoff not applied)", availableAt, before)
	}
	if lastError == nil || *lastError != "transient error" {
		t.Errorf("last_error = %v, want \"transient error\"", lastError)
	}
	if lockedAt != nil {
		t.Errorf("locked_at should be NULL after MarkFailed, got %v", lockedAt)
	}
	if lockedBy != nil {
		t.Errorf("locked_by should be NULL after MarkFailed, got %v", lockedBy)
	}
}

func TestMarkFailedAtMaxMovesToFailed(t *testing.T) {
	t.Parallel()
	ctx, cancel := integrationContext(t)
	defer cancel()

	// Arrange — insert a job with attempt_count already at maxAttempts-1 so one more failure tips it over.
	repo, pool := newNotificationTestDB(t, ctx)
	truncateNotificationTables(t, ctx, pool)
	subID := insertSubscription(t, ctx, pool)
	if err := repo.RecordConfirmation(ctx, "handler.test", uuid.NewString(), subID, "to@example.com", "Subject", "<p>body</p>", uuid.NewString()); err != nil {
		t.Fatalf("RecordConfirmation: %v", err)
	}
	// Artificially set attempt_count to maxAttempts-1 so the next failure crosses the threshold.
	const maxAttempts = 3
	if _, err := pool.Exec(ctx, `UPDATE notification_jobs SET attempt_count=$1`, maxAttempts-1); err != nil {
		t.Fatalf("set attempt_count: %v", err)
	}
	jobs, err := repo.ClaimPending(ctx, "worker-1", 10)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("ClaimPending: err=%v, jobs=%d", err, len(jobs))
	}

	// Act
	err = repo.MarkFailed(ctx, jobs, maxAttempts, errors.New("permanent error"))
	// Assert
	if err != nil {
		t.Fatalf("MarkFailed returned error: %v", err)
	}
	var status string
	var lastError *string
	var lockedAt *time.Time
	var lockedBy *string
	if err := pool.QueryRow(
		ctx,
		`SELECT status, last_error, locked_at, locked_by FROM notification_jobs WHERE id=$1::uuid`,
		jobs[0].ID,
	).Scan(&status, &lastError, &lockedAt, &lockedBy); err != nil {
		t.Fatalf("query notification_jobs: %v", err)
	}
	if status != StatusFailed {
		t.Errorf("status = %q, want failed", status)
	}
	if lastError == nil || *lastError != "permanent error" {
		t.Errorf("last_error = %v, want \"permanent error\"", lastError)
	}
	if lockedAt != nil {
		t.Errorf("locked_at should be NULL after MarkFailed at max, got %v", lockedAt)
	}
	if lockedBy != nil {
		t.Errorf("locked_by should be NULL after MarkFailed at max, got %v", lockedBy)
	}
}

func TestDeleteSentBefore(t *testing.T) {
	t.Parallel()
	ctx, cancel := integrationContext(t)
	defer cancel()

	repo, pool := newNotificationTestDB(t, ctx)
	truncateNotificationTables(t, ctx, pool)

	old := time.Now().UTC().Add(-8 * 24 * time.Hour)
	fresh := time.Now().UTC().Add(-6 * 24 * time.Hour)
	cutoff := time.Now().UTC().Add(-7 * 24 * time.Hour)

	oldSentID := insertNotificationJob(t, ctx, repo, pool, StatusSent)
	setNotificationJobUpdatedAt(t, ctx, pool, oldSentID, old)

	freshSentID := insertNotificationJob(t, ctx, repo, pool, StatusSent)
	setNotificationJobUpdatedAt(t, ctx, pool, freshSentID, fresh)

	oldPendingID := insertNotificationJob(t, ctx, repo, pool, StatusPending)
	setNotificationJobUpdatedAt(t, ctx, pool, oldPendingID, old)

	oldFailedID := insertNotificationJob(t, ctx, repo, pool, StatusFailed)
	setNotificationJobUpdatedAt(t, ctx, pool, oldFailedID, old)

	deleted, err := repo.DeleteSentBefore(ctx, cutoff)
	if err != nil {
		t.Fatalf("DeleteSentBefore returned error: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted rows = %d, want 1", deleted)
	}

	gotIDs := loadNotificationJobIDs(t, ctx, pool)
	wantIDs := []string{freshSentID, oldPendingID, oldFailedID}
	slices.Sort(wantIDs)
	if !slices.Equal(gotIDs, wantIDs) {
		t.Fatalf("remaining ids = %#v, want %#v", gotIDs, wantIDs)
	}
}

func insertNotificationJob(t *testing.T, ctx context.Context, repo *Repository, pool *pgxpool.Pool, status string) string {
	t.Helper()

	subID := insertSubscription(t, ctx, pool)
	if err := repo.RecordConfirmation(ctx, "handler."+uuid.NewString(), uuid.NewString(), subID, "to@example.com", "Subject", "<p>body</p>", uuid.NewString()); err != nil {
		t.Fatalf("RecordConfirmation: %v", err)
	}

	var id string
	if err := pool.QueryRow(ctx, `SELECT id::text FROM notification_jobs WHERE subscription_id=$1::uuid`, subID).Scan(&id); err != nil {
		t.Fatalf("load inserted notification job id: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE notification_jobs SET status=$2 WHERE id=$1::uuid`, id, status); err != nil {
		t.Fatalf("set notification job status: %v", err)
	}
	return id
}

func setNotificationJobUpdatedAt(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id string, updatedAt time.Time) {
	t.Helper()

	if _, err := pool.Exec(ctx, `UPDATE notification_jobs SET updated_at=$2 WHERE id=$1::uuid`, id, updatedAt); err != nil {
		t.Fatalf("set notification job updated_at: %v", err)
	}
}

func loadNotificationJobIDs(t *testing.T, ctx context.Context, pool *pgxpool.Pool) []string {
	t.Helper()

	rows, err := pool.Query(ctx, `SELECT id::text FROM notification_jobs ORDER BY id`)
	if err != nil {
		t.Fatalf("load notification job ids: %v", err)
	}
	defer rows.Close()

	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan notification job id: %v", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate notification job ids: %v", err)
	}
	return ids
}
