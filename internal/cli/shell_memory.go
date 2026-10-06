package cli

import (
	"fmt"
	"strings"

	"github.com/telecter/cmd-launcher/internal/cli/output"
	"github.com/telecter/cmd-launcher/pkg/memory"
)

// clearCmd handles "clear". On its own, or with "screen", it wipes the terminal. With "memory" it
// hands the physical memory the running processes are not using back to the system, which is what
// frees up memory for a game that is about to start.
func (s *shell) clearCmd(rest []string) {
	entry, _ := syntaxOf("clear")

	object := "screen"
	all := false
	for _, arg := range rest {
		flag, _, _ := splitFlag(arg)
		switch {
		case flag == "-a" || flag == "--all":
			all = true
		case strings.HasPrefix(flag, "-"):
			output.Error(output.Translate("shell.badflag"), arg)
			return
		case object == "screen":
			object = strings.ToLower(flag)
		default:
			output.Error(output.Translate("shell.unexpectedarg"), arg)
			return
		}
	}

	switch object {
	case "screen":
		fmt.Print("\033[2J\033[H")
	case "memory":
		s.clearMemory(all)
	default:
		output.Error(output.Translate("shell.badobject"), object)
		output.Info(output.Translate("shell.usage"), entry.syntax)
	}
}

// clearMemory trims the working sets of the running processes. Nothing is lost by doing so: the
// pages move to the page file and come back when they are read again.
func (s *shell) clearMemory(all bool) {
	result, err := memory.Clear(all)
	if err != nil {
		output.Error("%s", err)
		return
	}
	if result.Processes == 0 {
		output.Info(output.Translate("shell.mem.nothing"))
		return
	}
	output.Success(output.Translate("shell.mem.trimmed"), result.Processes, result.Megabytes())
	output.Info(output.Translate("shell.mem.note"))
}
