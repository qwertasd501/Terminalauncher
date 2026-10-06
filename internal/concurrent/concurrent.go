// Package concurrent runs independent work items in parallel with a bounded number of goroutines.
//
// The launcher spends most of its time waiting on the network: a version has a few hundred libraries
// and a few thousand assets, and a large instance has hundreds of mods to look up. Running those one
// after another leaves the connection idle between requests, so every bulk job here is spread over a
// fixed pool instead.
package concurrent

import "sync"

// Do calls fn for every index in [0,n) using at most limit goroutines.
//
// It returns once every call has finished. Each index is visited exactly once, and only the calling
// goroutine returns, so fn may write to a slice the caller owns without further locking: by the time
// Do returns, every write has happened.
//
// A limit below one is treated as one, and a limit above n is capped at n, so the pool never spawns
// more goroutines than there is work for.
func Do(n, limit int, fn func(i int)) {
	if n <= 0 {
		return
	}
	if limit < 1 {
		limit = 1
	}
	if limit > n {
		limit = n
	}

	next := make(chan int)
	var wg sync.WaitGroup
	wg.Add(limit)
	for worker := 0; worker < limit; worker++ {
		go func() {
			defer wg.Done()
			for i := range next {
				fn(i)
			}
		}()
	}

	for i := 0; i < n; i++ {
		next <- i
	}
	close(next)
	wg.Wait()
}

// Map calls fn for every entry of items, in parallel, and returns the results in the order of items.
//
// The ordering matters for the callers that print a table: a bulk update has to list the mods in the
// order they are installed, not in the order the network answered.
func Map[T any, R any](items []T, limit int, fn func(i int, item T) R) []R {
	results := make([]R, len(items))
	Do(len(items), limit, func(i int) {
		results[i] = fn(i, items[i])
	})
	return results
}
