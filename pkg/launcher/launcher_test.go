package launcher

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/qwertasd501/Terminalauncher/internal/meta"
	env "github.com/qwertasd501/Terminalauncher/pkg"
	"github.com/qwertasd501/Terminalauncher/pkg/auth"
)

func TestCreateInstance(t *testing.T) {
	env.SetDirs(t.TempDir())

	tests := []struct {
		name      string
		options   InstanceOptions
		wantError bool
	}{
		{
			name: "Vanilla",
			options: InstanceOptions{
				GameVersion: "release",
				Loader:      meta.LoaderVanilla,
			},
			wantError: false,
		},
		{
			name: "Vanilla Invalid Version",
			options: InstanceOptions{
				GameVersion: "not valid",
				Loader:      meta.LoaderVanilla,
			},
			wantError: true,
		},
		{
			name: "Fabric",
			options: InstanceOptions{
				GameVersion:   "release",
				Loader:        meta.LoaderFabric,
				LoaderVersion: "latest",
			},
			wantError: false,
		},
		{
			name: "Fabric Versioned",
			options: InstanceOptions{
				GameVersion:   "release",
				Loader:        meta.LoaderFabric,
				LoaderVersion: "0.16.14",
			},
			wantError: false,
		},
		{
			name: "Fabric Invalid Version",
			options: InstanceOptions{
				GameVersion:   "release",
				Loader:        meta.LoaderFabric,
				LoaderVersion: "not valid",
			},
			wantError: true,
		},
		{
			name: "Quilt",
			options: InstanceOptions{
				GameVersion:   "release",
				Loader:        meta.LoaderQuilt,
				LoaderVersion: "latest",
			},
			wantError: false,
		},
		{
			name: "Quilt Versioned",
			options: InstanceOptions{
				GameVersion:   "release",
				Loader:        meta.LoaderQuilt,
				LoaderVersion: "0.29.0-beta.7",
			},
			wantError: false,
		},
		{
			name: "Quilt Invalid Version",
			options: InstanceOptions{
				GameVersion:   "release",
				Loader:        meta.LoaderQuilt,
				LoaderVersion: "not valid",
			},
			wantError: true,
		},
		{
			name: "Forge",
			options: InstanceOptions{
				GameVersion:   "release",
				Loader:        meta.LoaderForge,
				LoaderVersion: "latest",
			},
			wantError: false,
		},
		{
			name: "Forge Versioned",
			options: InstanceOptions{
				GameVersion:   "1.21.5",
				Loader:        meta.LoaderForge,
				LoaderVersion: "1.21.5-55.0.22",
			},
			wantError: false,
		},
		{
			name: "Forge Invalid Version",
			options: InstanceOptions{
				GameVersion:   "release",
				Loader:        meta.LoaderForge,
				LoaderVersion: "not valid",
			},
			wantError: true,
		},
		{
			name: "NeoForge",
			options: InstanceOptions{
				GameVersion:   "release",
				Loader:        meta.LoaderNeoForge,
				LoaderVersion: "latest",
			},
			wantError: false,
		},
		{
			name: "NeoForge Versioned",
			options: InstanceOptions{
				GameVersion:   "release",
				Loader:        meta.LoaderNeoForge,
				LoaderVersion: "21.5.75",
			},
			wantError: false,
		},
		{
			name: "NeoForge Invalid Version",
			options: InstanceOptions{
				GameVersion:   "release",
				Loader:        meta.LoaderNeoForge,
				LoaderVersion: "not valid",
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.options.Name = uuid.NewString()
			inst, err := CreateInstance(tt.options)
			isError := err != nil
			if tt.wantError != isError {
				t.Errorf("got error: %t; wanted error to be: %t", isError, tt.wantError)
				if isError {
					t.Log(err)
				}
			}
			// A created version belongs in the standard versions directory, where PCL and the
			// official launcher keep them, not in the launcher's own instances directory. A failed
			// creation leaves no directory to look at, so it only applies where one was made.
			if tt.wantError {
				return
			}
			if inst.Layout != LayoutVersions {
				t.Errorf("the version was created with layout %q; want %q", inst.Layout, LayoutVersions)
			}
			if want := filepath.Join(env.VersionsDir, inst.Name); inst.Dir() != want {
				t.Errorf("the version directory is %q; want %q", inst.Dir(), want)
			}
			if _, err := os.Stat(inst.Dir()); err != nil {
				t.Errorf("instance directory should be accessible; got error: %s", err)
			}
		})
	}
}

func TestFetchAllInstances(t *testing.T) {
	env.SetDirs(t.TempDir())
	_, err := CreateInstance(InstanceOptions{
		Name:        uuid.NewString(),
		GameVersion: "release",
		Loader:      meta.LoaderVanilla,
	})
	if err != nil {
		t.Fatalf("unexpected error creating instance for test: %s", err)
	}
	insts, err := FetchAllInstances()
	if err != nil {
		t.Errorf("wanted no error; got: %s", err)
	}
	if len(insts) != 1 {
		t.Errorf("wanted number of instances to be 1; got %d", len(insts))
	}
}

func TestRemoveInstance(t *testing.T) {
	env.SetDirs(t.TempDir())
	inst, err := CreateInstance(InstanceOptions{
		Name:        uuid.NewString(),
		GameVersion: "release",
		Loader:      meta.LoaderVanilla,
	})
	if err != nil {
		t.Fatalf("unexpected error creating instance for test: %s", err)
	}
	if err := RemoveInstance(inst.Name); err != nil {
		t.Errorf("wanted no error; got: %s", err)
	}
	if _, err := os.Stat(inst.Dir()); err == nil {
		t.Error("instance directory should not exist; but does")
	}
}

func TestRenameInstance(t *testing.T) {
	env.SetDirs(t.TempDir())
	inst, err := CreateInstance(InstanceOptions{
		Name:        uuid.NewString(),
		GameVersion: "release",
		Loader:      meta.LoaderVanilla,
	})
	if err != nil {
		t.Fatalf("unexpected error creating instance for test: %s", err)
	}
	name := uuid.NewString()
	if err := inst.Rename(name); err != nil {
		t.Errorf("wanted no error; got: %s", err)
	}
	if _, err := os.Stat(filepath.Join(env.VersionsDir, name)); err != nil {
		t.Error("renamed instance directory does not exist; but should")
	}

}

func testingWatcher(event any) {
	switch e := event.(type) {
	case AssetsResolvedEvent:
		fmt.Printf("Identified %d assets\n", e.Total)
	case LibrariesResolvedEvent:
		fmt.Printf("Identified %d libraries\n", e.Total)

	case MetadataResolvedEvent:
		fmt.Println("Version metadata retrieved")

	}
}

func TestPrepare(t *testing.T) {
	env.SetDirs(t.TempDir())

	tests := []struct {
		name    string
		options InstanceOptions
	}{
		{
			name: "Vanilla",
			options: InstanceOptions{
				GameVersion: "release",
				Loader:      meta.LoaderVanilla,
				Config: InstanceConfig{
					Java: "java",
				},
			},
		},
		{
			name: "Vanilla with Mojang JVM",
			options: InstanceOptions{
				GameVersion: "release",
				Loader:      meta.LoaderVanilla,
			},
		},
		{
			name: "Fabric",
			options: InstanceOptions{
				GameVersion:   "release",
				Loader:        meta.LoaderFabric,
				LoaderVersion: "latest",
				Config: InstanceConfig{
					Java: "java",
				},
			},
		},
		{
			name: "Quilt",
			options: InstanceOptions{
				GameVersion:   "release",
				Loader:        meta.LoaderQuilt,
				LoaderVersion: "latest",
				Config: InstanceConfig{
					Java: "java",
				},
			},
		},
		{
			name: "Forge",
			options: InstanceOptions{
				GameVersion:   "release",
				Loader:        meta.LoaderForge,
				LoaderVersion: "latest",
				Config: InstanceConfig{
					Java: "java",
				},
			},
		},
		{
			name: "NeoForge",
			options: InstanceOptions{
				GameVersion:   "release",
				Loader:        meta.LoaderNeoForge,
				LoaderVersion: "latest",
				Config: InstanceConfig{
					Java: "java",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.options.Name = uuid.NewString()
			inst, err := CreateInstance(tt.options)
			if err != nil {
				t.Fatalf("unexpected error creating instance for test: %s", err)
			}

			_, err = Prepare(&inst, LaunchOptions{
				Session: auth.Session{
					Username: "testing",
				},
				InstanceConfig: inst.Config,
				skipAssets:     true,
			}, testingWatcher)
			if err != nil {
				t.Errorf("wanted no error; got: %s", err)
			}
		})
	}
}

// TestVersionsLayoutIsWhereVersionsGo pins where a downloaded version ends up: in the standard
// versions directory, next to the ones PCL and the official launcher write, and not inside the
// launcher's own instances directory.
//
// Fetching metadata is what CreateInstance does first, so the layout is checked without it: a
// version directory written here stands in for one CreateInstance would have made.
func TestVersionsLayoutIsWhereVersionsGo(t *testing.T) {
	env.SetDirs(t.TempDir())

	if got := (InstanceOptions{}).layout(); got != LayoutVersions {
		t.Errorf("a version with no layout asked for goes to %q; want %q", got, LayoutVersions)
	}
	if got := (InstanceOptions{Layout: LayoutInstances}).layout(); got != LayoutInstances {
		t.Errorf("an explicitly requested layout was overridden: got %q", got)
	}

	inst := Instance{
		Name:          "1.20.1-Forge_47.4.16-LTSC",
		GameVersion:   "1.20.1",
		Loader:        meta.LoaderForge,
		LoaderVersion: "47.4.16",
		Config:        DefaultInstanceConfig(),
		Layout:        LayoutVersions,
	}
	if err := inst.WriteConfig(); err != nil {
		t.Fatalf("writing the version configuration: %s", err)
	}

	if _, err := os.Stat(filepath.Join(env.InstancesDir, inst.Name)); err == nil {
		t.Error("the version landed in the instances directory; it belongs in versions")
	}
	if !DoesInstanceExist(inst.Name) {
		t.Error("a versions directory holding its own configuration was not recognised as a version")
	}

	found, err := FetchInstance(inst.Name)
	if err != nil {
		t.Fatalf("fetching the version: %s", err)
	}
	if found.Layout != LayoutVersions {
		t.Errorf("the version was read back with layout %q; want %q", found.Layout, LayoutVersions)
	}
	if want := filepath.Join(env.VersionsDir, inst.Name); found.Dir() != want {
		t.Errorf("the version directory is %q; want %q", found.Dir(), want)
	}
}
