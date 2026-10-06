package output

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/fatih/color"
)

// TestPaintKeepsLevelColorAcrossInnerStyles checks that a message with embedded styled segments
// (e.g. a bold instance name) does not lose the level color after each segment's reset.
func TestPaintKeepsLevelColorAcrossInnerStyles(t *testing.T) {
	prev := color.NoColor
	color.NoColor = false
	t.Cleanup(func() { color.NoColor = prev })

	blue := color.New(color.FgBlue)
	blueOpen := reopen(blue)
	if !strings.HasPrefix(blueOpen, "\x1b[") || !strings.HasSuffix(blueOpen, "m") {
		t.Fatalf("reopen returned %q", blueOpen)
	}

	body := "Deleted instance " + color.New(color.Bold).Sprint("name") + " from the list."
	painted := paint(blue, body)

	// The final reset must be the last escape, and every reset before it must be followed by the
	// level color being turned back on.
	if !strings.HasSuffix(painted, ansiReset) {
		t.Fatalf("painted output does not end with a reset: %q", painted)
	}
	inner := strings.TrimSuffix(painted, ansiReset)
	if strings.Contains(inner, ansiReset+ansiReset) {
		t.Fatalf("painted output has adjacent resets: %q", painted)
	}
	for _, at := range strings.Split(inner, ansiReset)[1:] {
		if !strings.HasPrefix(at, blueOpen) {
			t.Fatalf("level color not restored after an embedded style: %q", painted)
		}
	}

	// Plain bodies are simply wrapped, and NoColor disables everything.
	if got := paint(blue, "plain"); !strings.HasPrefix(got, blueOpen) {
		t.Fatalf("plain body not colored: %q", got)
	}
	color.NoColor = true
	if got := paint(blue, "plain"); got != "plain" {
		t.Fatalf("NoColor still wrapped the body: %q", got)
	}
}

// TestInfoLevelIsWhite pins the level colour of informational lines to bold white, so a later edit
// cannot quietly move it back to blue. It runs the real Info writer against a pipe and inspects the
// bytes, which is the only way to see the escape sequences when stdout is not a terminal.
func TestInfoLevelIsWhite(t *testing.T) {
	prevNoColor, prevOutput, prevStdout := color.NoColor, color.Output, os.Stdout
	color.NoColor = false

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	color.Output, os.Stdout = writer, writer
	t.Cleanup(func() {
		color.NoColor, color.Output, os.Stdout = prevNoColor, prevOutput, prevStdout
		writer.Close()
		reader.Close()
	})

	// A bold instance name inside the message exercises the reset-then-reopen path.
	Info("deleted %s", color.New(color.Bold).Sprint("name"))
	writer.Close()
	out, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)

	if strings.Contains(got, "\x1b[34m") || strings.Contains(got, "\x1b[94m") {
		t.Fatalf("info line still uses a blue escape: %q", got)
	}
	// The "| " prefix and the body are both painted bold white.
	if !strings.HasPrefix(got, "\x1b[1;37m| ") {
		t.Fatalf("info prefix is not bold white: %q", got)
	}
	if n := strings.Count(got, "\x1b[1;37m"); n < 2 {
		t.Fatalf("bold white escape appears %d times, want at least 2: %q", n, got)
	}
	// The line ends with a reset back to the terminal default, plus the newline.
	if !strings.HasSuffix(got, "0m\n") {
		t.Fatalf("info line does not end with a reset and newline: %q", got)
	}

	if got := reopen(color.New(color.Bold, color.FgWhite)); !strings.HasPrefix(got, "\x1b[1;37m") {
		t.Fatalf("reopen(bold white) = %q, want a bold white prefix", got)
	}
}
