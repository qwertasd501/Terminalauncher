package tui

import (
	"bytes"
	"strings"
	"testing"

	"github.com/fatih/color"
)

// testStyle returns the colour set a picker draws with.
//
// Colour is forced on: the cursor is drawn with colour only, so with colour disabled the frames
// before and after a cursor move are identical and there would be nothing to compare.
func testStyle(t *testing.T) pickerStyle {
	t.Helper()
	previous := color.NoColor
	color.NoColor = false
	t.Cleanup(func() { color.NoColor = previous })

	return pickerStyle{
		Bold:     color.New(color.Bold),
		Selected: color.New(color.FgCyan, color.Bold),
		Starred:  color.New(color.FgYellow),
		Dim:      color.New(color.Faint),
	}
}

// differingRows lists the rows two frames of the same length disagree on.
func differingRows(before, after []string) []int {
	var rows []int
	for i := range before {
		if before[i] != after[i] {
			rows = append(rows, i)
		}
	}
	return rows
}

// TestPickerFrameOnlyRestylesTwoRows is the flicker fix in numbers: moving the cursor has to change
// exactly the row it left and the row it arrived at, so the screen can repaint those two lines and
// leave the rest of the menu alone. The heading row is only present when the picker has a title.
func TestPickerFrameOnlyRestylesTwoRows(t *testing.T) {
	style := testStyle(t)

	t.Run("with a title", func(t *testing.T) {
		p := &Picker{Title: "Pick", Items: []string{"alpha", "beta", "gamma", "delta"}}
		rows := p.rows(0)

		before := p.frameLines(0, entryRow(rows, 0), rows, style)
		after := p.frameLines(0, entryRow(rows, 1), rows, style)

		if len(before) != len(after) {
			t.Fatalf("the frame changed size: %d lines, then %d", len(before), len(after))
		}
		changed := differingRows(before, after)
		if len(changed) != 2 || changed[0] != 1 || changed[1] != 2 {
			t.Errorf("moving the cursor changed rows %v, want the two entry rows [1 2]\nafter:  %q\nbefore: %q", changed, after, before)
		}
		if before[0] != after[0] {
			t.Error("the heading changed while moving the cursor")
		}
	})

	t.Run("without a title", func(t *testing.T) {
		p := &Picker{Items: []string{"alpha", "beta", "gamma", "delta"}}
		rows := p.rows(0)

		before := p.frameLines(0, entryRow(rows, 0), rows, style)
		after := p.frameLines(0, entryRow(rows, 1), rows, style)

		if len(before) != len(after) {
			t.Fatalf("the frame changed size: %d lines, then %d", len(before), len(after))
		}
		changed := differingRows(before, after)
		if len(changed) != 2 || changed[0] != 0 || changed[1] != 1 {
			t.Errorf("moving the cursor changed rows %v, want the two entry rows [0 1]\nafter:  %q\nbefore: %q", changed, after, before)
		}
		if strings.Contains(before[0], ">") && strings.Count(before[0], "alpha") == 0 {
			t.Errorf("an untitled picker printed a heading row: %q", before[0])
		}
	})
}

// TestScreenUpdateMovesDownBetweenChangedLines is the regression test for the stray-line bug: when
// two changed lines are not adjacent, the cursor has to move DOWN from the first to the second. The
// old code emitted a negative "up" escape, which terminals ignore, so the lower line's text was
// written over the upper one - the language menu then showed the same entry twice.
func TestScreenUpdateMovesDownBetweenChangedLines(t *testing.T) {
	var out bytes.Buffer
	s := newScreen(&out)
	s.draw([]string{"one", "two", "three", "four", "five"})
	out.Reset()

	s.update([]string{"one", "TWO", "three", "four", "FIVE"})

	got := out.String()
	// Up four to line 1, write it; down three to line 4, write it; back below the block.
	if want := "\x1b[4A\r\x1b[2KTWO\x1b[3B\r\x1b[2KFIVE\r\x1b[1B"; got != want {
		t.Errorf("update wrote %q, want %q", got, want)
	}
	if strings.Contains(got, "\x1b[-") {
		t.Errorf("update emitted a negative move, which terminals ignore: %q", got)
	}
}

// TestScreenUpdateMovesUpForALoneChangedLine covers the opposite order: the last changed line above
// the previous one leaves the cursor above the block, and the trailing move has to go up.
func TestScreenUpdateMovesUpForALoneChangedLine(t *testing.T) {
	var out bytes.Buffer
	s := newScreen(&out)
	s.draw([]string{"one", "two", "three"})
	out.Reset()

	s.update([]string{"ONE", "two", "three"})

	got := out.String()
	if want := "\x1b[3A\r\x1b[2KONE\r\x1b[3B"; got != want {
		t.Errorf("update wrote %q, want %q", got, want)
	}
}

// TestScreenDrawWritesOneBlock checks the initial frame: one erased line per row, and the block
// count kept so that the next write knows how far up the block starts.
func TestScreenDrawWritesOneBlock(t *testing.T) {
	var out bytes.Buffer
	s := newScreen(&out)

	s.draw([]string{"one", "two", "three"})

	got := out.String()
	if want := "\x1b[2Kone\r\n\x1b[2Ktwo\r\n\x1b[2Kthree\r\n"; got != want {
		t.Errorf("first frame is %q, want %q", got, want)
	}
	if lines := s.current(); len(lines) != 3 {
		t.Errorf("the screen remembers %d lines, want 3", len(lines))
	}
}

// TestScreenUpdateOnlyTouchesChangedLines checks the case the flicker came from: a cursor move must
// not clear the block, and must not rewrite the lines that stayed the same.
func TestScreenUpdateOnlyTouchesChangedLines(t *testing.T) {
	var out bytes.Buffer
	s := newScreen(&out)
	s.draw([]string{"one", "two", "three"})
	out.Reset()

	s.update([]string{"one", "TWO", "three"})

	got := out.String()
	// Up two lines to reach row 1, erase it, write it, then back down to the line below the block.
	if want := "\x1b[2A\r\x1b[2KTWO\r\x1b[2B"; got != want {
		t.Errorf("update wrote %q, want %q", got, want)
	}
	if strings.Contains(got, "\x1b[J") {
		t.Error("the update cleared the whole block, which is what makes the menu flash")
	}
	if strings.Count(got, "\x1b[2K") != 1 {
		t.Errorf("the update erased %d lines, want only the one that changed", strings.Count(got, "\x1b[2K"))
	}
}

// TestScreenUpdateSkipsUnchangedFrames checks that a frame identical to the one on screen writes
// nothing at all, so a key that changes nothing cannot make the menu blink.
func TestScreenUpdateSkipsUnchangedFrames(t *testing.T) {
	var out bytes.Buffer
	s := newScreen(&out)
	s.draw([]string{"one", "two"})
	out.Reset()

	s.update([]string{"one", "two"})

	if out.Len() != 0 {
		t.Errorf("an unchanged frame wrote %q", out.String())
	}
}

// TestScreenUpdateRedrawsWhenTheBlockResizes checks the fallback: a frame with a different number of
// lines cannot be patched line by line, so it is drawn again, clearing the lines it no longer covers.
func TestScreenUpdateRedrawsWhenTheBlockResizes(t *testing.T) {
	var out bytes.Buffer
	s := newScreen(&out)
	s.draw([]string{"one", "two", "three"})
	out.Reset()

	s.update([]string{"one"})

	got := out.String()
	if !strings.Contains(got, "\x1b[3A\x1b[J") {
		t.Errorf("a shorter frame did not clear the old block: %q", got)
	}
	if strings.Count(got, "\x1b[2K") != 1 {
		t.Errorf("the new frame erased %d lines, want 1: %q", strings.Count(got, "\x1b[2K"), got)
	}
	if want := "\x1b[2Kone\r\n"; !strings.HasSuffix(got, want) {
		t.Errorf("the new frame ends with %q, want %q", got, want)
	}
}

// TestScreenFinishLeavesOneLine checks how a widget ends: the block is replaced by the one line the
// caller keeps on screen.
func TestScreenFinishLeavesOneLine(t *testing.T) {
	var out bytes.Buffer
	s := newScreen(&out)
	s.draw([]string{"one", "two"})
	out.Reset()

	s.finish("chosen")

	if got, want := out.String(), "\x1b[2A\x1b[J\x1b[2Kchosen\r\n"; got != want {
		t.Errorf("finish wrote %q, want %q", got, want)
	}
	if lines := s.current(); len(lines) != 1 || lines[0] != "chosen" {
		t.Errorf("the screen remembers %q, want the single chosen line", lines)
	}
}
