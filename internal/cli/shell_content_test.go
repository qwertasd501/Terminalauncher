package cli

import (
	"strings"
	"testing"

	"github.com/qwertasd501/Terminalauncher/internal/meta"
	"github.com/qwertasd501/Terminalauncher/pkg/launcher"
)

func TestContentKindOf(t *testing.T) {
	cases := map[string]launcher.ContentKind{
		"mods":          launcher.ContentMods,
		"resourcepacks": launcher.ContentResourcePacks,
		"shaderpacks":   launcher.ContentShaderPacks,
		"datapacks":     launcher.ContentDataPacks,
	}
	for object, want := range cases {
		got, ok := contentKindOf(object)
		if !ok || got != want {
			t.Errorf("contentKindOf(%q) = %q, %v; want %q, true", object, got, ok, want)
		}
	}
	if _, ok := contentKindOf("modpacks"); ok {
		t.Error("contentKindOf accepted modpacks, which is not a directory category")
	}
}

// TestNormalizeObjectCoversContent checks that the spellings a user is likely to type all land on
// the folder names the game uses.
func TestNormalizeObjectCoversContent(t *testing.T) {
	cases := map[string]string{
		"mod":           "mods",
		"mods":          "mods",
		"resourcepack":  "resourcepacks",
		"resourcepacks": "resourcepacks",
		"texturepacks":  "resourcepacks",
		"shader":        "shaderpacks",
		"shaders":       "shaderpacks",
		"shaderpacks":   "shaderpacks",
		"datapack":      "datapacks",
		"datapacks":     "datapacks",
		"modpack":       "modpacks",
		"modpacks":      "modpacks",
		"version":       "instance",
		"instances":     "instance",
		"users":         "user",
		"fabric":        "fabric",
		"nonsense":      "",
	}
	for word, want := range cases {
		if got := normalizeObject(word); got != want {
			t.Errorf("normalizeObject(%q) = %q, want %q", word, got, want)
		}
	}
}

func TestTrimDisabled(t *testing.T) {
	cases := map[string]string{
		"a.jar":          "a.jar",
		"a.jar.disabled": "a.jar",
		"a.jar.DISABLED": "a.jar",
		"a.disabled":     "a",
		"":               "",
	}
	for name, want := range cases {
		if got := trimDisabled(name); got != want {
			t.Errorf("trimDisabled(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestFindContent(t *testing.T) {
	items := []launcher.ContentItem{
		{FileName: "sodium.jar", Name: "Sodium"},
		{FileName: "old.jar.disabled", Name: "Old Mod", Enabled: false},
	}

	byFile, ok := findContent(items, "sodium.jar", "")
	if !ok || byFile.FileName != "sodium.jar" {
		t.Errorf("by file name: %+v, %v", byFile, ok)
	}
	byDisplay, ok := findContent(items, "Sodium", "")
	if !ok || byDisplay.FileName != "sodium.jar" {
		t.Errorf("by display name: %+v, %v", byDisplay, ok)
	}
	// A disabled file is reachable with or without its marker.
	withoutMarker, ok := findContent(items, "old.jar", "")
	if !ok || withoutMarker.FileName != "old.jar.disabled" {
		t.Errorf("by name without the marker: %+v, %v", withoutMarker, ok)
	}
	withMarker, ok := findContent(items, "old.jar.disabled", "")
	if !ok || withMarker.FileName != "old.jar.disabled" {
		t.Errorf("by name with the marker: %+v, %v", withMarker, ok)
	}
	// With no name the current selection is used.
	selected, ok := findContent(items, "", "sodium.jar")
	if !ok || selected.FileName != "sodium.jar" {
		t.Errorf("by selection: %+v, %v", selected, ok)
	}
	if _, ok := findContent(items, "missing", ""); ok {
		t.Error("a missing name matched something")
	}
	if _, ok := findContent(items, "", ""); ok {
		t.Error("an empty name with no selection matched something")
	}
}

func TestContentConflict(t *testing.T) {
	mod := func(source string) launcher.ContentItem {
		return launcher.ContentItem{Kind: launcher.ContentMods, Source: source, FileName: "a.jar", Enabled: true}
	}

	cases := []struct {
		name     string
		loader   meta.Loader
		source   string
		conflict bool
	}{
		{"fabric mod on fabric", meta.LoaderFabric, "fabric", false},
		{"fabric mod on quilt", meta.LoaderQuilt, "fabric", false},
		{"fabric mod on forge", meta.LoaderForge, "fabric", true},
		{"forge mod on forge", meta.LoaderForge, "forge", false},
		{"forge mod on neoforge", meta.LoaderNeoForge, "forge", false},
		{"neoforge mod on neoforge", meta.LoaderNeoForge, "neoforge", false},
		{"neoforge mod on forge", meta.LoaderForge, "neoforge", true},
		{"quilt mod on fabric", meta.LoaderFabric, "quilt", true},
		{"mod without metadata", meta.LoaderForge, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			inst := launcher.Instance{Loader: c.loader, GameVersion: "1.20.1"}
			reason, conflict := contentConflict(inst, mod(c.source))
			if conflict != c.conflict {
				t.Errorf("contentConflict = %q, %v; want conflict %v", reason, conflict, c.conflict)
			}
			if conflict && reason == "" {
				t.Error("a conflict without a reason")
			}
		})
	}

	// A resource pack is never loader specific.
	inst := launcher.Instance{Loader: meta.LoaderForge}
	pack := launcher.ContentItem{Kind: launcher.ContentResourcePacks, Source: "fabric"}
	if _, conflict := contentConflict(inst, pack); conflict {
		t.Error("a resource pack was reported as conflicting")
	}
}

func TestContentLoaders(t *testing.T) {
	inst := launcher.Instance{Loader: meta.LoaderQuilt}
	if got := contentLoaders(inst, launcher.ContentMods); len(got) != 2 || got[0] != "quilt" {
		t.Errorf("mod loaders = %v, want quilt and fabric", got)
	}
	for _, kind := range []launcher.ContentKind{launcher.ContentResourcePacks, launcher.ContentShaderPacks, launcher.ContentDataPacks} {
		if got := contentLoaders(inst, kind); len(got) != 0 {
			t.Errorf("%s loaders = %v, want none", kind, got)
		}
	}
}

func TestVersionKey(t *testing.T) {
	cases := []struct {
		value, gameVersion, want string
	}{
		{"0.5.13", "1.20.1", "0.5.13"},
		{"mc1.20.1-0.5.13-fabric", "1.20.1", "0.5.13"},
		{"v1.2", "", "1.2"},
		// The game version is passed over even when it is the longest candidate.
		{"1.20.1", "1.20.1", "1.20.1"},
		{"", "", ""},
	}
	for _, c := range cases {
		if got := versionKey(c.value, c.gameVersion); got != c.want {
			t.Errorf("versionKey(%q, %q) = %q, want %q", c.value, c.gameVersion, got, c.want)
		}
	}
}

func TestIsNewerVersion(t *testing.T) {
	cases := []struct {
		name                       string
		installed, available, game string
		want                       bool
	}{
		{"the same version", "0.5.13", "0.5.13", "1.20.1", false},
		{"the same version, differently spelled", "0.5.13", "mc1.20.1-0.5.13-fabric", "1.20.1", false},
		{"a newer version", "0.5.13", "mc1.20.1-0.6.0-fabric", "1.20.1", true},
		{"nothing installed yet", "", "1.0", "1.20.1", true},
		{"no version available", "1.0", "", "1.20.1", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isNewerVersion(c.installed, c.available, c.game); got != c.want {
				t.Errorf("isNewerVersion(%q, %q) = %v, want %v", c.installed, c.available, got, c.want)
			}
		})
	}
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Sodium":         "sodium",
		"Reese's Sodium": "reese-s-sodium",
		"Iris Shaders":   "iris-shaders",
		"  Trailing  ":   "trailing",
		"already-a-slug": "already-a-slug",
	}
	for name, want := range cases {
		if got := slugify(name); got != want {
			t.Errorf("slugify(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestHumanSize(t *testing.T) {
	cases := map[int64]string{
		0:       "-",
		512:     "512 B",
		2048:    "2.0 KiB",
		5 << 20: "5.0 MiB",
		3 << 30: "3.0 GiB",
	}
	for size, want := range cases {
		if got := humanSize(size); got != want {
			t.Errorf("humanSize(%d) = %q, want %q", size, got, want)
		}
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("short", 10); got != "short" {
		t.Errorf("truncate = %q, want short", got)
	}
	if got := truncate("abcdefghij", 6); got != "abc..." {
		t.Errorf("truncate = %q, want abc...", got)
	}
	// Runes are counted, not bytes.
	if got := truncate("日本語テキスト", 4); got != "日..." {
		t.Errorf("truncate = %q, want 日...", got)
	}
	if got := truncate("abcdef", 3); got != "abc" {
		t.Errorf("truncate = %q, want abc", got)
	}
}

func TestSanitizeInstanceName(t *testing.T) {
	cases := map[string]string{
		"Fabulously Optimized": "Fabulously Optimized",
		`a/b\c:d*e?f"g<h>i|j`:  "abcdefghij",
		"..trailing..":         "trailing",
		"":                     "",
	}
	for name, want := range cases {
		if got := sanitizeInstanceName(name); got != want {
			t.Errorf("sanitizeInstanceName(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestPackFormatLabel(t *testing.T) {
	if got := packFormatLabel(15); got != "15 (1.20 - 1.20.1)" {
		t.Errorf("packFormatLabel(15) = %q", got)
	}
	if got := packFormatLabel(9999); got != "9999" {
		t.Errorf("packFormatLabel(9999) = %q, want the bare number", got)
	}
}

func TestContentEntryFlagsWrongLoader(t *testing.T) {
	inst := launcher.Instance{Loader: meta.LoaderForge, GameVersion: "1.20.1"}
	item := launcher.ContentItem{
		Kind:     launcher.ContentMods,
		FileName: "sodium.jar",
		Source:   "fabric",
		Enabled:  true,
		Version:  "0.5.13",
	}

	entry := contentEntry(inst, item)
	if !strings.Contains(entry, "Sodium") && !strings.Contains(entry, "sodium") {
		t.Errorf("entry %q does not name the file", entry)
	}
	if !strings.Contains(entry, "0.5.13") {
		t.Errorf("entry %q does not show the version", entry)
	}
	if !strings.Contains(entry, "wrong loader") {
		t.Errorf("entry %q does not flag the wrong loader", entry)
	}
}

// TestContentArgs covers the shared flag parsing of the content commands. A flag left in the
// positional arguments would be used as a search term, which silently matches nothing; that was a
// real bug for "list datapacks -w World1".
func TestContentArgs(t *testing.T) {
	cases := []struct {
		rest      []string
		allowYes  bool
		wantWorld string
		wantYes   bool
		wantArgs  []string
	}{
		{[]string{"-w", "World1"}, false, "World1", false, nil},
		{[]string{"-w=World1"}, false, "World1", false, nil},
		{[]string{"--world", "World1", "sodium"}, false, "World1", false, []string{"sodium"}},
		{[]string{"sodium"}, false, "", false, []string{"sodium"}},
		{[]string{"-y", "sodium"}, true, "", true, []string{"sodium"}},
		{[]string{"sodium", "-y"}, true, "", true, []string{"sodium"}},
		{nil, true, "", false, nil},
	}
	for _, c := range cases {
		world, yes, args, ok := contentArgs(c.rest, true, c.allowYes)
		if !ok {
			t.Errorf("contentArgs(%q) reported a bad invocation", c.rest)
			continue
		}
		if world != c.wantWorld {
			t.Errorf("contentArgs(%q) world = %q, want %q", c.rest, world, c.wantWorld)
		}
		if yes != c.wantYes {
			t.Errorf("contentArgs(%q) yes = %v, want %v", c.rest, yes, c.wantYes)
		}
		if strings.Join(args, " ") != strings.Join(c.wantArgs, " ") {
			t.Errorf("contentArgs(%q) args = %q, want %q", c.rest, args, c.wantArgs)
		}
	}

	// -y is only accepted where the command documents it.
	if _, _, _, ok := contentArgs([]string{"-y"}, false, false); ok {
		t.Error("contentArgs accepted -y when it was not allowed")
	}
	// An unknown flag is rejected instead of being treated as a search term.
	if _, _, _, ok := contentArgs([]string{"--nope"}, true, true); ok {
		t.Error("contentArgs accepted an unknown flag")
	}
	// A flag missing its value is rejected.
	if _, _, _, ok := contentArgs([]string{"-w"}, true, true); ok {
		t.Error("contentArgs accepted a flag without its value")
	}
}
