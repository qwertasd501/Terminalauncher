package launcher

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/telecter/cmd-launcher/internal/meta"
	env "github.com/telecter/cmd-launcher/pkg"
)

// TestDefaultInstanceName covers the name the launcher builds when the caller does not give one:
// "<game version>-<Loader>_<loader version>[-<suffix>]", the way PCL names its version folders.
func TestDefaultInstanceName(t *testing.T) {
	cases := []struct {
		name          string
		gameVersion   string
		loader        meta.Loader
		loaderVersion string
		suffix        string
		want          string
	}{
		{"forge", "1.20.1", meta.LoaderForge, "47.4.6", "", "1.20.1-Forge_47.4.6"},
		{"forge with a suffix", "1.20.1", meta.LoaderForge, "47.4.16", "LTSC", "1.20.1-Forge_47.4.16-LTSC"},
		{"fabric", "1.21.5", meta.LoaderFabric, "0.16.10", "", "1.21.5-Fabric_0.16.10"},
		{"quilt", "1.21.1", meta.LoaderQuilt, "0.27.1", "", "1.21.1-Quilt_0.27.1"},
		{
			"a loader version repeating the game version is shortened",
			"26.3", meta.LoaderForge, "26.3-66.0.9", "", "26.3-Forge_66.0.9",
		},
		{
			"a loader version repeating the game version is shortened before the suffix",
			"26.3", meta.LoaderForge, "26.3-66.0.9", "LTSC", "26.3-Forge_66.0.9-LTSC",
		},
		{
			"neoforge keeps its capital letters",
			"1.21.1", meta.LoaderNeoForge, "21.1.228", "", "1.21.1-NeoForge_21.1.228",
		},
		{"vanilla has no loader to name", "1.21.5", meta.LoaderVanilla, "", "", "1.21.5"},
		{"vanilla keeps the suffix", "1.21.5", meta.LoaderVanilla, "latest", "LTSC", "1.21.5-LTSC"},
		{
			"a loader version that is not concrete is left out",
			"1.20.1", meta.LoaderForge, "latest", "", "1.20.1-Forge",
		},
		{"a missing loader version is left out", "1.20.1", meta.LoaderFabric, "", "", "1.20.1-Fabric"},
		{
			"an unknown loader is written as it was given",
			"1.20.1", meta.Loader("liteloader"), "1.0", "", "1.20.1-liteloader_1.0",
		},
		{"the suffix is trimmed", "1.20.1", meta.LoaderForge, "47.4.6", "  LTSC  ", "1.20.1-Forge_47.4.6-LTSC"},
		{"everything is trimmed", " 1.20.1 ", meta.LoaderForge, " 47.4.6 ", "", "1.20.1-Forge_47.4.6"},
		{"without a game version there is nothing to name", "", meta.LoaderForge, "47.4.6", "LTSC", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := DefaultInstanceName(c.gameVersion, c.loader, c.loaderVersion, c.suffix)
			if got != c.want {
				t.Errorf(
					"DefaultInstanceName(%q, %q, %q, %q) = %q, want %q",
					c.gameVersion, c.loader, c.loaderVersion, c.suffix, got, c.want,
				)
			}
		})
	}
}

// TestLoaderDisplay checks the loader names written into instance names.
func TestLoaderDisplay(t *testing.T) {
	cases := map[meta.Loader]string{
		meta.LoaderVanilla:   "Vanilla",
		meta.LoaderFabric:    "Fabric",
		meta.LoaderQuilt:     "Quilt",
		meta.LoaderForge:     "Forge",
		meta.LoaderNeoForge:  "NeoForge",
		meta.Loader(""):      "",
		meta.Loader("other"): "other",
	}
	for loader, want := range cases {
		if got := loader.Display(); got != want {
			t.Errorf("Loader(%q).Display() = %q, want %q", loader, got, want)
		}
	}
}

// TestCreateInstanceRejectsATakenName checks that a name already in use is refused before anything is
// fetched, so a repeated "create" fails fast instead of downloading a version first.
func TestCreateInstanceRejectsATakenName(t *testing.T) {
	dirs := []*string{
		&env.RootDir, &env.InstancesDir, &env.VersionsDir, &env.LibrariesDir,
		&env.CachesDir, &env.AssetsDir, &env.TmpDir, &env.JavaDir,
	}
	previous := make([]string, len(dirs))
	for i, dir := range dirs {
		previous[i] = *dir
	}
	if err := env.SetDirs(t.TempDir()); err != nil {
		t.Fatalf("SetDirs: %v", err)
	}
	t.Cleanup(func() {
		for i, dir := range dirs {
			*dir = previous[i]
		}
	})

	taken := "1.20.1-Forge_47.4.6"
	if err := os.MkdirAll(filepath.Join(env.InstancesDir, taken), 0755); err != nil {
		t.Fatal(err)
	}

	// The game version is never fetched: the name is refused first, which is what keeps this test
	// offline. The name a caller does not give is covered by TestDefaultInstanceName.
	_, err := CreateInstance(InstanceOptions{
		Name:        taken,
		GameVersion: "1.20.1",
		Loader:      meta.LoaderForge,
	})
	if err == nil {
		t.Errorf("CreateInstance accepted the name %q, which is already in use", taken)
	}
}
