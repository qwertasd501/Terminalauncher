package concurrent_test

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/telecter/cmd-launcher/internal/concurrent"
)

// TestDoVisitsEveryIndexOnce checks the contract the callers rely on: every index is handed to fn
// exactly once, so a caller can fill a slice by index and read it afterwards.
func TestDoVisitsEveryIndexOnce(t *testing.T) {
	const n = 500
	seen := make([]int32, n)

	concurrent.Do(n, 8, func(i int) {
		atomic.AddInt32(&seen[i], 1)
	})

	for i, count := range seen {
		if count != 1 {
			t.Fatalf("index %d was visited %d times", i, count)
		}
	}
}

// TestDoBoundsConcurrency checks that the pool never runs more items at once than it was told to,
// and that it does use more than one goroutine when asked.
func TestDoBoundsConcurrency(t *testing.T) {
	const n = 40
	const limit = 4

	var running, peak int32
	concurrent.Do(n, limit, func(int) {
		current := atomic.AddInt32(&running, 1)
		for {
			old := atomic.LoadInt32(&peak)
			if current <= old || atomic.CompareAndSwapInt32(&peak, old, current) {
				break
			}
		}
		time.Sleep(time.Millisecond)
		atomic.AddInt32(&running, -1)
	})

	if peak > limit {
		t.Errorf("ran %d items at once, limit was %d", peak, limit)
	}
	if peak < 2 {
		t.Errorf("never ran more than one item at once (peak %d)", peak)
	}
}

// TestDoEdgeCases checks the empty list and the limits that have to be clamped.
func TestDoEdgeCases(t *testing.T) {
	calls := 0
	concurrent.Do(0, 4, func(int) { calls++ })
	if calls != 0 {
		t.Errorf("empty batch called fn %d times", calls)
	}

	// A limit below one must still run the work, one item at a time.
	var mu sync.Mutex
	concurrent.Do(3, 0, func(int) {
		mu.Lock()
		calls++
		mu.Unlock()
	})
	if calls != 3 {
		t.Errorf("limit 0 called fn %d times, want 3", calls)
	}

	// A limit above the number of items must not block or skip anything.
	calls = 0
	concurrent.Do(2, 64, func(int) { calls++ })
	if calls != 2 {
		t.Errorf("oversized limit called fn %d times, want 2", calls)
	}
}

// TestMapKeepsOrder checks that results come back in the order of the input, even though the work
// finishes out of order.
func TestMapKeepsOrder(t *testing.T) {
	items := []int{20, 1, 15, 3, 8, 2}
	results := concurrent.Map(items, 6, func(i, item int) int {
		// The first entries sleep the longest, so completion order is the reverse of input order.
		time.Sleep(time.Duration(len(items)-i) * time.Millisecond)
		return item * 2
	})

	want := []int{40, 2, 30, 6, 16, 4}
	for i := range want {
		if results[i] != want[i] {
			t.Fatalf("results[%d] = %d, want %d", i, results[i], want[i])
		}
	}
}
