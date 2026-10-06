package launcher

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"

	"github.com/telecter/cmd-launcher/internal/meta"
	env "github.com/telecter/cmd-launcher/pkg"
)

// writeMrpack builds a modpack archive with an index and an overrides tree.
func writeMrpack(t *testing.T, path, index string, overrides map[string]string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	writer := zip.NewWriter(file)

	entry, err := writer.Create("modrinth.index.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte(index)); err != nil {
		t.Fatal(err)
	}
	for name, content := range overrides {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

const sampleIndex = `{
	"formatVersion": 1,
	"game": "minecraft",
	"versionId": "1.15.9",
	"name": "Remarkably Optimized",
	"summary": "A light pack.",
	"files": [
		{"path":"mods/modmenu.jar","hashes":{"sha1":"abc"},"env":{"client":"required"},"downloads":["https://example.invalid/modmenu.jar"],"fileSize":10},
		{"path":"mods/serveronly.jar","hashes":{"sha1":"def"},"env":{"client":"unsupported"},"downloads":["https://example.invalid/serveronly.jar"],"fileSize":10}
	],
	"dependencies": {"minecraft": "1.20.1", "fabric-loader": "0.18.4"}
}`

func TestReadMrpack(t *testing.T) {
	dir := t.TempDir()
	path := writeMrpack(t, filepath.Join(dir, "pack.mrpack"), sampleIndex, map[string]string{
		"overrides/options.txt":             "fov:0.5",
		"client-overrides/config/zoom.json": "{}",
	})

	index, err := ReadMrpack(path)
	if err != nil {
		t.Fatalf("ReadMrpack: %v", err)
	}
	if index.Name != "Remarkably Optimized" || index.VersionID != "1.15.9" {
		t.Errorf("name/version = %q/%q", index.Name, index.VersionID)
	}
	if index.Dependencies["minecraft"] != "1.20.1" {
		t.Errorf("dependencies = %v", index.Dependencies)
	}
	if len(index.Files) != 2 {
		t.Errorf("files = %d, want 2", len(index.Files))
	}
}

func TestReadMrpackRejectsForeignArchives(t *testing.T) {
	dir := t.TempDir()

	// A plain zip is not a modpack.
	plain := writeArchive(t, filepath.Join(dir, "plain.zip"), map[string]string{"readme.txt": "hi"})
	if _, err := ReadMrpack(plain); err == nil {
		t.Error("a zip without an index was accepted")
	}

	// An index without a Minecraft version cannot be installed.
	noGame := writeMrpack(t, filepath.Join(dir, "nogame.mrpack"), `{"name":"x","files":[],"dependencies":{}}`, nil)
	if _, err := ReadMrpack(noGame); err == nil {
		t.Error("an index without a Minecraft version was accepted")
	}
}

func TestMrpackLoader(t *testing.T) {
	cases := []struct {
		dependencies map[string]string
		loader       meta.Loader
		version      string
	}{
		{map[string]string{"minecraft": "1.20.1", "fabric-loader": "0.18.4"}, meta.LoaderFabric, "0.18.4"},
		{map[string]string{"minecraft": "1.20.1", "quilt-loader": "0.23.1"}, meta.LoaderQuilt, "0.23.1"},
		{map[string]string{"minecraft": "1.20.1", "forge": "47.4.0"}, meta.LoaderForge, "47.4.0"},
		{map[string]string{"minecraft": "1.20.1", "neoforge": "21.1.0"}, meta.LoaderNeoForge, "21.1.0"},
		{map[string]string{"minecraft": "1.20.1"}, meta.LoaderVanilla, ""},
	}
	for _, c := range cases {
		index := MrpackIndex{Dependencies: c.dependencies}
		loader, version := index.Loader()
		if loader != c.loader || version != c.version {
			t.Errorf("Loader(%v) = %s %q, want %s %q", c.dependencies, loader, version, c.loader, c.version)
		}
	}
}

func TestModpackDownloads(t *testing.T) {
	dir := t.TempDir()
	index := MrpackIndex{
		Files: []MrpackFile{
			{Path: "mods/a.jar", Hashes: map[string]string{"sha1": "abc"}, Downloads: []string{"https://example.invalid/a.jar"}},
			// A client never needs a server-only file.
			{Path: "mods/server.jar", Env: map[string]string{"client": "unsupported"}, Downloads: []string{"https://example.invalid/s.jar"}},
			// A file with no download cannot be fetched.
			{Path: "mods/local.jar"},
		},
	}

	files, err := modpackDownloads(dir, index)
	if err != nil {
		t.Fatalf("modpackDownloads: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("got %d downloads, want 1: %+v", len(files), files)
	}
	if files[0].Sha1 != "abc" {
		t.Errorf("sha1 = %q, want abc", files[0].Sha1)
	}
	if filepath.Base(files[0].Path) != "a.jar" {
		t.Errorf("path = %q", files[0].Path)
	}

	// A path that escapes the instance is refused.
	escaping := MrpackIndex{Files: []MrpackFile{{
		Path:      "../../evil.jar",
		Downloads: []string{"https://example.invalid/e.jar"},
	}}}
	if _, err := modpackDownloads(dir, escaping); err == nil {
		t.Error("a path escaping the instance directory was accepted")
	}
}

func TestSafeJoin(t *testing.T) {
	dir := t.TempDir()
	for _, relative := range []string{"mods/a.jar", "config/sub/x.json", "options.txt"} {
		if _, err := safeJoin(dir, relative); err != nil {
			t.Errorf("safeJoin(%q) = %v, want nil", relative, err)
		}
	}
	for _, relative := range []string{"../escape", "mods/../../escape", "/absolute", `..\escape`} {
		if _, err := safeJoin(dir, relative); err == nil {
			t.Errorf("safeJoin(%q) was accepted", relative)
		}
	}
}

func TestExtractOverrides(t *testing.T) {
	dir := t.TempDir()
	path := writeMrpack(t, filepath.Join(dir, "pack.mrpack"), sampleIndex, map[string]string{
		"overrides/options.txt":             "fov:0.5",
		"overrides/config/nested/deep.json": "{}",
		"client-overrides/config/zoom.json": "{}",
	})

	target := t.TempDir()
	if err := extractOverrides(path, target); err != nil {
		t.Fatalf("extractOverrides: %v", err)
	}

	for _, relative := range []string{"options.txt", "config/nested/deep.json", "config/zoom.json"} {
		if _, err := os.Stat(filepath.Join(target, filepath.FromSlash(relative))); err != nil {
			t.Errorf("%s was not extracted: %v", relative, err)
		}
	}
	// The index itself is not part of the overrides.
	if _, err := os.Stat(filepath.Join(target, "modrinth.index.json")); err == nil {
		t.Error("the index was extracted as an override")
	}
}

func TestModpackMarker(t *testing.T) {
	if err := env.SetDirs(t.TempDir()); err != nil {
		t.Fatalf("SetDirs: %v", err)
	}
	inst := Instance{Name: "pack-instance", Layout: LayoutInstances}
	if err := os.MkdirAll(inst.Dir(), 0755); err != nil {
		t.Fatal(err)
	}

	if _, ok := ReadModpackMarker(inst); ok {
		t.Error("a fresh instance already has a modpack marker")
	}

	marker := ModpackMarkerFile{Name: "Remarkably Optimized", Version: "1.15.9", Summary: "A light pack."}
	if err := WriteModpackMarker(inst, marker); err != nil {
		t.Fatalf("WriteModpackMarker: %v", err)
	}
	got, ok := ReadModpackMarker(inst)
	if !ok {
		t.Fatal("the marker could not be read back")
	}
	if got.Name != marker.Name || got.Version != marker.Version {
		t.Errorf("marker = %+v, want %+v", got, marker)
	}
}
