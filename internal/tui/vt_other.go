//go:build !windows

package tui

import "os"

// enableVirtualTerminal is a no-op on platforms whose terminals already interpret ANSI escapes.
func enableVirtualTerminal(f *os.File) {}
