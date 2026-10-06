package launcher

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/pelletier/go-toml/v2"
)

// A ContentKind is one of the file categories an instance can hold.
//
// These are the four categories PCL manages per version (mods, resource packs, shader packs and
// data packs), so that the shell can offer a single command family for all of them.
type ContentKind string

const (
	ContentMods          ContentKind = "mods"
	ContentResourcePacks ContentKind = "resourcepacks"
	ContentShaderPacks   ContentKind = "shaderpacks"
	ContentDataPacks     ContentKind = "datapacks"
)

// ContentKinds lists the categories that live in the game directory, in menu order.
var ContentKinds = []ContentKind{ContentMods, ContentResourcePacks, ContentShaderPacks, ContentDataPacks}

// DisabledSuffix is appended to a file name to switch it off without deleting it, which is how
// both PCL and this launcher disable a mod.
const DisabledSuffix = ".disabled"

// A ContentItem is one managed file together with the metadata read out of it.
type ContentItem struct {
	Kind      ContentKind
	FileName  string // Name on disk, including the .disabled suffix when it is switched off.
	Path      string // Absolute path.
	Enabled   bool
	Directory bool  // An unpacked folder rather than an archive.
	Size      int64 // Size in bytes; a directory reports 0.

	ID          string            // Mod id, or an empty string when the file declares none.
	Name        string            // Display name from the file's own metadata.
	Version     string            // Version declared by the file itself.
	Authors     []string          //
	Source      string            // "fabric", "quilt", "forge", "neoforge", "legacy", "pack", or "".
	Description string            //
	Depends     map[string]string // Mod id to version range, for mods.
	PackFormat  int               // Resource and data packs only.
	Err         error             // Set when the file could not be read.
}

// DisplayName is the name to show in listings, falling back to the file name.
func (item ContentItem) DisplayName() string {
	if item.Name != "" {
		return item.Name
	}
	return strings.TrimSuffix(stripDisabled(item.FileName), filepath.Ext(stripDisabled(item.FileName)))
}

// stripDisabled removes the ".disabled" marker from a file name.
func stripDisabled(name string) string {
	if strings.HasSuffix(strings.ToLower(name), DisabledSuffix) {
		return name[:len(name)-len(DisabledSuffix)]
	}
	return name
}

// ContentDir returns the directory a category is stored in.
//
// Data packs belong to a world, so a world name is required for them.
func ContentDir(inst Instance, kind ContentKind, world string) (string, error) {
	game := inst.GameDir()
	switch kind {
	case ContentMods, ContentResourcePacks, ContentShaderPacks:
		return filepath.Join(game, string(kind)), nil
	case ContentDataPacks:
		if world == "" {
			return "", fmt.Errorf("no world selected")
		}
		if err := ValidateWorldName(world); err != nil {
			return "", err
		}
		return filepath.Join(game, "saves", world, "datapacks"), nil
	}
	return "", fmt.Errorf("unknown content kind %q", kind)
}

// ValidateWorldName rejects a world name that would escape the saves directory.
func ValidateWorldName(name string) error {
	if name == "" || name == "." || name == ".." {
		return fmt.Errorf("invalid world name %q", name)
	}
	if strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("invalid world name %q", name)
	}
	return nil
}

// Worlds lists the worlds of an instance, that is the directories under its saves folder.
func Worlds(inst Instance) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(inst.GameDir(), "saves"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	var worlds []string
	for _, entry := range entries {
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
			worlds = append(worlds, entry.Name())
		}
	}
	sort.Slice(worlds, func(i, j int) bool { return strings.ToLower(worlds[i]) < strings.ToLower(worlds[j]) })
	return worlds, nil
}

// isContentName reports whether a directory entry belongs to a category.
func isContentName(kind ContentKind, name string, isDir bool) bool {
	if strings.HasPrefix(name, ".") {
		return false
	}
	base := stripDisabled(name)
	ext := strings.ToLower(filepath.Ext(base))

	switch kind {
	case ContentMods:
		return !isDir && ext == ".jar"
	case ContentResourcePacks, ContentShaderPacks:
		return ext == ".zip" || isDir
	case ContentDataPacks:
		return ext == ".zip" || ext == ".jar" || isDir
	}
	return false
}

// ListContent reads every managed file in a directory.
//
// A missing directory is not an error: an instance that never had a mod installed simply has no
// mods folder yet.
func ListContent(dir string, kind ContentKind) ([]ContentItem, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	items := make([]ContentItem, 0, len(entries))
	for _, entry := range entries {
		if !isContentName(kind, entry.Name(), entry.IsDir()) {
			continue
		}
		item := ReadContentItem(filepath.Join(dir, entry.Name()), kind, entry.Name(), entry.IsDir())
		if info, err := entry.Info(); err == nil && !entry.IsDir() {
			item.Size = info.Size()
		}
		items = append(items, item)
	}

	sort.Slice(items, func(i, j int) bool {
		return strings.ToLower(items[i].FileName) < strings.ToLower(items[j].FileName)
	})
	return items, nil
}

// ReadContentItem reads one file's metadata.
//
// The returned item is usable even when the metadata cannot be read: Err then explains why, and
// the display name falls back to the file name, so that a broken download stays visible and can be
// removed from the shell.
func ReadContentItem(path string, kind ContentKind, fileName string, isDir bool) ContentItem {
	if fileName == "" {
		fileName = filepath.Base(path)
	}
	item := ContentItem{
		Kind:      kind,
		FileName:  fileName,
		Path:      path,
		Enabled:   !strings.HasSuffix(strings.ToLower(fileName), DisabledSuffix),
		Directory: isDir,
	}

	if isDir {
		readFromDir(path, kind, &item)
	} else {
		readFromArchive(path, kind, &item)
	}

	// A few mods ship text that was decoded as Latin-1 before being written as UTF-8, so the same
	// bytes get mangled a second time. Fix it once, here, whoever supplied the text.
	item.Name = fixMojibake(item.Name)
	item.Description = fixMojibake(item.Description)
	for i, author := range item.Authors {
		item.Authors[i] = fixMojibake(author)
	}
	return item
}

// fixMojibake repairs text that was read as Latin-1 before being stored as UTF-8, the mistake that
// turns "Explorer's Kit" into "Explorerâ€™s Kit" and "café" into "cafÃ©". Re-encoding the runes as
// bytes recovers the original. The guard is that every rune must fit in one byte and the result
// must be valid UTF-8: real multi-byte text, Chinese included, has runes above 0xFF and is returned
// untouched, as is a Latin-1 sequence that only looks like one because it happens to be invalid
// UTF-8.
func fixMojibake(s string) string {
	raw := make([]byte, 0, len(s))
	for _, r := range s {
		if r > 0xff {
			return s
		}
		raw = append(raw, byte(r))
	}
	if !utf8.Valid(raw) {
		return s
	}
	if decoded := string(raw); decoded != s {
		return decoded
	}
	return s
}

// readFromArchive reads the metadata of an archive such as a mod jar or a pack zip.
func readFromArchive(path string, kind ContentKind, item *ContentItem) {
	r, err := zip.OpenReader(path)
	if err != nil {
		item.Err = fmt.Errorf("open archive: %w", err)
		return
	}
	defer r.Close()

	switch kind {
	case ContentMods:
		readModMeta(r.File, item)
	case ContentResourcePacks, ContentDataPacks:
		readPackMeta(r.File, item)
	case ContentShaderPacks:
		readShaderMeta(r.File, item)
	}
}

// readFromDir reads the metadata of an unpacked folder, which packs (but never mods) may be.
func readFromDir(dir string, kind ContentKind, item *ContentItem) {
	if kind == ContentMods {
		item.Err = fmt.Errorf("a mod must be a jar file")
		return
	}
	data, err := os.ReadFile(filepath.Join(dir, "pack.mcmeta"))
	if err != nil {
		// An unpacked shader pack has no pack.mcmeta; its folder name is all there is to show.
		if kind == ContentShaderPacks {
			if _, err := os.Stat(filepath.Join(dir, "shaders")); err == nil {
				item.Source = "shader"
				return
			}
		}
		item.Err = fmt.Errorf("read pack.mcmeta: %w", err)
		return
	}
	decodePackMeta(data, item)
}

// zipEntry finds an entry by its exact path.
func zipEntry(files []*zip.File, name string) *zip.File {
	for _, file := range files {
		if strings.EqualFold(file.Name, name) {
			return file
		}
	}
	return nil
}

// readZipFile reads one entry, refusing anything unreasonably large for a metadata file.
func readZipFile(file *zip.File) ([]byte, error) {
	if file.UncompressedSize64 > 4<<20 {
		return nil, fmt.Errorf("%s is too large to be metadata", file.Name)
	}
	r, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(io.LimitReader(r, 4<<20))
}

// hasEntryPrefix reports whether the archive contains a directory of that name.
func hasEntryPrefix(files []*zip.File, prefix string) bool {
	prefix = strings.ToLower(prefix)
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	for _, file := range files {
		if strings.HasPrefix(strings.ToLower(file.Name), prefix) {
			return true
		}
	}
	return false
}

// readModMeta reads whichever metadata format a mod jar uses, newest first.
func readModMeta(files []*zip.File, item *ContentItem) {
	man := readManifest(files)

	if file := zipEntry(files, "fabric.mod.json"); file != nil {
		if data, err := readZipFile(file); err == nil {
			if meta, ok := decodeFabricMeta(data); ok {
				item.Source = "fabric"
				item.ID = meta.id
				item.Name, item.Version = meta.name, resolveJarVersion(meta.version, man)
				item.Description = meta.description
				item.Authors = meta.authors
				item.Depends = meta.depends
				return
			}
		}
	}

	if file := zipEntry(files, "quilt.mod.json"); file != nil {
		if data, err := readZipFile(file); err == nil {
			if meta, ok := decodeQuiltMeta(data); ok {
				item.Source = "quilt"
				item.ID = meta.id
				item.Name, item.Version = meta.name, resolveJarVersion(meta.version, man)
				item.Description = meta.description
				item.Authors = meta.authors
				return
			}
		}
	}

	// NeoForge renamed the descriptor in 1.20.5; both names are still in the wild.
	for _, candidate := range []struct{ path, source string }{
		{"META-INF/neoforge.mods.toml", "neoforge"},
		{"META-INF/mods.toml", "forge"},
	} {
		file := zipEntry(files, candidate.path)
		if file == nil {
			continue
		}
		if data, err := readZipFile(file); err == nil && readForgeModsToml(data, item) {
			item.Source = candidate.source
			item.Version = resolveJarVersion(item.Version, man)
			return
		}
	}

	if file := zipEntry(files, "mcmod.info"); file != nil {
		if data, err := readZipFile(file); err == nil && readMcmodInfo(data, item) {
			item.Source = "legacy"
			item.Version = resolveJarVersion(item.Version, man)
			return
		}
	}

	// Not a mod after all: a resource or data pack in the wrong folder still shows its own name.
	if hasEntryPrefix(files, "shaders") {
		item.Source = "shader"
		return
	}
	if file := zipEntry(files, "pack.mcmeta"); file != nil {
		if data, err := readZipFile(file); err == nil {
			item.Source = "pack"
			decodePackMeta(data, item)
		}
	}

	// A wrapper jar: Forge's JarJar and Fabric's jar-in-jar let a mod bundle others, and the real
	// descriptor sits one level down. Kotlin For Forge and Sinytra Connector both ship that way,
	// with an outer manifest that names them only vaguely.
	if inner := nestedJarFiles(files); inner != nil {
		readModMeta(inner, item)
		if item.Source != "" {
			return
		}
	}

	// Some jars carry no descriptor at all: a loader library (kotlinforforge) or a mod whose real
	// metadata sits in a nested jarjar (Sinytra Connector). Their manifest still names them, which
	// beats falling back to the file name.
	if man.Title() != "" {
		item.Source = "jar"
		item.Name = man.Title()
		item.Version = man.Version()
		return
	}

	// Nothing recognisable: keep the file listed under its own name rather than calling it broken.
	// Not being self-describing is normal for a handful of libraries, and a red "unreadable" would
	// suggest a failed download that the user ought to re-fetch.
}

// jarManifest holds the two manifest attributes worth showing in a listing.
type jarManifest struct {
	implTitle, specTitle     string
	implVersion, specVersion string
}

// Title prefers the implementation attributes, which describe the jar itself, over the
// specification ones, which describe the API it implements.
func (m jarManifest) Title() string {
	if m.implTitle != "" {
		return m.implTitle
	}
	return m.specTitle
}

// Version follows the same rule as Title.
func (m jarManifest) Version() string {
	if m.implVersion != "" {
		return m.implVersion
	}
	return m.specVersion
}

// nestedJarFiles returns the entries of the first bundled jar that describes itself as a mod, so
// that a wrapper jar can be shown by what it wraps. Forge's JarJar uses META-INF/jarjar, while
// Fabric's jar-in-jar and some Forge builds use META-INF/jars; both are accepted.
func nestedJarFiles(files []*zip.File) []*zip.File {
	for _, file := range files {
		name := strings.ToLower(file.Name)
		bundled := strings.HasPrefix(name, "meta-inf/jarjar/") || strings.HasPrefix(name, "meta-inf/jars/")
		if !bundled || !strings.HasSuffix(name, ".jar") {
			continue
		}
		data, err := readZipFile(file)
		if err != nil {
			continue
		}
		inner, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			continue
		}
		if hasModDescriptor(inner.File) {
			return inner.File
		}
	}
	return nil
}

// hasModDescriptor reports whether a jar carries any file that describes it as a mod.
func hasModDescriptor(files []*zip.File) bool {
	for _, name := range []string{
		"fabric.mod.json",
		"quilt.mod.json",
		"META-INF/neoforge.mods.toml",
		"META-INF/mods.toml",
		"mcmod.info",
	} {
		if zipEntry(files, name) != nil {
			return true
		}
	}
	return false
}

// readManifest reads META-INF/MANIFEST.MF, which every jar built by Gradle or the JDK tools has.
func readManifest(files []*zip.File) jarManifest {
	file := zipEntry(files, "META-INF/MANIFEST.MF")
	if file == nil {
		return jarManifest{}
	}
	data, err := readZipFile(file)
	if err != nil {
		return jarManifest{}
	}
	return parseManifest(data)
}

// parseManifest picks the handful of attributes used above out of a manifest. The format is one
// "Name: value" pair per line, with a leading space marking a continuation of the previous value.
func parseManifest(data []byte) jarManifest {
	var m jarManifest
	var module string

	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")

	key, value := "", ""
	save := func() {
		switch strings.ToLower(key) {
		case "implementation-title":
			m.implTitle = value
		case "specification-title":
			m.specTitle = value
		case "implementation-version":
			m.implVersion = value
		case "specification-version":
			m.specVersion = value
		case "automatic-module-name":
			module = value
		}
	}

	for _, line := range strings.Split(text, "\n") {
		switch {
		case line == "":
			save()
			key, value = "", ""
		case strings.HasPrefix(line, " "):
			value += line[1:]
		default:
			save()
			if k, v, ok := strings.Cut(line, ":"); ok {
				key, value = strings.TrimSpace(k), strings.TrimPrefix(v, " ")
			} else {
				key, value = "", ""
			}
		}
	}
	save()

	// A library jar often has no friendly title but still names itself through its automatic module;
	// the last segment of that name is what a user would recognise
	// ("thedarkcolour.kotlinforforge" -> "kotlinforforge").
	if m.Title() == "" && module != "" {
		if i := strings.LastIndex(module, "."); i >= 0 {
			module = module[i+1:]
		}
		m.specTitle = module
	}
	return m
}

// resolveJarVersion substitutes the version a build left unexpanded. Gradle's resource expansion
// replaces ${file.jarVersion} in mods.toml with the manifest version at build time, but a jar built
// without that step ships the placeholder verbatim. Forge substitutes the manifest value when it
// loads such a jar, and the listing should agree with what the game sees rather than print
// "${file.jarVersion}" to the user.
func resolveJarVersion(version string, man jarManifest) string {
	if !strings.Contains(version, "${") {
		return version
	}
	return man.Version()
}

// modMeta is the part of a mod's own metadata file that the launcher displays.
type modMeta struct {
	id          string
	name        string
	version     string
	description string
	authors     []string
	depends     map[string]string
}

// decodeFabricMeta reads fabric.mod.json.
//
// Fields are pulled out one at a time rather than decoded into one struct, because a single
// unexpected shape must not throw away everything else: "depends": {"minecraft": ["1.20", "1.20.1"]}
// is a list where the specification suggests a string, and decoding the file as one struct made such
// mods appear to have no metadata at all.
func decodeFabricMeta(data []byte) (modMeta, bool) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return modMeta{}, false
	}
	id := jsonText(raw["id"])
	if id == "" {
		return modMeta{}, false
	}
	return modMeta{
		id:          id,
		name:        jsonText(raw["name"]),
		version:     jsonText(raw["version"]),
		description: jsonText(raw["description"]),
		authors:     authorList(jsonValue(raw["authors"])),
		depends:     fabricDepends(raw["depends"]),
	}, true
}

// decodeQuiltMeta reads quilt.mod.json, with the same leniency as the fabric reader.
func decodeQuiltMeta(data []byte) (modMeta, bool) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return modMeta{}, false
	}
	var loader map[string]json.RawMessage
	if err := json.Unmarshal(raw["quilt_loader"], &loader); err != nil {
		return modMeta{}, false
	}
	id := jsonText(loader["id"])
	if id == "" {
		return modMeta{}, false
	}

	var metadata map[string]json.RawMessage
	_ = json.Unmarshal(loader["metadata"], &metadata)

	return modMeta{
		id:      id,
		name:    jsonText(metadata["name"]),
		version: jsonText(loader["version"]),
		// Quilt allows a description object as well as a plain string.
		description: packDescription(jsonValue(metadata["description"])),
		authors:     authorList(jsonValue(metadata["contributors"])),
	}, true
}

// jsonValue decodes a raw value of any shape, so that the helpers written for "any" can be reused.
// A missing or malformed value becomes nil, which those helpers already treat as empty.
func jsonValue(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil
	}
	return value
}

// jsonText renders a value that is expected to be a string. A number is accepted too, because a
// handful of mods write the version without quotes.
func jsonText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return strings.TrimSpace(text)
	}
	var number json.Number
	if err := json.Unmarshal(raw, &number); err == nil {
		return number.String()
	}
	return ""
}

// fabricDepends renders a fabric "depends" object, whose values are either a version range or a list
// of them.
func fabricDepends(raw json.RawMessage) map[string]string {
	if len(raw) == 0 {
		return nil
	}
	var entries map[string]json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil
	}
	out := make(map[string]string, len(entries))
	for id, value := range entries {
		if text := joinText(jsonValue(value)); text != "" {
			out[id] = text
		}
	}
	return out
}

// joinText renders a string, or a list of strings, as one comma-separated line.
func joinText(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case []any:
		parts := make([]string, 0, len(v))
		for _, entry := range v {
			if text := joinText(entry); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, ", ")
	}
	return ""
}

// forgeModsToml is the Forge and NeoForge mod descriptor.
//
// Several fields are free-form in practice (a single author or a list, a plain description or a
// block literal), so they are decoded as any and coerced afterwards.
type forgeModsToml struct {
	ModLoader     string `toml:"modLoader"`
	LoaderVersion string `toml:"loaderVersion"`
	License       string `toml:"license"`
	Mods          []struct {
		ModID       string `toml:"modId"`
		Version     string `toml:"version"`
		DisplayName string `toml:"displayName"`
		Authors     any    `toml:"authors"`
		Description string `toml:"description"`
	} `toml:"mods"`
	Dependencies map[string][]struct {
		ModID        string `toml:"modId"`
		Mandatory    bool   `toml:"mandatory"`
		VersionRange string `toml:"versionRange"`
		Side         string `toml:"side"`
	} `toml:"dependencies"`
}

// readForgeModsToml decodes a mods.toml, reporting whether it described a mod.
func readForgeModsToml(data []byte, item *ContentItem) bool {
	var parsed forgeModsToml
	if err := toml.Unmarshal(data, &parsed); err != nil || len(parsed.Mods) == 0 {
		return false
	}

	mod := parsed.Mods[0]
	item.ID, item.Version = mod.ModID, mod.Version
	item.Name = strings.TrimSpace(mod.DisplayName)
	item.Description = strings.TrimSpace(mod.Description)
	item.Authors = authorList(mod.Authors)

	if deps := parsed.Dependencies[mod.ModID]; len(deps) > 0 {
		item.Depends = make(map[string]string, len(deps))
		for _, dep := range deps {
			if dep.ModID != "" {
				item.Depends[dep.ModID] = dep.VersionRange
			}
		}
	}
	return true
}

// readMcmodInfo decodes the 1.12-era mcmod.info, which may be a bare list or wrapped in an object.
func readMcmodInfo(data []byte, item *ContentItem) bool {
	type legacyEntry struct {
		ModID       string   `json:"modid"`
		Name        string   `json:"name"`
		Version     string   `json:"version"`
		Description string   `json:"description"`
		AuthorList  []string `json:"authorList"`
		Authors     []string `json:"authors"`
	}

	var list []legacyEntry
	if err := json.Unmarshal(data, &list); err != nil {
		var wrapper struct {
			ModList []legacyEntry `json:"modList"`
		}
		if err := json.Unmarshal(data, &wrapper); err != nil {
			return false
		}
		list = wrapper.ModList
	}
	if len(list) == 0 {
		return false
	}

	entry := list[0]
	item.ID, item.Name, item.Version = entry.ModID, entry.Name, entry.Version
	item.Description = strings.TrimSpace(entry.Description)
	item.Authors = append(entry.AuthorList, entry.Authors...)
	return true
}

// packMcmeta is the descriptor shared by resource and data packs.
type packMcmeta struct {
	Pack struct {
		PackFormat  int `json:"pack_format"`
		Description any `json:"description"`
	} `json:"pack"`
}

// readPackMeta reads a resource or data pack descriptor.
func readPackMeta(files []*zip.File, item *ContentItem) {
	file := zipEntry(files, "pack.mcmeta")
	if file == nil {
		item.Err = fmt.Errorf("no pack.mcmeta")
		return
	}
	data, err := readZipFile(file)
	if err != nil {
		item.Err = err
		return
	}
	decodePackMeta(data, item)
}

// decodePackMeta decodes a pack.mcmeta into the item.
func decodePackMeta(data []byte, item *ContentItem) {
	var parsed packMcmeta
	if err := json.Unmarshal(data, &parsed); err != nil {
		item.Err = fmt.Errorf("decode pack.mcmeta: %w", err)
		return
	}
	item.PackFormat = parsed.Pack.PackFormat
	item.Description = packDescription(parsed.Pack.Description)
}

// packDescription renders a pack description, which since 1.20.3 may be a chat component rather
// than a plain string.
func packDescription(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case map[string]any:
		if text, ok := v["text"].(string); ok {
			return strings.TrimSpace(text)
		}
		if translate, ok := v["translate"].(string); ok {
			return strings.TrimSpace(translate)
		}
	case []any:
		parts := make([]string, 0, len(v))
		for _, entry := range v {
			if text := packDescription(entry); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, " ")
	}
	return ""
}

// readShaderMeta identifies a shader pack, whose metadata is the folder layout rather than a
// descriptor file. Iris and OptiFine both expect the shaders to sit in a "shaders" folder.
func readShaderMeta(files []*zip.File, item *ContentItem) {
	if !hasEntryPrefix(files, "shaders") {
		item.Err = fmt.Errorf("no shaders folder")
		return
	}
	item.Source = "shader"

	// OptiFine and Iris both read a properties file that may carry a display title.
	file := zipEntry(files, "shaders/shaders.properties")
	if file == nil {
		return
	}
	data, err := readZipFile(file)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "title", "name":
			if title := strings.TrimSpace(value); title != "" {
				item.Name = title
				return
			}
		}
	}
}

// authorList turns the several shapes mod metadata uses for authors into a flat list of names.
func authorList(value any) []string {
	switch v := value.(type) {
	case string:
		var out []string
		for _, part := range strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ';' }) {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(v))
		for _, entry := range v {
			out = append(out, authorList(entry)...)
		}
		return out
	case []string:
		return v
	case map[string]any:
		// fabric.mod.json allows {"name": "..."} entries, and quilt maps each name to a role.
		if name, ok := v["name"].(string); ok && name != "" {
			return []string{name}
		}
		names := make([]string, 0, len(v))
		for name := range v {
			names = append(names, name)
		}
		sort.Strings(names)
		return names
	}
	return nil
}

// SetContentEnabled switches a file on or off by renaming it.
func SetContentEnabled(item *ContentItem, enabled bool) error {
	base := stripDisabled(item.FileName)
	target := base
	if !enabled {
		target = base + DisabledSuffix
	}
	if target == item.FileName {
		item.Enabled = enabled
		return nil
	}

	dir := filepath.Dir(item.Path)
	path := filepath.Join(dir, target)
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists", target)
	}
	if err := os.Rename(item.Path, path); err != nil {
		return err
	}
	item.Path, item.FileName, item.Enabled = path, target, enabled
	return nil
}

// RemoveContent deletes the file a content item points at.
func RemoveContent(item ContentItem) error {
	if err := os.RemoveAll(item.Path); err != nil {
		return err
	}
	return nil
}

// baseFileName returns a file name without the disabled marker, that is the name it has while it
// is switched on.
func baseFileName(name string) string {
	if strings.HasSuffix(strings.ToLower(name), DisabledSuffix) {
		return name[:len(name)-len(DisabledSuffix)]
	}
	return name
}

// RenameContent renames a content item, keeping its disabled state.
//
// The extension decides which category a file belongs to, so a rename never changes it: a name
// typed without one gets the old extension appended, and a different extension is refused rather
// than making the file disappear from the listing.
func RenameContent(item *ContentItem, newName string) error {
	newName = strings.TrimSpace(newName)
	if newName == "" || strings.ContainsAny(newName, `/\`) || newName == "." || newName == ".." {
		return fmt.Errorf("invalid name %q", newName)
	}

	oldExt := strings.ToLower(filepath.Ext(baseFileName(item.FileName)))
	switch newExt := strings.ToLower(filepath.Ext(newName)); {
	case newExt == "":
		newName += oldExt
	case newExt != oldExt:
		return fmt.Errorf("cannot change the file extension from %q to %q", oldExt, newExt)
	}

	// Only the disabled marker is re-attached, never re-typed by the user.
	target := newName
	if !item.Enabled {
		target += DisabledSuffix
	}
	if target == item.FileName {
		return nil
	}

	path := filepath.Join(filepath.Dir(item.Path), target)
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists", target)
	}
	if err := os.Rename(item.Path, path); err != nil {
		return err
	}
	item.Path, item.FileName = path, target
	return nil
}

// An ImportOutcome reports what happened to one imported file.
type ImportOutcome struct {
	Name    string
	Skipped bool  // A file of that name was already there.
	Err     error // Set when the file could not be copied.
}

// ImportContent copies files, or every recognised file of a folder, into the content directory.
//
// Sources are expanded one level: a folder is not walked recursively, which keeps "import mods
// <downloads folder>" from pulling in an entire tree.
func ImportContent(dir string, kind ContentKind, sources []string) []ImportOutcome {
	var outcomes []ImportOutcome

	for _, source := range sources {
		info, err := os.Stat(source)
		if err != nil {
			outcomes = append(outcomes, ImportOutcome{Name: source, Err: err})
			continue
		}

		if !info.IsDir() {
			outcomes = append(outcomes, importFile(dir, kind, source))
			continue
		}

		entries, err := os.ReadDir(source)
		if err != nil {
			outcomes = append(outcomes, ImportOutcome{Name: source, Err: err})
			continue
		}
		for _, entry := range entries {
			if !isContentName(kind, entry.Name(), entry.IsDir()) {
				continue
			}
			outcomes = append(outcomes, importFile(dir, kind, filepath.Join(source, entry.Name())))
		}
	}

	return outcomes
}

// importFile copies one file into the content directory, reporting a clash instead of overwriting.
func importFile(dir string, kind ContentKind, source string) ImportOutcome {
	name := filepath.Base(source)
	if !isContentName(kind, name, false) {
		return ImportOutcome{Name: name, Err: fmt.Errorf("not a %s file", kind)}
	}

	target := filepath.Join(dir, name)
	if _, err := os.Stat(target); err == nil {
		return ImportOutcome{Name: name, Skipped: true}
	}

	if err := copyFile(source, target); err != nil {
		return ImportOutcome{Name: name, Err: err}
	}
	return ImportOutcome{Name: name}
}

// copyFile copies a file, creating the destination directory first.
func copyFile(source, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}

	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(target)
	if err != nil {
		return err
	}

	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(target)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(target)
		return err
	}
	return nil
}
