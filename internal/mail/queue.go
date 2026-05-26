package mail

// Queue accepts email messages for later delivery.
type Queue interface {
	Enqueue(msg Message) bool
}

// ChannelQueue adapts a mail message channel to Queue.
type ChannelQueue struct {
	ch chan<- Message
}

// NewChannelQueue returns a non-blocking channel-backed mail queue.
func NewChannelQueue(ch chan<- Message) *ChannelQueue {
	return &ChannelQueue{ch: ch}
}

// Enqueue attempts to queue msg without blocking.
func (q *ChannelQueue) Enqueue(msg Message) bool {
	select {
	case q.ch <- msg:
		return true
	default:
		return false
	}
}
