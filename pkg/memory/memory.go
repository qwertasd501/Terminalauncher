// Package memory hands the physical memory of running processes back to the system.
//
// Every process holds a working set: the pages it currently keeps in physical memory. Asking the
// system to trim that set loses no data - the pages are written to the page file, which is virtual
// memory, and read back when they are needed again - but it does take them out of physical memory
// right away. A game that has just been closed, or a launcher that has just finished its work, is
// usually holding hundreds of megabytes it will never touch again.
package memory

import (
	"errors"
	"strings"
)

// ErrUnsupported reports that freeing memory is not implemented for this system.
var ErrUnsupported = errors.New("freeing memory is not supported on this system")

// A Result describes what freeing memory did.
type Result struct {
	// Processes is the number of processes whose working set was trimmed.
	Processes int
	// Skipped is the number of processes that were left alone, either because only the game was
	// asked for or because the system did not let them be opened.
	Skipped int
	// Freed is how many bytes left physical memory, measured per process.
	Freed int64
}

// Megabytes is Freed in whole megabytes, which is the unit the launcher reports in.
func (r Result) Megabytes() int {
	return int(r.Freed / (1024 * 1024))
}

// Clear frees the physical memory the running processes are not using.
//
// Only the game and this program are trimmed unless all is set, in which case every process that
// can be opened is trimmed as well - that is what a memory cleaner does, and it also makes other
// programs slower until their pages are read back.
func Clear(all bool) (Result, error) {
	return clear(all)
}

// isGameProcess reports whether an executable name belongs to a running Minecraft: the game is a
// Java virtual machine, whatever launcher started it.
func isGameProcess(exe string) bool {
	switch strings.ToLower(strings.TrimSpace(exe)) {
	case "java", "java.exe", "javaw", "javaw.exe", "javaws.exe":
		return true
	}
	return false
}
