package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/alecthomas/kong"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/qwertasd501/Terminalauncher/internal/cli/output"
	"github.com/qwertasd501/Terminalauncher/internal/meta"
)

// searchKinds lists the version searches the command offers, in the order the picker shows them.
var searchKinds = []string{"versions", "fabric", "quilt", "forge"}

// stdinReader is shared by every prompt so buffered input is never lost between calls. Creating a
// new bufio.Reader per prompt would discard bytes that reader pre-read from stdin.
var stdinReader = bufio.NewReader(os.Stdin)

// SearchCmd searches game and mod loader versions, then offers to download the one picked.
//
// The flags stay for scripts, but when run without them the command asks interactively: for the
// query first, then (if no --kind was given) for the kind, and finally for the row to download.
type SearchCmd struct {
	Query   string `arg:"" help:"Search query" optional:""`
	Kind    string `help:"Search kind" short:"k"`
	Reverse bool   `short:"r" help:"Reverse the order"`
}

// Search builds the result rows for a version search without rendering or prompting. It is shared
// by the top-level command and the shell, which renders and drives the picker itself.
func Search(kind, query string, reverse bool) (table.Row, []table.Row, error) {
	var rows []table.Row
	var header table.Row

	switch kind {
	case "versions":
		header = table.Row{
			output.Translate("search.table.version"),
			output.Translate("search.table.type"),
			output.Translate("search.table.date"),
		}
		manifest, err := meta.FetchVersionManifest()
		if err != nil {
			return nil, nil, fmt.Errorf("retrieve version manifest: %w", err)
		}
		for _, version := range manifest.Versions {
			if strings.Contains(version.ID, query) {
				rows = append(rows, table.Row{version.ID, version.Type, version.ReleaseTime.Format(time.DateTime)})
			}
		}
	case "fabric", "quilt":
		header = table.Row{
			output.Translate("search.table.version"),
		}
		var versions meta.FabricVersionList

		api := meta.Fabric
		if kind == "quilt" {
			api = meta.Quilt
		}

		versions, err := api.FetchVersions()
		if err != nil {
			return nil, nil, fmt.Errorf("retrieve versions: %w", err)
		}
		for _, version := range versions {
			if strings.Contains(version.Version, query) {
				rows = append(rows, table.Row{version.Version})
			}
		}
	case "forge":
		header = table.Row{
			output.Translate("search.table.version"),
			"Game Version",
			"Type",
		}

		versions, err := meta.FetchForgePromotions()
		if err != nil {
			return nil, nil, fmt.Errorf("retrieve Forge versions: %w", err)
		}
		for _, gameVersion := range versions.Keys() {
			version, _ := versions.Get(gameVersion)
			parts := strings.Split(gameVersion, "-")
			if len(parts) < 2 {
				continue
			}
			if strings.Contains(parts[0], query) {
				rows = append(rows, table.Row{version.(string), parts[0], parts[1]})
			}
		}
	}

	if reverse {
		// slices.Reverse keeps the rows stable; only their order flips.
		for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
			rows[i], rows[j] = rows[j], rows[i]
		}
	}
	return header, rows, nil
}

// Run searches, then (in interactive use) lets the user pick a row to download.
func (c *SearchCmd) Run(ctx *kong.Context) error {
	query := c.Query
	if query == "" {
		line, err := promptLine(output.Translate("search.interactive.query"))
		if err != nil {
			return err
		}
		query = strings.TrimSpace(line)
		if query == "" {
			output.Info(output.Translate("search.interactive.empty"))
			return nil
		}
	}

	kind := c.Kind
	if kind == "" {
		picked, ok := promptChoice(output.Translate("search.interactive.kind"), searchKinds)
		if !ok {
			return nil
		}
		kind = picked
	} else if !slices.Contains(searchKinds, kind) {
		return fmt.Errorf(output.Translate("shell.badkind"), kind)
	}

	header, rows, err := Search(kind, query, c.Reverse)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		output.Info(output.Translate("search.complete"), 0)
		return nil
	}

	renderTable(header, rows)

	index, ok := promptIndex(len(rows))
	if !ok {
		return nil
	}
	if err := downloadFromSearch(kind, rows[index]); err != nil {
		output.Error("%s", err)
	}
	return nil
}

// downloadFromSearch turns a picked result row into a create and runs it. Fabric and Quilt need a
// game version that the version listing does not carry, so it is asked for on the spot.
func downloadFromSearch(kind string, row table.Row) error {
	var gameVersion, loader, loaderVersion string
	switch kind {
	case "versions":
		gameVersion, _ = row[0].(string)
		loader, loaderVersion = "vanilla", "latest"
	case "fabric", "quilt":
		loaderVersion, _ = row[0].(string)
		loader = kind
		line, err := promptLine(output.Translate("search.interactive.gameversion"))
		if err != nil {
			return err
		}
		gameVersion = strings.TrimSpace(line)
		if gameVersion == "" {
			return errors.New(output.Translate("search.interactive.empty"))
		}
	case "forge":
		loaderVersion, _ = row[0].(string)
		gameVersion, _ = row[1].(string)
		loader = "forge"
	}

	create := CreateCmd{Version: gameVersion, Loader: loader, LoaderVersion: loaderVersion}
	return create.Run(nil, 0)
}

// renderTable prints a search result the way the command always has.
func renderTable(header table.Row, rows []table.Row) {
	output.Success(output.Translate("search.complete"), len(rows))
	t := table.NewWriter()
	t.SetStyle(table.StyleLight)
	t.SetOutputMirror(os.Stdout)
	t.AppendHeader(header)
	t.AppendRows(rows)
	t.Render()
}

// promptLine reads one line of input, used by the top-level command where no richer reader exists.
func promptLine(prompt string) (string, error) {
	fmt.Print(prompt + " ")
	line, err := stdinReader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// promptChoice shows a numbered menu and returns the chosen entry, or "" when the user cancels.
func promptChoice(title string, options []string) (string, bool) {
	fmt.Println(title)
	for i, option := range options {
		fmt.Printf("  %d) %s\n", i+1, option)
	}
	line, err := promptLine("")
	if err != nil {
		return "", false
	}
	n, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || n < 1 || n > len(options) {
		output.Error(output.Translate("search.interactive.badindex"))
		return "", false
	}
	return options[n-1], true
}

// promptIndex asks for a result number and returns it (1-based), or false to cancel.
func promptIndex(n int) (int, bool) {
	line, err := promptLine(output.Translate("search.interactive.pick"))
	if err != nil {
		return 0, false
	}
	s := strings.TrimSpace(line)
	if s == "" {
		return 0, false
	}
	i, err := strconv.Atoi(s)
	if err != nil || i < 1 || i > n {
		output.Error(output.Translate("search.interactive.badindex"))
		return 0, false
	}
	return i - 1, true
}
