package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	env "github.com/qwertasd501/Terminalauncher/pkg"
)

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestCloseEnough(t *testing.T) {
	tests := []struct {
		a, b string
		max  int
		want bool
	}{
		{"mineraft", "minecraft", 2, true},  // the typo that created a junk directory
		{"mineraft", "minecraft", 0, false}, // no edits allowed
		{"abc", "xyz", 2, false},
		{"abc", "abc", 0, true},
		{"mods", "minecraft", 2, false},
	}

	for _, tt := range tests {
		if got := closeEnough(tt.a, tt.b, tt.max); got != tt.want {
			t.Errorf("closeEnough(%q, %q, %d) = %v, want %v", tt.a, tt.b, tt.max, got, tt.want)
		}
	}
}

func TestParseResolutionUnchanged(t *testing.T) {
	// Guards the helper reused by the settings menus.
	if w, h, ok := parseResolution("1920 1080"); !ok || w != 1920 || h != 1080 {
		t.Fatalf("parseResolution(\"1920 1080\") = %d, %d, %v", w, h, ok)
	}
	if w, h, ok := parseResolution("1280x720"); !ok || w != 1280 || h != 720 {
		t.Fatalf("parseResolution(\"1280x720\") = %d, %d, %v", w, h, ok)
	}
}

func TestPathCompletions(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"minecraft", "mods"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "modlist.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	separator := string(os.PathSeparator)

	// A bare name completes to the directory, keeping the trailing separator so the next component
	// can be typed straight away.
	if got := pathCompletions("min"); !equalStrings(got, []string{"minecraft" + separator}) {
		t.Fatalf("pathCompletions(\"min\") = %q", got)
	}

	// The typed prefix, including "./", is preserved.
	want := []string{"." + separator + "modlist.txt", "." + separator + "mods" + separator}
	if got := pathCompletions("." + separator + "mo"); !equalStrings(got, want) {
		t.Fatalf("pathCompletions(\".%smo\") = %q, want %q", separator, got, want)
	}

	// Nothing matches.
	if got := pathCompletions("zzz"); len(got) != 0 {
		t.Fatalf("pathCompletions(\"zzz\") = %q", got)
	}
}

func TestCompleteCommands(t *testing.T) {
	// An empty game directory keeps instance and account completions out of the way.
	env.SetDirs(t.TempDir())

	s := &shell{}
	tests := []struct {
		line string
		want []string
	}{
		{"li", []string{"list"}},
		{"list ", []string{"datapacks", "fabric", "forge", "game", "modpacks", "mods", "quilt", "resourcepacks", "shaderpacks", "users", "versions"}},
		{"list u", []string{"users"}},
		{"list mod", []string{"modpacks", "mods"}},
		{"auth ", []string{"login", "logout"}},
		// Completion is prefix based, so "l" matches both.
		{"auth l", []string{"login", "logout"}},
		{"auth lo", []string{"login", "logout"}},
		{"set ", []string{"global", "versions"}},
		{"select j", []string{"java"}},
		{"select ", []string{"datapacks", "java", "mods", "resourcepacks", "shaderpacks", "users", "versions"}},
		{"download ", []string{"datapacks", "modpacks", "mods", "resourcepacks", "shaderpacks", "version"}},
		{"enable ", []string{"datapacks", "mods", "resourcepacks", "shaderpacks"}},
		{"update mod", []string{"mods"}},
		{"del ", []string{"datapacks", "mods", "resourcepacks", "shaderpacks", "users", "version"}},
		{"download version ", nil},
		{"start --de", []string{"--demo"}},
		{"start --dis", []string{"--disable-chat", "--disable-multiplayer"}},
		{"list bogus", nil},
	}

	for _, tt := range tests {
		got, _ := s.complete([]rune(tt.line), len([]rune(tt.line)))
		if !equalStrings(got, tt.want) {
			t.Errorf("complete(%q) = %q, want %q", tt.line, got, tt.want)
		}
	}
}

func TestCompleteReportsReplaceStart(t *testing.T) {
	env.SetDirs(t.TempDir())

	s := &shell{}
	line := "list u"
	_, start := s.complete([]rune(line), len(line))
	if start != len("list ") {
		t.Fatalf("replace start = %d, want %d", start, len("list "))
	}
}

func TestResolvePath(t *testing.T) {
	root := t.TempDir()
	env.SetDirs(root)

	// Relative paths resolve against the game directory, because that is where the shell "is".
	got, err := resolvePath("minecraft")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "minecraft"); got != want {
		t.Fatalf("resolvePath(\"minecraft\") = %q, want %q", got, want)
	}

	// A parent reference still works.
	got, err = resolvePath(filepath.Join("..", "elsewhere"))
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(filepath.Dir(root), "elsewhere"); got != want {
		t.Fatalf("resolvePath(\"..\") = %q, want %q", got, want)
	}

	// An absolute path is left alone.
	absolute := filepath.Join(root, "sub")
	got, err = resolvePath(absolute)
	if err != nil {
		t.Fatal(err)
	}
	if got != absolute {
		t.Fatalf("resolvePath(%q) = %q", absolute, got)
	}
}

func TestSimilarDirs(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"minecraft", "saves", "logs"} {
		if err := os.Mkdir(filepath.Join(root, name), 0755); err != nil {
			t.Fatal(err)
		}
	}

	got := similarDirs(filepath.Join(root, "mineraft"))
	if !equalStrings(got, []string{"minecraft"}) {
		t.Fatalf("similarDirs = %q, want [minecraft]", got)
	}

	// Nothing close by.
	if got := similarDirs(filepath.Join(root, "totally-different")); len(got) != 0 {
		t.Fatalf("similarDirs returned %q", got)
	}

	// Substring matches are also offered.
	if got := similarDirs(filepath.Join(root, "save")); !strings.Contains(strings.Join(got, ","), "saves") {
		t.Fatalf("similarDirs(\"save\") = %q, want saves", got)
	}
}
