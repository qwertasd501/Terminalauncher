package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/alecthomas/kong"
	"github.com/buildkite/shellwords"
	"github.com/fatih/color"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/qwertasd501/Terminalauncher/internal/cli/cmd"
	"github.com/qwertasd501/Terminalauncher/internal/cli/output"
	"github.com/qwertasd501/Terminalauncher/internal/meta"
	"github.com/qwertasd501/Terminalauncher/internal/tui"
	env "github.com/qwertasd501/Terminalauncher/pkg"
	"github.com/qwertasd501/Terminalauncher/pkg/launcher"
	"golang.org/x/term"
)

// errEndOfInput reports that standard input ended.
var errEndOfInput = errors.New("end of input")

// ShellCmd runs the interactive command shell.
//
// It is the default command, so running the launcher without any command enters the shell.
// Commands are entered at a prompt using "<verb> <object>" syntax, similar to tools like diskpart.
type ShellCmd struct{}

// A shellHelpEntry is one row of the listing printed by the "help" command.
type shellHelpEntry struct {
	command string   // Command as typed, including its object
	helpKey string   // Translation key of its description
	syntax  string   // Full usage, shown by "help <command>"
	aliases []string // Other spellings the prompt accepts, e.g. "launch" for "start"
}

// label is the command column of the listing: the command, plus every alternative spelling. The
// listing and the prompt have to agree, so an alias is shown rather than kept secret.
func (e shellHelpEntry) label() string {
	if len(e.aliases) == 0 {
		return e.command
	}
	return e.command + " | " + strings.Join(e.aliases, " | ")
}

// matches reports whether the query names this entry, either by its command or by an alias.
func (e shellHelpEntry) matches(query string) bool {
	if strings.HasPrefix(e.command, query) {
		return true
	}
	for _, alias := range e.aliases {
		if strings.HasPrefix(alias, query) {
			return true
		}
	}
	return false
}

// shellHelp lists every shell command, in the order they are shown by "help".
var shellHelp = []shellHelpEntry{
	{"list versions", "list", "list versions | list users | list game [<query>] | list fabric|quilt|forge [<query>]", nil},
	{"list mods", "shell.listcontent", "list mods|resourcepacks|shaderpacks|datapacks [<query>] [-w <world>]", nil},
	{"list modpacks", "shell.listmodpacks", "list modpacks", nil},
	{"select versions", "shell.select", "select versions [<name>]", nil},
	{"select users", "shell.selectusers", "select users", nil},
	{"select java", "shell.selectjava", "select java [<path>]", nil},
	{"select mods", "shell.selectcontent", "select mods|resourcepacks|shaderpacks|datapacks [<name>] [-w <world>]", nil},
	{"deselect", "shell.deselect", "deselect", nil},
	{"list users", "shell.listusers", "list users", nil},
	{"create users", "shell.createusers", "create users [<offline username>] [-n] | with no name: select the account type", nil},
	{"delete users", "shell.delusers", "delete users [<name>]", []string{"del users"}},
	{"download version", "shell.downloadversion", "download version [<name>] [-v <version>] [-l <loader>] [--loader-version <version>] [-x <extra>] [--no-fill]", []string{"create instance"}},
	{"download mods", "shell.downloadcontent", "download mods|resourcepacks|shaderpacks|datapacks [<query>] [-w <world>]", nil},
	{"download modpacks", "shell.downloadmodpack", "download modpacks [<query> | <file.mrpack>] [-n <name>]", nil},
	{"enable mods", "shell.enablecontent", "enable mods|resourcepacks|shaderpacks|datapacks [<name>] [-w <world>]", nil},
	{"disable mods", "shell.disablecontent", "disable mods|resourcepacks|shaderpacks|datapacks [<name>] [-w <world>]", nil},
	{"delete mods", "shell.deletecontent", "delete mods|resourcepacks|shaderpacks|datapacks [<name>] [-y] [-w <world>]", nil},
	{"rename mods", "shell.renamecontent", "rename mods|resourcepacks|shaderpacks|datapacks <name> <new name> [-w <world>]", nil},
	{"import mods", "shell.importcontent", "import mods|resourcepacks|shaderpacks|datapacks <path> ... [-w <world>]", nil},
	{"update mods", "shell.updatecontent", "update mods|resourcepacks|shaderpacks|datapacks [<name>] [-y] [-w <world>]", nil},
	{"search mods", "shell.downloadcontent.search", "search mods|resourcepacks|shaderpacks|datapacks|modpacks <query>", nil},
	{"set versions", "shell.setversions", "set versions [<name>]", nil},
	{"set global", "shell.setglobal", "set global", nil},
	{"settings", "shell.settings", "settings [<option>] [<value>]", []string{"options", "prefs"}},
	{"info", "shell.info", "info", nil},
	{"info mods", "shell.infocontent", "info mods|resourcepacks|shaderpacks|datapacks [<name>] [-w <world>]", nil},
	{"open", "shell.open", "open root|version|saves|mods|resourcepacks|shaderpacks|datapacks|config|logs|screenshots|backups", nil},
	{"cd", "shell.cd", "cd [-y] [<path>]", nil},
	{"pwd", "shell.pwd", "pwd", nil},
	{"delete version", "delete", "delete version [<name>] [-y]", nil},
	{"rename version", "rename", "rename version <name> <new name>", nil},
	{"start", "start", "start [<name>] [-u <username>] [-s <server>] [-w <world>] [--demo] [--disable-multiplayer] [--disable-chat] [--prepare] [--width <n>] [--height <n>] [--jvm <path>] [-a|--jvm-args <args>] [--min-memory <mb>] [--max-memory <mb>]", []string{"launch"}},
	{"search", "search", "search [<query>] [-k game|fabric|quilt|forge] [-r]", nil},
	{"auth login", "login", "auth login [--no-browser]", nil},
	{"auth logout", "logout", "auth logout", nil},
	{"about", "about", "about", nil},
	{"help", "shell.help", "help [<command>]", []string{"?"}},
	{"clear", "shell.clear", "clear [screen|memory] [-a|--all]", []string{"cls"}},
	{"exit | quit", "shell.exit", "exit | quit", nil},
}

// A shell is the state of an interactive session.
type shell struct {
	verbosity int            // Verbosity level, matching the --verbosity flag
	selected  string         // Name of the currently selected instance, if any
	in        *bufio.Scanner // Line source for non-interactive input
	tty       bool           // Whether standard input is a terminal

	selectedContent string // File name of the mod or pack the content commands default to
	contentWorld    string // World the data pack commands default to

	line    *tui.LineReader // Line editor, used only when interactive
	history []string        // Previously entered commands

	namesCache []string  // Cached instance names, for completion
	namesAt    time.Time // When namesCache was filled
}

func (ShellCmd) Run(ctx *kong.Context, verbosity int) error {
	// The paging rows of every arrow key list are translated once, here, so that a widget never
	// has to look at the language files itself.
	refreshLabels()

	s := &shell{
		verbosity: verbosity,
		in:        bufio.NewScanner(os.Stdin),
	}
	s.in.Buffer(make([]byte, 0, 4096), 1<<20)
	s.line = &tui.LineReader{
		Complete: s.complete,
	}
	return s.run()
}

// refreshLabels re-translates the wording the arrow key widgets use. They read it once, so a
// language change has to call this again. It is also called once at start-up, from AfterApply, so
// that the widgets speak the stored language even outside the shell.
func refreshLabels() {
	tui.SetLabels(tui.Labels{
		Next:    output.Translate("shell.page.next"),
		Prev:    output.Translate("shell.page.prev"),
		Page:    output.Translate("shell.page.word"),
		Select:  output.Translate("tui.select"),
		Invalid: output.Translate("tui.invalid"),
		Empty:   output.Translate("tui.empty"),
	})
}

// run starts the read-eval-print loop, returning when the user exits or input ends.
func (s *shell) run() error {
	s.tty = term.IsTerminal(int(os.Stdin.Fd()))
	s.banner()

	ended := false
	for !ended {
		line, err := s.readCommand()
		switch {
		case errors.Is(err, tui.ErrEOF), errors.Is(err, errEndOfInput):
			ended = true
		case errors.Is(err, tui.ErrInterrupted):
			// Ctrl-c clears the current line rather than leaving the shell.
		case err != nil:
			return err
		default:
			if line == "" {
				continue
			}
			s.history = append(s.history, line)
			if s.dispatch(line) {
				ended = true
			}
		}
	}

	if s.tty {
		fmt.Println()
	}
	return nil
}

// banner prints the session greeting.
func (s *shell) banner() {
	color.New(color.Bold).Println(name, version)
	fmt.Println(output.Translate("launcher.description"))
	output.Info(output.Translate("shell.rootdir"), env.RootDir)
	instances, err := launcher.FetchAllInstances()
	switch {
	case err != nil:
		output.Warning("%s", err)
	default:
		output.Info(output.Translate("shell.instances"), len(instances))
	}
	output.Info(output.Translate("shell.info.accountis"), currentAccountLabel())
	output.Info(output.Translate("shell.welcome"))
	if s.tty {
		fmt.Println()
	}
}

// promptText returns the command prompt.
//
// The selected instance and the active account are shown side by side, as in
// "Terminalauncher [1.20.1-Forge_47.4.16-LTSC]_[123456]>", so that the next command's target is
// always visible without running "info". A part that is not set is shown as "-", which keeps the
// two slots in the same place; while neither is set the brackets are dropped altogether.
func (s *shell) promptText() string {
	instance := s.selected
	account := currentAccountLabel()
	if instance == "" && account == "-" {
		return promptStyle.Sprintf("%s> ", name)
	}
	if instance == "" {
		instance = "-"
	}
	return promptStyle.Sprintf("%s [%s]_[%s]> ", name, instance, account)
}

// promptStyle colours the command prompt.
var promptStyle = color.New(color.Bold, color.FgCyan)

// readCommand reads a command line, showing the prompt only on a terminal.
func (s *shell) readCommand() (string, error) {
	if s.tty {
		return s.readPrompt(s.promptText())
	}
	if !s.in.Scan() {
		return "", errEndOfInput
	}
	return strings.TrimSpace(s.in.Text()), nil
}

// readPrompt prints prompt and reads a line, editing it when a terminal is available.
func (s *shell) readPrompt(prompt string) (string, error) {
	if s.tty && s.line != nil {
		s.line.Prompt = prompt
		s.line.History = s.history
		line, err := s.line.ReadLine()
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(line), nil
	}

	fmt.Fprint(os.Stdout, prompt)
	if !s.in.Scan() {
		return "", errEndOfInput
	}
	return strings.TrimSpace(s.in.Text()), nil
}

// confirm asks a yes/no question.
//
// This must not use fmt.Scanln, which would race the reader already in use.
func (s *shell) confirm(format string, a ...any) bool {
	answer, err := s.readPrompt(fmt.Sprintf(format, a...))
	if err != nil {
		return false
	}
	switch strings.ToLower(answer) {
	case "y", "yes", "j", "ja", "o", "oui", "s", "sí", "д", "да": // localized letters for the language prompts
		return true
	}
	return false
}

// usage reports an invalid invocation and prints the command's syntax.
func (s *shell) usage(entry shellHelpEntry) {
	output.Error(output.Translate("shell.badusage"))
	output.Info(output.Translate("shell.usage"), entry.syntax)
}

// syntaxOf returns the help entry whose command starts with prefix.
func syntaxOf(prefix string) (shellHelpEntry, bool) {
	for _, entry := range shellHelp {
		if strings.HasPrefix(entry.command, prefix) {
			return entry, true
		}
	}
	return shellHelpEntry{}, false
}

// dispatch parses and runs one line of input. It returns true if the shell should exit.
func (s *shell) dispatch(line string) bool {
	args, err := shellwords.Split(line)
	if err != nil {
		output.Error("%s", err)
		return false
	}
	if len(args) == 0 {
		return false
	}

	verb, rest := strings.ToLower(args[0]), args[1:]

	switch verb {
	case "exit", "quit":
		return true
	case "help", "?":
		s.helpCmd(rest)
	case "clear", "cls":
		s.clearCmd(rest)
	case "about":
		_ = aboutCmd{}.Run(nil)
	case "cd":
		s.cdCmd(rest)
	case "pwd":
		fmt.Fprintln(os.Stdout, env.RootDir)
	case "list":
		s.listCmd(rest)
	case "select", "choose":
		// "choose" is kept as a hidden alias: it used to be the verb for the arrow key pickers,
		// and the single "select" covers every object now.
		s.selectCmd(rest)
	case "deselect":
		s.selected = ""
		s.selectedContent = ""
		output.Info(output.Translate("shell.deselected"))
	case "download":
		s.createCmd(rest)
	case "set":
		s.setCmd(rest)
	case "settings", "options", "prefs":
		s.settingsCmd(rest)
	case "info":
		s.infoCmd(rest)
	case "open":
		s.openCmd(rest)
	case "create":
		s.createCmd(rest)
	case "delete", "del":
		s.deleteCmd(rest)
	case "rename":
		s.renameCmd(rest)
	case "enable":
		s.setContentEnabled(rest, true)
	case "disable":
		s.setContentEnabled(rest, false)
	case "import":
		s.importCmd(rest)
	case "update":
		s.updateCmd(rest)
	case "start", "launch":
		s.startCmd(rest)
	case "search":
		s.searchCmd(rest)
	case "auth":
		s.authCmd(rest)
	default:
		output.Error(output.Translate("shell.unknowncmd"), verb)
	}
	return false
}

// contentObjectOf splits the object of a content command off its arguments.
func contentObjectOf(rest []string) (launcher.ContentKind, []string, bool) {
	if len(rest) == 0 {
		return "", nil, false
	}
	kind, ok := contentKindOf(normalizeObject(strings.ToLower(rest[0])))
	if !ok {
		return "", rest, false
	}
	return kind, rest[1:], true
}

// setContentEnabled is the "enable" and "disable" command.
func (s *shell) setContentEnabled(rest []string, enabled bool) {
	kind, args, ok := contentObjectOf(rest)
	if !ok {
		s.badContentObject(rest)
		return
	}
	s.setContentEnabledCmd(kind, enabled, args)
}

// importCmd is the "import" command.
func (s *shell) importCmd(rest []string) {
	kind, args, ok := contentObjectOf(rest)
	if !ok {
		s.badContentObject(rest)
		return
	}
	s.importContentCmd(kind, args)
}

// updateCmd is the "update" command.
func (s *shell) updateCmd(rest []string) {
	kind, args, ok := contentObjectOf(rest)
	if !ok {
		s.badContentObject(rest)
		return
	}
	s.updateContentCmd(kind, args)
}

// badContentObject reports a content command that named no usable object.
func (s *shell) badContentObject(rest []string) {
	if len(rest) == 0 {
		output.Error(output.Translate("shell.content.needobject"))
		return
	}
	output.Error(output.Translate("shell.content.badobject"), rest[0])
}

// normalizeObject maps the spellings of an object onto a canonical name, returning "" if unknown.
//
// "version" means a game installation, following PCL's wording, not a downloadable game version.
// The content categories keep the folder name PCL gives them, so that "mods", "resourcepacks",
// "shaderpacks" and "datapacks" are spelled the same way in the game directory and in the shell.
func normalizeObject(object string) string {
	switch object {
	case "instance", "inst", "instances", "version", "versions":
		return "instance"
	case "user", "users", "account", "accounts":
		return "user"
	case "game", "games", "minecraft":
		return "game"
	case "mod", "mods":
		return "mods"
	case "resourcepack", "resourcepacks", "texturepack", "texturepacks":
		return "resourcepacks"
	case "shader", "shaders", "shaderpack", "shaderpacks":
		return "shaderpacks"
	case "datapack", "datapacks", "data":
		return "datapacks"
	case "modpack", "modpacks", "pack", "packs":
		return "modpacks"
	case "fabric", "quilt", "forge":
		return object
	}
	return ""
}

// splitFlag separates the "--flag=value" form into its parts. hasValue reports whether "=" was present.
func splitFlag(arg string) (name, value string, hasValue bool) {
	if !strings.HasPrefix(arg, "-") {
		return arg, "", false
	}
	if i := strings.IndexByte(arg, '='); i >= 0 {
		return arg[:i], arg[i+1:], true
	}
	return arg, "", false
}

// flagValue returns the value of a flag, either from its "=" form or from the following argument.
// i is the index of the flag itself and is advanced past the value when it is separate.
func flagValue(inline string, hasInline bool, rest []string, i *int) (string, error) {
	if hasInline {
		return inline, nil
	}
	if *i+1 >= len(rest) {
		return "", fmt.Errorf("missing value")
	}
	*i++
	return rest[*i], nil
}

// helpQueryVariants expands a "help <command>" query into the spellings the listing might use: the
// query itself, its object in the singular, the long "delete <object>" form of "del", and the
// objects the prompt accepts as synonyms of the ones in the listing ("instance" for "version",
// "account" for "user"). Only the first spelling that matches anything is used, so the most literal
// reading of the query wins.
func helpQueryVariants(query string) []string {
	seeds := []string{query}
	if rest, ok := strings.CutPrefix(query, "del "); ok {
		seeds = append(seeds, "delete "+rest)
	}
	// "choose" is a hidden alias of "select", so the help answers to it as well.
	if rest, ok := strings.CutPrefix(query, "choose"); ok {
		seeds = append(seeds, "select"+rest)
	}
	// range evaluates the slice header once, so the entries appended below are picked up by the
	// next pair rather than by the loop that added them.
	for _, pair := range [][2]string{{"instance", "version"}, {"account", "user"}} {
		for _, seed := range seeds {
			if swapped := strings.ReplaceAll(seed, pair[0], pair[1]); swapped != seed {
				seeds = append(seeds, swapped)
			}
		}
	}

	var variants []string
	for _, seed := range seeds {
		variants = append(variants, seed)
		if singular := singularizeQuery(seed); singular != seed {
			variants = append(variants, singular)
		}
	}
	return variants
}

// singularizeQuery drops a trailing "s" from the last word, so that "del versions" finds the
// listing row "delete version".
func singularizeQuery(query string) string {
	words := strings.Fields(query)
	if len(words) == 0 {
		return query
	}
	last := words[len(words)-1]
	if len(last) <= 1 || !strings.HasSuffix(last, "s") || strings.HasSuffix(last, "ss") {
		return query
	}
	words[len(words)-1] = strings.TrimSuffix(last, "s")
	return strings.Join(words, " ")
}

// helpMatches returns every listing row named by query, by its command or by one of its aliases.
func helpMatches(query string) []shellHelpEntry {
	for _, q := range helpQueryVariants(query) {
		var matches []shellHelpEntry
		for _, entry := range shellHelp {
			if entry.matches(q) {
				matches = append(matches, entry)
			}
		}
		if len(matches) > 0 {
			return matches
		}
	}
	return nil
}

func (s *shell) helpCmd(rest []string) {
	if len(rest) > 0 {
		query := strings.ToLower(strings.Join(rest, " "))
		matches := helpMatches(query)
		if len(matches) == 0 {
			output.Error(output.Translate("shell.unknowncmd"), query)
			return
		}
		for _, entry := range matches {
			output.Info("%s  %s", color.New(color.Bold).Sprint(entry.syntax), output.Translate(entry.helpKey))
		}
		return
	}

	t := table.NewWriter()
	t.SetStyle(table.StyleLight)
	t.SetOutputMirror(os.Stdout)
	t.AppendHeader(table.Row{
		output.Translate("shell.command"),
		output.Translate("shell.description"),
	})
	for _, entry := range shellHelp {
		t.AppendRow(table.Row{entry.label(), output.Translate(entry.helpKey)})
	}
	t.Render()
	output.Tip(output.Translate("shell.helpusage"))
	output.Info("%s: %s", output.Translate("launcher.project"), projectURL)
}

func (s *shell) listCmd(rest []string) {
	entry, _ := syntaxOf("list versions")
	if len(rest) == 0 {
		s.usage(entry)
		return
	}
	word := strings.ToLower(rest[0])
	object, rest := normalizeObject(word), rest[1:]
	switch object {
	case "instance":
		s.listInstances()
	case "user":
		s.listUsers()
	case "game":
		s.searchKind("versions", strings.Join(rest, " "), false)
	case "modpacks":
		s.listModpacksCmd()
	case "fabric", "quilt", "forge":
		s.searchKind(object, strings.Join(rest, " "), false)
	default:
		if kind, ok := contentKindOf(object); ok {
			s.listContentCmd(kind, rest)
			return
		}
		output.Error(output.Translate("shell.badobject"), word)
	}
}

// listInstances prints all instances, marking the selected one.
func (s *shell) listInstances() {
	instances, err := launcher.FetchAllInstances()
	if err != nil {
		output.Error("%s", err)
		return
	}

	t := table.NewWriter()
	t.SetStyle(table.StyleLight)
	t.SetOutputMirror(os.Stdout)
	t.AppendHeader(table.Row{
		"",
		"#",
		output.Translate("search.table.name"),
		output.Translate("search.table.version"),
		output.Translate("search.table.type"),
	})
	for i, inst := range instances {
		mark := ""
		if inst.Name == s.selected {
			mark = "*"
		}
		t.AppendRow(table.Row{mark, i, inst.Name, inst.GameVersion, inst.Loader})
	}
	t.Render()

	if len(instances) == 0 {
		output.Info(output.Translate("shell.noinstances"))
	}
}

// instanceName resolves an instance name argument, falling back to the current selection.
func (s *shell) instanceName(name string) (string, bool) {
	if name == "" {
		name = s.selected
	}
	if name == "" {
		output.Error(output.Translate("shell.noselection"))
		return "", false
	}
	if !launcher.DoesInstanceExist(name) {
		output.Error(output.Translate("shell.notexist"), name)
		return "", false
	}
	return name, true
}

func (s *shell) selectCmd(rest []string) {
	entry, _ := syntaxOf("select versions")

	// Java and the accounts have no name to type, so they are always picked from a list. Every
	// other object keeps the "select versions [<name>]" and "select <name>" forms.
	if len(rest) > 0 {
		word := strings.ToLower(rest[0])
		if word == "java" || word == "jvm" {
			s.selectJava(rest[1:])
			return
		}
		if normalizeObject(word) == "user" {
			s.selectUser()
			return
		}
	}

	// Both "select versions [<name>]" and the shorthand "select <name>" are accepted.
	object := ""
	if len(rest) > 0 {
		if name := normalizeObject(strings.ToLower(rest[0])); name != "" {
			object, rest = name, rest[1:]
		}
	}
	if object != "" && object != "instance" {
		if kind, ok := contentKindOf(object); ok {
			s.selectContentCmd(kind, rest)
			return
		}
		output.Error(output.Translate("shell.badobject"), object)
		return
	}

	if len(rest) == 0 {
		// No name given: let the user pick with the arrow keys.
		if name, ok := s.pickInstance(); ok {
			s.selectInstance(name)
		}
		return
	}
	if len(rest) > 1 {
		s.usage(entry)
		return
	}
	s.selectInstance(rest[0])
}

// selectInstance makes the named instance current, reporting when it does not exist.
func (s *shell) selectInstance(name string) bool {
	if !launcher.DoesInstanceExist(name) {
		output.Error(output.Translate("shell.notexist"), name)
		return false
	}
	s.selected = name
	s.selectedContent, s.contentWorld = "", ""
	output.Success(output.Translate("shell.selected"), color.New(color.Bold).Sprint(name))
	return true
}

func (s *shell) createCmd(rest []string) {
	entry, _ := syntaxOf("download version")
	if len(rest) == 0 {
		// With nothing to go on, ask for the details instead of reporting a usage error.
		s.downloadWizard()
		return
	}
	word := strings.ToLower(rest[0])
	object, rest := normalizeObject(word), rest[1:]
	if kind, ok := contentKindOf(object); ok {
		s.downloadContentCmd(kind, rest)
		return
	}
	if object == "modpacks" {
		s.downloadModpackCmd(rest)
		return
	}
	switch object {
	case "user":
		s.createUsersCmd(rest)
		return
	case "instance":
	default:
		output.Error(output.Translate("shell.badobject"), word)
		return
	}

	// With no name and no flags there is nothing to go on, so ask for the details instead.
	if len(rest) == 0 {
		s.downloadWizard()
		return
	}

	var name string
	// Empty means "not given": the name is built from the version and loader, and anything else is
	// asked for with the arrow key menus.
	loader, loaderVersion, gameVersion, extra := "", "", "", ""
	// The files the version needs are fetched as part of creating it; --no-fill leaves that to the
	// first launch instead.
	noFill := false

	for i := 0; i < len(rest); i++ {
		flag, inline, hasInline := splitFlag(rest[i])
		switch flag {
		case "--no-fill":
			noFill = true
		case "-v", "--version", "-l", "--loader", "--loader-version", "-x", "--suffix":
			value, err := flagValue(inline, hasInline, rest, &i)
			if err != nil {
				output.Error(output.Translate("shell.missingvalue"), flag)
				return
			}
			switch flag {
			case "-v", "--version":
				gameVersion = value
			case "-l", "--loader":
				loader = strings.ToLower(value)
			case "-x", "--suffix":
				extra = value
			default:
				loaderVersion = value
			}
		default:
			if strings.HasPrefix(flag, "-") {
				output.Error(output.Translate("shell.badflag"), flag)
				return
			}
			if name != "" {
				output.Error(output.Translate("shell.unexpectedarg"), flag)
				return
			}
			name = flag
		}
	}

	// Whatever was not spelled out is asked with the arrow key menus.
	loader, gameVersion, loaderVersion, ok := s.selectVersionDetails(gameVersion, loader, loaderVersion)
	if !ok {
		return
	}

	// No name given: name the instance after the version and loader it was created with, which is
	// also how PCL names its version folders.
	if name = strings.TrimSpace(name); name == "" {
		name = launcher.DefaultInstanceName(gameVersion, meta.Loader(loader), loaderVersion, extra)
		if name == "" {
			output.Error(output.Translate("shell.missingname"))
			output.Info(output.Translate("shell.usage"), entry.syntax)
			return
		}
		output.Info(output.Translate("shell.dl.autoname"), color.New(color.Bold).Sprint(name))
	}

	s.createVersion(name, loader, gameVersion, loaderVersion, noFill)
}

func (s *shell) deleteCmd(rest []string) {
	entry, _ := syntaxOf("delete version")
	if len(rest) == 0 {
		s.usage(entry)
		return
	}
	word := strings.ToLower(rest[0])
	object, rest := normalizeObject(word), rest[1:]
	switch object {
	case "user":
		s.delUsersCmd(rest)
		return
	case "instance":
	default:
		if kind, ok := contentKindOf(object); ok {
			s.deleteContentCmd(kind, rest)
			return
		}
		output.Error(output.Translate("shell.badobject"), word)
		return
	}

	var name string
	assumeYes := false
	for _, arg := range rest {
		flag, _, _ := splitFlag(arg)
		switch {
		case flag == "-y" || flag == "--yes":
			assumeYes = true
		case strings.HasPrefix(flag, "-"):
			output.Error(output.Translate("shell.badflag"), flag)
			return
		case name != "":
			output.Error(output.Translate("shell.unexpectedarg"), flag)
			return
		default:
			name = flag
		}
	}

	name, ok := s.instanceName(name)
	if !ok {
		return
	}

	if !assumeYes && !s.confirm(output.Translate("shell.confirm"), color.New(color.Bold).Sprint(name)) {
		output.Info(output.Translate("delete.abort"))
		return
	}
	if err := launcher.RemoveInstance(name); err != nil {
		output.Error("remove instance: %s", err)
		return
	}
	output.Success(output.Translate("delete.complete"), color.New(color.Bold).Sprint(name))
	if s.selected == name {
		s.selected = ""
		s.selectedContent, s.contentWorld = "", ""
	}
}

func (s *shell) renameCmd(rest []string) {
	entry, _ := syntaxOf("rename version")
	if len(rest) == 0 {
		s.usage(entry)
		return
	}

	if kind, args, ok := contentObjectOf(rest); ok {
		s.renameContentCmd(kind, args)
		return
	}

	oldName := ""
	switch {
	case normalizeObject(strings.ToLower(rest[0])) == "instance":
		// "rename instance <name> <new name>"
		rest = rest[1:]
		if len(rest) < 2 {
			s.usage(entry)
			return
		}
		oldName, rest = rest[0], rest[1:]
	case len(rest) >= 2:
		// "rename <name> <new name>"
		oldName, rest = rest[0], rest[1:]
	case s.selected != "":
		// "rename <new name>", relying on the selection
		oldName = s.selected
	default:
		s.usage(entry)
		return
	}

	newName := strings.Join(rest, " ")
	if newName == "" {
		s.usage(entry)
		return
	}
	if launcher.DoesInstanceExist(newName) {
		output.Error(output.Translate("shell.exists"), newName)
		return
	}

	inst, err := launcher.FetchInstance(oldName)
	if err != nil {
		output.Error("%s", err)
		return
	}
	if err := inst.Rename(newName); err != nil {
		output.Error("rename instance: %s", err)
		return
	}
	if s.selected == oldName {
		s.selected = newName
	}
	output.Success(output.Translate("rename.complete"))
}

func (s *shell) startCmd(rest []string) {
	sc := cmd.StartCmd{}
	var name string

	for i := 0; i < len(rest); i++ {
		flag, inline, hasInline := splitFlag(rest[i])

		text := func() (string, bool) {
			value, err := flagValue(inline, hasInline, rest, &i)
			if err != nil {
				output.Error(output.Translate("shell.missingvalue"), flag)
				return "", false
			}
			return value, true
		}
		number := func() (int, bool) {
			value, ok := text()
			if !ok {
				return 0, false
			}
			n, err := strconv.Atoi(value)
			if err != nil {
				output.Error(output.Translate("shell.badnumber"), value)
				return 0, false
			}
			return n, true
		}

		switch flag {
		case "-u", "--username":
			value, ok := text()
			if !ok {
				return
			}
			sc.Options.Username = value
		case "-s", "--server":
			value, ok := text()
			if !ok {
				return
			}
			sc.Options.Server = value
		case "-w", "--world":
			value, ok := text()
			if !ok {
				return
			}
			sc.Options.World = value
		case "--demo":
			sc.Options.Demo = true
		case "--disable-multiplayer":
			sc.Options.DisableMP = true
		case "--disable-chat":
			sc.Options.DisableChat = true
		case "--prepare":
			sc.Prepare = true
		case "--width":
			if n, ok := number(); ok {
				sc.Overrides.Width = n
			} else {
				return
			}
		case "--height":
			if n, ok := number(); ok {
				sc.Overrides.Height = n
			} else {
				return
			}
		case "--jvm":
			value, ok := text()
			if !ok {
				return
			}
			sc.Overrides.JVM = value
		case "-a", "--jvm-args":
			value, ok := text()
			if !ok {
				return
			}
			if sc.Overrides.JVMArgs != "" {
				sc.Overrides.JVMArgs += " "
			}
			sc.Overrides.JVMArgs += value
		case "--min-memory":
			if n, ok := number(); ok {
				sc.Overrides.MinMemory = n
			} else {
				return
			}
		case "--max-memory":
			if n, ok := number(); ok {
				sc.Overrides.MaxMemory = n
			} else {
				return
			}
		default:
			if strings.HasPrefix(flag, "-") {
				output.Error(output.Translate("shell.badflag"), flag)
				return
			}
			if name != "" {
				output.Error(output.Translate("shell.unexpectedarg"), flag)
				return
			}
			name = flag
		}
	}

	name, ok := s.instanceName(name)
	if !ok {
		return
	}
	sc.ID = name

	if err := sc.Run(nil, s.verbosity); err != nil {
		output.Error("%s", err)
	}
}

func (s *shell) searchCmd(rest []string) {
	kind := "versions"
	reverse := false
	var query []string

	for i := 0; i < len(rest); i++ {
		flag, inline, hasInline := splitFlag(rest[i])
		switch flag {
		case "-k", "--kind":
			value, err := flagValue(inline, hasInline, rest, &i)
			if err != nil {
				output.Error(output.Translate("shell.missingvalue"), flag)
				return
			}
			kind = strings.ToLower(value)
		case "-r", "--reverse":
			reverse = true
		default:
			if strings.HasPrefix(flag, "-") {
				output.Error(output.Translate("shell.badflag"), flag)
				return
			}
			// A leading bare word may name the kind, e.g. "search fabric 0.16"
			if len(query) == 0 {
				switch object := normalizeObject(strings.ToLower(flag)); object {
				case "game":
					kind = "versions"
					continue
				case "":
				default:
					kind = object
					continue
				}
			}
			query = append(query, flag)
		}
	}

	if object, ok := contentKindOf(kind); ok {
		s.searchContentCmd(object, strings.Join(query, " "))
		return
	}
	if kind == "modpacks" {
		s.searchModpacksCmd(strings.Join(query, " "))
		return
	}

	switch kind {
	case "versions", "version", "game":
		kind = "versions"
	case "fabric", "quilt", "forge":
	default:
		output.Error(output.Translate("shell.badkind"), kind)
		return
	}
	s.searchKind(kind, strings.Join(query, " "), reverse)
}

// searchKind runs the version search interactively: it asks for the query and kind when they were
// not given, shows the results, and then downloads the row the user picks.
func (s *shell) searchKind(kind, query string, reverse bool) {
	if query == "" {
		q, changed, err := s.askText(output.Translate("search.interactive.query"), "")
		if err != nil || !changed {
			return
		}
		query = q
	}
	if kind == "" {
		kinds := []string{"versions", "fabric", "quilt", "forge"}
		index, ok := s.pick(output.Translate("search.interactive.kind"), kinds, 0)
		if !ok {
			return
		}
		kind = kinds[index]
	}

	header, rows, err := cmd.Search(kind, query, reverse)
	if err != nil {
		output.Error("%s", err)
		return
	}
	if len(rows) == 0 {
		output.Info(output.Translate("search.complete"), 0)
		return
	}

	t := table.NewWriter()
	t.SetStyle(table.StyleLight)
	t.SetOutputMirror(os.Stdout)
	t.AppendHeader(header)
	t.AppendRows(rows)
	t.Render()

	entries := make([]string, len(rows))
	for i, row := range rows {
		entries[i] = fmt.Sprint(row[0])
	}
	index, ok := s.pick(output.Translate("search.interactive.pick"), entries, 0)
	if !ok {
		return
	}
	s.createFromSearch(kind, rows[index])
}

func (s *shell) authCmd(rest []string) {
	sub := "login"
	if len(rest) > 0 {
		sub, rest = strings.ToLower(rest[0]), rest[1:]
	}

	switch sub {
	case "login":
		login := cmd.LoginCmd{}
		for _, arg := range rest {
			switch arg {
			case "-n", "--no-browser":
				login.NoBrowser = true
			default:
				output.Error(output.Translate("shell.badflag"), arg)
				return
			}
		}
		if err := login.Run(nil); err != nil {
			output.Error("%s", err)
		}
	case "logout":
		logout := cmd.LogoutCmd{}
		if err := logout.Run(nil); err != nil {
			output.Error("%s", err)
		}
	default:
		output.Error(output.Translate("shell.badobject"), sub)
	}
}
