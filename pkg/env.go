// Package env provides directories used by the launcher for various data.
package env

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// configDirName is the per-user directory name used by a normal (non-portable) installation.
const configDirName = "cmd-launcher"

// portableMarker is a file that, placed next to the launcher executable, enables portable mode
// without any command-line flag. Distributing "cmd-launcher.exe" together with this file is
// enough to get a self-contained launcher.
const portableMarker = "portable.txt"

// portableHomeEnv names a directory to use as the portable base, overriding the executable's
// own directory. Useful for shortcuts and scripts that keep the launcher data elsewhere.
const portableHomeEnv = "CMD_LAUNCHER_HOME"

var RootDir string // Base launcher directory. Defaults to "$HOME/.minecraft"

var LibrariesDir string // Java libraries directory

var InstancesDir string // Instances directory, using the launcher's own layout

var VersionsDir string // Versions directory, using the standard (Minecraft launcher / PCL) layout

var CachesDir string // Caches directory, e.g. version metadata, version manifest.

var AssetsDir string // Game assets directory and asset index

var TmpDir string // Directory for temporary files

var JavaDir string // Mojang Java installations

// ConfigDir is a per-user directory, independent of RootDir, holding the account store.
//
// Accounts are deliberately not scoped to a single game directory: changing the game directory
// should not log the user out.
var ConfigDir string

var AuthStorePath string // Path of the global authentication store

// LegacyAuthStorePath is where the account store lived before it became global. It is read once,
// to migrate existing installations.
var LegacyAuthStorePath string

// Portable reports whether the launcher runs in portable mode, keeping everything it writes next
// to the executable instead of in the user profile. That way the launcher folder can simply be
// copied to another computer.
var Portable bool

// SetDirs sets all directories to defaults from rootDir. These values can also be changed individually.
// However, they should not be changed between operations, as the launcher will not be able to find necessary files.
//
// The launcher's own directories are created here; use setPaths to assign them without touching the
// file system.
func SetDirs(rootDir string) error {
	setPaths(rootDir)
	return EnsureDirs()
}

// EnsureDirs creates the directories the launcher writes to, if missing.
func EnsureDirs() error {
	if err := os.MkdirAll(RootDir, 0755); err != nil {
		return fmt.Errorf("create root directory: %w", err)
	}
	if err := os.MkdirAll(ConfigDir, 0755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	return nil
}

// ExecutableDir returns the directory holding the running launcher executable, or an empty
// string if it cannot be determined.
func ExecutableDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return filepath.Dir(exe)
}

// PortableBase returns the base directory of the portable layout, and whether portable mode
// applies at all.
//
// Portable mode is used when the CMD_LAUNCHER_HOME environment variable names a directory, or
// when a "portable.txt" file sits next to the launcher executable.
func PortableBase() (string, bool) {
	if dir := strings.TrimSpace(os.Getenv(portableHomeEnv)); dir != "" {
		if abs, err := filepath.Abs(dir); err == nil {
			return abs, true
		}
		return dir, true
	}

	dir := ExecutableDir()
	if dir == "" {
		return "", false
	}
	if _, err := os.Stat(filepath.Join(dir, portableMarker)); err != nil {
		return "", false
	}
	return dir, true
}

// UsePortable switches the launcher to the portable layout rooted at base: game data is kept in
// base/minecraft, and launcher-wide data such as the account store in base/config.
func UsePortable(base string) {
	ConfigDir = filepath.Join(base, "config")
	setPaths(filepath.Join(base, "minecraft"))
	Portable = true
}

// UsePortableIfPresent enables the portable layout when PortableBase finds one. It reports
// whether the launcher ended up in portable mode.
func UsePortableIfPresent() bool {
	if Portable {
		return true
	}
	base, ok := PortableBase()
	if !ok {
		return false
	}
	UsePortable(base)
	return true
}

// MigrateUserConfig copies the account store of a previous, non-portable installation into the
// portable config directory. Missing source or existing destination are not errors, so calling
// it on every start is safe.
func MigrateUserConfig() error {
	if !Portable || ConfigDir == "" {
		return nil
	}
	dst := filepath.Join(ConfigDir, "accounts.json")
	if _, err := os.Stat(dst); err == nil {
		return nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(base, configDirName, "accounts.json"))
	if err != nil {
		return nil
	}
	if err := os.MkdirAll(ConfigDir, 0755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	return os.WriteFile(dst, data, 0600)
}

// setPaths assigns every directory derived from rootDir without creating anything.
func setPaths(rootDir string) {
	RootDir = rootDir
	InstancesDir = filepath.Join(RootDir, "instances")
	VersionsDir = filepath.Join(RootDir, "versions")
	LibrariesDir = filepath.Join(RootDir, "libraries")
	CachesDir = filepath.Join(RootDir, "caches")
	AssetsDir = filepath.Join(RootDir, "assets")
	TmpDir = filepath.Join(RootDir, "tmp")
	JavaDir = filepath.Join(RootDir, "java")
	LegacyAuthStorePath = filepath.Join(RootDir, "account.json")

	if ConfigDir == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			base = RootDir
		}
		ConfigDir = filepath.Join(base, configDirName)
	}
	AuthStorePath = filepath.Join(ConfigDir, "accounts.json")
}

func init() {
	home, _ := os.UserHomeDir()
	// Only the paths are computed here. Directories are created once the launcher actually needs
	// them, so that merely running a command like --help does not litter the disk.
	setPaths(filepath.Join(home, ".minecraft"))
}
