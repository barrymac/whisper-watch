package api

import (
	"fmt"
	"sync"
	"testing"
)

func TestSeenSet_NewIDs(t *testing.T) {
	s := newSeenSet(5)
	if s.seen("a") {
		t.Error("first occurrence of 'a' should not be seen")
	}
	if s.seen("b") {
		t.Error("first occurrence of 'b' should not be seen")
	}
	if s.len() != 2 {
		t.Errorf("expected len 2, got %d", s.len())
	}
}

func TestSeenSet_Duplicate(t *testing.T) {
	s := newSeenSet(5)
	s.seen("x")
	if !s.seen("x") {
		t.Error("second occurrence of 'x' should be seen")
	}
	if s.len() != 1 {
		t.Errorf("duplicate should not grow len: want 1, got %d", s.len())
	}
}

func TestSeenSet_EvictsOldestWhenFull(t *testing.T) {
	s := newSeenSet(3)
	s.seen("a")
	s.seen("b")
	s.seen("c")
	if s.len() != 3 {
		t.Fatalf("expected len 3, got %d", s.len())
	}
	s.seen("d")
	if s.len() != 3 {
		t.Errorf("len should remain at cap after eviction, got %d", s.len())
	}
	if !s.seen("b") {
		t.Error("'b' should still be present (only 'a' evicted)")
	}
	if !s.seen("c") {
		t.Error("'c' should still be present (only 'a' evicted)")
	}
	if !s.seen("d") {
		t.Error("'d' should be present (just inserted)")
	}
}

func TestSeenSet_CapacityOne(t *testing.T) {
	s := newSeenSet(1)
	s.seen("first")
	if !s.seen("first") {
		t.Error("'first' should be seen")
	}
	s.seen("second")
	if s.seen("first") {
		t.Error("'first' should have been evicted by 'second'")
	}
}

func TestSeenSet_ConcurrentAccess(t *testing.T) {
	s := newSeenSet(100)
	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			id := fmt.Sprintf("id-%d", n%50)
			s.seen(id)
		}(i)
	}
	wg.Wait()
}
