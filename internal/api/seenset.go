package api

import "sync"

// seenSet is a fixed-capacity ring buffer. When full, the oldest entry
// is silently evicted. Necessary because the index arithmetic is non-obvious.
type seenSet struct {
	mu   sync.Mutex
	cap  int
	ring []string
	head int
	size int
}

func newSeenSet(capacity int) *seenSet {
	return &seenSet{
		cap:  capacity,
		ring: make([]string, capacity),
	}
}

func (s *seenSet) seen(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := 0; i < s.size; i++ {
		idx := (s.head - 1 - i + s.cap) % s.cap
		if s.ring[idx] == id {
			return true
		}
	}
	s.ring[s.head] = id
	s.head = (s.head + 1) % s.cap
	if s.size < s.cap {
		s.size++
	}
	return false
}

// len returns the number of IDs currently tracked.
func (s *seenSet) len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.size
}
