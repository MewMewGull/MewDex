package engine

import "sync"

// A slow session consumer must not block the stdout reader from receiving RPC
// responses. Backlog is bounded; overflow disconnects the engine instead of
// dropping ordered execution events or silently accepting control commands.
type notificationStream struct {
	mu     sync.Mutex
	queue  []Notification
	bytes  int
	closed bool
	wake   chan struct{}
	out    chan Notification
}

func newNotificationStream() *notificationStream {
	s := &notificationStream{wake: make(chan struct{}, 1), out: make(chan Notification)}
	go s.deliver()
	return s
}
func (s *notificationStream) push(n Notification) bool {
	s.mu.Lock()
	if s.closed || len(s.queue) >= 4096 || s.bytes+len(n.Params) > 32*1024*1024 {
		s.mu.Unlock()
		return false
	}
	s.queue = append(s.queue, n)
	s.bytes += len(n.Params)
	s.mu.Unlock()
	s.signal()
	return true
}
func (s *notificationStream) signal() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}
func (s *notificationStream) finish() { s.mu.Lock(); s.closed = true; s.mu.Unlock(); s.signal() }
func (s *notificationStream) deliver() {
	defer close(s.out)
	for {
		s.mu.Lock()
		if len(s.queue) == 0 {
			closed := s.closed
			s.mu.Unlock()
			if closed {
				return
			}
			<-s.wake
			continue
		}
		n := s.queue[0]
		s.queue[0] = Notification{}
		s.queue = s.queue[1:]
		s.bytes -= len(n.Params)
		if len(s.queue) == 0 {
			s.queue = nil
		}
		s.mu.Unlock()
		s.out <- n
	}
}
