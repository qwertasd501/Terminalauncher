package cmd

import (
	"github.com/qwertasd501/Terminalauncher/internal/cli/output"
	"github.com/qwertasd501/Terminalauncher/pkg/auth"
	"github.com/qwertasd501/Terminalauncher/pkg/launcher"
)

// fill fetches everything an instance needs in order to start: its libraries, the assets of its
// game version, and the Java runtime when none is configured. Starting the game does the same work
// first, so doing it while the instance is created only moves the wait earlier - the first launch
// then goes straight to the game instead of downloading for minutes.
//
// A failure is reported but not returned. The instance exists either way, and starting it repeats
// this step, so nothing is lost by leaving the rest for later.
func fill(inst launcher.Instance, verbosity int) {
	output.Info(output.Translate("create.filling"))

	// The session only reaches the arguments the game would be started with, and those are thrown
	// away here: fetching the files must not require an account, which would also make it fail in
	// offline mode without a stored one.
	_, err := launcher.Prepare(&inst, launcher.LaunchOptions{
		Session:        auth.Session{Username: "Player"},
		InstanceConfig: inst.Config.Resolve(),
	}, watcher(verbosity))
	if err != nil {
		output.Warning(output.Translate("create.fillfailed"), err)
		return
	}
	output.Success(output.Translate("create.filled"))
}
