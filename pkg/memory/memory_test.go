package memory

import (
	"errors"
	"runtime"
	"testing"
)

// TestIsGameProcess checks what "the game" means when only the game is trimmed: a Java virtual
// machine, whatever its name, and nothing else. Being too generous here would trim programs the
// user is working in.
func TestIsGameProcess(t *testing.T) {
	cases := map[string]bool{
		"javaw.exe":      true,
		"java.exe":       true,
		"javaw":          true,
		"JAVA.EXE":       true,
		" java.exe":      true,
		"explorer.exe":   false,
		"cmd.exe":        false,
		"javadoc.exe":    false,
		"minecraft.exe":  false,
		"javalauncher":   false,
		"javaw.exe.bak":  false,
		"deceased craft": false,
	}
	for exe, want := range cases {
		if got := isGameProcess(exe); got != want {
			t.Errorf("isGameProcess(%q) = %v, want %v", exe, got, want)
		}
	}
}

// TestResultMegabytes checks the unit the launcher reports in: whole megabytes, never rounded up,
// so that a small release is not announced as a larger one.
func TestResultMegabytes(t *testing.T) {
	cases := []struct {
		freed int64
		want  int
	}{
		{0, 0},
		{1024 * 1024, 1},
		{1024*1024 - 1, 0},
		{1536 * 1024, 1},
		{512 * 1024 * 1024, 512},
	}
	for _, c := range cases {
		if got := (Result{Freed: c.freed}).Megabytes(); got != c.want {
			t.Errorf("Megabytes(%d bytes) = %d, want %d", c.freed, got, c.want)
		}
	}
}

// TestClearTrimsTheLauncher checks the one process that is always there: the launcher itself. On a
// system without working sets the call has to say so instead of pretending it did something.
func TestClearTrimsTheLauncher(t *testing.T) {
	result, err := Clear(false)

	if runtime.GOOS != "windows" {
		if !errors.Is(err, ErrUnsupported) {
			t.Fatalf("Clear() on %s = %v, want ErrUnsupported", runtime.GOOS, err)
		}
		return
	}

	if err != nil {
		t.Fatalf("Clear() = %v", err)
	}
	if result.Processes == 0 {
		t.Fatal("Clear() trimmed nothing, not even this process")
	}
	if result.Freed < 0 {
		t.Fatalf("Clear() reported %d freed bytes", result.Freed)
	}
}
