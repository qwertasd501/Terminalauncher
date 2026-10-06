package main

import (
	"github.com/qwertasd501/Terminalauncher/internal/cli"
)

func main() {
	// Parse and run the main CLI.
	exiter, code := cli.Run()
	exiter(code)
}
