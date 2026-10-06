package env

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPortableBaseFromEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(portableHomeEnv, dir)

	base, ok := PortableBase()
	if !ok {
		t.Fatalf("%s should enable portable mode", portableHomeEnv)
	}
	if base != dir {
		t.Fatalf("base = %q, want %q", base, dir)
	}
}

func TestUsePortableLayout(t *testing.T) {
	base := t.TempDir()
	oldConfig, oldPortable := ConfigDir, Portable
	t.Cleanup(func() {
		ConfigDir, Portable = oldConfig, oldPortable
		setPaths(filepath.Join(base, "minecraft"))
	})

	UsePortable(base)

	if !Portable {
		t.Error("Portable = false, want true")
	}
	paths := map[string]struct {
		got  string
		want string
	}{
		"RootDir":       {RootDir, filepath.Join(base, "minecraft")},
		"VersionsDir":   {VersionsDir, filepath.Join(base, "minecraft", "versions")},
		"InstancesDir":  {InstancesDir, filepath.Join(base, "minecraft", "instances")},
		"LibrariesDir":  {LibrariesDir, filepath.Join(base, "minecraft", "libraries")},
		"ConfigDir":     {ConfigDir, filepath.Join(base, "config")},
		"AuthStorePath": {AuthStorePath, filepath.Join(base, "config", "accounts.json")},
	}
	for name, p := range paths {
		if p.got != p.want {
			t.Errorf("%s = %q, want %q", name, p.got, p.want)
		}
	}
}

func TestEnsureDirsCreatesPortableLayout(t *testing.T) {
	base := t.TempDir()
	oldConfig, oldPortable := ConfigDir, Portable
	t.Cleanup(func() {
		ConfigDir, Portable = oldConfig, oldPortable
		setPaths(filepath.Join(base, "minecraft"))
	})

	UsePortable(base)
	if err := EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}

	for _, dir := range []string{RootDir, ConfigDir} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Errorf("stat %s: %v", dir, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("%s is not a directory", dir)
		}
	}
}
