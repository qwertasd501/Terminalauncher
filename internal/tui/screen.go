package tui

import (
	"fmt"
	"io"
	"strings"
)

// A screen is the block of lines a widget has on the terminal, and the only place that touches the
// screen while it runs.
//
// Repainting the whole block on every keystroke is what makes an arrow key menu flicker: the
// terminal gets one write per line, and the erased area is visible between them. A screen writes a
// frame in a single call instead, and when only a few lines changed it moves to those lines and
// leaves the rest of the block untouched - walking a list with the arrow keys only restyles the row
// the cursor left and the row it arrived at.
type screen struct {
	out   io.Writer
	lines []string // the lines currently on screen
}

// newScreen returns a screen that draws to out.
func newScreen(out io.Writer) *screen {
	return &screen{out: out}
}

// draw replaces the block with a new frame.
//
// Every line is erased before it is written, because a new line is often shorter than the old one:
// the styles differ in length too, so simply overwriting would leave the tail of the previous line
// behind.
func (s *screen) draw(lines []string) {
	var b strings.Builder
	if len(s.lines) > 0 {
		fmt.Fprintf(&b, "\x1b[%dA", len(s.lines))
		if len(lines) < len(s.lines) {
			// The new block is shorter, so the lines it no longer covers have to be cleared.
			b.WriteString("\x1b[J")
		}
	}
	for _, line := range lines {
		b.WriteString("\x1b[2K")
		b.WriteString(line)
		b.WriteString("\r\n")
	}
	io.WriteString(s.out, b.String())
	s.lines = lines
}

// update repaints the lines that differ from the frame on screen.
//
// A frame of a different length is drawn in full, because the block then covers a different number
// of lines. Curiously the cursor always sits on the line below the block, so a line is reached by
// moving relative to where the last write left the cursor: up for a line above it, down for one
// below. The cursor is returned below the block at the end, which is where the caller's next write
// expects it.
func (s *screen) update(lines []string) {
	if len(lines) != len(s.lines) {
		s.draw(lines)
		return
	}

	var b strings.Builder
	row := len(s.lines)
	for i, line := range lines {
		if line == s.lines[i] {
			continue
		}
		// The rows are visited from top to bottom, so the cursor sometimes has to jump back up and
		// sometimes further down; a negative count would be an invalid escape the terminal ignores,
		// leaving the next line's text written over this one.
		switch step := row - i; {
		case step > 0:
			fmt.Fprintf(&b, "\x1b[%dA", step)
		case step < 0:
			fmt.Fprintf(&b, "\x1b[%dB", -step)
		}
		b.WriteString("\r\x1b[2K")
		b.WriteString(line)
		row = i
	}
	if b.Len() == 0 {
		return
	}

	b.WriteString("\r")
	switch lower := len(lines) - row; {
	case lower > 0:
		fmt.Fprintf(&b, "\x1b[%dB", lower)
	case lower < 0:
		fmt.Fprintf(&b, "\x1b[%dA", -lower)
	}
	io.WriteString(s.out, b.String())
	s.lines = lines
}

// finish replaces the block with a single line, which is how a widget leaves its choice on screen
// instead of wiping the list away.
func (s *screen) finish(line string) {
	var b strings.Builder
	if len(s.lines) > 0 {
		fmt.Fprintf(&b, "\x1b[%dA\x1b[J", len(s.lines))
	}
	b.WriteString("\x1b[2K")
	b.WriteString(line)
	b.WriteString("\r\n")
	io.WriteString(s.out, b.String())
	s.lines = []string{line}
}

// current returns the lines the screen is showing.
func (s *screen) current() []string {
	return s.lines
}
