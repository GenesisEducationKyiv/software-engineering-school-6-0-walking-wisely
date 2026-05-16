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
}

func (f *fakeReleaseClient) GetLatestRelease(_ context.Context, _ string) (*Release, error) {
	f.calls++
	return f.release, f.err
}

type fakeReleaseCache struct {
	release *Release
	ok      bool
	getErr  error
	setErr  error
	setTTL  time.Duration
	setRepo string
	set     *Release
}

func (f *fakeReleaseCache) GetRelease(_ context.Context, _ string) (*Release, bool, error) {
	return f.release, f.ok, f.getErr
}

func (f *fakeReleaseCache) SetRelease(_ context.Context, repo string, release *Release, ttl time.Duration) error {
	f.setRepo = repo
	f.set = release
	f.setTTL = ttl
	return f.setErr
}

func TestCachedReleaseClient_CacheHit(t *testing.T) {
	cached := &Release{TagName: "v1.0.0"}
	next := &fakeReleaseClient{release: &Release{TagName: "v2.0.0"}}
	cache := &fakeReleaseCache{release: cached, ok: true}

	client := NewCachedReleaseClient(next, cache, ReleaseCacheTTL)
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
}

func TestCachedReleaseClient_CacheMissFetchesAndStores(t *testing.T) {
	fresh := &Release{TagName: "v2.0.0"}
	next := &fakeReleaseClient{release: fresh}
	cache := &fakeReleaseCache{}

	client := NewCachedReleaseClient(next, cache, 5*time.Minute)
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

func TestCachedReleaseClient_CacheErrorsDoNotHideFreshRelease(t *testing.T) {
	fresh := &Release{TagName: "v2.0.0"}
	next := &fakeReleaseClient{release: fresh}
	cache := &fakeReleaseCache{
		getErr: errors.New("redis unavailable"),
		setErr: errors.New("redis still unavailable"),
	}

	client := NewCachedReleaseClient(next, cache, ReleaseCacheTTL)
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

func TestCachedReleaseClient_PropagatesFetchError(t *testing.T) {
	wantErr := errors.New("github unavailable")
	next := &fakeReleaseClient{err: wantErr}
	cache := &fakeReleaseCache{}

	client := NewCachedReleaseClient(next, cache, ReleaseCacheTTL)
	_, err := client.GetLatestRelease(context.Background(), "owner/repo")
	if !errors.Is(err, wantErr) {
		t.Fatalf("got error %v, want %v", err, wantErr)
	}
}
