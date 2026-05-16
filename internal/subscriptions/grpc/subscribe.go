package subscriptiongrpc

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/gen/subscription/v1"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions"
)

var repoPattern = regexp.MustCompile(`^[a-zA-Z0-9._-]+/[a-zA-Z0-9._-]+$`)

// Subscribe handles POST /api/subscribe.
// It validates the request, confirms the repo exists on GitHub, persists the
// subscription (guarded by SELECT FOR UPDATE), and queues a confirmation email.
func (s *SubscriptionService) Subscribe(
	ctx context.Context,
	req *pb.SubscribeRequest,
) (*pb.SubscribeResponse, error) {
	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	req.Repo = strings.TrimSpace(req.Repo)

	if !isValidEmail(req.Email) {
		return nil, status.Error(codes.InvalidArgument, "invalid email format")
	}

	if !repoPattern.MatchString(req.Repo) {
		return nil, status.Error(codes.InvalidArgument, "invalid repo format, expected owner/repo")
	}

	if err := s.deps.Github.ValidateRepo(ctx, req.Repo); err != nil {
		if errors.Is(err, subscriptions.ErrRepoNotFound) {
			return nil, status.Error(codes.NotFound, "repository not found on GitHub")
		}
		var rle *subscriptions.RateLimitError
		if errors.As(err, &rle) {
			return nil, handleRateLimitError(ctx, rle)
		}
		slog.Error("subscribe: validate repo", "repo", req.Repo, "err", err)
		return nil, status.Error(codes.Internal, "internal error")
	}

	confirmToken, err := subscriptions.GenerateToken(s.deps.EmailSecretKey)
	if err != nil {
		slog.Error("subscribe: generate confirm token", "err", err)
		return nil, status.Error(codes.Internal, "internal error")
	}
	unsubToken, err := subscriptions.GenerateToken(s.deps.EmailSecretKey)
	if err != nil {
		slog.Error("subscribe: generate unsub token", "err", err)
		return nil, status.Error(codes.Internal, "internal error")
	}

	if err := s.deps.SubRepo.Subscribe(ctx, req.Email, req.Repo, confirmToken, unsubToken); err != nil {
		if errors.Is(err, subscriptions.ErrAlreadySubscribed) {
			return nil, status.Error(codes.AlreadyExists, "email already subscribed to this repository")
		}
		slog.Error("subscribe: db error", "repo", req.Repo, "err", err)
		return nil, status.Error(codes.Internal, "internal error")
	}

	confirmURL := fmt.Sprintf("%s/api/confirm/%s", s.deps.BaseURL, confirmToken)
	unsubURL := fmt.Sprintf("%s/api/unsubscribe/%s", s.deps.BaseURL, unsubToken)

	select {
	case s.deps.EmailChan <- buildConfirmEmail(req.Email, req.Repo, confirmURL, unsubURL):
	default:
		// Channel full - log with repo only, not email (PII).
		slog.Warn("subscribe: email channel full, confirmation email dropped", "repo", req.Repo)
	}

	slog.Info("subscribe: subscription created", "repo", req.Repo)
	return &pb.SubscribeResponse{}, nil
}

func buildConfirmEmail(email, repo, confirmURL, unsubURL string) subscriptions.EmailMessage {
	return subscriptions.EmailMessage{
		To:      email,
		Subject: fmt.Sprintf("Confirm your subscription to %s releases", repo),
		HTML: fmt.Sprintf(`<p>You requested release notifications for <strong>%s</strong>.</p>
<p><a href="%s">Confirm subscription</a></p>
<p><small>Didn't request this? <a href="%s">Unsubscribe</a></small></p>`,
			repo, confirmURL, unsubURL),
	}
}
