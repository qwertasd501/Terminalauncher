package cli

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fatih/color"
	"github.com/telecter/cmd-launcher/internal/cli/output"
	"github.com/telecter/cmd-launcher/internal/network"
	env "github.com/telecter/cmd-launcher/pkg"
	"github.com/telecter/cmd-launcher/pkg/launcher"
)

// testShell returns a shell whose input is already exhausted, so commands that ask a question
// return instead of blocking.
func testShell(t *testing.T) (*shell, string) {
	t.Helper()

	root := t.TempDir()
	previousRoot := env.RootDir
	previousColor := color.NoColor
	t.Cleanup(func() {
		env.RootDir = previousRoot
		color.NoColor = previousColor
		output.SetLangCode("")
	})
	if err := env.SetDirs(root); err != nil {
		t.Fatalf("SetDirs(%q): %v", root, err)
	}
	color.NoColor = true

	s := &shell{in: bufio.NewScanner(strings.NewReader(""))}
	return s, root
}

// TestLanguageCode checks the spellings accepted by "settings language", including the words that
// mean "follow the system language".
func TestLanguageCode(t *testing.T) {
	cases := map[string]string{
		"system":  "",
		"auto":    "",
		"follow":  "",
		"en":      "en",
		"EN":      "en",
		"English": "en",
		"de":      "de",
		"deutsch": "de",
		"zh":      "zh",
		"简体中文":    "zh",
		"klingon": "",
		"":        "",
	}
	valid := map[string]bool{"": true, "en": true, "de": true, "zh": true}

	for word, want := range cases {
		got, ok := languageCode(word)
		if word == "klingon" {
			if ok {
				t.Errorf("languageCode(%q) accepted an unknown language", word)
			}
			continue
		}
		if !ok || got != want {
			t.Errorf("languageCode(%q) = %q, %v; want %q, true", word, got, ok, want)
		}
		if !valid[got] {
			t.Errorf("languageCode(%q) returned %q, which is not a known code", word, got)
		}
	}
}

// TestFindGlobalSetting checks that the names a user is likely to type all resolve.
func TestFindGlobalSetting(t *testing.T) {
	cases := map[string]string{
		"language":          "language",
		"lang":              "language",
		"java":              "java",
		"jvm":               "java",
		"memory":            "memory",
		"memory-min":        "memory-min",
		"min-memory":        "memory-min",
		"max-memory":        "memory-max",
		"download-threads":  "download-threads",
		"threads":           "download-threads",
		"window-title":      "window-title",
		"title":             "window-title",
		"version-isolation": "version-isolation",
		"isolation":         "version-isolation",
		"login":             "login",
		"offline-user":      "offline-user",
		"resolution":        "resolution",
	}
	for word, want := range cases {
		entry, ok := findGlobalSetting(word)
		if !ok {
			t.Errorf("findGlobalSetting(%q) found nothing", word)
			continue
		}
		if entry.key != want {
			t.Errorf("findGlobalSetting(%q) = %q, want %q", word, entry.key, want)
		}
	}
	if _, ok := findGlobalSetting("nonsense"); ok {
		t.Error("findGlobalSetting accepted an unknown option")
	}
}

// TestSettingsLanguageWritesTheFile checks that "settings language <code>" stores the code in the
// global settings, and that the change takes effect immediately.
func TestSettingsLanguageWritesTheFile(t *testing.T) {
	s, root := testShell(t)

	s.settingsCmd([]string{"language", "zh"})

	settings, err := launcher.LoadGlobalSettings()
	if err != nil {
		t.Fatalf("LoadGlobalSettings: %v", err)
	}
	if settings.Language != "zh" {
		t.Errorf("stored language is %q, want %q", settings.Language, "zh")
	}
	if output.CurrentLanguage() != "zh" {
		t.Errorf("the language is %q after the change, want %q", output.CurrentLanguage(), "zh")
	}

	// The file must keep the rest of the settings, and stay parseable.
	data, err := os.ReadFile(filepath.Join(root, "launcher.toml"))
	if err != nil {
		t.Fatalf("read launcher.toml: %v", err)
	}
	if !strings.Contains(string(data), "language = 'zh'") {
		t.Errorf("launcher.toml does not store the language:\n%s", data)
	}
	if settings.MaxMemory != launcher.DefaultGlobalSettings().MaxMemory {
		t.Errorf("the remaining settings were lost: max_memory is %d", settings.MaxMemory)
	}
}

// TestSettingsDownloadThreads checks that the download thread count is stored, applied immediately,
// and that zero means "keep the built-in default".
func TestSettingsDownloadThreads(t *testing.T) {
	previous := network.MaxConcurrentDownloads()
	t.Cleanup(func() { network.SetMaxConcurrentDownloads(previous) })

	s, _ := testShell(t)

	s.settingsCmd([]string{"download-threads", "5"})
	settings, err := launcher.LoadGlobalSettings()
	if err != nil {
		t.Fatalf("LoadGlobalSettings: %v", err)
	}
	if settings.DownloadThreads != 5 {
		t.Errorf("stored download_threads is %d, want 5", settings.DownloadThreads)
	}
	if got := network.MaxConcurrentDownloads(); got != 5 {
		t.Errorf("the change did not take effect immediately: %d threads", got)
	}

	// "threads" is the short spelling.
	s.settingsCmd([]string{"threads", "auto"})
	if settings, err = launcher.LoadGlobalSettings(); err != nil {
		t.Fatalf("LoadGlobalSettings: %v", err)
	}
	if settings.DownloadThreads != 0 {
		t.Errorf("stored download_threads is %d, want 0", settings.DownloadThreads)
	}
	if got := network.MaxConcurrentDownloads(); got != network.DefaultMaxConcurrentDownloads {
		t.Errorf("after clearing the setting there are %d threads, want the default %d", got, network.DefaultMaxConcurrentDownloads)
	}

	// A negative count is refused and leaves the stored value alone.
	s.settingsCmd([]string{"download-threads", "-3"})
	if settings, err = launcher.LoadGlobalSettings(); err != nil {
		t.Fatalf("LoadGlobalSettings: %v", err)
	}
	if settings.DownloadThreads != 0 {
		t.Errorf("a rejected value changed download_threads to %d", settings.DownloadThreads)
	}
}

// TestSettingsReadsAboutOption checks that a known option is written, and that an unknown one and a
// bad value are refused without touching the file.
func TestSettingsReadsAboutOption(t *testing.T) {
	s, root := testShell(t)

	s.settingsCmd([]string{"memory-max", "8192"})
	settings, err := launcher.LoadGlobalSettings()
	if err != nil {
		t.Fatalf("LoadGlobalSettings: %v", err)
	}
	if settings.MaxMemory != 8192 {
		t.Errorf("max_memory is %d, want 8192", settings.MaxMemory)
	}

	s.settingsCmd([]string{"memory-max", "lots"})
	if settings, err = launcher.LoadGlobalSettings(); err != nil {
		t.Fatalf("LoadGlobalSettings: %v", err)
	}
	if settings.MaxMemory != 8192 {
		t.Errorf("a rejected value changed max_memory to %d", settings.MaxMemory)
	}

	s.settingsCmd([]string{"nonsense", "1"})
	if _, err := os.Stat(filepath.Join(root, "launcher.toml")); err != nil {
		t.Errorf("launcher.toml disappeared: %v", err)
	}
}
