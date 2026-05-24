package github

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeReleaseClient struct {
	release *Release
	err     error
	calls   int
	ctx     context.Context
	repo    string
}

func (f *fakeReleaseClient) GetLatestRelease(ctx context.Context, repo string) (*Release, error) {
	f.calls++
	f.ctx = ctx
	f.repo = repo
	return f.release, f.err
}

type fakeReleaseCache struct {
	release *Release
	ok      bool
	getErr  error
	setErr  error
	setTTL  time.Duration
	setRepo string
	getRepo string
	getCtx  context.Context
	setCtx  context.Context
	set     *Release
	gets    int
	sets    int
}

func (f *fakeReleaseCache) GetRelease(ctx context.Context, repo string) (*Release, bool, error) {
	f.gets++
	f.getCtx = ctx
	f.getRepo = repo
	return f.release, f.ok, f.getErr
}

func (f *fakeReleaseCache) SetRelease(ctx context.Context, repo string, release *Release, ttl time.Duration) error {
	f.sets++
	f.setCtx = ctx
	f.setRepo = repo
	f.set = release
	f.setTTL = ttl
	return f.setErr
}

func TestCachedReleaseClient_CacheHit(t *testing.T) {
	cached := &Release{TagName: "v1.0.0"}
	next := &fakeReleaseClient{release: &Release{TagName: "v2.0.0"}}
	cache := &fakeReleaseCache{release: cached, ok: true}

	client := NewCachedReleaseClient(next, cache, ReleaseCacheTTL, nil)
	got, err := client.GetLatestRelease(context.Background(), "owner/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != cached {
		t.Fatalf("got %#v, want cached release", got)
	}
	if next.calls != 0 {
		t.Fatalf("next calls = %d, want 0", next.calls)
	}
	if cache.sets != 0 {
		t.Fatalf("cache sets = %d, want 0", cache.sets)
	}
}

func TestCachedReleaseClient_CacheMissFetchesAndStores(t *testing.T) {
	fresh := &Release{TagName: "v2.0.0"}
	next := &fakeReleaseClient{release: fresh}
	cache := &fakeReleaseCache{}

	client := NewCachedReleaseClient(next, cache, 5*time.Minute, nil)
	got, err := client.GetLatestRelease(context.Background(), "owner/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != fresh {
		t.Fatalf("got %#v, want fresh release", got)
	}
	if next.calls != 1 {
		t.Fatalf("next calls = %d, want 1", next.calls)
	}
	if cache.set != fresh || cache.setRepo != "owner/repo" || cache.setTTL != 5*time.Minute {
		t.Fatalf("cache set = (%#v, %q, %s), want fresh owner/repo 5m", cache.set, cache.setRepo, cache.setTTL)
	}
}

func TestCachedReleaseClient_ForwardsContextAndRepo(t *testing.T) {
	fresh := &Release{TagName: "v2.0.0"}
	next := &fakeReleaseClient{release: fresh}
	cache := &fakeReleaseCache{}
	ctx := context.WithValue(context.Background(), struct{}{}, "marker")

	client := NewCachedReleaseClient(next, cache, ReleaseCacheTTL, nil)
	if _, err := client.GetLatestRelease(ctx, "owner/repo"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cache.getCtx != ctx || cache.getRepo != "owner/repo" {
		t.Fatalf("cache get = (%v, %q), want original context and owner/repo", cache.getCtx, cache.getRepo)
	}
	if next.ctx != ctx || next.repo != "owner/repo" {
		t.Fatalf("next get = (%v, %q), want original context and owner/repo", next.ctx, next.repo)
	}
	if cache.setCtx != ctx || cache.setRepo != "owner/repo" {
		t.Fatalf("cache set = (%v, %q), want original context and owner/repo", cache.setCtx, cache.setRepo)
	}
}

func TestCachedReleaseClient_CacheErrorsDoNotHideFreshRelease(t *testing.T) {
	fresh := &Release{TagName: "v2.0.0"}
	next := &fakeReleaseClient{release: fresh}
	cache := &fakeReleaseCache{
		getErr: errors.New("redis unavailable"),
		setErr: errors.New("redis still unavailable"),
	}

	client := NewCachedReleaseClient(next, cache, ReleaseCacheTTL, nil)
	got, err := client.GetLatestRelease(context.Background(), "owner/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != fresh {
		t.Fatalf("got %#v, want fresh release", got)
	}
	if next.calls != 1 {
		t.Fatalf("next calls = %d, want 1", next.calls)
	}
	if cache.set != fresh || cache.setRepo != "owner/repo" || cache.setTTL != ReleaseCacheTTL {
		t.Fatalf("cache set = (%#v, %q, %s), want fresh owner/repo %s", cache.set, cache.setRepo, cache.setTTL, ReleaseCacheTTL)
	}
}

func TestCachedReleaseClient_PropagatesFetchError(t *testing.T) {
	wantErr := errors.New("github unavailable")
	next := &fakeReleaseClient{err: wantErr}
	cache := &fakeReleaseCache{}

	client := NewCachedReleaseClient(next, cache, ReleaseCacheTTL, nil)
	_, err := client.GetLatestRelease(context.Background(), "owner/repo")
	if !errors.Is(err, wantErr) {
		t.Fatalf("got error %v, want %v", err, wantErr)
	}
	if cache.sets != 0 {
		t.Fatalf("cache sets = %d, want 0", cache.sets)
	}
}

func TestCachedReleaseClient_CacheReadErrorWithFetchErrorReturnsFetchError(t *testing.T) {
	cacheErr := errors.New("redis unavailable")
	wantErr := errors.New("github unavailable")
	next := &fakeReleaseClient{err: wantErr}
	cache := &fakeReleaseCache{getErr: cacheErr}

	client := NewCachedReleaseClient(next, cache, ReleaseCacheTTL, nil)
	_, err := client.GetLatestRelease(context.Background(), "owner/repo")
	if !errors.Is(err, wantErr) {
		t.Fatalf("got error %v, want fetch error %v", err, wantErr)
	}
	if errors.Is(err, cacheErr) {
		t.Fatalf("got error %v, want cache read error to be ignored", err)
	}
	if cache.sets != 0 {
		t.Fatalf("cache sets = %d, want 0", cache.sets)
	}
}

func TestCachedReleaseClient_CacheReadErrorIgnoresReturnedRelease(t *testing.T) {
	cached := &Release{TagName: "v1.0.0"}
	fresh := &Release{TagName: "v2.0.0"}
	next := &fakeReleaseClient{release: fresh}
	cache := &fakeReleaseCache{
		release: cached,
		ok:      true,
		getErr:  errors.New("redis unavailable"),
	}

	client := NewCachedReleaseClient(next, cache, ReleaseCacheTTL, nil)
	got, err := client.GetLatestRelease(context.Background(), "owner/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != fresh {
		t.Fatalf("got %#v, want fresh release", got)
	}
	if next.calls != 1 {
		t.Fatalf("next calls = %d, want 1", next.calls)
	}
}
