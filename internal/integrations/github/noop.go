package github

import "context"

// NoopRepoValidator always reports the repo as valid without contacting GitHub.
// Use GITHUB_SKIP_REPO_VALIDATION=true to wire this in place of the real client
// during benchmarks or local dev where GitHub quota is a concern.
type NoopRepoValidator struct{}

func (NoopRepoValidator) ValidateRepo(_ context.Context, _ string) error { return nil }
