package launcher

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/qwertasd501/Terminalauncher/internal/meta"
	env "github.com/qwertasd501/Terminalauncher/pkg"
)

// A Layout says where an instance lives.
type Layout string

const (
	// LayoutInstances is the launcher's own layout: <game directory>/instances/<name>.
	LayoutInstances Layout = "instances"
	// LayoutVersions is the standard Minecraft launcher layout: <game directory>/versions/<name>,
	// which is also what PCL and other launchers use.
	LayoutVersions Layout = "versions"
)

// An Instance represents a full installation of Minecraft and its information.
type Instance struct {
	Name          string         `toml:"-" json:"-"`
	GameVersion   string         `toml:"game_version" json:"game_version"`
	Loader        meta.Loader    `toml:"mod_loader" json:"mod_loader"`
	LoaderVersion string         `toml:"mod_loader_version,omitempty" json:"mod_loader_version,omitempty"`
	Config        InstanceConfig `toml:"config" json:"config"`

	// Layout is derived from where the instance was found, and is never written to disk.
	Layout Layout `toml:"-" json:"-"`
}

// A Resolution is the size of the game window.
type Resolution struct {
	Width  int `toml:"width" json:"width"`
	Height int `toml:"height" json:"height"`
}

// InstanceConfig represents the configurable values of an Instance.
//
// Values that can be left to the launcher-wide defaults are stored empty and filled in by Resolve.
type InstanceConfig struct {
	WindowResolution Resolution `toml:"resolution" json:"resolution" comment:"Game window resolution"`
	Java             string     `toml:"java" json:"java" comment:"Path to a Java executable. If blank, a Mojang-provided JVM will be downloaded."`
	JavaArgs         string     `toml:"java_args" json:"java_args" comment:"Extra arguments to pass to the JVM"`
	CustomJar        string     `toml:"custom_jar" json:"custom_jar" comment:"Path to a custom JAR to use instead of the normal Minecraft client"`

	// MemoryMode decides which memory values are used: "follow" uses the global settings,
	// "custom" uses MinMemory and MaxMemory, and "auto" leaves both unset so the JVM decides.
	MemoryMode string `toml:"memory_mode" json:"memory_mode" comment:"follow, auto or custom"`
	MinMemory  int    `toml:"min_memory" json:"min_memory" comment:"Minimum game memory, in MB"`
	MaxMemory  int    `toml:"max_memory" json:"max_memory" comment:"Maximum game memory, in MB"`

	// The settings below mirror a PCL version setup page.
	WindowTitle      string `toml:"window_title" json:"window_title" comment:"Game window title. Blank to follow the global setting."`
	CustomInfo       string `toml:"custom_info" json:"custom_info" comment:"Text shown in the corner of the game window. Blank to follow the global setting."`
	VersionIsolation string `toml:"version_isolation" json:"version_isolation" comment:"follow, on or off. When on, the version directory is used as the game directory."`
	AutoJoinServer   string `toml:"auto_join_server" json:"auto_join_server" comment:"Server joined automatically on start"`
	LoginMode        string `toml:"login_mode" json:"login_mode" comment:"online or offline. Blank to follow the global setting."`
	OfflineUsername  string `toml:"offline_username" json:"offline_username" comment:"Username used when launching in offline mode"`
	Icon             string `toml:"icon" json:"icon" comment:"Path to an icon shown in launcher listings"`
	Category         string `toml:"category" json:"category" comment:"Free-form group the instance belongs to"`
	Favorite         bool   `toml:"favorite" json:"favorite" comment:"Whether the instance is marked as a favourite"`
	Description      string `toml:"description" json:"description" comment:"Free-form description of the instance"`
}

// DefaultInstanceConfig returns the configuration a new instance starts with.
func DefaultInstanceConfig() InstanceConfig {
	return InstanceConfig{
		WindowResolution: Resolution{Width: 1708, Height: 960},
		MemoryMode:       "follow",
		MinMemory:        512,
		MaxMemory:        4096,
		VersionIsolation: "follow",
	}
}

// isolationMarkers are the entries whose presence in a version directory means the game data lives
// there rather than in the game directory root.
var isolationMarkers = []string{"saves", "mods", "config", "options.txt", "resourcepacks", "shaderpacks"}

// detectIsolation reports whether a version directory looks like it holds its own game data.
func detectIsolation(dir string) bool {
	for _, marker := range isolationMarkers {
		if _, err := os.Stat(filepath.Join(dir, marker)); err == nil {
			return true
		}
	}
	return false
}

// WriteConfig writes the instances configuration to its configuration file.
//
// The Name and Layout fields are ignored, as they are based on the instance's directory.
func (inst Instance) WriteConfig() error {
	data, err := toml.Marshal(inst)
	if err != nil {
		return fmt.Errorf("encode instance configuration: %w", err)
	}
	if err := os.MkdirAll(inst.Dir(), 0755); err != nil {
		return fmt.Errorf("create instance directory: %w", err)
	}
	return os.WriteFile(inst.ConfigPath(), data, 0644)
}

// dirFor returns the directory an instance of the given layout and name lives in.
func dirFor(layout Layout, name string) string {
	if layout == LayoutVersions {
		return filepath.Join(env.VersionsDir, name)
	}
	return filepath.Join(env.InstancesDir, name)
}

// Dir returns the directory the instance lives in.
func (inst Instance) Dir() string {
	return dirFor(inst.Layout, inst.Name)
}

// ConfigPath returns the path of the instance's configuration file.
func (inst Instance) ConfigPath() string {
	return filepath.Join(inst.Dir(), "instance.toml")
}

// Isolated reports whether the version directory is used as the game directory.
//
// Instances using the launcher's own layout are always isolated, so this only matters for
// instances found in the standard version directory.
func (inst Instance) Isolated() bool {
	if inst.Layout != LayoutVersions {
		return true
	}
	switch inst.Config.VersionIsolation {
	case "on":
		return true
	case "off":
		return false
	}

	if global, err := LoadGlobalSettings(); err == nil {
		switch global.VersionIsolation {
		case "on":
			return true
		case "off":
			return false
		}
	}
	return detectIsolation(inst.Dir())
}

// GameDir returns the directory the game runs in.
func (inst Instance) GameDir() string {
	if inst.Isolated() {
		return inst.Dir()
	}
	return env.RootDir
}

// NativesDir returns the path to the instance's natives extraction directory.
func (inst Instance) NativesDir() string {
	return filepath.Join(inst.Dir(), "natives")
}

// Resolve returns a copy of the configuration with every value set to follow the global settings
// replaced by the global value.
func (config InstanceConfig) Resolve() InstanceConfig {
	global, err := LoadGlobalSettings()
	if err != nil {
		global = DefaultGlobalSettings()
	}

	if config.Java == "" {
		config.Java = global.Java
	}
	if config.JavaArgs == "" {
		config.JavaArgs = global.JavaArgs
	}
	if config.WindowTitle == "" {
		config.WindowTitle = global.WindowTitle
	}
	if config.CustomInfo == "" {
		config.CustomInfo = global.CustomInfo
	}
	if config.LoginMode == "" {
		config.LoginMode = global.LoginMode
	}
	if config.OfflineUsername == "" {
		config.OfflineUsername = global.OfflineUsername
	}
	if config.VersionIsolation == "" {
		config.VersionIsolation = "follow"
	}
	if config.WindowResolution.Width == 0 {
		config.WindowResolution.Width = global.Width
	}
	if config.WindowResolution.Height == 0 {
		config.WindowResolution.Height = global.Height
	}

	switch config.MemoryMode {
	case "custom":
		// The instance's own values are used as they are.
	case "auto":
		config.MinMemory, config.MaxMemory = 0, 0
	default:
		config.MemoryMode = "follow"
		config.MinMemory, config.MaxMemory = global.MinMemory, global.MaxMemory
	}
	return config
}

// Rename renames instance to the specified new name
func (inst *Instance) Rename(new string) error {
	if err := os.Rename(inst.Dir(), dirFor(inst.Layout, new)); err != nil {
		return err
	}
	inst.Name = new
	return nil
}

// InstanceOptions are options used to designate an instance's version and other parameters on creation.
type InstanceOptions struct {
	Name          string
	GameVersion   string
	Loader        meta.Loader
	LoaderVersion string

	// Suffix is appended to the name that CreateInstance builds itself, the way PCL versions often
	// end in something like "-LTSC". It is ignored when Name is set.
	Suffix string

	Config InstanceConfig
}

// DefaultInstanceName builds the name to use when the caller does not give one itself:
//
//	<game version>-<Loader>_<loader version>[-<suffix>]
//
// for example "1.20.1-Forge_47.4.6-LTSC". Vanilla has no loader to name, so it comes out as
// "<game version>[-<suffix>]" instead, and a loader version that is not a concrete version ("latest",
// or nothing at all) is left out rather than written into the name.
func DefaultInstanceName(gameVersion string, loader meta.Loader, loaderVersion, suffix string) string {
	name := strings.TrimSpace(gameVersion)
	if name == "" {
		// Without a game version there is nothing worth naming, so the caller decides what to do.
		return ""
	}
	if loader != "" && loader != meta.LoaderVanilla {
		segment := loader.Display()
		switch loaderVersion = strings.TrimSpace(loaderVersion); loaderVersion {
		case "", "latest":
		default:
			// Forge repeats the game version inside its own ("26.3-66.0.9"), which would stutter in
			// the middle of the name, so it is dropped where it repeats what is already there.
			segment += "_" + strings.TrimPrefix(loaderVersion, name+"-")
		}
		if segment != "" {
			name += "-" + segment
		}
	}
	if suffix = strings.TrimSpace(suffix); suffix != "" {
		name += "-" + suffix
	}
	return name
}

// CreateInstance creates a new instance with the specified options.
//
// The options may leave the name empty, in which case the instance is named after what it turns out
// to be (see DefaultInstanceName) using the versions the metadata resolves to, so that aliases such
// as "release" and "latest" still produce a name like "1.21.5-Forge_52.0.2".
func CreateInstance(options InstanceOptions) (Instance, error) {
	if options.Name != "" && DoesInstanceExist(options.Name) {
		return Instance{}, fmt.Errorf("instance already exists")
	}

	version, err := meta.FetchAllVersionMeta(options.Loader, options.GameVersion, options.LoaderVersion)
	if err != nil {
		return Instance{}, err
	}

	// Without a name of its own the instance is named after the versions the metadata resolved.
	name := options.Name
	if name == "" {
		name = DefaultInstanceName(version.ID, options.Loader, version.LoaderID, options.Suffix)
		if name == "" {
			return Instance{}, fmt.Errorf("invalid instance name")
		}
		if DoesInstanceExist(name) {
			return Instance{}, fmt.Errorf("instance already exists")
		}
	}

	// A caller that does not care about the settings gets the same starting point as one that does,
	// so that a new instance never ends up with a zero window size or memory limit.
	config := options.Config
	if config == (InstanceConfig{}) {
		config = DefaultInstanceConfig()
	}

	inst := Instance{
		Name:          name,
		GameVersion:   version.ID,
		Loader:        options.Loader,
		LoaderVersion: version.LoaderID,
		Config:        config,
		Layout:        LayoutInstances,
	}

	if err := os.MkdirAll(inst.Dir(), 0755); err != nil {
		return Instance{}, fmt.Errorf("create instance directory: %w", err)
	}

	if err := inst.WriteConfig(); err != nil {
		return Instance{}, fmt.Errorf("write instance configuration: %w", err)
	}

	return inst, nil
}

// RemoveInstance removes the instance with the specified name.
func RemoveInstance(name string) error {
	inst, err := FetchInstance(name)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(inst.Dir()); err != nil {
		return fmt.Errorf("remove instance directory: %w", err)
	}
	return nil
}

// A configFormat is the on-disk encoding of an instance configuration.
type configFormat int

const (
	formatTOML configFormat = iota
	formatJSON
)

// readConfig reads an instance configuration from dir, reporting how it was encoded.
func readConfig(dir string) ([]byte, configFormat, error) {
	data, err := os.ReadFile(filepath.Join(dir, "instance.toml"))
	if err == nil {
		return data, formatTOML, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, 0, fmt.Errorf("read instance configuration: %w", err)
	}

	data, err = os.ReadFile(filepath.Join(dir, "instance.json"))
	if err == nil {
		return data, formatJSON, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil, 0, fmt.Errorf("instance configuration missing")
	}
	return nil, 0, fmt.Errorf("read instance configuration (JSON): %w", err)
}

// A localVersionJSON is the subset of a standard version JSON needed to describe an instance.
type localVersionJSON struct {
	ID string `json:"id"`
	// InheritsFrom is set by the official launcher and by Forge installers.
	InheritsFrom string `json:"inheritsFrom"`
	// ClientVersion is what PCL writes to record the base game version, because it rewrites ID to the
	// name of the version directory.
	ClientVersion string `json:"clientVersion"`
	MainClass     string `json:"mainClass"`
	Arguments     struct {
		Game []any `json:"game"`
	} `json:"arguments"`
	Libraries []struct {
		Name string `json:"name"`
	} `json:"libraries"`
}

// gameArgs indexes the string entries of the game arguments that appear as "--name value" pairs.
func (version localVersionJSON) gameArgs() map[string]string {
	args := make([]string, 0, len(version.Arguments.Game))
	for _, arg := range version.Arguments.Game {
		if value, ok := arg.(string); ok {
			args = append(args, value)
		}
	}

	values := make(map[string]string)
	for i := 0; i+1 < len(args); i++ {
		if strings.HasPrefix(args[i], "--") {
			values[strings.TrimPrefix(args[i], "--")] = args[i+1]
		}
	}
	return values
}

// looksLikeGameVersion reports whether s is a bare game version such as "1.20.1".
func looksLikeGameVersion(s string) bool {
	if s == "" || !strings.Contains(s, ".") {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && r != '.' {
			return false
		}
	}
	return true
}

// describeLocalVersion derives the game version and mod loader of a standard version directory.
func describeLocalVersion(version localVersionJSON) (string, meta.Loader, string) {
	args := version.gameArgs()

	gameVersion := args["fml.mcVersion"]
	if gameVersion == "" {
		gameVersion = version.ClientVersion
	}
	if gameVersion == "" {
		gameVersion = version.InheritsFrom
	}
	if gameVersion == "" && looksLikeGameVersion(version.ID) {
		gameVersion = version.ID
	}

	// A mod loader's own version often carries the game version as a prefix, e.g. 1.20.1-47.4.16.
	stripGame := func(s string) string {
		if gameVersion == "" {
			return s
		}
		return strings.TrimPrefix(s, gameVersion+"-")
	}

	if loaderVersion := args["fml.neoForgeVersion"]; loaderVersion != "" {
		return gameVersion, meta.LoaderNeoForge, stripGame(loaderVersion)
	}
	if loaderVersion := args["fml.forgeVersion"]; loaderVersion != "" {
		return gameVersion, meta.LoaderForge, stripGame(loaderVersion)
	}

	for _, library := range version.Libraries {
		group, rest, ok := strings.Cut(library.Name, ":")
		if !ok {
			continue
		}
		artifact, rest, ok := strings.Cut(rest, ":")
		if !ok {
			continue
		}
		loaderVersion, _, _ := strings.Cut(rest, ":")

		switch group + ":" + artifact {
		case "net.minecraftforge:forge", "net.minecraftforge:fmlloader":
			return gameVersion, meta.LoaderForge, stripGame(loaderVersion)
		case "net.neoforged:neoforge", "net.neoforged:forge":
			return gameVersion, meta.LoaderNeoForge, stripGame(loaderVersion)
		case "net.fabricmc:fabric-loader":
			return gameVersion, meta.LoaderFabric, loaderVersion
		case "org.quiltmc:quilt-loader":
			return gameVersion, meta.LoaderQuilt, loaderVersion
		}
	}

	// Nothing conclusive in the libraries: fall back to the main class.
	switch {
	case strings.Contains(version.MainClass, "bootstraplauncher"):
		return gameVersion, meta.LoaderForge, ""
	case strings.Contains(version.MainClass, "fabricmc"):
		return gameVersion, meta.LoaderFabric, ""
	case strings.Contains(version.MainClass, "quiltmc"):
		return gameVersion, meta.LoaderQuilt, ""
	}
	return gameVersion, meta.LoaderVanilla, ""
}

// inferInstance describes a standard version directory that carries no configuration of its own.
func inferInstance(inst Instance) (Instance, error) {
	data, err := os.ReadFile(filepath.Join(inst.Dir(), inst.Name+".json"))
	if err != nil {
		return Instance{}, fmt.Errorf("version metadata missing")
	}
	// Windows tools commonly prefix JSON files with a byte order mark.
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))

	var version localVersionJSON
	if err := json.Unmarshal(data, &version); err != nil {
		return Instance{}, fmt.Errorf("parse version metadata: %w", err)
	}

	gameVersion, loader, loaderVersion := describeLocalVersion(version)
	if gameVersion == "" {
		gameVersion = inst.Name
	}

	inst.GameVersion = gameVersion
	inst.Loader = loader
	inst.LoaderVersion = loaderVersion
	inst.Config = DefaultInstanceConfig()
	return inst, nil
}

// fetchInstanceIn loads the instance of the given name from one layout.
func fetchInstanceIn(layout Layout, name string) (Instance, error) {
	if name == "" {
		return Instance{}, fmt.Errorf("invalid instance name")
	}

	inst := Instance{Name: name, Layout: layout}
	info, err := os.Stat(inst.Dir())
	if err != nil || !info.IsDir() {
		return Instance{}, fmt.Errorf("instance does not exist")
	}

	data, format, err := readConfig(inst.Dir())
	if err != nil {
		if layout == LayoutVersions {
			return inferInstance(inst)
		}
		return Instance{}, err
	}

	var parsed Instance
	if format == formatJSON {
		err = json.Unmarshal(data, &parsed)
	} else {
		err = toml.Unmarshal(data, &parsed)
	}
	if err != nil {
		return Instance{}, fmt.Errorf("parse instance configuration: %w", err)
	}

	parsed.Name = name
	parsed.Layout = layout

	// Migrate a legacy JSON configuration. A TOML file is otherwise left untouched, so that merely
	// listing instances never writes into an existing game directory.
	if format == formatJSON {
		if err := parsed.WriteConfig(); err != nil {
			return Instance{}, fmt.Errorf("migrate instance configuration: %w", err)
		}
	}
	return parsed, nil
}

// FetchInstance retrieves the instance with the specified name.
//
// The launcher's own layout takes precedence when both layouts hold an instance of that name.
func FetchInstance(name string) (Instance, error) {
	if name == "" {
		return Instance{}, fmt.Errorf("invalid instance name")
	}
	for _, layout := range []Layout{LayoutInstances, LayoutVersions} {
		inst, err := fetchInstanceIn(layout, name)
		if err == nil {
			return inst, nil
		}
	}
	return Instance{}, fmt.Errorf("instance does not exist")
}

// FetchAllInstances retrieves every valid instance, from both the instance and version directories.
func FetchAllInstances() ([]Instance, error) {
	var insts []Instance
	seen := make(map[string]bool)

	for _, layout := range []Layout{LayoutInstances, LayoutVersions} {
		entries, err := os.ReadDir(dirFor(layout, ""))
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() || seen[entry.Name()] {
				continue
			}
			inst, err := fetchInstanceIn(layout, entry.Name())
			if err != nil {
				continue
			}
			seen[entry.Name()] = true
			insts = append(insts, inst)
		}
	}

	sort.Slice(insts, func(i, j int) bool { return insts[i].Name < insts[j].Name })
	return insts, nil
}

// DoesInstanceExist reports whether an instance with the specified name exists.
func DoesInstanceExist(name string) bool {
	if name == "" {
		return false
	}
	for _, layout := range []Layout{LayoutInstances, LayoutVersions} {
		dir := dirFor(layout, name)
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			continue
		}
		// A version directory must carry its version metadata to count as an instance.
		if layout == LayoutVersions {
			if _, err := os.Stat(filepath.Join(dir, name+".json")); err != nil {
				continue
			}
		}
		return true
	}
	return false
}
