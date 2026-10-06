package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fatih/color"
	"github.com/qwertasd501/Terminalauncher/internal/cli/output"
)

// captureOutput collects what the launcher prints while fn runs. The log lines go through the
// colour package for their prefix and standard output for their body, so both are redirected.
func captureOutput(t *testing.T, fn func()) string {
	t.Helper()

	previousOutput, previousStdout := color.Output, os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	color.Output, os.Stdout = writer, writer
	t.Cleanup(func() { color.Output, os.Stdout = previousOutput, previousStdout })

	// Read while fn runs: the pipe holds only a small buffer, so everything else would block.
	done := make(chan string, 1)
	go func() {
		body, _ := io.ReadAll(reader)
		done <- string(body)
	}()

	fn()
	writer.Close()
	out := <-done
	reader.Close()
	return out
}

// TestDownloadVersionNamesItself covers the command line: a version created without a name is named
// after the game version and the loader it is created with, and the name that comes out of it is
// refused when it is already taken. The refusal happens before anything is created, so the test
// never downloads an instance (the version list the menus come from is still fetched and cached).
func TestDownloadVersionNamesItself(t *testing.T) {
	s, root := testShell(t)
	// The assertions below match the English wording, and "current" is what the stored setting says
	// (empty means "follow the system language").
	previousLang := output.CurrentLanguage()
	output.SetLangCode("en")
	t.Cleanup(func() { output.SetLangCode(previousLang) })

	for _, taken := range []string{
		"1.20.1-Forge_47.4.6",      // no extra part
		"1.20.1-Forge_47.4.6-LTSC", // "-x LTSC"
		"1.21.5-LTSC",              // vanilla carries no loader segment
		"my-own-name",              // a name that was given, and used as it is
	} {
		if err := os.MkdirAll(filepath.Join(root, "instances", taken), 0755); err != nil {
			t.Fatal(err)
		}
	}

	cases := []struct {
		name string
		args []string
		want string
	}{
		{
			"the loader is part of the name",
			[]string{"version", "-v", "1.20.1", "-l", "forge", "--loader-version", "47.4.6"},
			"1.20.1-Forge_47.4.6",
		},
		{
			"the extra part is appended",
			[]string{"version", "-v", "1.20.1", "-l", "forge", "--loader-version", "47.4.6", "-x", "LTSC"},
			"1.20.1-Forge_47.4.6-LTSC",
		},
		{
			"vanilla has no loader to name",
			[]string{"version", "-v", "1.21.5", "-l", "vanilla", "-x", "LTSC"},
			"1.21.5-LTSC",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := captureOutput(t, func() { s.createCmd(c.args) })
			want := fmt.Sprintf(output.Translate("shell.dl.autoname"), c.want)
			if !strings.Contains(out, want) {
				t.Errorf("the launcher did not report naming it %q:\n%s", c.want, out)
			}
			if !strings.Contains(out, c.want) {
				t.Errorf("the name %q is missing from the output:\n%s", c.want, out)
			}
		})
	}

	// A name that was given is used as it stands: nothing is built from the version.
	t.Run("a given name is kept", func(t *testing.T) {
		out := captureOutput(t, func() {
			s.createCmd([]string{"version", "my-own-name", "-v", "1.20.1", "-l", "forge", "--loader-version", "47.4.6"})
		})
		built := fmt.Sprintf(output.Translate("shell.dl.autoname"), "1.20.1-Forge_47.4.6")
		if strings.Contains(out, built) {
			t.Errorf("a name was built even though one was given:\n%s", out)
		}
		if !strings.Contains(out, "my-own-name") {
			t.Errorf("the given name is missing from the output:\n%s", out)
		}
	})
}
