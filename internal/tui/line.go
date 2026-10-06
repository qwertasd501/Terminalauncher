package tui

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// ErrEOF reports that input ended.
var ErrEOF = errors.New("end of input")

// ErrInterrupted reports that the user cancelled the line being edited.
var ErrInterrupted = errors.New("interrupted")

// A Completer returns the candidates for the word that ends at the cursor.
//
// start is the index in the line where the candidates replace existing text.
type Completer func(line []rune, cursor int) (candidates []string, start int)

// Key kinds reported by readLineKey.
const (
	lineKeyRune = iota
	lineKeyEnter
	lineKeyBackspace
	lineKeyDelete
	lineKeyLeft
	lineKeyRight
	lineKeyHome
	lineKeyEnd
	lineKeyUp
	lineKeyDown
	lineKeyTab
	lineKeyInterrupt
	lineKeyEOF
	lineKeyEscape
	lineKeyClearLine
	lineKeyKillWord
	lineKeyIgnore
)

// A LineReader reads a line of input with cursor editing, history and tab completion.
type LineReader struct {
	Prompt   string // Printed before the input. May contain colour codes.
	Complete Completer
	History  []string
	In       *os.File // Defaults to os.Stdin
	Out      io.Writer

	reader *bufio.Reader
}

func (r *LineReader) out() io.Writer {
	if r.Out == nil {
		return os.Stdout
	}
	return r.Out
}

func (r *LineReader) in() *os.File {
	if r.In == nil {
		return os.Stdin
	}
	return r.In
}

// buffered returns the reader used for reading, creating it once.
//
// A single reader is kept so that bytes buffered by one read are not lost by the next.
func (r *LineReader) buffered() *bufio.Reader {
	if r.reader == nil {
		r.reader = bufio.NewReader(r.in())
	}
	return r.reader
}

// interactive reports whether a terminal is available for editing.
func (r *LineReader) interactive() bool {
	return term.IsTerminal(int(r.in().Fd()))
}

// ReadLine reads one line, returning ErrEOF at end of input and ErrInterrupted when the user
// cancels with ctrl-c.
func (r *LineReader) ReadLine() (string, error) {
	if !r.interactive() {
		return r.readLinePlain()
	}

	out := r.out()
	enableVirtualTerminal(r.in())
	if f, ok := out.(*os.File); ok {
		enableVirtualTerminal(f)
	}

	state, err := term.MakeRaw(int(r.in().Fd()))
	if err != nil {
		return r.readLinePlain()
	}
	defer term.Restore(int(r.in().Fd()), state)

	in := r.buffered()
	buf := make([]rune, 0, 64)
	cursor := 0
	history := len(r.History)

	draw := func() {
		fmt.Fprintf(out, "\r%s\x1b[J", r.Prompt+string(buf))
		if trailing := len(buf) - cursor; trailing > 0 {
			fmt.Fprintf(out, "\x1b[%dD", trailing)
		}
	}

	draw()
	for {
		kind, ch, err := readLineKey(in)
		if err != nil {
			fmt.Fprint(out, "\r\n")
			return "", ErrEOF
		}

		switch kind {
		case lineKeyEnter:
			fmt.Fprint(out, "\r\n")
			return string(buf), nil
		case lineKeyInterrupt:
			fmt.Fprint(out, "^C\r\n")
			return "", ErrInterrupted
		case lineKeyEOF:
			if len(buf) == 0 {
				fmt.Fprint(out, "\r\n")
				return "", ErrEOF
			}
			if cursor < len(buf) {
				buf = append(buf[:cursor], buf[cursor+1:]...)
			}
		case lineKeyBackspace:
			if cursor > 0 {
				buf = append(buf[:cursor-1], buf[cursor:]...)
				cursor--
			}
		case lineKeyLeft:
			if cursor > 0 {
				cursor--
			}
		case lineKeyRight:
			if cursor < len(buf) {
				cursor++
			}
		case lineKeyHome:
			cursor = 0
		case lineKeyEnd:
			cursor = len(buf)
		case lineKeyClearLine:
			buf, cursor = buf[:0], 0
		case lineKeyKillWord:
			for cursor > 0 && buf[cursor-1] == ' ' {
				buf = append(buf[:cursor-1], buf[cursor:]...)
				cursor--
			}
			for cursor > 0 && buf[cursor-1] != ' ' {
				buf = append(buf[:cursor-1], buf[cursor:]...)
				cursor--
			}
		case lineKeyUp:
			if history > 0 {
				history--
				buf, cursor = []rune(r.History[history]), len([]rune(r.History[history]))
			}
		case lineKeyDown:
			if history < len(r.History)-1 {
				history++
				buf, cursor = []rune(r.History[history]), len([]rune(r.History[history]))
			} else {
				history = len(r.History)
				buf, cursor = buf[:0], 0
			}
		case lineKeyTab:
			if r.Complete != nil {
				candidates, start := r.Complete(buf, cursor)
				// Several candidates open a list the user walks with the arrow keys, which is what makes
				// completion usable for instance names, versions and accounts.
				if len(candidates) > 1 {
					fmt.Fprint(out, "\r\x1b[J")
					chosen, ok := r.pickCompletion(candidates)
					fmt.Fprint(out, "\r\x1b[J")
					if ok {
						buf, cursor = replaceRange(buf, start, cursor, completionText(chosen))
					}
					break
				}
				applyCompletion(candidates, start, &buf, &cursor, out)
			}
		case lineKeyRune:
			buf = insertRune(buf, cursor, ch)
			cursor++
		}

		draw()
	}
}

// readLinePlain reads a line without editing, for piped input or when raw mode is unavailable.
func (r *LineReader) readLinePlain() (string, error) {
	fmt.Fprint(r.out(), r.Prompt)

	line, err := r.buffered().ReadString('\n')
	if err != nil {
		if line == "" {
			return "", ErrEOF
		}
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// insertRune returns buf with ch inserted at index i.
func insertRune(buf []rune, i int, ch rune) []rune {
	buf = append(buf, 0)
	copy(buf[i+1:], buf[i:])
	buf[i] = ch
	return buf
}

// replaceRange returns buf with [start,end) replaced by text, and the new cursor position.
func replaceRange(buf []rune, start, end int, text string) ([]rune, int) {
	replacement := []rune(text)
	out := make([]rune, 0, len(buf)-(end-start)+len(replacement))
	out = append(out, buf[:start]...)
	out = append(out, replacement...)
	out = append(out, buf[end:]...)
	return out, start + len(replacement)
}

// completionText returns the text a candidate inserts, adding a separating space after a finished
// word. A directory is left without it, so that the next path component can be typed straight away.
func completionText(candidate string) string {
	if strings.HasSuffix(candidate, "/") || strings.HasSuffix(candidate, `\`) {
		return candidate
	}
	return candidate + " "
}

// pickCompletion shows the completion candidates and returns the entry the user picked.
//
// The picker takes over the keyboard itself, which is why this is only reachable on a terminal.
// The caller's reader is handed over so that input already buffered is not lost.
func (r *LineReader) pickCompletion(candidates []string) (string, bool) {
	// The candidate list opens without a heading: the candidates themselves say enough.
	picker := Picker{
		Items:  candidates,
		Reader: r.buffered(),
		In:     r.in(),
		Out:    r.out(),
	}
	index, err := picker.Select()
	if err != nil || index < 0 || index >= len(candidates) {
		return "", false
	}
	return candidates[index], true
}

// applyCompletion inserts the completed text, or lists the candidates when there is nothing more
// to add.
func applyCompletion(candidates []string, start int, buf *[]rune, cursor *int, out io.Writer) {
	if len(candidates) == 0 || start > *cursor {
		return
	}
	current := string((*buf)[start:*cursor])

	if len(candidates) == 1 {
		*buf, *cursor = replaceRange(*buf, start, *cursor, completionText(candidates[0]))
		return
	}

	// Several candidates: extend to the longest shared prefix when that adds anything.
	prefix := commonPrefix(candidates)
	if len(prefix) > len(current) {
		*buf, *cursor = replaceRange(*buf, start, *cursor, prefix)
		return
	}

	// Otherwise list them, and let the caller redraw the prompt line.
	fmt.Fprint(out, "\r\x1b[J")
	for _, candidate := range candidates {
		fmt.Fprintf(out, "%s\r\n", candidate)
	}
}

// commonPrefix returns the longest prefix shared by every entry.
func commonPrefix(values []string) string {
	if len(values) == 0 {
		return ""
	}
	prefix := values[0]
	for _, value := range values[1:] {
		for !strings.HasPrefix(value, prefix) {
			r := []rune(prefix)
			if len(r) == 0 {
				return ""
			}
			prefix = string(r[:len(r)-1])
		}
	}
	return prefix
}

// readLineKey decodes one key press, including common escape sequences.
func readLineKey(r *bufio.Reader) (int, rune, error) {
	ch, _, err := r.ReadRune()
	if err != nil {
		return lineKeyEOF, 0, err
	}

	switch ch {
	case '\r', '\n':
		return lineKeyEnter, 0, nil
	case 0x7f, 0x08:
		return lineKeyBackspace, 0, nil
	case 0x03:
		return lineKeyInterrupt, 0, nil
	case 0x04:
		return lineKeyEOF, 0, nil
	case 0x01:
		return lineKeyHome, 0, nil
	case 0x05:
		return lineKeyEnd, 0, nil
	case 0x15:
		return lineKeyClearLine, 0, nil
	case 0x17:
		return lineKeyKillWord, 0, nil
	case '\t':
		return lineKeyTab, 0, nil
	case 0x1b:
		return readEscape(r)
	}
	return lineKeyRune, ch, nil
}

// readEscape decodes the sequence that follows an escape byte.
//
// A lone escape, which is what pressing esc produces, cannot be distinguished from the start of a
// sequence by reading alone, so anything not already buffered is treated as a sequence that will
// never arrive.
func readEscape(r *bufio.Reader) (int, rune, error) {
	if r.Buffered() == 0 {
		return lineKeyEscape, 0, nil
	}

	second, _, err := r.ReadRune()
	if err != nil {
		return lineKeyEscape, 0, nil
	}
	if second != '[' && second != 'O' {
		return lineKeyEscape, 0, nil
	}

	// Modifier parameters, such as the "1;5" of ctrl+left.
	for r.Buffered() > 0 {
		peek, err := r.Peek(1)
		if err != nil || len(peek) == 0 {
			break
		}
		if (peek[0] >= '0' && peek[0] <= '9') || peek[0] == ';' {
			_, _ = r.ReadByte()
			continue
		}
		break
	}
	if r.Buffered() == 0 {
		return lineKeyEscape, 0, nil
	}

	third, _, err := r.ReadRune()
	if err != nil {
		return lineKeyEscape, 0, nil
	}
	switch third {
	case 'A':
		return lineKeyUp, 0, nil
	case 'B':
		return lineKeyDown, 0, nil
	case 'C':
		return lineKeyRight, 0, nil
	case 'D':
		return lineKeyLeft, 0, nil
	case 'H':
		return lineKeyHome, 0, nil
	case 'F':
		return lineKeyEnd, 0, nil
	case '3':
		if next, _, err := r.ReadRune(); err == nil && next == '~' {
			return lineKeyDelete, 0, nil
		}
	}
	return lineKeyIgnore, 0, nil
}
