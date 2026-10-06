//go:build windows

package tui

import (
	"os"

	"golang.org/x/sys/windows"
)

// enableVirtualTerminalProcessing lets the Windows console interpret ANSI escapes.
//
// The constant is spelled out rather than taken from x/sys so the build does not depend on the
// exact version of that module.
const enableVirtualTerminalProcessing = 0x0004

func enableVirtualTerminal(f *os.File) {
	if f == nil {
		return
	}
	handle := windows.Handle(f.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(handle, &mode); err != nil {
		return
	}
	if mode&enableVirtualTerminalProcessing != 0 {
		return
	}
	_ = windows.SetConsoleMode(handle, mode|enableVirtualTerminalProcessing)
}
