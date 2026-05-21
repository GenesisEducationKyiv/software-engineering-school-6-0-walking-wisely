package worker

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/mail"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/releases"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions"
)

type fakeReleaseScanRepo struct {
	repos       []string
	listErr     error
	subscribers map[string][]subscriptions.Subscription
	subsErr     error
	updateErr   error

	listReposCalls   int
	listSubsCalls    int
	updateCalls      int
	updatedRepo      string
	updatedTag       string
	listedSubscriber []string
}

func (f *fakeReleaseScanRepo) ListDistinctConfirmedRepos(_ context.Context) ([]string, error) {
	f.listReposCalls++
	return f.repos, f.listErr
}

func (f *fakeReleaseScanRepo) ListConfirmedSubscribersForRepo(_ context.Context, repo string) ([]subscriptions.Subscription, error) {
	f.listSubsCalls++
	f.listedSubscriber = append(f.listedSubscriber, repo)
	return f.subscribers[repo], f.subsErr
}

func (f *fakeReleaseScanRepo) UpdateLastSeenTag(_ context.Context, repo, tag string) error {
	f.updateCalls++
	f.updatedRepo = repo
	f.updatedTag = tag
	return f.updateErr
}

type fakeReleaseClient struct {
	release *releases.Release
	err     error
	calls   int
	repos   []string
}

func (f *fakeReleaseClient) GetLatestRelease(_ context.Context, repo string) (*releases.Release, error) {
	f.calls++
	f.repos = append(f.repos, repo)
	return f.release, f.err
}

func newScannerDeps(repo *fakeReleaseScanRepo, client *fakeReleaseClient, ch chan mail.Message) ScannerDeps {
	return ScannerDeps{
		Repo:      repo,
		GitHub:    client,
		EmailChan: ch,
		BaseURL:   "https://example.com",
	}
}

func strPtr(s string) *string {
	return &s
}

func TestRunScanNotifiesSubscribersAndUpdatesLastSeenTag(t *testing.T) {
	repo := &fakeReleaseScanRepo{
		repos: []string{"owner/repo"},
		subscribers: map[string][]subscriptions.Subscription{
			"owner/repo": {
				{
					ID:               "sub-1",
					Email:            "new@example.com",
					Repo:             "owner/repo",
					UnsubscribeToken: "unsub-1",
				},
				{
					ID:               "sub-2",
					Email:            "seen@example.com",
					Repo:             "owner/repo",
					LastSeenTag:      strPtr("v1.2.3"),
					UnsubscribeToken: "unsub-2",
				},
			},
		},
	}
	client := &fakeReleaseClient{
		release: &releases.Release{
			TagName: "v1.2.3",
			HTMLURL: "https://github.com/owner/repo/releases/tag/v1.2.3",
			Name:    "Release 1.2.3",
		},
	}
	ch := make(chan mail.Message, 2)

	runScan(context.Background(), newScannerDeps(repo, client, ch))

	if client.calls != 1 {
		t.Fatalf("release client calls = %d, want 1", client.calls)
	}
	if repo.listSubsCalls != 1 {
		t.Fatalf("list subscribers calls = %d, want 1", repo.listSubsCalls)
	}
	if repo.updateCalls != 1 || repo.updatedRepo != "owner/repo" || repo.updatedTag != "v1.2.3" {
		t.Fatalf("update = (%d, %q, %q), want (1, owner/repo, v1.2.3)", repo.updateCalls, repo.updatedRepo, repo.updatedTag)
	}

	select {
	case msg := <-ch:
		if msg.To != "new@example.com" {
			t.Fatalf("email recipient = %q, want new@example.com", msg.To)
		}
		if msg.Subject != "[owner/repo] New release: v1.2.3" {
			t.Fatalf("subject = %q", msg.Subject)
		}
		if !strings.Contains(msg.HTML, "Release 1.2.3") || !strings.Contains(msg.HTML, "unsub-1") {
			t.Fatalf("email HTML missing release name or unsubscribe token: %s", msg.HTML)
		}
	default:
		t.Fatal("expected one notification email")
	}

	if len(ch) != 0 {
		t.Fatalf("remaining emails = %d, want 0", len(ch))
	}
}

func TestRunScanNoReposDoesNothing(t *testing.T) {
	repo := &fakeReleaseScanRepo{subscribers: map[string][]subscriptions.Subscription{}}
	client := &fakeReleaseClient{release: &releases.Release{TagName: "v1"}}
	ch := make(chan mail.Message, 1)

	runScan(context.Background(), newScannerDeps(repo, client, ch))

	if repo.listReposCalls != 1 {
		t.Fatalf("list repos calls = %d, want 1", repo.listReposCalls)
	}
	if client.calls != 0 {
		t.Fatalf("release client calls = %d, want 0", client.calls)
	}
	if repo.listSubsCalls != 0 || repo.updateCalls != 0 || len(ch) != 0 {
		t.Fatalf("unexpected work: listSubs=%d update=%d emails=%d", repo.listSubsCalls, repo.updateCalls, len(ch))
	}
}

func TestRunScanListReposErrorStopsScan(t *testing.T) {
	repo := &fakeReleaseScanRepo{
		listErr:     errors.New("database unavailable"),
		subscribers: map[string][]subscriptions.Subscription{},
	}
	client := &fakeReleaseClient{release: &releases.Release{TagName: "v1"}}
	ch := make(chan mail.Message, 1)

	runScan(context.Background(), newScannerDeps(repo, client, ch))

	if client.calls != 0 {
		t.Fatalf("release client calls = %d, want 0", client.calls)
	}
	if repo.listSubsCalls != 0 || repo.updateCalls != 0 || len(ch) != 0 {
		t.Fatalf("unexpected work: listSubs=%d update=%d emails=%d", repo.listSubsCalls, repo.updateCalls, len(ch))
	}
}

func TestRunScanStopsWhenContextCancelledBeforeRepoScan(t *testing.T) {
	repo := &fakeReleaseScanRepo{
		repos:       []string{"owner/repo"},
		subscribers: map[string][]subscriptions.Subscription{},
	}
	client := &fakeReleaseClient{release: &releases.Release{TagName: "v1"}}
	ch := make(chan mail.Message, 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	runScan(ctx, newScannerDeps(repo, client, ch))

	if client.calls != 0 {
		t.Fatalf("release client calls = %d, want 0", client.calls)
	}
	if repo.listSubsCalls != 0 || repo.updateCalls != 0 || len(ch) != 0 {
		t.Fatalf("unexpected work: listSubs=%d update=%d emails=%d", repo.listSubsCalls, repo.updateCalls, len(ch))
	}
}

func TestScanRepoReleaseClientErrorsSkipRepo(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{name: "generic error", err: errors.New("github unavailable")},
		{name: "rate limit error", err: &subscriptions.RateLimitError{Service: "GitHub", RetryAfter: time.Minute}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeReleaseScanRepo{
				subscribers: map[string][]subscriptions.Subscription{
					"owner/repo": {{ID: "sub-1", Email: "user@example.com", Repo: "owner/repo"}},
				},
			}
			client := &fakeReleaseClient{err: tc.err}
			ch := make(chan mail.Message, 1)

			scanRepo(context.Background(), newScannerDeps(repo, client, ch), "owner/repo")

			if client.calls != 1 {
				t.Fatalf("release client calls = %d, want 1", client.calls)
			}
			if repo.listSubsCalls != 0 || repo.updateCalls != 0 || len(ch) != 0 {
				t.Fatalf("unexpected work: listSubs=%d update=%d emails=%d", repo.listSubsCalls, repo.updateCalls, len(ch))
			}
		})
	}
}

func TestScanRepoNilReleaseSkipsRepo(t *testing.T) {
	repo := &fakeReleaseScanRepo{
		subscribers: map[string][]subscriptions.Subscription{
			"owner/repo": {{ID: "sub-1", Email: "user@example.com", Repo: "owner/repo"}},
		},
	}
	client := &fakeReleaseClient{}
	ch := make(chan mail.Message, 1)

	scanRepo(context.Background(), newScannerDeps(repo, client, ch), "owner/repo")

	if repo.listSubsCalls != 0 || repo.updateCalls != 0 || len(ch) != 0 {
		t.Fatalf("unexpected work: listSubs=%d update=%d emails=%d", repo.listSubsCalls, repo.updateCalls, len(ch))
	}
}

func TestScanRepoListSubscribersErrorSkipsUpdate(t *testing.T) {
	repo := &fakeReleaseScanRepo{
		subsErr:     errors.New("database unavailable"),
		subscribers: map[string][]subscriptions.Subscription{},
	}
	client := &fakeReleaseClient{release: &releases.Release{TagName: "v1"}}
	ch := make(chan mail.Message, 1)

	scanRepo(context.Background(), newScannerDeps(repo, client, ch), "owner/repo")

	if repo.listSubsCalls != 1 {
		t.Fatalf("list subscribers calls = %d, want 1", repo.listSubsCalls)
	}
	if repo.updateCalls != 0 || len(ch) != 0 {
		t.Fatalf("unexpected update/email: update=%d emails=%d", repo.updateCalls, len(ch))
	}
}

func TestScanRepoNoSubscribersSkipsUpdate(t *testing.T) {
	repo := &fakeReleaseScanRepo{subscribers: map[string][]subscriptions.Subscription{"owner/repo": nil}}
	client := &fakeReleaseClient{release: &releases.Release{TagName: "v1"}}
	ch := make(chan mail.Message, 1)

	scanRepo(context.Background(), newScannerDeps(repo, client, ch), "owner/repo")

	if repo.listSubsCalls != 1 {
		t.Fatalf("list subscribers calls = %d, want 1", repo.listSubsCalls)
	}
	if repo.updateCalls != 0 || len(ch) != 0 {
		t.Fatalf("unexpected update/email: update=%d emails=%d", repo.updateCalls, len(ch))
	}
}

func TestScanRepoAllSubscribersAlreadySeenSkipsUpdate(t *testing.T) {
	repo := &fakeReleaseScanRepo{
		subscribers: map[string][]subscriptions.Subscription{
			"owner/repo": {
				{ID: "sub-1", Email: "one@example.com", Repo: "owner/repo", LastSeenTag: strPtr("v1")},
				{ID: "sub-2", Email: "two@example.com", Repo: "owner/repo", LastSeenTag: strPtr("v1")},
			},
		},
	}
	client := &fakeReleaseClient{release: &releases.Release{TagName: "v1"}}
	ch := make(chan mail.Message, 2)

	scanRepo(context.Background(), newScannerDeps(repo, client, ch), "owner/repo")

	if repo.updateCalls != 0 || len(ch) != 0 {
		t.Fatalf("unexpected update/email: update=%d emails=%d", repo.updateCalls, len(ch))
	}
}

func TestScanRepoFullEmailChannelDropsNotificationsAndSkipsUpdate(t *testing.T) {
	repo := &fakeReleaseScanRepo{
		subscribers: map[string][]subscriptions.Subscription{
			"owner/repo": {{ID: "sub-1", Email: "user@example.com", Repo: "owner/repo"}},
		},
	}
	client := &fakeReleaseClient{release: &releases.Release{TagName: "v1"}}
	ch := make(chan mail.Message, 1)
	ch <- mail.Message{To: "queued@example.com"}

	scanRepo(context.Background(), newScannerDeps(repo, client, ch), "owner/repo")

	if repo.updateCalls != 0 {
		t.Fatalf("update calls = %d, want 0", repo.updateCalls)
	}
	if len(ch) != 1 {
		t.Fatalf("email queue length = %d, want existing message only", len(ch))
	}
}

func TestScanRepoPartialEmailDropsStillUpdateLastSeenTag(t *testing.T) {
	repo := &fakeReleaseScanRepo{
		subscribers: map[string][]subscriptions.Subscription{
			"owner/repo": {
				{ID: "sub-1", Email: "one@example.com", Repo: "owner/repo"},
				{ID: "sub-2", Email: "two@example.com", Repo: "owner/repo"},
			},
		},
	}
	client := &fakeReleaseClient{release: &releases.Release{TagName: "v1"}}
	ch := make(chan mail.Message, 1)

	scanRepo(context.Background(), newScannerDeps(repo, client, ch), "owner/repo")

	if repo.updateCalls != 1 || repo.updatedTag != "v1" {
		t.Fatalf("update = (%d, %q), want (1, v1)", repo.updateCalls, repo.updatedTag)
	}
	if len(ch) != 1 {
		t.Fatalf("email queue length = %d, want 1", len(ch))
	}
}

func TestScanRepoUpdateFailureDoesNotDropQueuedEmail(t *testing.T) {
	repo := &fakeReleaseScanRepo{
		subscribers: map[string][]subscriptions.Subscription{
			"owner/repo": {{ID: "sub-1", Email: "user@example.com", Repo: "owner/repo"}},
		},
		updateErr: errors.New("database unavailable"),
	}
	client := &fakeReleaseClient{release: &releases.Release{TagName: "v1"}}
	ch := make(chan mail.Message, 1)

	scanRepo(context.Background(), newScannerDeps(repo, client, ch), "owner/repo")

	if repo.updateCalls != 1 {
		t.Fatalf("update calls = %d, want 1", repo.updateCalls)
	}
	if len(ch) != 1 {
		t.Fatalf("email queue length = %d, want 1", len(ch))
	}
}

func TestBuildReleaseEmailFallsBackToTagNameWhenNameMissing(t *testing.T) {
	sub := &subscriptions.Subscription{
		Email:            "user@example.com",
		Repo:             "owner/repo",
		UnsubscribeToken: "unsub-token",
	}
	release := &releases.Release{
		TagName: "v1.0.0",
		HTMLURL: "https://example.com/release",
	}

	msg := buildReleaseEmail(sub, release, "https://service.example")

	if msg.To != "user@example.com" {
		t.Fatalf("to = %q, want user@example.com", msg.To)
	}
	if msg.Subject != "[owner/repo] New release: v1.0.0" {
		t.Fatalf("subject = %q", msg.Subject)
	}
	for _, want := range []string{"v1.0.0", "https://example.com/release", "https://service.example/api/unsubscribe/unsub-token"} {
		if !strings.Contains(msg.HTML, want) {
			t.Fatalf("email HTML missing %q: %s", want, msg.HTML)
		}
	}
}
