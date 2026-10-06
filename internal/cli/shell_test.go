package cli

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/fatih/color"
	"github.com/telecter/cmd-launcher/internal/cli/output"
	env "github.com/telecter/cmd-launcher/pkg"
	"github.com/telecter/cmd-launcher/pkg/auth"
	"github.com/telecter/cmd-launcher/pkg/launcher"
)

// TestPromptText checks that the prompt carries both the selected instance and the active account,
// in the "cmd-launcher [instance]_[account]> " form.
func TestPromptText(t *testing.T) {
	previous := auth.Store
	previousColor := color.NoColor
	t.Cleanup(func() {
		auth.Store = previous
		color.NoColor = previousColor
	})
	color.NoColor = true

	offline := func(username string) *auth.Account {
		return &auth.Account{Type: auth.AccountOffline, Username: username}
	}
	online := func(username string) *auth.Account {
		account := &auth.Account{}
		account.Minecraft.Username = username
		return account
	}

	inst := "1.20.1-Forge_47.4.16-LTSC"

	cases := []struct {
		name     string
		selected string
		accounts []*auth.Account
		current  int
		want     string
	}{
		{
			name:    "nothing selected and no account",
			current: -1,
			want:    "cmd-launcher> ",
		},
		{
			name:     "instance only",
			selected: inst,
			current:  -1,
			want:     "cmd-launcher [" + inst + "]_[-]> ",
		},
		{
			name:     "account only",
			accounts: []*auth.Account{offline("123456")},
			current:  0,
			want:     "cmd-launcher [-]_[123456]> ",
		},
		{
			name:     "instance and offline account",
			selected: inst,
			accounts: []*auth.Account{offline("123456")},
			current:  0,
			want:     "cmd-launcher [" + inst + "]_[123456]> ",
		},
		{
			name:     "instance and microsoft account",
			selected: inst,
			accounts: []*auth.Account{offline("123456"), online("Notch")},
			current:  1,
			want:     "cmd-launcher [" + inst + "]_[Notch]> ",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			auth.Store = auth.AuthStore{Accounts: c.accounts, Current: c.current}
			s := &shell{selected: c.selected}

			got := s.promptText()
			if got != c.want {
				t.Errorf("promptText() = %q, want %q", got, c.want)
			}
			if strings.Contains(got, "\x1b") {
				t.Errorf("promptText() = %q, want no colour codes", got)
			}
		})
	}
}

// TestPromptTextUnchangedWhenStoreMissing covers a store that was never loaded, where indexing the
// account list would panic if it were not guarded.
func TestPromptTextUnchangedWhenStoreMissing(t *testing.T) {
	previous := auth.Store
	t.Cleanup(func() { auth.Store = previous })
	auth.Store = auth.AuthStore{Current: -1}

	s := &shell{}
	if got := s.promptText(); got != "cmd-launcher> " {
		t.Errorf("promptText() = %q, want %q", got, "cmd-launcher> ")
	}
}

// TestHelpMatches covers the "help <command>" lookup. The prompt accepts spellings the listing does
// not use ("del mods" for "delete mods", "delete instance" for "delete version") and alternative
// verbs that the listing shows as aliases ("launch", "options", "?", "cls"), so every one of them
// has to find its row.
func TestHelpMatches(t *testing.T) {
	cases := []struct {
		query string
		want  string // syntax of the first match, "" when nothing should match
	}{
		{"list mods", "list mods|resourcepacks|shaderpacks|datapacks [<query>] [-w <world>]"},
		{"delete mods", "delete mods|resourcepacks|shaderpacks|datapacks [<name>] [-y] [-w <world>]"},
		{"del mods", "delete mods|resourcepacks|shaderpacks|datapacks [<name>] [-y] [-w <world>]"},
		{"del versions", "delete version [<name>] [-y]"},
		{"delete version", "delete version [<name>] [-y]"},
		{"delete instance", "delete version [<name>] [-y]"},
		{"del instance", "delete version [<name>] [-y]"},
		{"del users", "delete users [<name>]"},
		{"delete account", "delete users [<name>]"},
		{"launch", "start [<name>]"},
		{"options", "settings [<option>] [<value>]"},
		{"prefs", "settings [<option>] [<value>]"},
		{"create instance", "download version [<name>]"},
		{"cls", "clear"},
		{"?", "help [<command>]"},
		{"del modpacks", ""},
		{"nonsense", ""},
	}
	for _, c := range cases {
		matches := helpMatches(c.query)
		if c.want == "" {
			if len(matches) != 0 {
				t.Errorf("helpMatches(%q) = %d matches, want none", c.query, len(matches))
			}
			continue
		}
		if len(matches) == 0 {
			t.Errorf("helpMatches(%q) found nothing, want %q", c.query, c.want)
			continue
		}
		if !strings.HasPrefix(matches[0].syntax, c.want) {
			t.Errorf("helpMatches(%q) syntax = %q, want %q", c.query, matches[0].syntax, c.want)
		}
	}
}

// TestShellHelpEntries checks the listing itself: every row must describe a command that a user can
// look up, the description has to have a translation, and an alias may not shadow another command.
// This is what keeps the help in step with the shell as commands get added.
func TestShellHelpEntries(t *testing.T) {
	byCommand := make(map[string]shellHelpEntry)
	for _, entry := range shellHelp {
		if entry.command == "" || entry.syntax == "" {
			t.Errorf("incomplete help entry %+v", entry)
			continue
		}
		if previous, ok := byCommand[entry.command]; ok {
			t.Errorf("%q is listed twice (%q and %q)", entry.command, previous.syntax, entry.syntax)
		}
		byCommand[entry.command] = entry

		if got := output.Translate(entry.helpKey); got == entry.helpKey {
			t.Errorf("%q has no translation for %q", entry.command, entry.helpKey)
		}
		if !strings.HasPrefix(entry.syntax, entry.command) {
			t.Errorf("%q shows the unrelated syntax %q", entry.command, entry.syntax)
		}
	}

	for _, entry := range shellHelp {
		for _, alias := range entry.aliases {
			if _, ok := byCommand[alias]; ok {
				t.Errorf("alias %q of %q is also a listed command", alias, entry.command)
			}
		}
	}
}

// TestCdReportsInstanceCount checks that changing the game directory also says how many instances
// were found there: a mistyped path looks the same as a correct one otherwise.
func TestCdReportsInstanceCount(t *testing.T) {
	s, _ := testShell(t)

	target := t.TempDir()
	previousRoot := env.RootDir
	t.Cleanup(func() {
		// Put every derived directory back, not just the root, so that later tests see the same
		// state this one started from.
		if err := env.SetDirs(previousRoot); err != nil {
			t.Errorf("restoring %q: %v", previousRoot, err)
		}
	})
	// The count comes from reading the instances, so each one needs a configuration file: an empty
	// directory is not an instance.
	if err := env.SetDirs(target); err != nil {
		t.Fatalf("SetDirs: %v", err)
	}
	for _, name := range []string{"one", "two"} {
		inst := launcher.Instance{Name: name, Layout: launcher.LayoutInstances}
		if err := os.MkdirAll(inst.Dir(), 0755); err != nil {
			t.Fatal(err)
		}
		if err := inst.WriteConfig(); err != nil {
			t.Fatal(err)
		}
	}

	out := captureOutput(t, func() { s.cdCmd([]string{target}) })
	want := fmt.Sprintf(output.Translate("shell.instances"), 2)
	if !strings.Contains(out, want) {
		t.Errorf("cd did not report %q:/n%s", want, out)
	}

	empty := t.TempDir()
	out = captureOutput(t, func() { s.cdCmd([]string{empty}) })
	want = fmt.Sprintf(output.Translate("shell.instances"), 0)
	if !strings.Contains(out, want) {
		t.Errorf("cd into an empty directory did not report %q:/n%s", want, out)
	}
}
