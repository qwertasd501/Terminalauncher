// Package tui provides small interactive terminal widgets used by the shell.
package tui

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/fatih/color"
	"golang.org/x/term"
)

// ErrCancelled is returned when the user aborts a widget.
var ErrCancelled = errors.New("cancelled")

// A LineScanner is the subset of bufio.Scanner a widget needs to read input when standard input
// is not a terminal. The caller must pass the same reader it uses itself, so that input already
// buffered by the caller is not lost.
type LineScanner interface {
	Scan() bool
	Text() string
}

// A Picker is a list the user navigates with the arrow keys, confirming with enter.
//
// A list longer than one page is shown a page at a time, ending with a row that turns to the next
// page, the way PCL's version lists behave.
type Picker struct {
	Title   string   // Heading printed above the list
	Items   []string // Entries to select from
	Initial int      // Index selected when the picker opens
	Star    bool     // Draw a "*" on the Initial entry, marking the recommended choice

	// PageSize is how many entries one page shows. Zero selects a default.
	PageSize int

	Lines LineScanner
	In    *os.File // Defaults to os.Stdin when nil
	Out   io.Writer

	// NextLabel is the text of the row that opens the following page; an empty value uses the
	// picker's own wording. The shell replaces these with translated text.
	NextLabel string
	PrevLabel string
	PageLabel string

	// Reader is used for keyboard input instead of a new reader over In. A caller which has already
	// buffered input, such as the line editor, passes its own reader so nothing is lost.
	Reader *bufio.Reader
}

// defaultPageSize is how many entries a picker draws at once when it is not told otherwise. Twenty
// entries fit a normal terminal together with the heading and the page row.
const defaultPageSize = 20

const (
	keyUp = iota + 1
	keyDown
	keyHome
	keyEnd
	keyPageUp
	keyPageDown
	keyEnter
	keyAbort
)

// selectable reports whether the picker can drive a terminal itself.
func (p *Picker) selectable() bool {
	in := p.In
	if in == nil {
		in = os.Stdin
	}
	return term.IsTerminal(int(in.Fd()))
}

// out returns the writer the picker draws to.
func (p *Picker) out() io.Writer {
	if p.Out == nil {
		return os.Stdout
	}
	return p.Out
}

// pageSize returns how many entries fit on one page.
func (p *Picker) pageSize() int {
	if p.PageSize > 0 {
		return p.PageSize
	}
	return defaultPageSize
}

// Labels are the texts the widgets print around a list. The shell translates them once at
// startup, so that a widget never has to know about the launcher's language files.
type Labels struct {
	Next    string // The row that opens the following page
	Prev    string // The row that goes back a page
	Page    string // The word before the "2/45" of a heading
	Select  string // The numbered prompt; $1 is the item count, $2 the preselected number
	Invalid string // The error for an answer that is not a number; $1 is the quoted answer
	Empty   string // The error for a picker without entries
}

// defaultLabels is the wording every widget falls back to.
var defaultLabels Labels

// SetLabels sets the wording used by widgets that were not given their own.
func SetLabels(labels Labels) {
	defaultLabels = labels
}

// expand replaces the "$1", "$2", ... placeholders of a translated text with the arguments.
// Placeholders instead of printf verbs keep the translations free of format strings, which the
// widgets fill in without vet-visible printf calls.
func expand(text string, args ...string) string {
	for i, arg := range args {
		text = strings.ReplaceAll(text, "$"+strconv.Itoa(i+1), arg)
	}
	return text
}

// label returns the wording to use, preferring the widget's own over the session's and the
// built-in English over both.
func label(custom, session, fallback string) string {
	if strings.TrimSpace(custom) != "" {
		return custom
	}
	if strings.TrimSpace(session) != "" {
		return session
	}
	return fallback
}

// nextLabel is the text of the row that opens the following page.
func (p *Picker) nextLabel() string {
	return label(p.NextLabel, defaultLabels.Next, "more ->")
}

// prevLabel is the text of the row that goes back a page.
func (p *Picker) prevLabel() string {
	return label(p.PrevLabel, defaultLabels.Prev, "<- previous page")
}

// pageWord is the word printed before the page number.
func (p *Picker) pageWord() string {
	return label(p.PageLabel, defaultLabels.Page, "page")
}

// pageBounds returns the first and last entry index, last exclusive, of a page.
func pageBounds(total, page, size int) (first, last int) {
	if size <= 0 {
		size = defaultPageSize
	}
	if page < 0 {
		page = 0
	}
	first = page * size
	if first > total {
		first = total
	}
	last = first + size
	if last > total {
		last = total
	}
	return first, last
}

// pageOf returns the page an entry belongs to.
func pageOf(index, size int) int {
	if size <= 0 {
		size = defaultPageSize
	}
	if index < 0 {
		return 0
	}
	return index / size
}

// pageCount returns how many pages a list of the given length needs, at least one.
func pageCount(total, size int) int {
	if size <= 0 {
		size = defaultPageSize
	}
	if total <= 0 {
		return 1
	}
	return (total + size - 1) / size
}

// A pickerRow is one line of the picker: an entry of the list, or the row that turns the page.
type pickerRow struct {
	index int    // Index into Items, or -1 for a page row.
	label string // Shown for a page row only.
	page  int    // Page the row turns to, for a page row.
}

// rows builds the lines of one page, including the row that leaves it.
func (p *Picker) rows(page int) []pickerRow {
	size := p.pageSize()
	total := len(p.Items)
	first, last := pageBounds(total, page, size)

	var out []pickerRow
	if page > 0 {
		out = append(out, pickerRow{
			index: -1,
			label: p.prevLabel(),
			page:  page - 1,
		})
	}
	for i := first; i < last; i++ {
		out = append(out, pickerRow{index: i})
	}
	if last < total {
		out = append(out, pickerRow{
			index: -1,
			label: fmt.Sprintf("%s (%d)", p.nextLabel(), total-last),
			page:  page + 1,
		})
	}
	return out
}

// heading returns the title with the page number appended when there is more than one page. A
// picker without a title shows no heading at all - no hint line, no page counter.
func (p *Picker) heading(page int) string {
	title := p.Title
	if title == "" {
		return ""
	}
	count := pageCount(len(p.Items), p.pageSize())
	if count <= 1 {
		return title
	}
	return fmt.Sprintf("%s %s %d/%d", title, p.pageWord(), page+1, count)
}

// marker returns the two-character gutter drawn before an entry.
func marker(index, current int, starred bool) string {
	switch {
	case index == current && starred:
		return ">*"
	case index == current:
		return " >"
	case starred:
		return " *"
	}
	return "  "
}

// Select runs the picker and returns the index of the chosen item.
//
// When standard input is not a terminal it falls back to a numbered prompt read from Lines,
// which keeps the widget usable from scripts and tests.
func (p *Picker) Select() (int, error) {
	if len(p.Items) == 0 {
		return -1, errors.New(label("", defaultLabels.Empty, "nothing to select from"))
	}
	if p.Initial < 0 || p.Initial >= len(p.Items) {
		p.Initial = 0
	}
	if !p.selectable() {
		return p.selectNumbered()
	}
	return p.selectArrowKeys()
}

// selectNumbered is the non-interactive fallback.
//
// One page is printed at a time, and "n" turns to the next page, so that a list of hundreds of
// versions stays readable and can still be walked from a script.
func (p *Picker) selectNumbered() (int, error) {
	out := p.out()
	size := p.pageSize()
	page := pageOf(p.Initial, size)

	for {
		if heading := p.heading(page); heading != "" {
			fmt.Fprintln(out, heading)
		}

		first, last := pageBounds(len(p.Items), page, size)
		for i := first; i < last; i++ {
			lead := "  "
			if i == p.Initial {
				lead = "* "
			}
			fmt.Fprintf(out, "%s%d) %s\n", lead, i+1, p.Items[i])
		}
		if last < len(p.Items) {
			fmt.Fprintf(out, "   %s (%d)\n", p.nextLabel(), len(p.Items)-last)
		}
		fmt.Fprint(out, expand(label("", defaultLabels.Select, "Select [1-$1] (enter for $2): "),
			strconv.Itoa(len(p.Items)), strconv.Itoa(p.Initial+1)))

		if p.Lines == nil || !p.Lines.Scan() {
			return -1, ErrCancelled
		}
		// A terminal echoes the typed answer, a pipe does not, so end the prompt line here. Without
		// this, anything printed next - the heading of a nested list, or the shell prompt - would
		// appear on the same line as "Select [...]".
		fmt.Fprintln(out)
		answer := strings.TrimSpace(p.Lines.Text())
		switch strings.ToLower(answer) {
		case "":
			return p.Initial, nil
		case "n", "next", ">":
			if last < len(p.Items) {
				page++
			}
			continue
		case "p", "prev", "previous", "<":
			if page > 0 {
				page--
			}
			continue
		}

		n, err := strconv.Atoi(answer)
		if err != nil || n < 1 || n > len(p.Items) {
			return -1, errors.New(expand(label("", defaultLabels.Invalid, "invalid choice $1"), strconv.Quote(answer)))
		}
		return n - 1, nil
	}
}

// selectArrowKeys is the interactive path, using raw mode and ANSI escapes.
func (p *Picker) selectArrowKeys() (int, error) {
	in := p.In
	if in == nil {
		in = os.Stdin
	}
	out := p.out()

	enableVirtualTerminal(in)
	if f, ok := out.(*os.File); ok {
		enableVirtualTerminal(f)
	}

	state, err := term.MakeRaw(int(in.Fd()))
	if err != nil {
		return -1, err
	}
	defer term.Restore(int(in.Fd()), state)

	reader := p.Reader
	if reader == nil {
		reader = bufio.NewReader(in)
	}

	style := pickerStyle{
		Bold:     color.New(color.Bold),
		Selected: color.New(color.FgCyan, color.Bold),
		Starred:  color.New(color.FgYellow),
		Dim:      color.New(color.Faint),
	}

	page := pageOf(p.Initial, p.pageSize())
	rows := p.rows(page)
	cursor := entryRow(rows, p.Initial)

	// lines renders the whole block: the heading, then one line per row.
	lines := func() []string {
		return p.frameLines(page, cursor, rows, style)
	}

	fmt.Fprint(out, "\x1b[?25l")       // hide the cursor
	defer fmt.Fprint(out, "\x1b[?25h") // restore it on the way out

	// The screen owns every write from here on: it knows how many lines the block occupies, and
	// only redraws the ones that changed, so moving the cursor does not repaint the menu.
	view := newScreen(out)
	view.draw(lines())

	for {
		key, err := readKey(reader)
		if err != nil {
			return -1, err
		}

		switch key {
		case keyUp:
			cursor--
			if cursor < 0 {
				cursor = len(rows) - 1
			}
		case keyDown:
			cursor++
			if cursor >= len(rows) {
				cursor = 0
			}
		case keyHome:
			cursor = entryRow(rows, 0)
		case keyEnd:
			cursor = lastEntryRow(rows)
		case keyPageUp, keyPageDown:
			target := page - 1
			if key == keyPageDown {
				target = page + 1
			}
			if target < 0 || target >= pageCount(len(p.Items), p.pageSize()) {
				continue
			}
			page, rows = target, p.rows(target)
			cursor = entryRow(rows, 0)
		case keyEnter:
			row := rows[cursor]
			if row.index >= 0 {
				// Redraw the final state so the chosen entry stays on screen.
				final := style.Selected.Sprint(p.Items[row.index])
				if heading := p.heading(page); heading != "" {
					final = style.Bold.Sprint(heading) + " " + final
				}
				view.finish(final)
				return row.index, nil
			}
			// A page row: turn the page without leaving the picker.
			back := row.page < page
			page = row.page
			rows = p.rows(page)
			if back {
				cursor = lastEntryRow(rows)
			} else {
				cursor = entryRow(rows, 0)
			}
		case keyAbort:
			view.finish(style.Bold.Sprint(p.heading(page)))
			return -1, ErrCancelled
		default:
			continue
		}

		view.update(lines())
	}
}

// A pickerStyle is the colour set one frame is drawn with. It is a value so that a test can render
// a frame without a terminal.
type pickerStyle struct {
	Bold     *color.Color
	Selected *color.Color
	Starred  *color.Color
	Dim      *color.Color
}

// frameLines renders the block shown for one page and cursor position: the heading, then one line
// per row.
//
// Rendering is separate from drawing so that a frame can be compared with the previous one: the
// screen writes the lines that changed, which is what keeps an arrow key from repainting the menu.
func (p *Picker) frameLines(page, cursor int, rows []pickerRow, style pickerStyle) []string {
	frame := make([]string, 0, len(rows)+1)
	if heading := p.heading(page); heading != "" {
		frame = append(frame, style.Bold.Sprint(heading))
	}
	for i, row := range rows {
		if row.index < 0 {
			text := "-> " + row.label
			if i == cursor {
				frame = append(frame, style.Selected.Sprint(text))
			} else {
				frame = append(frame, style.Dim.Sprint(text))
			}
			continue
		}

		gutter := marker(row.index, p.Initial, p.Star && row.index == p.Initial)
		switch {
		case i == cursor:
			frame = append(frame, style.Selected.Sprint(gutter+" "+p.Items[row.index]))
		case p.Star && row.index == p.Initial:
			frame = append(frame, gutter+" "+style.Starred.Sprint(p.Items[row.index]))
		default:
			frame = append(frame, gutter+" "+p.Items[row.index])
		}
	}
	return frame
}

// entryRow returns the row position of an entry, falling back to the first entry row.
func entryRow(rows []pickerRow, index int) int {
	for i, row := range rows {
		if row.index == index {
			return i
		}
	}
	first := 0
	for i, row := range rows {
		if row.index >= 0 {
			first = i
			break
		}
	}
	return first
}

// lastEntryRow returns the row position of the last entry on a page.
func lastEntryRow(rows []pickerRow) int {
	last := 0
	for i, row := range rows {
		if row.index >= 0 {
			last = i
		}
	}
	return last
}

// readKey decodes one key press from r.
func readKey(r *bufio.Reader) (int, error) {
	b, err := r.ReadByte()
	if err != nil {
		return 0, err
	}

	switch b {
	case 0x0d, 0x0a:
		return keyEnter, nil
	case 0x03, 0x04: // ctrl-c, ctrl-d
		return keyAbort, nil
	case 'k':
		return keyUp, nil
	case 'j':
		return keyDown, nil
	case 'q':
		return keyAbort, nil
	case 0x1b:
		// A lone escape aborts, but arrow keys arrive as a burst, so anything already
		// buffered belongs to an escape sequence.
		if r.Buffered() == 0 {
			return keyAbort, nil
		}
		second, err := r.ReadByte()
		if err != nil {
			return keyAbort, nil
		}
		if second != '[' && second != 'O' {
			return keyAbort, nil
		}
		if r.Buffered() == 0 {
			return keyAbort, nil
		}
		third, err := r.ReadByte()
		if err != nil {
			return keyAbort, nil
		}
		// The page keys arrive as ESC [ 5 ~ and ESC [ 6 ~.
		if (third == '5' || third == '6') && r.Buffered() > 0 {
			if last, err := r.ReadByte(); err == nil && last == '~' {
				if third == '5' {
					return keyPageUp, nil
				}
				return keyPageDown, nil
			}
		}
		// Consume anything else belonging to this sequence.
		for r.Buffered() > 0 {
			_, _ = r.ReadByte()
		}
		switch third {
		case 'A':
			return keyUp, nil
		case 'B':
			return keyDown, nil
		case 'C':
			return keyPageDown, nil
		case 'D':
			return keyPageUp, nil
		case 'H':
			return keyHome, nil
		case 'F':
			return keyEnd, nil
		}
		return 0, nil
	}
	return 0, nil
}
