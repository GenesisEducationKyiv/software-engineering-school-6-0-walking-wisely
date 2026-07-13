package mail

import "context"

// NoopSender discards all messages without delivering them.
// Use EMAIL_SINK=noop to wire this in place of the real provider.
type NoopSender struct{}

func (NoopSender) SendBatch(_ context.Context, _ []Message) error { return nil }
func (NoopSender) MaxBatchSize() int                              { return 100 }
