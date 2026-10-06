package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qwertasd501/Terminalauncher/internal/cli/output"
	env "github.com/qwertasd501/Terminalauncher/pkg"
	"github.com/qwertasd501/Terminalauncher/pkg/launcher"
)

// withGameDir points the launcher at a temporary game directory for the duration of a test. It
// returns an instance that lives in it, together with that instance's game directory, which is where
// a category folder is looked for.
func withGameDir(t *testing.T) (launcher.Instance, string) {
	t.Helper()
	root := t.TempDir()
	previousRoot, previousInstances := env.RootDir, env.InstancesDir
	env.RootDir = root
	env.InstancesDir = filepath.Join(root, "instances")
	t.Cleanup(func() {
		env.RootDir = previousRoot
		env.InstancesDir = previousInstances
	})

	// The launcher's own layout keeps the game data with the instance, which is the layout whose
	// game directory a test can point at a temporary folder.
	inst := launcher.Instance{Name: "test", Layout: launcher.LayoutInstances}
	return inst, inst.Dir()
}

// createFiles creates the named files under dir.
func createFiles(t *testing.T, dir string, names ...string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("creating %s: %v", dir, err)
	}
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0644); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}
}

// TestContentCounts covers the cheap counting the version page does: parsing a few hundred jars
// would make the page pause, so the counts come from the folder listing alone.
func TestContentCounts(t *testing.T) {
	inst, game := withGameDir(t)
	createFiles(t, filepath.Join(game, "mods"), "sodium.jar", "lithium.jar", "iris.jar.disabled", "stale.DISABLED")
	if err := os.MkdirAll(filepath.Join(game, "mods", "not-a-mod"), 0755); err != nil {
		t.Fatalf("creating a subdirectory: %v", err)
	}

	total, on, ok := contentCounts(inst, launcher.ContentMods, "")
	if !ok || total != 4 || on != 2 {
		t.Errorf("contentCounts = %d total, %d on, ok=%v; want 4, 2, true", total, on, ok)
	}

	// A category that was never used has no folder: it is empty, not unreadable.
	total, on, ok = contentCounts(inst, launcher.ContentResourcePacks, "")
	if !ok || total != 0 || on != 0 {
		t.Errorf("missing folder: %d total, %d on, ok=%v; want 0, 0, true", total, on, ok)
	}

	// A data pack category is only countable once a world is known.
	if _, _, ok := contentCounts(inst, launcher.ContentDataPacks, ""); ok {
		t.Error("contentCounts counted data packs without a world")
	}
}

// TestContentSummary checks the row value of the version page: both numbers are named, and a
// category that cannot be counted says so instead of pretending to be empty.
func TestContentSummary(t *testing.T) {
	inst, game := withGameDir(t)
	createFiles(t, filepath.Join(game, "mods"), "sodium.jar", "iris.jar.disabled")

	got := summaryOf(inst, launcher.ContentMods, "")
	if !strings.Contains(got, "2") || !strings.Contains(got, "1") {
		t.Errorf("summary %q does not show 2 files and 1 enabled", got)
	}

	if got := summaryOf(inst, launcher.ContentDataPacks, ""); got != output.Translate("shell.na") {
		t.Errorf("uncountable category summarised as %q, want %q", got, output.Translate("shell.na"))
	}
}

// TestContentStateShowsVersion checks the value column of a category page: the state of the file,
// plus its version when the file could be read.
func TestContentStateShowsVersion(t *testing.T) {
	item := launcher.ContentItem{
		Kind:     launcher.ContentMods,
		FileName: "sodium.jar",
		Enabled:  true,
		Version:  "0.5.13",
	}
	state := contentState(launcher.Instance{}, item)
	if !strings.Contains(state, "0.5.13") {
		t.Errorf("state %q does not show the version", state)
	}
	if !strings.Contains(state, output.Translate("shell.content.enabled")) {
		t.Errorf("state %q does not say the file is enabled", state)
	}

	item.Enabled = false
	state = contentState(launcher.Instance{}, item)
	if !strings.Contains(state, output.Translate("shell.content.disabled")) {
		t.Errorf("state %q does not say the file is disabled", state)
	}
}
