package releasemonitoringapp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/contracts"
	contractevents "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/contracts/events"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/events"
	releasemonitoringdomain "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/release_monitoring/domain"
)

type fakeReleaseScanRepo struct {
	repos         []string
	listErr       error
	subscribers   map[string][]releasemonitoringdomain.Subscriber
	subsErr       error
	updateErr     error
	subsErrByRepo map[string]error

	listReposCalls int
	listSubsCalls  int
	updateCalls    int
	updatedRepo    string
	updatedTag     string
	listReposCtx   context.Context
	listSubsCtxs   []context.Context
	updateCtx      context.Context
	listedRepos    []string
}

func (f *fakeReleaseScanRepo) ListDistinctConfirmedRepos(ctx context.Context) ([]string, error) {
	f.listReposCalls++
	f.listReposCtx = ctx
	return f.repos, f.listErr
}

func (f *fakeReleaseScanRepo) ListConfirmedSubscribersForRepo(ctx context.Context, repo string) ([]releasemonitoringdomain.Subscriber, error) {
	f.listSubsCalls++
	f.listSubsCtxs = append(f.listSubsCtxs, ctx)
	f.listedRepos = append(f.listedRepos, repo)
	if err := f.subsErrByRepo[repo]; err != nil {
		return nil, err
	}
	return f.subscribers[repo], f.subsErr
}

func (f *fakeReleaseScanRepo) UpdateLastSeenTag(ctx context.Context, repo, tag string) error {
	f.updateCalls++
	f.updateCtx = ctx
	f.updatedRepo = repo
	f.updatedTag = tag
	return f.updateErr
}

type fakeReleaseClient struct {
	release *contracts.Release
	err     error
	calls   int
	repos   []string
	ctxs    []context.Context
	byRepo  map[string]*contracts.Release
	errs    map[string]error
}

func (f *fakeReleaseClient) GetLatestRelease(ctx context.Context, repo string) (*contracts.Release, error) {
	f.calls++
	f.repos = append(f.repos, repo)
	f.ctxs = append(f.ctxs, ctx)
	if err := f.errs[repo]; err != nil {
		return nil, err
	}
	if release := f.byRepo[repo]; release != nil {
		return release, nil
	}
	return f.release, f.err
}

type fakeTxManager struct {
	err   error
	calls int
	ctx   context.Context
}

func (f *fakeTxManager) WithinTransaction(ctx context.Context, fn func(context.Context) error) error {
	f.calls++
	f.ctx = ctx
	if f.err != nil {
		return f.err
	}
	return fn(ctx)
}

type recordingPublisher struct {
	err    error
	events []events.Event
	ctxs   []context.Context
}

func (p *recordingPublisher) Publish(ctx context.Context, event events.Event) error {
	p.ctxs = append(p.ctxs, ctx)
	p.events = append(p.events, event)
	return p.err
}

type recordingScannerLogger struct {
	infos    []recordedScannerLog
	warnings []recordedScannerLog
	errors   []recordedScannerLog
}

type recordedScannerLog struct {
	msg  string
	args []any
}

func (l *recordingScannerLogger) Debug(string, ...any) {}
func (l *recordingScannerLogger) Info(msg string, args ...any) {
	l.infos = append(l.infos, recordedScannerLog{msg: msg, args: append([]any(nil), args...)})
}

func (l *recordingScannerLogger) Warn(msg string, args ...any) {
	l.warnings = append(l.warnings, recordedScannerLog{msg: msg, args: append([]any(nil), args...)})
}

func (l *recordingScannerLogger) Error(msg string, args ...any) {
	l.errors = append(l.errors, recordedScannerLog{msg: msg, args: append([]any(nil), args...)})
}
func (l *recordingScannerLogger) ErrorContext(context.Context, string, ...any) {}

func strPtr(s string) *string {
	return &s
}

type scannerContextKey struct{}

func newScannerService(repo *fakeReleaseScanRepo, client *fakeReleaseClient, txManager *fakeTxManager, publisher events.Publisher, log *recordingScannerLogger) *ScannerService {
	return NewScannerService(&ScannerDeps{
		Repo:      repo,
		GitHub:    client,
		TxManager: txManager,
		Publisher: publisher,
		Log:       log,
	})
}

func TestScanNotifiesSubscribersAndUpdatesLastSeenTag(t *testing.T) {
	repo := &fakeReleaseScanRepo{
		repos: []string{"owner/repo"},
		subscribers: map[string][]releasemonitoringdomain.Subscriber{
			"owner/repo": {
				{
					SubscriptionID:   "sub-1",
					Email:            "new@example.com",
					Repo:             "owner/repo",
					UnsubscribeToken: "unsub-1",
				},
				{
					SubscriptionID:   "sub-2",
					Email:            "seen@example.com",
					Repo:             "owner/repo",
					LastSeenTag:      strPtr("v1.2.3"),
					UnsubscribeToken: "unsub-2",
				},
			},
		},
	}
	client := &fakeReleaseClient{
		release: &contracts.Release{
			TagName: "v1.2.3",
			HTMLURL: "https://github.com/owner/repo/releases/tag/v1.2.3",
			Name:    "Release 1.2.3",
		},
	}
	txManager := &fakeTxManager{}
	publisher := &recordingPublisher{}

	newScannerService(repo, client, txManager, publisher, &recordingScannerLogger{}).Scan(context.Background())

	if client.calls != 1 {
		t.Fatalf("release client calls = %d, want 1", client.calls)
	}
	if repo.listSubsCalls != 1 {
		t.Fatalf("list subscribers calls = %d, want 1", repo.listSubsCalls)
	}
	if repo.updateCalls != 1 || repo.updatedRepo != "owner/repo" || repo.updatedTag != "v1.2.3" {
		t.Fatalf("update = (%d, %q, %q), want (1, owner/repo, v1.2.3)", repo.updateCalls, repo.updatedRepo, repo.updatedTag)
	}
	if txManager.calls != 1 {
		t.Fatalf("transactions = %d, want 1", txManager.calls)
	}
	if len(publisher.events) != 1 {
		t.Fatalf("published events = %d, want 1", len(publisher.events))
	}

	event, ok := publisher.events[0].(contractevents.ReleaseDetected)
	if !ok {
		t.Fatalf("published event type = %T, want ReleaseDetected", publisher.events[0])
	}
	if event.Repo != "owner/repo" || event.Release.TagName != "v1.2.3" {
		t.Fatalf("event = %#v", event)
	}
	if len(event.Subscribers) != 1 || event.Subscribers[0].Email != "new@example.com" {
		t.Fatalf("event subscribers = %#v, want only unseen subscriber", event.Subscribers)
	}
}

func TestScanPassesContextToDependencies(t *testing.T) {
	ctx := context.WithValue(context.Background(), scannerContextKey{}, "request-123")
	repo := &fakeReleaseScanRepo{
		repos: []string{"owner/repo"},
		subscribers: map[string][]releasemonitoringdomain.Subscriber{
			"owner/repo": {{SubscriptionID: "sub-1", Email: "user@example.com", Repo: "owner/repo"}},
		},
	}
	client := &fakeReleaseClient{release: &contracts.Release{TagName: "v1"}}
	txManager := &fakeTxManager{}
	publisher := &recordingPublisher{}

	newScannerService(repo, client, txManager, publisher, &recordingScannerLogger{}).Scan(ctx)

	if repo.listReposCtx != ctx {
		t.Errorf("list repos context was not passed through")
	}
	if len(client.ctxs) != 1 || client.ctxs[0] != ctx {
		t.Fatalf("github contexts = %#v, want original context once", client.ctxs)
	}
	if len(repo.listSubsCtxs) != 1 || repo.listSubsCtxs[0] != ctx {
		t.Fatalf("list subscriber contexts = %#v, want original context once", repo.listSubsCtxs)
	}
	if repo.updateCtx != ctx {
		t.Errorf("update context was not passed through")
	}
	if txManager.ctx != ctx {
		t.Errorf("transaction context was not passed through")
	}
	if len(publisher.ctxs) != 1 || publisher.ctxs[0] != ctx {
		t.Fatalf("publisher contexts = %#v, want original context once", publisher.ctxs)
	}
}

func TestScanNoReposDoesNothing(t *testing.T) {
	repo := &fakeReleaseScanRepo{subscribers: map[string][]releasemonitoringdomain.Subscriber{}}
	client := &fakeReleaseClient{release: &contracts.Release{TagName: "v1"}}
	publisher := &recordingPublisher{}

	newScannerService(repo, client, &fakeTxManager{}, publisher, &recordingScannerLogger{}).Scan(context.Background())

	if repo.listReposCalls != 1 {
		t.Fatalf("list repos calls = %d, want 1", repo.listReposCalls)
	}
	if client.calls != 0 {
		t.Fatalf("release client calls = %d, want 0", client.calls)
	}
	if repo.listSubsCalls != 0 || repo.updateCalls != 0 || len(publisher.events) != 0 {
		t.Fatalf("unexpected work: listSubs=%d update=%d events=%d", repo.listSubsCalls, repo.updateCalls, len(publisher.events))
	}
}

func TestScanListReposErrorStopsScan(t *testing.T) {
	log := &recordingScannerLogger{}
	repo := &fakeReleaseScanRepo{
		listErr:     errors.New("database unavailable"),
		subscribers: map[string][]releasemonitoringdomain.Subscriber{},
	}
	client := &fakeReleaseClient{release: &contracts.Release{TagName: "v1"}}
	publisher := &recordingPublisher{}

	newScannerService(repo, client, &fakeTxManager{}, publisher, log).Scan(context.Background())

	if client.calls != 0 {
		t.Fatalf("release client calls = %d, want 0", client.calls)
	}
	if repo.listSubsCalls != 0 || repo.updateCalls != 0 || len(publisher.events) != 0 {
		t.Fatalf("unexpected work: listSubs=%d update=%d events=%d", repo.listSubsCalls, repo.updateCalls, len(publisher.events))
	}
	if len(log.errors) != 1 || log.errors[0].msg != "scanner: list repos failed" {
		t.Fatalf("errors = %#v, want list repos failure log", log.errors)
	}
}

func TestScanStopsWhenContextCancelledBeforeRepoScan(t *testing.T) {
	repo := &fakeReleaseScanRepo{
		repos:       []string{"owner/repo"},
		subscribers: map[string][]releasemonitoringdomain.Subscriber{},
	}
	client := &fakeReleaseClient{release: &contracts.Release{TagName: "v1"}}
	publisher := &recordingPublisher{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	newScannerService(repo, client, &fakeTxManager{}, publisher, &recordingScannerLogger{}).Scan(ctx)

	if client.calls != 0 {
		t.Fatalf("release client calls = %d, want 0", client.calls)
	}
	if repo.listSubsCalls != 0 || repo.updateCalls != 0 || len(publisher.events) != 0 {
		t.Fatalf("unexpected work: listSubs=%d update=%d events=%d", repo.listSubsCalls, repo.updateCalls, len(publisher.events))
	}
}

func TestScanRepoReleaseClientErrorsSkipRepo(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{name: "generic error", err: errors.New("github unavailable")},
		{name: "rate limit error", err: &contracts.RateLimitError{Service: "GitHub", RetryAfter: time.Minute}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			log := &recordingScannerLogger{}
			repo := &fakeReleaseScanRepo{
				repos: []string{"owner/repo"},
				subscribers: map[string][]releasemonitoringdomain.Subscriber{
					"owner/repo": {{SubscriptionID: "sub-1", Email: "user@example.com", Repo: "owner/repo"}},
				},
			}
			client := &fakeReleaseClient{err: tc.err}
			publisher := &recordingPublisher{}

			newScannerService(repo, client, &fakeTxManager{}, publisher, log).Scan(context.Background())

			if client.calls != 1 {
				t.Fatalf("release client calls = %d, want 1", client.calls)
			}
			if repo.listSubsCalls != 0 || repo.updateCalls != 0 || len(publisher.events) != 0 {
				t.Fatalf("unexpected work: listSubs=%d update=%d events=%d", repo.listSubsCalls, repo.updateCalls, len(publisher.events))
			}
			if tc.name == "rate limit error" {
				if len(log.warnings) != 1 || log.warnings[0].msg != "scanner: github rate limited, skipping repo" {
					t.Fatalf("warnings = %#v, want rate limit warning", log.warnings)
				}
			} else if len(log.errors) == 0 || log.errors[0].msg != "scanner: get latest release failed" {
				t.Fatalf("errors = %#v, want release failure log", log.errors)
			}
		})
	}
}

func TestScanRepoNilReleaseSkipsRepo(t *testing.T) {
	log := &recordingScannerLogger{}
	repo := &fakeReleaseScanRepo{
		repos: []string{"owner/repo"},
		subscribers: map[string][]releasemonitoringdomain.Subscriber{
			"owner/repo": {{SubscriptionID: "sub-1", Email: "user@example.com", Repo: "owner/repo"}},
		},
	}
	client := &fakeReleaseClient{}
	publisher := &recordingPublisher{}

	newScannerService(repo, client, &fakeTxManager{}, publisher, log).Scan(context.Background())

	if repo.listSubsCalls != 0 || repo.updateCalls != 0 || len(publisher.events) != 0 {
		t.Fatalf("unexpected work: listSubs=%d update=%d events=%d", repo.listSubsCalls, repo.updateCalls, len(publisher.events))
	}
	if len(log.errors) != 1 || log.errors[0].msg != "scanner: release client returned nil release" {
		t.Fatalf("errors = %#v, want nil release log", log.errors)
	}
}

func TestScanRepoListSubscribersErrorSkipsUpdate(t *testing.T) {
	log := &recordingScannerLogger{}
	repo := &fakeReleaseScanRepo{
		repos:       []string{"owner/repo"},
		subsErr:     errors.New("database unavailable"),
		subscribers: map[string][]releasemonitoringdomain.Subscriber{},
	}
	client := &fakeReleaseClient{release: &contracts.Release{TagName: "v1"}}
	publisher := &recordingPublisher{}

	newScannerService(repo, client, &fakeTxManager{}, publisher, log).Scan(context.Background())

	if repo.listSubsCalls != 1 {
		t.Fatalf("list subscribers calls = %d, want 1", repo.listSubsCalls)
	}
	if repo.updateCalls != 0 || len(publisher.events) != 0 {
		t.Fatalf("unexpected update/event: update=%d events=%d", repo.updateCalls, len(publisher.events))
	}
	if len(log.errors) == 0 || log.errors[0].msg != "scanner: list subscribers failed" {
		t.Fatalf("errors = %#v, want list subscribers failure log", log.errors)
	}
}

func TestScanRepoNoSubscribersSkipsUpdate(t *testing.T) {
	repo := &fakeReleaseScanRepo{
		repos:       []string{"owner/repo"},
		subscribers: map[string][]releasemonitoringdomain.Subscriber{"owner/repo": nil},
	}
	client := &fakeReleaseClient{release: &contracts.Release{TagName: "v1"}}
	publisher := &recordingPublisher{}

	newScannerService(repo, client, &fakeTxManager{}, publisher, &recordingScannerLogger{}).Scan(context.Background())

	if repo.listSubsCalls != 1 {
		t.Fatalf("list subscribers calls = %d, want 1", repo.listSubsCalls)
	}
	if repo.updateCalls != 0 || len(publisher.events) != 0 {
		t.Fatalf("unexpected update/event: update=%d events=%d", repo.updateCalls, len(publisher.events))
	}
}

func TestScanRepoAllSubscribersAlreadySeenSkipsUpdate(t *testing.T) {
	repo := &fakeReleaseScanRepo{
		repos: []string{"owner/repo"},
		subscribers: map[string][]releasemonitoringdomain.Subscriber{
			"owner/repo": {
				{SubscriptionID: "sub-1", Email: "one@example.com", Repo: "owner/repo", LastSeenTag: strPtr("v1")},
				{SubscriptionID: "sub-2", Email: "two@example.com", Repo: "owner/repo", LastSeenTag: strPtr("v1")},
			},
		},
	}
	client := &fakeReleaseClient{release: &contracts.Release{TagName: "v1"}}
	publisher := &recordingPublisher{}

	newScannerService(repo, client, &fakeTxManager{}, publisher, &recordingScannerLogger{}).Scan(context.Background())

	if repo.updateCalls != 0 || len(publisher.events) != 0 {
		t.Fatalf("unexpected update/event: update=%d events=%d", repo.updateCalls, len(publisher.events))
	}
}

func TestScanRepoPublishFailureSkipsUpdate(t *testing.T) {
	log := &recordingScannerLogger{}
	repo := &fakeReleaseScanRepo{
		repos: []string{"owner/repo"},
		subscribers: map[string][]releasemonitoringdomain.Subscriber{
			"owner/repo": {{SubscriptionID: "sub-1", Email: "user@example.com", Repo: "owner/repo"}},
		},
	}
	client := &fakeReleaseClient{release: &contracts.Release{TagName: "v1"}}
	publisher := &recordingPublisher{err: errors.New("outbox unavailable")}

	newScannerService(repo, client, &fakeTxManager{}, publisher, log).Scan(context.Background())

	if repo.updateCalls != 0 {
		t.Fatalf("update calls = %d, want 0", repo.updateCalls)
	}
	if len(log.errors) == 0 || log.errors[0].msg != "scanner: persist release detection failed" {
		t.Fatalf("errors = %#v, want persist failure log", log.errors)
	}
}

func TestScanRepoUpdateFailureLeavesPublishedEventRecorded(t *testing.T) {
	log := &recordingScannerLogger{}
	repo := &fakeReleaseScanRepo{
		repos: []string{"owner/repo"},
		subscribers: map[string][]releasemonitoringdomain.Subscriber{
			"owner/repo": {{SubscriptionID: "sub-1", Email: "user@example.com", Repo: "owner/repo"}},
		},
		updateErr: errors.New("database unavailable"),
	}
	client := &fakeReleaseClient{release: &contracts.Release{TagName: "v1"}}
	publisher := &recordingPublisher{}
	txManager := &fakeTxManager{}

	newScannerService(repo, client, txManager, publisher, log).Scan(context.Background())

	if repo.updateCalls != 1 {
		t.Fatalf("update calls = %d, want 1", repo.updateCalls)
	}
	if len(publisher.events) != 1 {
		t.Fatalf("published events = %d, want 1", len(publisher.events))
	}
	if len(log.errors) == 0 || log.errors[0].msg != "scanner: persist release detection failed" {
		t.Fatalf("errors = %#v, want persist failure log", log.errors)
	}
}

func TestScanSummaryLogsCounts(t *testing.T) {
	log := &recordingScannerLogger{}
	repo := &fakeReleaseScanRepo{
		repos: []string{"owner/ok", "owner/fail"},
		subscribers: map[string][]releasemonitoringdomain.Subscriber{
			"owner/ok":   {{SubscriptionID: "sub-1", Email: "ok@example.com", Repo: "owner/ok"}},
			"owner/fail": {{SubscriptionID: "sub-2", Email: "fail@example.com", Repo: "owner/fail"}},
		},
	}
	client := &fakeReleaseClient{
		byRepo: map[string]*contracts.Release{
			"owner/ok": {TagName: "v1"},
		},
		errs: map[string]error{
			"owner/fail": errors.New("boom"),
		},
	}
	publisher := &recordingPublisher{}

	newScannerService(repo, client, &fakeTxManager{}, publisher, log).Scan(context.Background())

	if len(log.infos) < 2 {
		t.Fatalf("info logs = %#v, want start and summary", log.infos)
	}
	summary := log.infos[len(log.infos)-1]
	if summary.msg != "scanner: scan complete" {
		t.Fatalf("summary log msg = %q, want scanner: scan complete", summary.msg)
	}
	args := fmt.Sprint(summary.args)
	for _, want := range []string{"repos_total", "2", "repos_checked", "1", "repos_failed", "1", "notifications_enqueued", "1"} {
		if !strings.Contains(args, want) {
			t.Fatalf("summary args missing %q: %#v", want, summary.args)
		}
	}
}
