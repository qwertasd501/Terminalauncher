package launcher

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"

	env "github.com/telecter/cmd-launcher/pkg"
)

// writeArchive creates a zip file with the given entries, standing in for a mod jar or a pack.
func writeArchive(t *testing.T, path string, entries map[string]string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	writer := zip.NewWriter(file)
	for name, content := range entries {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("create entry %s: %v", name, err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatalf("write entry %s: %v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close archive: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close file: %v", err)
	}
	return path
}

// writeRawArchive creates a zip whose entries are raw bytes, which is what a nested jar needs.
func writeRawArchive(t *testing.T, path string, entries map[string][]byte) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	writer := zip.NewWriter(file)
	for name, content := range entries {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("create entry %s: %v", name, err)
		}
		if _, err := entry.Write(content); err != nil {
			t.Fatalf("write entry %s: %v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close archive: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close file: %v", err)
	}
	return path
}

const fabricModJSON = `{
	"schemaVersion": 1,
	"id": "sodium",
	"version": "0.5.13",
	"name": "Sodium",
	"description": "A modern rendering engine.",
	"authors": ["jellysquid3", {"name": "Other"}],
	"depends": {"fabricloader": ">=0.14", "minecraft": "1.20.1"}
}`

func TestReadModItemFromFabricJar(t *testing.T) {
	dir := t.TempDir()
	path := writeArchive(t, filepath.Join(dir, "sodium-fabric-0.5.13.jar"), map[string]string{
		"fabric.mod.json":        fabricModJSON,
		"assets/sodium/icon.png": "",
	})

	item := ReadContentItem(path, ContentMods, "sodium-fabric-0.5.13.jar", false)

	if item.Err != nil {
		t.Fatalf("ReadContentItem: %v", item.Err)
	}
	if item.ID != "sodium" || item.Name != "Sodium" || item.Version != "0.5.13" {
		t.Errorf("id/name/version = %q/%q/%q, want sodium/Sodium/0.5.13", item.ID, item.Name, item.Version)
	}
	if item.Source != "fabric" {
		t.Errorf("source = %q, want fabric", item.Source)
	}
	if !item.Enabled {
		t.Error("item is not enabled")
	}
	if len(item.Authors) != 2 || item.Authors[0] != "jellysquid3" {
		t.Errorf("authors = %v, want jellysquid3 and Other", item.Authors)
	}
	if item.Depends["minecraft"] != "1.20.1" {
		t.Errorf("depends = %v, want minecraft 1.20.1", item.Depends)
	}
	if item.DisplayName() != "Sodium" {
		t.Errorf("DisplayName() = %q, want Sodium", item.DisplayName())
	}
}

func TestReadModItemFromQuiltJar(t *testing.T) {
	dir := t.TempDir()
	path := writeArchive(t, filepath.Join(dir, "quilted.jar"), map[string]string{
		"quilt.mod.json": `{"quilt_loader":{"id":"quilted","version":"1.2.3",
			"metadata":{"name":"Quilted Mod","description":"A quilt mod","contributors":{"Alice":"Author"}}}}`,
	})

	item := ReadContentItem(path, ContentMods, "quilted.jar", false)
	if item.Err != nil {
		t.Fatalf("ReadContentItem: %v", item.Err)
	}
	if item.ID != "quilted" || item.Name != "Quilted Mod" || item.Version != "1.2.3" {
		t.Errorf("got %q/%q/%q, want quilted/Quilted Mod/1.2.3", item.ID, item.Name, item.Version)
	}
	if item.Source != "quilt" {
		t.Errorf("source = %q, want quilt", item.Source)
	}
	if len(item.Authors) != 1 || item.Authors[0] != "Alice" {
		t.Errorf("authors = %v, want [Alice]", item.Authors)
	}
}

func TestReadModItemFromForgeToml(t *testing.T) {
	dir := t.TempDir()
	path := writeArchive(t, filepath.Join(dir, "example.jar"), map[string]string{
		"META-INF/mods.toml": `
modLoader="javafml"
loaderVersion="[47,)"
license="MIT"

[[mods]]
modId="examplemod"
version="1.7"
displayName="Example Mod"
authors="Ada, Grace"
description='''A Forge mod.'''

[[dependencies.examplemod]]
    modId="forge"
    mandatory=true
    versionRange="[47,)"
`,
	})

	item := ReadContentItem(path, ContentMods, "example.jar", false)
	if item.Err != nil {
		t.Fatalf("ReadContentItem: %v", item.Err)
	}
	if item.Source != "forge" {
		t.Errorf("source = %q, want forge", item.Source)
	}
	if item.ID != "examplemod" || item.Name != "Example Mod" || item.Version != "1.7" {
		t.Errorf("got %q/%q/%q, want examplemod/Example Mod/1.7", item.ID, item.Name, item.Version)
	}
	if len(item.Authors) != 2 {
		t.Errorf("authors = %v, want two names split on the comma", item.Authors)
	}
	if item.Depends["forge"] != "[47,)" {
		t.Errorf("depends = %v, want forge [47,)", item.Depends)
	}
}

// TestNeoforgeDescriptorWins checks that the descriptor NeoForge renamed in 1.20.5 is recognised.
func TestNeoforgeDescriptorWins(t *testing.T) {
	dir := t.TempDir()
	path := writeArchive(t, filepath.Join(dir, "neo.jar"), map[string]string{
		"META-INF/neoforge.mods.toml": `
[[mods]]
modId="neomod"
version="21.1.0"
displayName="Neo Mod"
authors=["Neo"]
`,
	})

	item := ReadContentItem(path, ContentMods, "neo.jar", false)
	if item.Source != "neoforge" {
		t.Errorf("source = %q, want neoforge", item.Source)
	}
	if item.ID != "neomod" {
		t.Errorf("id = %q, want neomod", item.ID)
	}
	if len(item.Authors) != 1 || item.Authors[0] != "Neo" {
		t.Errorf("authors = %v, want [Neo]", item.Authors)
	}
}

func TestReadModItemFromLegacyMcmodInfo(t *testing.T) {
	cases := map[string]string{
		"a bare list": `[{"modid":"old","name":"Old Mod","version":"1.0","authorList":["Someone"]}]`,
		"a wrapper":   `{"modList":[{"modid":"old","name":"Old Mod","version":"1.0","authors":["Someone"]}],"modpack":{}}`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := writeArchive(t, filepath.Join(dir, "old.jar"), map[string]string{"mcmod.info": content})

			item := ReadContentItem(path, ContentMods, "old.jar", false)
			if item.Err != nil {
				t.Fatalf("ReadContentItem: %v", item.Err)
			}
			if item.Source != "legacy" {
				t.Errorf("source = %q, want legacy", item.Source)
			}
			if item.ID != "old" || item.Name != "Old Mod" {
				t.Errorf("got %q/%q, want old/Old Mod", item.ID, item.Name)
			}
			if len(item.Authors) != 1 || item.Authors[0] != "Someone" {
				t.Errorf("authors = %v, want [Someone]", item.Authors)
			}
		})
	}
}

func TestReadPackMeta(t *testing.T) {
	dir := t.TempDir()

	plain := writeArchive(t, filepath.Join(dir, "pack.zip"), map[string]string{
		"pack.mcmeta": `{"pack":{"pack_format":15,"description":"A plain description"}}`,
	})
	item := ReadContentItem(plain, ContentResourcePacks, "pack.zip", false)
	if item.PackFormat != 15 {
		t.Errorf("pack format = %d, want 15", item.PackFormat)
	}
	if item.Description != "A plain description" {
		t.Errorf("description = %q", item.Description)
	}

	// Since 1.20.3 a description may be a chat component.
	component := writeArchive(t, filepath.Join(dir, "component.zip"), map[string]string{
		"pack.mcmeta": `{"pack":{"pack_format":32,"description":{"text":"A component description"}}}`,
	})
	item = ReadContentItem(component, ContentResourcePacks, "component.zip", false)
	if item.Description != "A component description" {
		t.Errorf("description = %q, want the text of the component", item.Description)
	}
}

func TestReadShaderPack(t *testing.T) {
	dir := t.TempDir()
	path := writeArchive(t, filepath.Join(dir, "shiny.zip"), map[string]string{
		"shaders/shaders.properties": "title=Shiny Shaders\nscreen=...\n",
		"shaders/final.fsh":          "#version 330\n",
	})

	item := ReadContentItem(path, ContentShaderPacks, "shiny.zip", false)
	if item.Err != nil {
		t.Fatalf("ReadContentItem: %v", item.Err)
	}
	if item.Source != "shader" {
		t.Errorf("source = %q, want shader", item.Source)
	}
	if item.Name != "Shiny Shaders" {
		t.Errorf("name = %q, want the title from shaders.properties", item.Name)
	}

	// A zip without a shaders folder is not a shader pack.
	notShader := writeArchive(t, filepath.Join(dir, "nope.zip"), map[string]string{"readme.txt": "hi"})
	item = ReadContentItem(notShader, ContentShaderPacks, "nope.zip", false)
	if item.Err == nil {
		t.Error("a zip without a shaders folder was accepted")
	}
	if item.DisplayName() != "nope" {
		t.Errorf("DisplayName() = %q, want the file name without extension", item.DisplayName())
	}
}

// TestBrokenArchiveIsStillListed checks that an unreadable file stays visible and can be removed.
func TestBrokenArchiveIsStillListed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "broken.jar")
	if err := os.WriteFile(path, []byte("this is not a zip"), 0644); err != nil {
		t.Fatal(err)
	}

	items, err := ListContent(dir, ContentMods)
	if err != nil {
		t.Fatalf("ListContent: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	if items[0].Err == nil {
		t.Error("a file that is not a zip was read without error")
	}
	if items[0].DisplayName() != "broken" {
		t.Errorf("DisplayName() = %q, want broken", items[0].DisplayName())
	}
}

func TestListContentFiltersByKind(t *testing.T) {
	dir := t.TempDir()
	writeArchive(t, filepath.Join(dir, "a.jar"), map[string]string{"fabric.mod.json": fabricModJSON})
	writeArchive(t, filepath.Join(dir, "b.zip"), map[string]string{"pack.mcmeta": `{"pack":{"pack_format":15}}`})
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".hidden.jar"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	mods, err := ListContent(dir, ContentMods)
	if err != nil {
		t.Fatalf("ListContent(mods): %v", err)
	}
	if len(mods) != 1 || mods[0].FileName != "a.jar" {
		t.Errorf("mods = %v, want just a.jar", mods)
	}

	packs, err := ListContent(dir, ContentResourcePacks)
	if err != nil {
		t.Fatalf("ListContent(resourcepacks): %v", err)
	}
	if len(packs) != 1 || packs[0].FileName != "b.zip" {
		t.Errorf("packs = %v, want just b.zip", packs)
	}

	empty, err := ListContent(filepath.Join(dir, "missing"), ContentMods)
	if err != nil {
		t.Fatalf("ListContent on a missing directory: %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("a missing directory returned %d items", len(empty))
	}
}

func TestSetContentEnabled(t *testing.T) {
	dir := t.TempDir()
	writeArchive(t, filepath.Join(dir, "a.jar"), map[string]string{"fabric.mod.json": fabricModJSON})

	items, _ := ListContent(dir, ContentMods)
	item := items[0]

	if err := SetContentEnabled(&item, false); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if item.Enabled || item.FileName != "a.jar.disabled" {
		t.Errorf("after disabling: %+v", item)
	}
	if _, err := os.Stat(filepath.Join(dir, "a.jar.disabled")); err != nil {
		t.Errorf("disabled file missing: %v", err)
	}
	// The disabled file is still listed, and listed as switched off.
	items, _ = ListContent(dir, ContentMods)
	if len(items) != 1 || items[0].Enabled {
		t.Errorf("disabled file was not listed as disabled: %+v", items)
	}

	if err := SetContentEnabled(&item, true); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if !item.Enabled || item.FileName != "a.jar" {
		t.Errorf("after enabling: %+v", item)
	}

	// Enabling an enabled file is a no-op rather than an error.
	if err := SetContentEnabled(&item, true); err != nil {
		t.Errorf("enabling twice: %v", err)
	}
}

func TestRemoveAndRenameContent(t *testing.T) {
	dir := t.TempDir()
	writeArchive(t, filepath.Join(dir, "a.jar"), map[string]string{"fabric.mod.json": fabricModJSON})

	items, _ := ListContent(dir, ContentMods)
	item := items[0]

	if err := RenameContent(&item, "renamed.jar"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if item.FileName != "renamed.jar" {
		t.Errorf("file name = %q, want renamed.jar", item.FileName)
	}
	if err := RenameContent(&item, "../escape.jar"); err == nil {
		t.Error("a name with a separator was accepted")
	}

	if err := RemoveContent(item); err != nil {
		t.Fatalf("remove: %v", err)
	}
	items, _ = ListContent(dir, ContentMods)
	if len(items) != 0 {
		t.Errorf("file still there after removal: %+v", items)
	}
}

// TestRenameKeepsExtension guards the rule that a rename never changes the extension, which decides
// which category a file belongs to: without it, renaming a .jar to .zip would silently hide the mod.
func TestRenameKeepsExtension(t *testing.T) {
	dir := t.TempDir()
	writeArchive(t, filepath.Join(dir, "a.jar"), map[string]string{"fabric.mod.json": fabricModJSON})

	items, _ := ListContent(dir, ContentMods)
	item := items[0]

	// A name typed without an extension gets the old one back.
	if err := RenameContent(&item, "renamed"); err != nil {
		t.Fatalf("rename without extension: %v", err)
	}
	if item.FileName != "renamed.jar" {
		t.Errorf("file name = %q, want renamed.jar", item.FileName)
	}

	// A different extension is refused, so the file cannot be moved out of its category.
	if err := RenameContent(&item, "renamed.zip"); err == nil {
		t.Error("a changed extension was accepted")
	}
	if item.FileName != "renamed.jar" {
		t.Errorf("file name changed to %q by a refused rename", item.FileName)
	}

	// The disabled marker survives a rename and is never typed twice.
	if err := SetContentEnabled(&item, false); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if err := RenameContent(&item, "off.jar"); err != nil {
		t.Fatalf("rename while disabled: %v", err)
	}
	if item.FileName != "off.jar"+DisabledSuffix {
		t.Errorf("file name = %q, want off.jar%s", item.FileName, DisabledSuffix)
	}

	items, _ = ListContent(dir, ContentMods)
	if len(items) != 1 || items[0].FileName != item.FileName {
		t.Errorf("listing after rename = %+v, want the renamed file", items)
	}
}

func TestImportContent(t *testing.T) {
	dir := t.TempDir()
	source := t.TempDir()
	writeArchive(t, filepath.Join(source, "one.jar"), map[string]string{"fabric.mod.json": fabricModJSON})
	if err := os.WriteFile(filepath.Join(source, "two.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	outcomes := ImportContent(dir, ContentMods, []string{source})
	if len(outcomes) != 1 || outcomes[0].Name != "one.jar" || outcomes[0].Err != nil {
		t.Fatalf("importing a folder: %+v", outcomes)
	}
	if _, err := os.Stat(filepath.Join(dir, "one.jar")); err != nil {
		t.Errorf("imported file missing: %v", err)
	}

	// A second import of the same file is reported as kept, not as an error.
	outcomes = ImportContent(dir, ContentMods, []string{filepath.Join(source, "one.jar")})
	if len(outcomes) != 1 || !outcomes[0].Skipped {
		t.Fatalf("re-importing: %+v", outcomes)
	}

	// A file of the wrong kind is refused.
	outcomes = ImportContent(dir, ContentMods, []string{filepath.Join(source, "two.txt")})
	if len(outcomes) != 1 || outcomes[0].Err == nil {
		t.Fatalf("importing a text file: %+v", outcomes)
	}

	outcomes = ImportContent(dir, ContentMods, []string{filepath.Join(source, "nope.jar")})
	if len(outcomes) != 1 || outcomes[0].Err == nil {
		t.Fatalf("importing a missing file: %+v", outcomes)
	}
}

func TestContentDirAndWorlds(t *testing.T) {
	if err := env.SetDirs(t.TempDir()); err != nil {
		t.Fatalf("SetDirs: %v", err)
	}
	inst := Instance{Name: "test", Layout: LayoutInstances, GameVersion: "1.20.1"}
	if err := os.MkdirAll(filepath.Join(inst.Dir(), "saves", "World One", "datapacks"), 0755); err != nil {
		t.Fatal(err)
	}

	dir, err := ContentDir(inst, ContentMods, "")
	if err != nil {
		t.Fatalf("ContentDir(mods): %v", err)
	}
	if filepath.Base(dir) != "mods" {
		t.Errorf("mods directory = %q", dir)
	}

	if _, err := ContentDir(inst, ContentDataPacks, ""); err == nil {
		t.Error("a data pack directory was built without a world")
	}
	if _, err := ContentDir(inst, ContentDataPacks, "../escape"); err == nil {
		t.Error("a world name with a separator was accepted")
	}
	if _, err := ContentDir(inst, ContentDataPacks, "World One"); err != nil {
		t.Errorf("ContentDir(datapacks): %v", err)
	}

	worlds, err := Worlds(inst)
	if err != nil {
		t.Fatalf("Worlds: %v", err)
	}
	if len(worlds) != 1 || worlds[0] != "World One" {
		t.Errorf("worlds = %v, want [World One]", worlds)
	}
}

func TestValidateWorldName(t *testing.T) {
	for _, name := range []string{"world", "World One", "w-1_2"} {
		if err := ValidateWorldName(name); err != nil {
			t.Errorf("ValidateWorldName(%q) = %v, want nil", name, err)
		}
	}
	for _, name := range []string{"", ".", "..", "a/b", `a\b`} {
		if err := ValidateWorldName(name); err == nil {
			t.Errorf("ValidateWorldName(%q) was accepted", name)
		}
	}
}

// TestDirectoryContent covers an unpacked pack folder, which resource and data packs may be.
func TestDirectoryContent(t *testing.T) {
	dir := t.TempDir()
	packDir := filepath.Join(dir, "My Pack")
	if err := os.MkdirAll(packDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packDir, "pack.mcmeta"), []byte(`{"pack":{"pack_format":15,"description":"Folder pack"}}`), 0644); err != nil {
		t.Fatal(err)
	}

	items, err := ListContent(dir, ContentResourcePacks)
	if err != nil {
		t.Fatalf("ListContent: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	if items[0].Description != "Folder pack" {
		t.Errorf("description = %q", items[0].Description)
	}

	// A folder is never a mod.
	mods, err := ListContent(dir, ContentMods)
	if err != nil {
		t.Fatalf("ListContent(mods): %v", err)
	}
	if len(mods) != 0 {
		t.Errorf("a folder was listed as a mod: %+v", mods)
	}
}

func TestDisplayNameFallback(t *testing.T) {
	item := ContentItem{FileName: "Some-Mod-1.2.3.jar.disabled"}
	if got := item.DisplayName(); got != "Some-Mod-1.2.3" {
		t.Errorf("DisplayName() = %q, want Some-Mod-1.2.3", got)
	}

	named := ContentItem{FileName: "a.jar", Name: "Declared Name"}
	if got := named.DisplayName(); got != "Declared Name" {
		t.Errorf("DisplayName() = %q, want the declared name", got)
	}
}

// TestReadModItemWithArrayDepends covers the fabric metadata that lists several supported game
// versions for one dependency. The list was decoded into a string field, which failed with
// "cannot unmarshal array" and threw away the whole file's metadata, leaving the mod nameless in
// the listing (Iris, Indium and Dynamic FPS all ship that shape).
func TestReadModItemWithArrayDepends(t *testing.T) {
	dir := t.TempDir()
	path := writeArchive(t, filepath.Join(dir, "iris-1.7.6+mc1.20.1.jar"), map[string]string{
		"fabric.mod.json": `{
			"schemaVersion": 1,
			"id": "iris",
			"version": "1.7.6+mc1.20.1",
			"name": "Iris",
			"description": "A modern shaders mod.",
			"authors": ["coderbot"],
			"depends": {"minecraft": ["1.20", "1.20.1", "1.20.2"], "sodium": ">=0.5.0"},
			"recommends": {"sodium": "*"}
		}`,
	})

	item := ReadContentItem(path, ContentMods, "iris-1.7.6+mc1.20.1.jar", false)

	if item.Err != nil {
		t.Fatalf("unexpected error: %v", item.Err)
	}
	if item.Source != "fabric" {
		t.Errorf("source = %q, want fabric", item.Source)
	}
	if item.Name != "Iris" || item.Version != "1.7.6+mc1.20.1" {
		t.Errorf("name/version = %q/%q, want Iris/1.7.6+mc1.20.1", item.Name, item.Version)
	}
	if got := item.Depends["minecraft"]; got != "1.20, 1.20.1, 1.20.2" {
		t.Errorf("depends[minecraft] = %q, want the list joined", got)
	}
	if got := item.Depends["sodium"]; got != ">=0.5.0" {
		t.Errorf("depends[sodium] = %q, want >=0.5.0", got)
	}
	if len(item.Authors) != 1 || item.Authors[0] != "coderbot" {
		t.Errorf("authors = %v, want [coderbot]", item.Authors)
	}
}

// TestReadModItemWithoutDescriptorFallsBackToFileName checks that a jar carrying no descriptor is
// still listed under its own name. A handful of loader libraries are shaped that way, and flagging
// them as unreadable suggested a failed download the user ought to re-fetch.
func TestReadModItemWithoutDescriptorFallsBackToFileName(t *testing.T) {
	dir := t.TempDir()
	path := writeArchive(t, filepath.Join(dir, "mystery.jar"), map[string]string{"data/thing.bin": "x"})

	item := ReadContentItem(path, ContentMods, "mystery.jar", false)
	if item.Err != nil {
		t.Fatalf("a jar with no descriptor was reported as broken: %v", item.Err)
	}
	if item.Source != "" {
		t.Errorf("source = %q, want empty", item.Source)
	}
	if got := item.DisplayName(); got != "mystery" {
		t.Errorf("display name = %q, want the file name without its extension", got)
	}
}

// TestReadModItemSubstitutesFileJarVersion covers the ${file.jarVersion} placeholder that Gradle
// leaves in mods.toml when a build does not expand resources. Forge substitutes the manifest
// version when it loads such a jar; printing the placeholder instead showed gibberish for around a
// third of a real 152-mod instance (Iceberg, Jade, Twilight Forest, The Aether, ...).
func TestReadModItemSubstitutesFileJarVersion(t *testing.T) {
	dir := t.TempDir()
	path := writeArchive(t, filepath.Join(dir, "Iceberg-1.20.1-forge-1.1.25.jar"), map[string]string{
		"META-INF/MANIFEST.MF": "Manifest-Version: 1.0\r\nSpecification-Title: Iceberg\r\nSpecification-Version: 1.1.25\r\nImplementation-Title: Iceberg\r\nImplementation-Version: 1.1.25\r\n\r\n",
		"META-INF/mods.toml":   "modLoader=\"javafml\"\nloaderVersion=\"[47,)\"\nlicense=\"MIT\"\n\n[[mods]]\nmodId=\"iceberg\"\nversion=\"${file.jarVersion}\"\ndisplayName=\"Iceberg\"\nauthors=\"Grend\"\ndescription=\"A library of helpers.\"\n",
	})

	item := ReadContentItem(path, ContentMods, "Iceberg-1.20.1-forge-1.1.25.jar", false)
	if item.Err != nil {
		t.Fatalf("unexpected error: %v", item.Err)
	}
	if item.Version != "1.1.25" {
		t.Errorf("version = %q, want the manifest version 1.1.25", item.Version)
	}
	if item.Name != "Iceberg" {
		t.Errorf("name = %q, want Iceberg", item.Name)
	}
	if item.Source != "forge" {
		t.Errorf("source = %q, want forge", item.Source)
	}
}

// TestReadModItemFromNestedJar covers a wrapper jar: the descriptor lives one level down, in a jar
// under META-INF/jarjar or META-INF/jars. Sinytra Connector, Kotlin For Forge and
// lazyyyyy-lexforge-core all ship that way, with an outer jar that says almost nothing.
func TestReadModItemFromNestedJar(t *testing.T) {
	dir := t.TempDir()

	inner := writeRawArchive(t, filepath.Join(dir, "kffmod-4.12.0.jar"), map[string][]byte{
		"META-INF/mods.toml": []byte("modLoader=\"kotlinforforge\"\nloaderVersion=\"[4,)\"\nlicense=\"LGPL\"\n\n[[mods]]\nmodId=\"kotlinforforge\"\nversion=\"4.12.0\"\ndisplayName=\"Kotlin For Forge\"\n"),
	})
	innerData, err := os.ReadFile(inner)
	if err != nil {
		t.Fatalf("read nested jar: %v", err)
	}

	// Forge's JarJar layout.
	outer := writeRawArchive(t, filepath.Join(dir, "kotlinforforge-4.12.0-all.jar"), map[string][]byte{
		"META-INF/MANIFEST.MF":              []byte("Manifest-Version: 1.0\r\nAutomatic-Module-Name: thedarkcolour.kotlinforforge\r\nFMLModType: LIBRARY\r\n\r\n"),
		"META-INF/jarjar/kffmod-4.12.0.jar": innerData,
	})

	item := ReadContentItem(outer, ContentMods, "kotlinforforge-4.12.0-all.jar", false)
	if item.Err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if item.Name != "Kotlin For Forge" || item.Version != "4.12.0" {
		t.Errorf("name/version = %q/%q, want Kotlin For Forge/4.12.0", item.Name, item.Version)
	}
	if item.Source != "forge" {
		t.Errorf("source = %q, want forge", item.Source)
	}

	// The Fabric jar-in-jar layout, under META-INF/jars instead.
	other := writeRawArchive(t, filepath.Join(dir, "lazyyyyy-lexforge-core-0.14.19.jar"), map[string][]byte{
		"META-INF/MANIFEST.MF":                    []byte("Manifest-Version: 1.0\r\nFMLModType: LIBRARY\r\n\r\n"),
		"META-INF/jars/lazyyyyy-lexforge-mod.jar": innerData,
	})
	item = ReadContentItem(other, ContentMods, "lazyyyyy-lexforge-core-0.14.19.jar", false)
	if item.Name != "Kotlin For Forge" {
		t.Errorf("a jar under META-INF/jars was not opened: name = %q", item.Name)
	}
}

// TestReadModItemFromManifestOnly covers the last resort: a jar whose only self-description is its
// manifest. Kotlin For Forge's fat jar names itself through its automatic module name.
func TestReadModItemFromManifestOnly(t *testing.T) {
	dir := t.TempDir()
	path := writeArchive(t, filepath.Join(dir, "library.jar"), map[string]string{
		"META-INF/MANIFEST.MF": "Manifest-Version: 1.0\r\nAutomatic-Module-Name: thedarkcolour.kotlinforforge\r\nFMLModType: LIBRARY\r\n\r\n",
	})

	item := ReadContentItem(path, ContentMods, "library.jar", false)
	if item.Err != nil {
		t.Fatalf("unexpected error: %v", item.Err)
	}
	if item.Name != "kotlinforforge" {
		t.Errorf("name = %q, want the last segment of the module name", item.Name)
	}
	if item.Source != "jar" {
		t.Errorf("source = %q, want jar", item.Source)
	}
}

// TestFixMojibake covers names that were decoded as Latin-1 before being written back as UTF-8.
// "Explorer's Kit" ships that way in a real modpack, and the listing showed "Explorerâ€™s Kit".
func TestFixMojibake(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Explorer\u00e2\u0080\u0099s Kit", "Explorer\u2019s Kit"}, // UTF-8 -> Latin-1, as mods.toml ships it
		{"caf\u00c3\u00a9", "caf\u00e9"},                           // the same mistake on a two-byte character
		{"Iceberg", "Iceberg"},                                     // plain ASCII is left alone
		{"Touhou Little Maid", "Touhou Little Maid"},
		{"\u4e2d\u6587\u540d\u79f0", "\u4e2d\u6587\u540d\u79f0"}, // real multi-byte text is left alone
		{"\u00c3", "\u00c3"},                                     // not valid UTF-8 as bytes, so left alone
		{"", ""},
	}
	for _, c := range cases {
		if got := fixMojibake(c.in); got != c.want {
			t.Errorf("fixMojibake(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
