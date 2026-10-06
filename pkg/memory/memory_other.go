//go:build !windows

package memory

// clear has nothing to offer on a system without working sets: Linux and macOS already reclaim the
// pages of a process on their own, and there is no portable way to ask them to do it now.
func clear(all bool) (Result, error) {
	return Result{}, ErrUnsupported
}
