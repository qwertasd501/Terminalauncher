package meta

import (
	"strings"
	"testing"

	env "github.com/telecter/cmd-launcher/pkg"
)

func TestWithGamePrefix(t *testing.T) {
	cases := []struct {
		game    string
		version string
		want    string
	}{
		{"1.20.1", "47.4.16", "1.20.1-47.4.16"},
		{"1.20.1", "1.20.1-47.4.16", "1.20.1-47.4.16"},
		{"1.7.10", "10.13.4.1614", "1.7.10-10.13.4.1614"},
		{"1.20.1", "", ""},
		{"", "47.4.16", "47.4.16"},
	}
	for _, c := range cases {
		if got := withGamePrefix(c.game, c.version); got != c.want {
			t.Errorf("withGamePrefix(%q, %q) = %q, want %q", c.game, c.version, got, c.want)
		}
	}
}

// TestForgeInstallerURLCandidates checks that a loader version recorded without its game version
// prefix, which is what most launchers store, still resolves to a real Maven artifact.
func TestForgeInstallerURLCandidates(t *testing.T) {
	cases := []struct {
		name        string
		loader      forge
		gameVersion string
		version     string
		wantFirst   string
		wantAny     string
		wantCount   int
	}{
		{
			name:        "forge bare version gains the game prefix",
			loader:      Forge,
			gameVersion: "1.20.1",
			version:     "47.4.16",
			wantFirst:   "net/minecraftforge/forge/1.20.1-47.4.16/",
			wantAny:     "1.20.1-47.4.16",
			wantCount:   2,
		},
		{
			name:        "forge prefixed version is used as is",
			loader:      Forge,
			gameVersion: "1.20.1",
			version:     "1.20.1-47.4.16",
			wantFirst:   "net/minecraftforge/forge/1.20.1-47.4.16/",
			wantCount:   1,
		},
		{
			name:        "bare neoforge version for 1.20.1 falls back to the legacy artifact",
			loader:      Neoforge,
			gameVersion: "1.20.1",
			version:     "47.1.106",
			wantFirst:   "net/neoforged/neoforge/47.1.106/",
			wantAny:     "net/neoforged/forge/1.20.1-47.1.106/",
			wantCount:   2,
		},
		{
			name:        "prefixed neoforge version uses the legacy artifact",
			loader:      Neoforge,
			gameVersion: "1.20.1",
			version:     "1.20.1-47.1.106",
			wantFirst:   "net/neoforged/forge/1.20.1-47.1.106/",
			wantCount:   1,
		},
		{
			name:        "modern neoforge version is bare",
			loader:      Neoforge,
			gameVersion: "1.21.1",
			version:     "21.1.228",
			wantFirst:   "net/neoforged/neoforge/21.1.228/",
			wantCount:   1,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			urls := c.loader.urls(c.gameVersion, c.version)
			if len(urls) != c.wantCount {
				t.Fatalf("got %d candidates, want %d: %v", len(urls), c.wantCount, urls)
			}
			if !strings.Contains(urls[0], c.wantFirst) {
				t.Errorf("first candidate %q does not contain %q", urls[0], c.wantFirst)
			}
			if c.wantAny != "" {
				found := false
				for _, url := range urls {
					if strings.Contains(url, c.wantAny) {
						found = true
					}
				}
				if !found {
					t.Errorf("no candidate contains %q: %v", c.wantAny, urls)
				}
			}
		})
	}
}

// TestFetchForgeMetaBareLoaderVersion covers the case reported from the shell: an instance created
// by another launcher records only "47.4.16", and the installer used to 404 because Forge names its
// artifacts "1.20.1-47.4.16".
func TestFetchForgeMetaBareLoaderVersion(t *testing.T) {
	if testing.Short() {
		t.Skip("needs network access")
	}
	if err := env.SetDirs(t.TempDir()); err != nil {
		t.Fatalf("SetDirs: %v", err)
	}

	versionMeta, profile, err := Forge.FetchMeta("1.20.1", "47.4.16")
	if err != nil {
		t.Fatalf("FetchMeta(1.20.1, 47.4.16): %v", err)
	}
	if profile.Minecraft != "1.20.1" {
		t.Errorf("installer targets Minecraft %q, want 1.20.1", profile.Minecraft)
	}
	if versionMeta.ID == "" {
		t.Error("version metadata has no id")
	}
}
