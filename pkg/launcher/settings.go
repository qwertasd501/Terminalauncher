package launcher

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
	env "github.com/qwertasd501/Terminalauncher/pkg"
)

// GlobalSettings are launcher-wide defaults that instances can follow.
type GlobalSettings struct {
	Language         string `toml:"language"          json:"language"          comment:"Language of the launcher output. Blank follows the system language. One of: en, de, zh, fr, ru, es."`
	Java             string `toml:"java"              json:"java"              comment:"Path to a Java executable. If blank, a Mojang-provided JVM will be downloaded."`
	JavaArgs         string `toml:"java_args"         json:"java_args"         comment:"Extra arguments to pass to the JVM"`
	MemoryMode       string `toml:"memory_mode"       json:"memory_mode"       comment:"auto or custom. Instances set to follow use this."`
	MinMemory        int    `toml:"min_memory"        json:"min_memory"        comment:"Minimum game memory, in MB"`
	MaxMemory        int    `toml:"max_memory"        json:"max_memory"        comment:"Maximum game memory, in MB"`
	DownloadThreads  int    `toml:"download_threads"  json:"download_threads"  comment:"How many files are downloaded at once. 0 keeps the built-in default."`
	WindowTitle      string `toml:"window_title"      json:"window_title"      comment:"Game window title"`
	CustomInfo       string `toml:"custom_info"       json:"custom_info"       comment:"Text shown in the corner of the game window"`
	VersionIsolation string `toml:"version_isolation" json:"version_isolation" comment:"on, off, or blank to detect per version"`
	LoginMode        string `toml:"login_mode"        json:"login_mode"        comment:"online or offline"`
	OfflineUsername  string `toml:"offline_username"  json:"offline_username"  comment:"Username used when launching in offline mode"`
	Width            int    `toml:"width"             json:"width"             comment:"Game window width"`
	Height           int    `toml:"height"            json:"height"            comment:"Game window height"`
}

// DefaultGlobalSettings returns the settings used when no global settings file exists.
func DefaultGlobalSettings() GlobalSettings {
	return GlobalSettings{
		MemoryMode: "auto",
		MinMemory:  512,
		MaxMemory:  4096,
		LoginMode:  "online",
		Width:      1708,
		Height:     960,
	}
}

// GlobalSettingsPath returns the path of the global settings file.
func GlobalSettingsPath() string {
	return filepath.Join(env.RootDir, "launcher.toml")
}

// LoadGlobalSettings reads the global settings, falling back to the defaults.
//
// A missing file is not an error: the defaults are returned instead, so a fresh game directory
// works without any setup.
func LoadGlobalSettings() (GlobalSettings, error) {
	settings := DefaultGlobalSettings()

	data, err := os.ReadFile(GlobalSettingsPath())
	if errors.Is(err, os.ErrNotExist) {
		return settings, nil
	}
	if err != nil {
		return settings, fmt.Errorf("read global settings: %w", err)
	}
	if err := toml.Unmarshal(data, &settings); err != nil {
		return DefaultGlobalSettings(), fmt.Errorf("parse global settings: %w", err)
	}
	return settings, nil
}

// Save writes the global settings to the game directory.
func (settings GlobalSettings) Save() error {
	data, err := toml.Marshal(settings)
	if err != nil {
		return fmt.Errorf("encode global settings: %w", err)
	}
	if err := os.MkdirAll(env.RootDir, 0755); err != nil {
		return fmt.Errorf("create game directory: %w", err)
	}
	return os.WriteFile(GlobalSettingsPath(), data, 0644)
}
