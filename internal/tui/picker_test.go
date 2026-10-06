package tui

import (
	"strings"
	"testing"
)

func TestPageBounds(t *testing.T) {
	cases := []struct {
		name                string
		total, page, size   int
		wantFirst, wantLast int
	}{
		{"short list is one page", 3, 0, 20, 0, 3},
		{"a full first page", 45, 0, 20, 0, 20},
		{"the second page", 45, 1, 20, 20, 40},
		{"a short last page", 45, 2, 20, 40, 45},
		{"a page past the end is empty", 45, 3, 20, 45, 45},
		{"a zero size falls back to the default", 45, 0, 0, 0, 20},
	}
	for _, c := range cases {
		first, last := pageBounds(c.total, c.page, c.size)
		if first != c.wantFirst || last != c.wantLast {
			t.Errorf("%s: pageBounds(%d, %d, %d) = (%d, %d), want (%d, %d)",
				c.name, c.total, c.page, c.size, first, last, c.wantFirst, c.wantLast)
		}
	}
}

func TestPageOfAndCount(t *testing.T) {
	if got := pageOf(0, 20); got != 0 {
		t.Errorf("pageOf(0, 20) = %d, want 0", got)
	}
	if got := pageOf(19, 20); got != 0 {
		t.Errorf("pageOf(19, 20) = %d, want 0", got)
	}
	if got := pageOf(20, 20); got != 1 {
		t.Errorf("pageOf(20, 20) = %d, want 1", got)
	}
	if got := pageOf(45, 20); got != 2 {
		t.Errorf("pageOf(45, 20) = %d, want 2", got)
	}
	if got := pageOf(-1, 20); got != 0 {
		t.Errorf("pageOf(-1, 20) = %d, want 0", got)
	}

	cases := []struct{ total, want int }{{0, 1}, {1, 1}, {20, 1}, {21, 2}, {40, 2}, {41, 3}}
	for _, c := range cases {
		if got := pageCount(c.total, 20); got != c.want {
			t.Errorf("pageCount(%d, 20) = %d, want %d", c.total, got, c.want)
		}
	}
}

// TestRowsPageRow checks that a long list ends with the row that turns the page, and that the last
// page offers the way back instead.
func TestRowsPageRow(t *testing.T) {
	items := make([]string, 45)
	for i := range items {
		items[i] = "entry"
	}
	p := Picker{Items: items, Star: true}

	first := p.rows(0)
	// Twenty entries plus the "more" row, and no row back on the first page.
	if len(first) != 21 {
		t.Fatalf("first page has %d rows, want 21", len(first))
	}
	if first[0].index != 0 {
		t.Errorf("first page starts with index %d, want 0", first[0].index)
	}
	last := first[len(first)-1]
	if last.index != -1 || last.page != 1 {
		t.Errorf("first page ends with %+v, want a page row to page 1", last)
	}

	middle := p.rows(1)
	if len(middle) != 22 {
		t.Fatalf("second page has %d rows, want 22", len(middle))
	}
	if middle[0].index != -1 || middle[0].page != 0 {
		t.Errorf("second page starts with %+v, want a page row back to page 0", middle[0])
	}
	if middle[len(middle)-1].page != 2 {
		t.Errorf("second page does not offer the next one: %+v", middle[len(middle)-1])
	}

	lastPage := p.rows(2)
	// Five entries plus the row back, and no "more" row.
	if len(lastPage) != 6 {
		t.Fatalf("last page has %d rows, want 6", len(lastPage))
	}
	if lastPage[0].index != -1 || lastPage[0].page != 1 {
		t.Errorf("last page starts with %+v, want a page row back to page 1", lastPage[0])
	}
	if lastPage[len(lastPage)-1].index != 44 {
		t.Errorf("last page ends with %+v, want the last entry", lastPage[len(lastPage)-1])
	}
}

// TestRowsShortListHasNoPageRow checks that a list which fits is drawn as it is.
func TestRowsShortListHasNoPageRow(t *testing.T) {
	p := Picker{Items: []string{"a", "b", "c"}}
	rows := p.rows(0)
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(rows))
	}
	for _, row := range rows {
		if row.index < 0 {
			t.Errorf("short list contains a page row: %+v", row)
		}
	}
}

func TestHeading(t *testing.T) {
	short := Picker{Title: "Version", Items: []string{"a", "b"}}
	if got := short.heading(0); got != "Version" {
		t.Errorf("heading() = %q, want %q", got, "Version")
	}

	long := Picker{Title: "Version", Items: make([]string, 45)}
	if got := long.heading(0); got != "Version page 1/3" {
		t.Errorf("heading() = %q, want %q", got, "Version page 1/3")
	}
	if got := long.heading(2); got != "Version page 3/3" {
		t.Errorf("heading() = %q, want %q", got, "Version page 3/3")
	}

	translated := Picker{Title: "版本", Items: make([]string, 45), PageLabel: "页码"}
	if got := translated.heading(1); got != "版本 页码 2/3" {
		t.Errorf("heading() = %q, want %q", got, "版本 页码 2/3")
	}
}

// TestSelectNumberedPaging walks the non-interactive fallback through two pages.
func TestSelectNumberedPaging(t *testing.T) {
	items := make([]string, 25)
	for i := range items {
		items[i] = string(rune('a' + i))
	}

	var out strings.Builder
	p := Picker{
		Title:    "Pick one",
		Items:    items,
		Initial:  0,
		Lines:    &sliceScanner{lines: []string{"n", "22"}},
		Out:      &out,
		PageSize: 20,
	}

	index, err := p.Select()
	if err != nil {
		t.Fatalf("Select() error: %v", err)
	}
	if index != 21 {
		t.Errorf("Select() = %d, want 21", index)
	}
	text := out.String()
	if !strings.Contains(text, "Pick one page 1/2") {
		t.Errorf("first page heading missing:\n%s", text)
	}
	if !strings.Contains(text, "Pick one page 2/2") {
		t.Errorf("second page heading missing:\n%s", text)
	}
	if strings.Contains(text, "21) a") || !strings.Contains(text, "22) v") {
		t.Errorf("numbered entries are not global:\n%s", text)
	}
}

// TestSelectNumberedEnterTakesTheDefault checks that pressing enter keeps the given choice.
func TestSelectNumberedEnterTakesTheDefault(t *testing.T) {
	var out strings.Builder
	p := Picker{
		Items:   []string{"a", "b", "c"},
		Initial: 2,
		Lines:   &sliceScanner{lines: []string{""}},
		Out:     &out,
	}
	index, err := p.Select()
	if err != nil {
		t.Fatalf("Select() error: %v", err)
	}
	if index != 2 {
		t.Errorf("Select() = %d, want 2", index)
	}
	if !strings.Contains(out.String(), "* 3) c") {
		t.Errorf("the default entry is not marked:\n%s", out.String())
	}
}

// TestSelectNumberedEndsThePromptLine checks that the prompt is terminated, so that whatever is
// printed next - the heading of a nested list, or the shell prompt - starts on its own line. A
// terminal echoes the typed answer, which hides this, but a pipe does not.
func TestSelectNumberedEndsThePromptLine(t *testing.T) {
	var out strings.Builder
	p := Picker{
		Items: []string{"a", "b"},
		Lines: &sliceScanner{lines: []string{"1"}},
		Out:   &out,
	}
	if _, err := p.Select(); err != nil {
		t.Fatalf("Select() error: %v", err)
	}
	if !strings.HasSuffix(out.String(), "Select [1-2] (enter for 1): \n") {
		t.Errorf("the prompt is not terminated by a newline:\n%q", out.String())
	}
}

// TestSelectNumberedRejectsOutOfRange checks that a bad number is reported rather than guessed at.
func TestSelectNumberedRejectsOutOfRange(t *testing.T) {
	var out strings.Builder
	p := Picker{
		Items:   []string{"a", "b"},
		Lines:   &sliceScanner{lines: []string{"7"}},
		Out:     &out,
		Initial: 0,
	}
	if _, err := p.Select(); err == nil {
		t.Error("Select() accepted an out-of-range choice")
	}
}

func TestSelectEmptyList(t *testing.T) {
	if _, err := (&Picker{}).Select(); err == nil {
		t.Error("Select() on an empty list did not report an error")
	}
}

func TestMarker(t *testing.T) {
	cases := []struct {
		index, current int
		starred        bool
		want           string
	}{
		{0, 0, true, ">*"},
		{1, 1, true, ">*"},
		{0, 1, true, " *"},
		{1, 0, true, " *"},
		{1, 0, false, "  "},
		{2, 0, false, "  "},
		{0, 0, false, " >"},
	}
	for _, c := range cases {
		if got := marker(c.index, c.current, c.starred); got != c.want {
			t.Errorf("marker(%d, %d, %v) = %q, want %q", c.index, c.current, c.starred, got, c.want)
		}
	}
}

func TestPageSizeDefault(t *testing.T) {
	p := Picker{}
	if got := p.pageSize(); got != defaultPageSize {
		t.Errorf("pageSize() = %d, want %d", got, defaultPageSize)
	}
	p.PageSize = 3
	if got := p.pageSize(); got != 3 {
		t.Errorf("pageSize() = %d, want 3", got)
	}
}

func TestEntryRow(t *testing.T) {
	p := Picker{Items: make([]string, 45)}
	middle := p.rows(1)
	// Row 0 is the way back, so entry 25 sits at row 6.
	if got := entryRow(middle, 25); got != 6 {
		t.Errorf("entryRow(middle, 25) = %d, want 6", got)
	}
	if got := lastEntryRow(middle); got != 20 {
		t.Errorf("lastEntryRow(middle) = %d, want 20", got)
	}
	// An entry from another page falls back to the first entry of the drawn page.
	if got := entryRow(middle, 0); got != 1 {
		t.Errorf("entryRow(middle, 0) = %d, want 1", got)
	}
}

// sliceScanner feeds a fixed list of lines to a widget, standing in for standard input.
type sliceScanner struct {
	lines []string
	next  int
}

func (s *sliceScanner) Scan() bool {
	if s.next >= len(s.lines) {
		return false
	}
	s.next++
	return true
}

func (s *sliceScanner) Text() string {
	return s.lines[s.next-1]
}
