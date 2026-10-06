package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/mattn/go-runewidth"
	"github.com/pkg/browser"
	"github.com/telecter/cmd-launcher/internal/cli/cmd"
	"github.com/telecter/cmd-launcher/internal/cli/output"
	"github.com/telecter/cmd-launcher/internal/meta"
	"github.com/telecter/cmd-launcher/internal/network"
	"github.com/telecter/cmd-launcher/internal/tui"
	env "github.com/telecter/cmd-launcher/pkg"
	"github.com/telecter/cmd-launcher/pkg/auth"
	"github.com/telecter/cmd-launcher/pkg/launcher"
)

// A menuItem is one row of an interactive settings menu.
type menuItem struct {
	label string
	value func() string
	edit  func() error
}

// errMenuReload unwinds a menu whose rows no longer match what is on disk, so that the caller can
// build it again. A mod page needs this after a rename or a delete.
var errMenuReload = errors.New("reload the menu")

// pick runs a picker over the given entries, reporting whether a choice was made.
func (s *shell) pick(title string, entries []string, initial int) (int, bool) {
	return s.runPicker(tui.Picker{Title: title, Items: entries, Initial: initial, Lines: s.in})
}

// pickStar runs pick, marking the entry offered by default with a "*".
func (s *shell) pickStar(title string, entries []string, initial int) (int, bool) {
	return s.runPicker(tui.Picker{Title: title, Items: entries, Initial: initial, Star: true, Lines: s.in})
}

// runPicker shows a picker and turns its errors into shell output.
func (s *shell) runPicker(picker tui.Picker) (int, bool) {
	index, err := picker.Select()
	if err != nil {
		if errors.Is(err, tui.ErrCancelled) {
			output.Info(output.Translate("shell.cancelled"))
			return -1, false
		}
		output.Error("%s", err)
		return -1, false
	}
	return index, true
}

// menu shows the items plus a "return" entry, running the chosen item's editor in a loop.
// padDisplay pads text with spaces up to the given display width, counting East Asian characters
// and other double-width runes as two cells. The plain %-26s of fmt counts them as one, which
// lets the value column of the settings menu drift out of line in Chinese and Russian.
func padDisplay(text string, width int) string {
	if gap := width - runewidth.StringWidth(text); gap > 0 {
		return text + strings.Repeat(" ", gap)
	}
	return text
}

// menu runs a settings page until the user leaves it.
//
// It returns true when a row asked for the page to be rebuilt, which a list that mirrors the files
// on disk needs after a rename or a delete has changed its rows.
func (s *shell) menu(title string, items []menuItem) bool {
	for {
		labelWidth := 0
		for _, item := range items {
			if w := runewidth.StringWidth(item.label); w > labelWidth {
				labelWidth = w
			}
		}
		entries := make([]string, 0, len(items)+1)
		for _, item := range items {
			entries = append(entries, padDisplay(item.label, labelWidth)+" "+item.value())
		}
		entries = append(entries, output.Translate("shell.menu.return"))

		index, ok := s.pick(title, entries, 0)
		if !ok || index == len(items) {
			return false
		}
		if err := items[index].edit(); err != nil {
			if errors.Is(err, errMenuReload) {
				return true
			}
			if errors.Is(err, tui.ErrCancelled) {
				output.Info(output.Translate("shell.cancelled"))
				continue
			}
			output.Error("%s", err)
		}
	}
}

// askText prompts for a line of input. An empty answer keeps the current value, and a single dash
// clears it.
func (s *shell) askText(label, current string) (string, bool, error) {
	prompt := color.New(color.Bold).Sprint(label) + ": "
	if current != "" {
		prompt = color.New(color.Bold).Sprint(label) + fmt.Sprintf(" [%s]: ", current)
	}

	answer, err := s.readPrompt(prompt)
	if err != nil {
		if errors.Is(err, tui.ErrEOF) || errors.Is(err, errEndOfInput) || errors.Is(err, tui.ErrInterrupted) {
			return "", false, tui.ErrCancelled
		}
		return "", false, err
	}

	switch answer {
	case "":
		return current, false, nil
	case "-":
		return "", true, nil
	}
	return answer, true, nil
}

// askInt prompts for a whole number.
func (s *shell) askInt(label string, current int) (int, bool, error) {
	answer, changed, err := s.askText(label, strconv.Itoa(current))
	if err != nil || !changed {
		return current, false, err
	}
	value, err := strconv.Atoi(answer)
	if err != nil {
		return current, false, fmt.Errorf(output.Translate("shell.badnumber"), answer)
	}
	return value, true, nil
}

// askChoice shows a nested picker and returns the chosen option's index.
func (s *shell) askChoice(title string, options []string, current int) (int, bool, error) {
	if current < 0 || current >= len(options) {
		current = 0
	}
	index, err := (&tui.Picker{Title: title, Items: options, Initial: current, Lines: s.in}).Select()
	if err != nil {
		return -1, false, err
	}
	return index, true, nil
}

// pickInstance asks the user to select an instance with the arrow keys.
func (s *shell) pickInstance() (string, bool) {
	instances, err := launcher.FetchAllInstances()
	if err != nil {
		output.Error("%s", err)
		return "", false
	}
	if len(instances) == 0 {
		output.Info(output.Translate("shell.noinstances"))
		return "", false
	}

	entries := make([]string, 0, len(instances))
	initial := 0
	for i, inst := range instances {
		loader := string(inst.Loader)
		if inst.LoaderVersion != "" {
			loader += " " + inst.LoaderVersion
		}
		entries = append(entries, fmt.Sprintf("%-28s %-10s %s", inst.Name, inst.GameVersion, loader))
		if inst.Name == s.selected {
			initial = i
		}
	}

	index, ok := s.pick(output.Translate("shell.pickversion"), entries, initial)
	if !ok {
		return "", false
	}
	return instances[index].Name, true
}

// selectOrPick resolves an instance name, falling back to the current selection and then to an
// interactive picker.
func (s *shell) selectOrPick(name string) (string, bool) {
	if name != "" {
		if inst, err := launcher.FetchInstance(name); err == nil {
			return inst.Name, true
		}
		output.Error(output.Translate("shell.notexist"), name)
		return "", false
	}
	if s.selected != "" {
		return s.selected, true
	}
	return s.pickInstance()
}

// ---------------------------------------------------------------------------
// Creating a version
// ---------------------------------------------------------------------------

// loaderChoices lists the loaders an instance can be created with, in menu order.
var loaderChoices = []string{"vanilla", "fabric", "quilt", "forge", "neoforge"}

// downloadWizard asks for everything needed to create an instance.
//
// It runs when "download version" is entered without arguments, so the whole choice can be made from
// the arrow key menus, the way PCL's download page works. The name may be left blank, in which case
// the instance is named after what it turns out to be (see launcher.DefaultInstanceName), and the
// optional trailing part is asked for right after the version choices.
func (s *shell) downloadWizard() {
	name, _, err := s.askText(output.Translate("shell.dl.name"), "")
	if err != nil {
		output.Info(output.Translate("shell.cancelled"))
		return
	}
	name = strings.TrimSpace(name)

	loader, gameVersion, loaderVersion, ok := s.selectVersionDetails("", "", "")
	if !ok {
		return
	}

	if name == "" {
		extra, _, err := s.askText(output.Translate("shell.dl.extra"), "")
		if err != nil {
			output.Info(output.Translate("shell.cancelled"))
			return
		}
		name = launcher.DefaultInstanceName(gameVersion, meta.Loader(loader), loaderVersion, extra)
		if name == "" {
			output.Error(output.Translate("shell.dl.noname"))
			return
		}
		output.Info(output.Translate("shell.dl.autoname"), color.New(color.Bold).Sprint(name))
	}

	// The wizard always finishes the job: the version is created and its files are fetched, so the
	// player can start it right after.
	s.createVersion(name, loader, gameVersion, loaderVersion, false)
}

// selectVersionDetails fills in whatever of the game version, loader and loader version is still
// missing, asking with the arrow key menus. Anything given up front (through -v, -l or
// --loader-version) is kept as is. It reports false when the user cancels or something goes wrong.
func (s *shell) selectVersionDetails(gameVersion, loader, loaderVersion string) (string, string, string, bool) {
	gameVersions, err := meta.FetchGameVersions()
	if err != nil {
		output.Error(output.Translate("shell.dl.nogameversions"), err)
		return "", "", "", false
	}

	if gameVersion == "" {
		entries := make([]string, 0, len(gameVersions.Versions))
		for _, version := range gameVersions.Versions {
			entries = append(entries, version.Label())
		}
		index, ok := s.pickStar(output.Translate("shell.dl.version"), entries, gameVersions.Recommended)
		if !ok {
			return "", "", "", false
		}
		gameVersion = gameVersions.Versions[index].ID
	}

	if loader == "" {
		index, ok := s.pickStar(output.Translate("shell.dl.loader"), loaderChoices, 0)
		if !ok {
			return "", "", "", false
		}
		loader = loaderChoices[index]
	}

	switch loader {
	case "vanilla", "fabric", "quilt", "neoforge", "forge":
	default:
		output.Error(output.Translate("shell.badloader"), loader)
		return "", "", "", false
	}

	// Vanilla has no loader version to pick.
	if loader == "vanilla" {
		return loader, gameVersion, "latest", true
	}
	if loaderVersion != "" && loaderVersion != "latest" {
		return loader, gameVersion, loaderVersion, true
	}
	list, err := meta.FetchLoaderVersions(meta.Loader(loader), gameVersion)
	switch {
	case err != nil:
		output.Warning(output.Translate("shell.dl.noloaderversions"), loader, err)
		loaderVersion = "latest"
	case len(list.Versions) == 0:
		output.Error(output.Translate("shell.dl.noloaders"), loader, gameVersion)
		return "", "", "", false
	default:
		versionIndex, ok := s.pickStar(output.Translate("shell.dl.loaderversion"), list.Versions, list.Recommended)
		if !ok {
			return "", "", "", false
		}
		loaderVersion = list.Versions[versionIndex]
	}
	return loader, gameVersion, loaderVersion, true
}

// createVersion creates an instance and makes it current.
//
// Unless noFill is set, the files the version needs in order to start are fetched right away, so
// that starting it the first time does not have to download anything.
func (s *shell) createVersion(name, loader, gameVersion, loaderVersion string, noFill bool) {
	if launcher.DoesInstanceExist(name) {
		output.Error(output.Translate("shell.dl.nameexists"), color.New(color.Bold).Sprint(name))
		return
	}
	create := cmd.CreateCmd{
		ID:            name,
		Loader:        loader,
		Version:       gameVersion,
		LoaderVersion: loaderVersion,
		NoFill:        noFill,
	}
	if err := create.Run(nil, s.verbosity); err != nil {
		output.Error("%s", err)
		return
	}
	s.selected = name
	s.selectedContent, s.contentWorld = "", ""
}

// cdCmd changes the game directory.
//
// The directory is never created silently: a typo would otherwise leave junk behind, so the user is
// asked first and shown any similarly named sibling directory.
func (s *shell) cdCmd(rest []string) {
	entry, _ := syntaxOf("cd")
	if len(rest) == 0 {
		output.Info(output.Translate("shell.cd.current"), env.RootDir)
		return
	}

	assumeYes := false
	var args []string
	for _, arg := range rest {
		flag, _, _ := splitFlag(arg)
		switch {
		case flag == "-y" || flag == "--yes":
			assumeYes = true
		case strings.HasPrefix(flag, "-") && flag != "-":
			output.Error(output.Translate("shell.badflag"), flag)
			return
		default:
			args = append(args, arg)
		}
	}
	if len(args) == 0 {
		s.usage(entry)
		return
	}

	absolute, err := resolvePath(strings.Join(args, " "))
	if err != nil {
		output.Error(output.Translate("shell.cd.invalid"), strings.Join(args, " "))
		return
	}

	info, err := os.Stat(absolute)
	switch {
	case err == nil && !info.IsDir():
		output.Error(output.Translate("shell.cd.notdir"), absolute)
		return
	case errors.Is(err, os.ErrNotExist):
		if suggestions := similarDirs(absolute); len(suggestions) > 0 {
			output.Tip(output.Translate("shell.cd.similar"), strings.Join(suggestions, "   "))
		}
		if !assumeYes && !s.confirm(output.Translate("shell.cd.confirm"), color.New(color.Bold).Sprint(absolute)) {
			output.Info(output.Translate("delete.abort"))
			return
		}
		if err := os.MkdirAll(absolute, 0755); err != nil {
			output.Error("%s", err)
			return
		}
	case err != nil:
		output.Error("%s", err)
		return
	}

	if err := env.SetDirs(absolute); err != nil {
		output.Error("%s", err)
		return
	}
	// The account store is global, so reloading it only refreshes what is already known.
	if err := auth.ReadFromCache(); err != nil {
		output.Error("%s", err)
		return
	}
	if s.selected != "" && !launcher.DoesInstanceExist(s.selected) {
		s.selected = ""
	}
	s.selectedContent, s.contentWorld = "", ""
	// Instance names may have changed with the directory.
	s.namesCache, s.namesAt = nil, time.Time{}
	output.Success(output.Translate("shell.cd.changed"), absolute)
	// Say how many instances the new directory holds: the count is what tells a mistyped path from
	// the one that was meant, without having to list anything.
	instances, err := launcher.FetchAllInstances()
	switch {
	case err != nil:
		output.Warning("%s", err)
	default:
		output.Info(output.Translate("shell.instances"), len(instances))
	}
}

// expandHome turns a leading ~ into the user's home directory.
func expandHome(path string) (string, bool) {
	if path != "~" && !strings.HasPrefix(path, "~/") && !strings.HasPrefix(path, `~\`) {
		return path, false
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path, false
	}
	return filepath.Join(home, strings.TrimLeft(path[1:], `/\`)), true
}

// resolvePath turns a typed path into an absolute one.
//
// Relative paths are resolved against the game directory, because that is where the shell is.
func resolvePath(path string) (string, error) {
	path, _ = expandHome(path)

	// A bare drive letter means the root of that drive, not the remembered directory on it.
	if len(path) == 2 && path[1] == ':' {
		path += string(os.PathSeparator)
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}
	if env.RootDir == "" {
		return filepath.Abs(path)
	}
	return filepath.Join(env.RootDir, path), nil
}

// similarDirs lists existing sibling directories whose name is close to the missing one.
func similarDirs(missing string) []string {
	parent, name := filepath.Dir(missing), filepath.Base(missing)
	entries, err := os.ReadDir(parent)
	if err != nil {
		return nil
	}

	lower := strings.ToLower(name)
	var out []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		candidate := strings.ToLower(entry.Name())
		if strings.Contains(candidate, lower) || strings.Contains(lower, candidate) || closeEnough(lower, candidate, 2) {
			out = append(out, entry.Name())
		}
	}

	sort.Strings(out)
	if len(out) > 5 {
		out = out[:5]
	}
	return out
}

// closeEnough reports whether a and b are within max edits of each other.
func closeEnough(a, b string, max int) bool {
	if len(a) > len(b) {
		a, b = b, a
	}
	if len(b)-len(a) > max {
		return false
	}

	previous := make([]int, len(a)+1)
	current := make([]int, len(a)+1)
	for i := range previous {
		previous[i] = i
	}

	for j := 1; j <= len(b); j++ {
		current[0] = j
		best := current[0]
		for i := 1; i <= len(a); i++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			current[i] = min(previous[i]+1, current[i-1]+1, previous[i-1]+cost)
			if current[i] < best {
				best = current[i]
			}
		}
		if best > max {
			return false
		}
		copy(previous, current)
	}
	return previous[len(a)] <= max
}

// openCmd opens one of an instance's directories in the desktop file manager.
func (s *shell) openCmd(rest []string) {
	entry, _ := syntaxOf("open")
	if len(rest) == 0 {
		s.usage(entry)
		return
	}

	target := strings.ToLower(rest[0])
	var path string

	switch target {
	case "root", "minecraft":
		path = env.RootDir
	case "version", "instance", "versions":
		name, ok := s.selectOrPick("")
		if !ok {
			return
		}
		inst, err := launcher.FetchInstance(name)
		if err != nil {
			output.Error("%s", err)
			return
		}
		path = inst.Dir()
	case "saves", "mods", "config", "logs", "screenshots", "resourcepacks", "shaderpacks", "backups", "datapacks":
		name, ok := s.selectOrPick("")
		if !ok {
			return
		}
		inst, err := launcher.FetchInstance(name)
		if err != nil {
			output.Error("%s", err)
			return
		}
		if target == "datapacks" {
			// Data packs belong to a world, so opening the folder needs one first.
			world, ok := s.contentWorldFor(inst, "")
			if !ok {
				return
			}
			path = filepath.Join(inst.GameDir(), "saves", world, "datapacks")
			break
		}
		path = filepath.Join(inst.GameDir(), target)
	default:
		output.Error(output.Translate("shell.open.unknown"), target)
		return
	}

	if err := os.MkdirAll(path, 0755); err != nil {
		output.Error("%s", err)
		return
	}
	output.Info(output.Translate("shell.open.opened"), path)
	if err := browser.OpenURL(path); err != nil {
		output.Tip(output.Translate("shell.open.failed"), path)
	}
}

// infoCmd describes the current selection and the account in use.
//
// With a content object it describes that mod or pack instead, which is where PCL shows a mod's
// id, authors and dependencies.
func (s *shell) infoCmd(rest []string) {
	if kind, args, ok := contentObjectOf(rest); ok {
		s.infoContentCmd(kind, args)
		return
	}
	output.Info(output.Translate("shell.info.gamedir"), env.RootDir)
	output.Info(output.Translate("shell.info.accountis"), currentAccountLabel())

	name, ok := s.selectOrPick("")
	if !ok {
		return
	}
	inst, err := launcher.FetchInstance(name)
	if err != nil {
		output.Error("%s", err)
		return
	}

	loader := string(inst.Loader)
	if inst.LoaderVersion != "" {
		loader += " " + inst.LoaderVersion
	}
	config := inst.Config.Resolve()

	t := table.NewWriter()
	t.SetStyle(table.StyleLight)
	t.SetOutputMirror(os.Stdout)
	t.AppendHeader(table.Row{output.Translate("shell.description"), ""})
	t.AppendRows([]table.Row{
		{output.Translate("shell.info.name"), inst.Name},
		{output.Translate("shell.info.layout"), string(inst.Layout)},
		{output.Translate("shell.info.dir"), inst.Dir()},
		{output.Translate("shell.info.gamedir.short"), inst.GameDir()},
		{output.Translate("shell.info.version"), inst.GameVersion},
		{output.Translate("shell.info.loader"), loader},
		{output.Translate("shell.set.versionIsolation"), isolationLabel(inst)},
		{output.Translate("shell.set.java"), displayJava(config.Java)},
		{output.Translate("shell.set.memory"), describeMemory(config)},
		{output.Translate("shell.set.loginMode"), config.LoginMode},
		{output.Translate("shell.set.category"), config.Category},
		{output.Translate("shell.set.favorite"), yesNo(config.Favorite)},
		{output.Translate("shell.set.description"), config.Description},
	})
	t.Render()
}

// isolationLabel describes version isolation, which only applies to the standard layout.
func isolationLabel(inst launcher.Instance) string {
	if inst.Layout != launcher.LayoutVersions {
		return output.Translate("shell.na")
	}
	if inst.Isolated() {
		return output.Translate("shell.on")
	}
	return output.Translate("shell.off")
}

// currentAccountLabel returns the name of the active account, or a placeholder.
func currentAccountLabel() string {
	if account := auth.Store.Active(); account != nil {
		return account.Label()
	}
	return "-"
}

// yesNo renders a boolean for display.
func yesNo(value bool) string {
	if value {
		return output.Translate("shell.yes")
	}
	return output.Translate("shell.no")
}

// displayJava renders a Java path, marking the automatic choice.
func displayJava(path string) string {
	if path == "" {
		return output.Translate("shell.java.auto")
	}
	return path
}

// describeMemory renders the memory settings of a resolved configuration.
func describeMemory(config launcher.InstanceConfig) string {
	switch config.MemoryMode {
	case "custom":
		return fmt.Sprintf("%s (%d-%d MB)", choiceLabel("custom"), config.MinMemory, config.MaxMemory)
	case "auto":
		return choiceLabel("auto")
	default:
		return fmt.Sprintf("%s (%d-%d MB)", choiceLabel("follow"), config.MinMemory, config.MaxMemory)
	}
}

// ---------------------------------------------------------------------------
// Accounts
// ---------------------------------------------------------------------------

// listUsers prints every stored account.
func (s *shell) listUsers() {
	accounts := auth.Store.Accounts

	t := table.NewWriter()
	t.SetStyle(table.StyleLight)
	t.SetOutputMirror(os.Stdout)
	t.AppendHeader(table.Row{
		"",
		"#",
		output.Translate("shell.info.account"),
		output.Translate("shell.users.type"),
		output.Translate("shell.users.uuid"),
	})
	for i, account := range accounts {
		mark := ""
		if i == auth.Store.Current {
			mark = "*"
		}
		kind := output.Translate("shell.usertype." + account.Kind())
		t.AppendRow(table.Row{mark, i, account.Label(), kind, orDash(account.Minecraft.UUID)})
	}
	t.Render()

	if len(accounts) == 0 {
		output.Info(output.Translate("shell.nousers"))
	}
}

// createUsersCmd adds an account: a local username, or a Microsoft account.
func (s *shell) createUsersCmd(rest []string) {
	var name string
	noBrowser := false

	for _, arg := range rest {
		switch arg {
		case "-n", "--no-browser":
			noBrowser = true
		default:
			if strings.HasPrefix(arg, "-") {
				output.Error(output.Translate("shell.badflag"), arg)
				return
			}
			if name != "" {
				output.Error(output.Translate("shell.unexpectedarg"), arg)
				return
			}
			name = arg
		}
	}

	// An explicit name is the offline shorthand: "create users Steve".
	if name != "" {
		s.addOfflineUser(name)
		return
	}
	if noBrowser {
		s.loginMicrosoft(true)
		return
	}

	options := []string{
		output.Translate("shell.usertype.addoffline"),
		output.Translate("shell.usertype.addbrowser"),
		output.Translate("shell.usertype.adddevice"),
	}
	index, ok, err := s.askChoice(output.Translate("shell.selectusertype"), options, 0)
	if err != nil {
		output.Info(output.Translate("shell.cancelled"))
		return
	}
	if !ok {
		return
	}

	switch index {
	case 0:
		username, changed, err := s.askText(output.Translate("shell.offlineusername"), "")
		if err != nil {
			output.Info(output.Translate("shell.cancelled"))
			return
		}
		if !changed {
			output.Info(output.Translate("shell.unchanged"))
			return
		}
		s.addOfflineUser(username)
	case 1:
		s.loginMicrosoft(false)
	case 2:
		s.loginMicrosoft(true)
	}
}

// addOfflineUser stores a local username account, which needs no Microsoft login.
func (s *shell) addOfflineUser(username string) {
	if err := auth.ValidateUsername(username); err != nil {
		output.Error(output.Translate("shell.badusername"), username)
		output.Tip(output.Translate("shell.usernamerule"))
		return
	}
	if _, err := auth.Store.AddOffline(username); err != nil {
		output.Error("%s", err)
		return
	}
	if err := auth.Store.WriteToCache(); err != nil {
		output.Error("%s", err)
		return
	}
	output.Success(output.Translate("shell.useradded"), color.New(color.Bold).Sprint(username))
}

// loginMicrosoft runs the Microsoft sign-in flow, adding the account it authenticates.
func (s *shell) loginMicrosoft(deviceCode bool) {
	if deviceCode {
		output.Info(output.Translate("login.code.fetching"))
		code, err := auth.FetchDeviceCode()
		if err != nil {
			output.Error("fetch device code: %s", err)
			return
		}
		output.Info(output.Translate("login.code"), color.BlueString(code.UserCode), color.BlueString(code.VerificationURI))
		session, err := auth.AuthenticateWithCode(code)
		if err != nil {
			output.Error("add account: %s", err)
			return
		}
		output.Success(output.Translate("login.complete"), color.New(color.Bold).Sprint(session.Username))
		return
	}

	url := auth.AuthCodeURL()
	output.Info(output.Translate("login.browser"))
	output.Info(output.Translate("login.url"), url.String())
	if err := browser.OpenURL(url.String()); err != nil {
		output.Tip(output.Translate("shell.open.failed"), url.String())
	}

	session, err := auth.AuthenticateWithRedirect(output.Translate("login.redirect"), output.Translate("login.redirectfail"))
	if err != nil {
		output.Error("add account: %s", err)
		return
	}
	output.Success(output.Translate("login.complete"), color.New(color.Bold).Sprint(session.Username))
}

// delUsersCmd removes an account.
func (s *shell) delUsersCmd(rest []string) {
	accounts := auth.Store.Accounts
	if len(accounts) == 0 {
		output.Info(output.Translate("shell.nousers"))
		return
	}

	query := ""
	assumeYes := false
	for _, arg := range rest {
		flag, _, _ := splitFlag(arg)
		switch {
		case flag == "-y" || flag == "--yes":
			assumeYes = true
		case strings.HasPrefix(flag, "-"):
			output.Error(output.Translate("shell.badflag"), flag)
			return
		case query != "":
			output.Error(output.Translate("shell.unexpectedarg"), flag)
			return
		default:
			query = flag
		}
	}

	index := auth.Store.Current
	if query != "" {
		found := -1
		if n, err := strconv.Atoi(query); err == nil && n >= 0 && n < len(accounts) {
			found = n
		} else {
			for i, account := range accounts {
				if strings.EqualFold(account.Label(), query) {
					found = i
					break
				}
			}
		}
		if found == -1 {
			output.Error(output.Translate("shell.nouser"), query)
			return
		}
		index = found
	}
	if index < 0 || index >= len(accounts) {
		output.Info(output.Translate("shell.noselection"))
		return
	}

	label := accounts[index].Label()
	if !assumeYes && !s.confirm(output.Translate("shell.confirmuser"), color.New(color.Bold).Sprint(label)) {
		output.Info(output.Translate("delete.abort"))
		return
	}

	if err := auth.Store.Remove(index); err != nil {
		output.Error("%s", err)
		return
	}
	if err := auth.Store.WriteToCache(); err != nil {
		output.Error("%s", err)
		return
	}
	output.Success(output.Translate("shell.userremoved"), label)
}

// selectUser switches the active account.
func (s *shell) selectUser() {
	accounts := auth.Store.Accounts
	if len(accounts) == 0 {
		output.Info(output.Translate("shell.nousers"))
		return
	}

	entries := make([]string, 0, len(accounts))
	for _, account := range accounts {
		entries = append(entries, fmt.Sprintf("%s  %s", account.Label(), account.Minecraft.UUID))
	}

	index, ok := s.pick(output.Translate("shell.pickuser"), entries, auth.Store.Current)
	if !ok {
		return
	}
	if err := auth.Store.SetCurrent(index); err != nil {
		output.Error("%s", err)
		return
	}
	if err := auth.Store.WriteToCache(); err != nil {
		output.Error("%s", err)
		return
	}
	output.Success(output.Translate("shell.userchosen"), color.New(color.Bold).Sprint(accounts[index].Label()))
}

// ---------------------------------------------------------------------------
// Selecting with the arrow keys
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Choosing a Java executable
// ---------------------------------------------------------------------------

// javaExecutable is the name of the Java binary on this platform.
func javaExecutable() string {
	if runtime.GOOS == "windows" {
		return "java.exe"
	}
	return "java"
}

// javaCandidates lists every Java executable the launcher can find.
func javaCandidates() []string {
	var found []string
	seen := make(map[string]bool)
	java := javaExecutable()

	add := func(path string) {
		if path == "" || seen[path] {
			return
		}
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			return
		}
		seen[path] = true
		found = append(found, path)
	}

	// Runtimes this launcher downloaded.
	for _, root := range []string{env.JavaDir, filepath.Join(env.RootDir, "runtime")} {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			add(filepath.Join(root, entry.Name(), "bin", java))
		}
	}

	// Common installation roots.
	for _, root := range []string{
		`C:\Program Files\Java`,
		`C:\Program Files\Eclipse Adoptium`,
		`C:\Program Files\Microsoft`,
		`C:\Program Files\Amazon Corretto`,
		`C:\Program Files\Zulu`,
		`F:\java`,
		`D:\java`,
		`E:\java`,
	} {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			add(filepath.Join(root, entry.Name(), "bin", java))
		}
	}

	add(filepath.Join(os.Getenv("JAVA_HOME"), "bin", java))
	return found
}

// selectJava asks which Java executable an instance should use.
func (s *shell) selectJava(rest []string) {
	// An explicit path needs no picker.
	if len(rest) > 0 {
		s.applyJava(strings.Join(rest, " "))
		return
	}

	candidates := javaCandidates()
	entries := []string{output.Translate("shell.java.auto")}
	entries = append(entries, candidates...)
	entries = append(entries, output.Translate("shell.java.manual"))

	index, ok := s.pick(output.Translate("shell.pickjava"), entries, 0)
	if !ok {
		return
	}

	switch {
	case index == 0:
		s.applyJava("")
	case index == len(entries)-1:
		path, changed, err := s.askText(output.Translate("shell.java.manualprompt"), "")
		if err != nil {
			output.Info(output.Translate("shell.cancelled"))
			return
		}
		if !changed {
			return
		}
		s.applyJava(path)
	default:
		s.applyJava(candidates[index-1])
	}
}

// applyJava stores a Java path on the current instance, or in the global settings when no instance
// is selected.
func (s *shell) applyJava(path string) {
	if path != "" {
		if info, err := os.Stat(path); err != nil || info.IsDir() {
			output.Error(output.Translate("shell.java.missing"), path)
			return
		}
	}

	if s.selected == "" {
		global, err := launcher.LoadGlobalSettings()
		if err != nil {
			output.Error("%s", err)
			return
		}
		global.Java = path
		if err := global.Save(); err != nil {
			output.Error("%s", err)
			return
		}
		output.Success(output.Translate("shell.java.setglobal"), displayJava(path))
		return
	}

	inst, err := launcher.FetchInstance(s.selected)
	if err != nil {
		output.Error("%s", err)
		return
	}
	inst.Config.Java = path
	if err := inst.WriteConfig(); err != nil {
		output.Error("%s", err)
		return
	}
	output.Success(output.Translate("shell.java.set"), displayJava(path), inst.Name)
}

// ---------------------------------------------------------------------------
// Version settings
// ---------------------------------------------------------------------------

// cycles describes the choice lists used by the settings menus.
var isoChoices = []string{"follow", "on", "off"}
var memoryChoices = []string{"follow", "auto", "custom"}
var loginChoices = []string{"follow", "online", "offline"}

// choiceLabel renders one of the shared choice values.
func choiceLabel(value string) string {
	if value == "" {
		value = "follow"
	}
	return output.Translate("shell.choice." + value)
}

// choiceIndex returns the position of value in choices.
func choiceIndex(choices []string, value string) int {
	if value == "" {
		value = "follow"
	}
	for i, choice := range choices {
		if choice == value {
			return i
		}
	}
	return 0
}

// cycle shows a nested picker for one of the shared choice lists.
func (s *shell) cycle(title string, choices []string, current string) (string, bool, error) {
	labels := make([]string, 0, len(choices))
	for _, choice := range choices {
		labels = append(labels, choiceLabel(choice))
	}
	index, ok, err := s.askChoice(title, labels, choiceIndex(choices, current))
	if err != nil || !ok {
		return current, false, err
	}
	return choices[index], true, nil
}

// setCmd dispatches the "set" verb.
func (s *shell) setCmd(rest []string) {
	if len(rest) == 0 {
		entry, _ := syntaxOf("set versions")
		s.usage(entry)
		return
	}

	word := strings.ToLower(rest[0])
	switch word {
	case "global", "globals", "shared":
		s.setGlobal()
		return
	}

	switch normalizeObject(word) {
	case "instance":
		var name string
		if len(rest) > 1 {
			name = rest[1]
		}
		s.setVersions(name)
	default:
		output.Error(output.Translate("shell.badobject"), word)
	}
}

// setVersions opens the PCL-style settings page for one instance.
func (s *shell) setVersions(name string) {
	name, ok := s.selectOrPick(name)
	if !ok {
		return
	}
	inst, err := launcher.FetchInstance(name)
	if err != nil {
		output.Error("%s", err)
		return
	}

	config := inst.Config
	if config.VersionIsolation == "" {
		config.VersionIsolation = "follow"
	}
	if config.MemoryMode == "" {
		config.MemoryMode = "follow"
	}
	if config.LoginMode == "" {
		config.LoginMode = "follow"
	}

	// save writes the configuration, and reloads the instance so derived values stay in step.
	save := func() error {
		inst.Config = config
		if err := inst.WriteConfig(); err != nil {
			return err
		}
		if reloaded, err := launcher.FetchInstance(name); err == nil {
			inst = reloaded
		}
		return nil
	}

	title := fmt.Sprintf("%s - %s (%s)", output.Translate("shell.setversions"), name, inst.GameVersion)

	text := func(label string, target *string) func() error {
		return func() error {
			value, changed, err := s.askText(label, *target)
			if err != nil {
				return err
			}
			if !changed {
				output.Info(output.Translate("shell.unchanged"))
				return nil
			}
			*target = value
			return save()
		}
	}

	items := []menuItem{
		{
			// The files of the version are managed from its own page, which is where a PCL user
			// looks for them. The "list mods" family keeps working for scripts.
			label: output.Translate("shell.contentmgr"),
			value: func() string { return s.contentRow(inst) },
			edit: func() error {
				s.contentPage(name)
				return nil
			},
		},
		{
			label: output.Translate("shell.set.description"),
			value: func() string { return orDash(config.Description) },
			edit:  text(output.Translate("shell.set.description"), &config.Description),
		},
		{
			label: output.Translate("shell.set.versionIsolation"),
			value: func() string { return isolationLabel(inst) },
			edit: func() error {
				// The launcher's own layout always keeps game data with the instance.
				if inst.Layout != launcher.LayoutVersions {
					return errors.New(output.Translate("shell.set.isolationna"))
				}
				value, changed, err := s.cycle(output.Translate("shell.set.versionIsolation"), isoChoices, config.VersionIsolation)
				if err != nil || !changed {
					return err
				}
				config.VersionIsolation = value
				return save()
			},
		},
		{
			label: output.Translate("shell.set.windowTitle"),
			value: func() string { return orDash(config.WindowTitle) },
			edit:  text(output.Translate("shell.set.windowTitle"), &config.WindowTitle),
		},
		{
			label: output.Translate("shell.set.customInfo"),
			value: func() string { return orDash(config.CustomInfo) },
			edit:  text(output.Translate("shell.set.customInfo"), &config.CustomInfo),
		},
		{
			label: output.Translate("shell.set.java"),
			value: func() string { return displayJava(config.Java) },
			edit: func() error {
				path, changed, err := s.askText(output.Translate("shell.set.java"), config.Java)
				if err != nil || !changed {
					return err
				}
				config.Java = path
				return save()
			},
		},
		{
			label: output.Translate("shell.set.memory"),
			value: func() string { return describeMemory(config) },
			edit: func() error {
				value, changed, err := s.cycle(output.Translate("shell.set.memory"), memoryChoices, config.MemoryMode)
				if err != nil || !changed {
					return err
				}
				config.MemoryMode = value
				return save()
			},
		},
		{
			label: output.Translate("shell.set.memoryMin"),
			value: func() string { return fmt.Sprintf("%d MB", config.MinMemory) },
			edit: func() error {
				value, changed, err := s.askInt(output.Translate("shell.set.memoryMin"), config.MinMemory)
				if err != nil || !changed {
					return err
				}
				config.MinMemory, config.MemoryMode = value, "custom"
				return save()
			},
		},
		{
			label: output.Translate("shell.set.memoryMax"),
			value: func() string { return fmt.Sprintf("%d MB", config.MaxMemory) },
			edit: func() error {
				value, changed, err := s.askInt(output.Translate("shell.set.memoryMax"), config.MaxMemory)
				if err != nil || !changed {
					return err
				}
				config.MaxMemory, config.MemoryMode = value, "custom"
				return save()
			},
		},
		{
			label: output.Translate("shell.set.autoServer"),
			value: func() string { return orDash(config.AutoJoinServer) },
			edit:  text(output.Translate("shell.set.autoServer"), &config.AutoJoinServer),
		},
		{
			label: output.Translate("shell.set.loginMode"),
			value: func() string { return choiceLabel(config.LoginMode) },
			edit: func() error {
				value, changed, err := s.cycle(output.Translate("shell.set.loginMode"), loginChoices, config.LoginMode)
				if err != nil || !changed {
					return err
				}
				config.LoginMode = value
				return save()
			},
		},
		{
			label: output.Translate("shell.set.offlineUser"),
			value: func() string { return orDash(config.OfflineUsername) },
			edit:  text(output.Translate("shell.set.offlineUser"), &config.OfflineUsername),
		},
		{
			label: output.Translate("shell.set.resolution"),
			value: func() string {
				return fmt.Sprintf("%dx%d", config.WindowResolution.Width, config.WindowResolution.Height)
			},
			edit: func() error {
				answer, changed, err := s.askText(output.Translate("shell.set.resolution"),
					fmt.Sprintf("%d %d", config.WindowResolution.Width, config.WindowResolution.Height))
				if err != nil || !changed {
					return err
				}
				width, height, ok := parseResolution(answer)
				if !ok {
					return fmt.Errorf(output.Translate("shell.badresolution"), answer)
				}
				config.WindowResolution = launcher.Resolution{Width: width, Height: height}
				return save()
			},
		},
		{
			label: output.Translate("shell.set.icon"),
			value: func() string { return orDash(config.Icon) },
			edit:  text(output.Translate("shell.set.icon"), &config.Icon),
		},
		{
			label: output.Translate("shell.set.category"),
			value: func() string { return orDash(config.Category) },
			edit:  text(output.Translate("shell.set.category"), &config.Category),
		},
		{
			label: output.Translate("shell.set.favorite"),
			value: func() string { return yesNo(config.Favorite) },
			edit: func() error {
				config.Favorite = !config.Favorite
				return save()
			},
		},
		{
			label: output.Translate("shell.set.customJar"),
			value: func() string { return orDash(config.CustomJar) },
			edit:  text(output.Translate("shell.set.customJar"), &config.CustomJar),
		},
		{
			label: output.Translate("shell.set.javaArgs"),
			value: func() string { return orDash(config.JavaArgs) },
			edit:  text(output.Translate("shell.set.javaArgs"), &config.JavaArgs),
		},
	}

	s.menu(title, items)
	output.Success(output.Translate("shell.set.saved"), inst.ConfigPath())
}

// setGlobal opens the settings page shared by every instance.
func (s *shell) setGlobal() {
	global, err := launcher.LoadGlobalSettings()
	if err != nil {
		output.Error("%s", err)
		return
	}

	save := func() error { return global.Save() }

	text := func(label string, target *string) func() error {
		return func() error {
			value, changed, err := s.askText(label, *target)
			if err != nil {
				return err
			}
			if !changed {
				output.Info(output.Translate("shell.unchanged"))
				return nil
			}
			*target = value
			return save()
		}
	}

	items := []menuItem{
		{
			// The language comes first: it is the one setting that changes how the rest of this
			// page reads, and the one a user who cannot read the current language needs to find.
			label: output.Translate("shell.set.language"),
			value: func() string { return output.LanguageName(global.Language) },
			edit: func() error {
				code, changed, err := s.pickLanguage(global.Language)
				if err != nil || !changed {
					return err
				}
				global.Language = code
				if err := save(); err != nil {
					return err
				}
				s.applyLanguage(code)
				return nil
			},
		},
		{
			label: output.Translate("shell.set.java"),
			value: func() string { return displayJava(global.Java) },
			edit:  text(output.Translate("shell.set.java"), &global.Java),
		},
		{
			label: output.Translate("shell.set.javaArgs"),
			value: func() string { return orDash(global.JavaArgs) },
			edit:  text(output.Translate("shell.set.javaArgs"), &global.JavaArgs),
		},
		{
			label: output.Translate("shell.set.memory"),
			value: func() string {
				return fmt.Sprintf("%s (%d-%d MB)", choiceLabel(global.MemoryMode), global.MinMemory, global.MaxMemory)
			},
			edit: func() error {
				value, changed, err := s.cycle(output.Translate("shell.set.memory"), []string{"auto", "custom"}, global.MemoryMode)
				if err != nil || !changed {
					return err
				}
				global.MemoryMode = value
				return save()
			},
		},
		{
			label: output.Translate("shell.set.memoryMin"),
			value: func() string { return fmt.Sprintf("%d MB", global.MinMemory) },
			edit: func() error {
				value, changed, err := s.askInt(output.Translate("shell.set.memoryMin"), global.MinMemory)
				if err != nil || !changed {
					return err
				}
				global.MinMemory = value
				return save()
			},
		},
		{
			label: output.Translate("shell.set.memoryMax"),
			value: func() string { return fmt.Sprintf("%d MB", global.MaxMemory) },
			edit: func() error {
				value, changed, err := s.askInt(output.Translate("shell.set.memoryMax"), global.MaxMemory)
				if err != nil || !changed {
					return err
				}
				global.MaxMemory = value
				return save()
			},
		},
		{
			label: output.Translate("shell.set.windowTitle"),
			value: func() string { return orDash(global.WindowTitle) },
			edit:  text(output.Translate("shell.set.windowTitle"), &global.WindowTitle),
		},
		{
			label: output.Translate("shell.set.customInfo"),
			value: func() string { return orDash(global.CustomInfo) },
			edit:  text(output.Translate("shell.set.customInfo"), &global.CustomInfo),
		},
		{
			label: output.Translate("shell.set.versionIsolation"),
			value: func() string {
				if global.VersionIsolation == "" {
					return output.Translate("shell.choice.detect")
				}
				return choiceLabel(global.VersionIsolation)
			},
			edit: func() error {
				value, changed, err := s.cycle(output.Translate("shell.set.versionIsolation"), []string{"detect", "on", "off"}, global.VersionIsolation)
				if err != nil || !changed {
					return err
				}
				if value == "detect" {
					value = ""
				}
				global.VersionIsolation = value
				return save()
			},
		},
		{
			label: output.Translate("shell.set.loginMode"),
			value: func() string { return choiceLabel(global.LoginMode) },
			edit: func() error {
				value, changed, err := s.cycle(output.Translate("shell.set.loginMode"), []string{"online", "offline"}, global.LoginMode)
				if err != nil || !changed {
					return err
				}
				global.LoginMode = value
				return save()
			},
		},
		{
			label: output.Translate("shell.set.offlineUser"),
			value: func() string { return orDash(global.OfflineUsername) },
			edit:  text(output.Translate("shell.set.offlineUser"), &global.OfflineUsername),
		},
		{
			label: output.Translate("shell.set.resolution"),
			value: func() string { return fmt.Sprintf("%dx%d", global.Width, global.Height) },
			edit: func() error {
				answer, changed, err := s.askText(output.Translate("shell.set.resolution"), fmt.Sprintf("%d %d", global.Width, global.Height))
				if err != nil || !changed {
					return err
				}
				width, height, ok := parseResolution(answer)
				if !ok {
					return fmt.Errorf(output.Translate("shell.badresolution"), answer)
				}
				global.Width, global.Height = width, height
				return save()
			},
		},
	}

	s.menu(output.Translate("shell.setglobal"), items)
	output.Success(output.Translate("shell.set.saved"), launcher.GlobalSettingsPath())
}

// ---------------------------------------------------------------------------
// The "settings" command
// ---------------------------------------------------------------------------

// pickLanguage shows the language menu, returning the code of the chosen language.
func (s *shell) pickLanguage(inUse string) (string, bool, error) {
	languages := output.Languages()
	entries := make([]string, 0, len(languages))
	initial := 0
	for i, candidate := range languages {
		entries = append(entries, output.LanguageName(candidate.Code))
		if candidate.Code == inUse {
			initial = i
		}
	}

	index, ok, err := s.askChoice(output.Translate("shell.set.language"), entries, initial)
	if err != nil || !ok {
		return inUse, false, err
	}
	return languages[index].Code, true, nil
}

// applyLanguage switches the output language and refreshes the wording that was resolved once, at
// start-up. Everything printed afterwards, including the menu this was called from, uses the new
// language.
func (s *shell) applyLanguage(code string) {
	output.SetLangCode(code)
	refreshLabels()
	output.Success(output.Translate("shell.lang.changed"), color.New(color.Bold).Sprint(output.LanguageName(code)))
}

// A globalSetting is one option the "settings" command accepts on the command line.
type globalSetting struct {
	key   string   // Canonical name, used when a setting needs special treatment
	names []string // Spellings accepted when typing
	label string   // Translation key of its label, shared with the settings menu
	arg   string   // What a value looks like, shown in the usage hint
	value func(launcher.GlobalSettings) string
	set   func(*launcher.GlobalSettings, string) error
}

// languageArg renders the accepted values of the language setting, from the language registry, so
// that new languages show up without touching this table.
var languageArg = func() string {
	codes := []string{"system"}
	for _, candidate := range output.Languages() {
		if candidate.Code != "" {
			codes = append(codes, candidate.Code)
		}
	}
	return strings.Join(codes, "|")
}()

// globalSettings lists the settings the command line can change, in the order they are offered by
// the settings menu.
var globalSettings = []globalSetting{
	{
		key:   "language",
		names: []string{"language", "lang"},
		label: "shell.set.language",
		arg:   languageArg,
		value: func(global launcher.GlobalSettings) string { return output.LanguageName(global.Language) },
		set: func(global *launcher.GlobalSettings, value string) error {
			code, ok := languageCode(value)
			if !ok {
				return fmt.Errorf(output.Translate("shell.settings.badvalue"), value, output.Translate("shell.set.language"))
			}
			global.Language = code
			return nil
		},
	},
	{
		key:   "java",
		names: []string{"java", "jvm"},
		label: "shell.set.java",
		arg:   "<path>",
		value: func(global launcher.GlobalSettings) string { return displayJava(global.Java) },
		set: func(global *launcher.GlobalSettings, value string) error {
			value = textValue(value)
			if value != "" {
				if info, err := os.Stat(value); err != nil || info.IsDir() {
					return fmt.Errorf(output.Translate("shell.java.missing"), value)
				}
			}
			global.Java = value
			return nil
		},
	},
	{
		key:   "java-args",
		names: []string{"java-args", "javaargs", "jvm-args"},
		label: "shell.set.javaArgs",
		arg:   "<args>",
		value: func(global launcher.GlobalSettings) string { return orDash(global.JavaArgs) },
		set: func(global *launcher.GlobalSettings, value string) error {
			global.JavaArgs = textValue(value)
			return nil
		},
	},
	{
		key:   "memory",
		names: []string{"memory"},
		label: "shell.set.memory",
		arg:   "auto|custom",
		value: func(global launcher.GlobalSettings) string {
			return fmt.Sprintf("%s (%d-%d MB)", choiceLabel(global.MemoryMode), global.MinMemory, global.MaxMemory)
		},
		set: func(global *launcher.GlobalSettings, value string) error {
			switch strings.ToLower(value) {
			case "auto", "custom":
				global.MemoryMode = strings.ToLower(value)
				return nil
			}
			return fmt.Errorf(output.Translate("shell.settings.badvalue"), value, output.Translate("shell.set.memory"))
		},
	},
	{
		key:   "memory-min",
		names: []string{"memory-min", "min-memory"},
		label: "shell.set.memoryMin",
		arg:   "<MB>",
		value: func(global launcher.GlobalSettings) string { return fmt.Sprintf("%d MB", global.MinMemory) },
		set: func(global *launcher.GlobalSettings, value string) error {
			mb, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf(output.Translate("shell.badnumber"), value)
			}
			global.MinMemory = mb
			return nil
		},
	},
	{
		key:   "memory-max",
		names: []string{"memory-max", "max-memory"},
		label: "shell.set.memoryMax",
		arg:   "<MB>",
		value: func(global launcher.GlobalSettings) string { return fmt.Sprintf("%d MB", global.MaxMemory) },
		set: func(global *launcher.GlobalSettings, value string) error {
			mb, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf(output.Translate("shell.badnumber"), value)
			}
			global.MaxMemory = mb
			return nil
		},
	},
	{
		key:   "download-threads",
		names: []string{"download-threads", "threads", "parallel-downloads"},
		label: "shell.set.downloadThreads",
		arg:   "<count>",
		value: func(global launcher.GlobalSettings) string {
			if global.DownloadThreads < 1 {
				return fmt.Sprintf(output.Translate("shell.set.threadsAuto"), network.DefaultMaxConcurrentDownloads)
			}
			return fmt.Sprintf("%d", global.DownloadThreads)
		},
		set: func(global *launcher.GlobalSettings, value string) error {
			threads := 0
			switch strings.ToLower(strings.TrimSpace(value)) {
			case "", "auto", "default", "system":
				// Stored as zero, so the built-in default stays a moving target even for an
				// existing settings file.
				threads = 0
			default:
				count, err := strconv.Atoi(value)
				if err != nil || count < 0 {
					return fmt.Errorf(output.Translate("shell.settings.badvalue"), value, output.Translate("shell.set.downloadThreads"))
				}
				threads = count
			}
			global.DownloadThreads = threads
			network.SetMaxConcurrentDownloads(threads)
			return nil
		},
	},
	{
		key:   "window-title",
		names: []string{"window-title", "title"},
		label: "shell.set.windowTitle",
		arg:   "<text>",
		value: func(global launcher.GlobalSettings) string { return orDash(global.WindowTitle) },
		set: func(global *launcher.GlobalSettings, value string) error {
			global.WindowTitle = textValue(value)
			return nil
		},
	},
	{
		key:   "custom-info",
		names: []string{"custom-info", "info"},
		label: "shell.set.customInfo",
		arg:   "<text>",
		value: func(global launcher.GlobalSettings) string { return orDash(global.CustomInfo) },
		set: func(global *launcher.GlobalSettings, value string) error {
			global.CustomInfo = textValue(value)
			return nil
		},
	},
	{
		key:   "version-isolation",
		names: []string{"version-isolation", "isolation"},
		label: "shell.set.versionIsolation",
		arg:   "detect|on|off",
		value: func(global launcher.GlobalSettings) string {
			if global.VersionIsolation == "" {
				return output.Translate("shell.choice.detect")
			}
			return choiceLabel(global.VersionIsolation)
		},
		set: func(global *launcher.GlobalSettings, value string) error {
			switch strings.ToLower(value) {
			case "detect", "auto":
				global.VersionIsolation = ""
				return nil
			case "on", "off":
				global.VersionIsolation = strings.ToLower(value)
				return nil
			}
			return fmt.Errorf(output.Translate("shell.settings.badvalue"), value, output.Translate("shell.set.versionIsolation"))
		},
	},
	{
		key:   "login",
		names: []string{"login", "login-mode"},
		label: "shell.set.loginMode",
		arg:   "online|offline",
		value: func(global launcher.GlobalSettings) string { return choiceLabel(global.LoginMode) },
		set: func(global *launcher.GlobalSettings, value string) error {
			switch strings.ToLower(value) {
			case "online", "offline":
				global.LoginMode = strings.ToLower(value)
				return nil
			}
			return fmt.Errorf(output.Translate("shell.settings.badvalue"), value, output.Translate("shell.set.loginMode"))
		},
	},
	{
		key:   "offline-user",
		names: []string{"offline-user", "offline-username"},
		label: "shell.set.offlineUser",
		arg:   "<name>",
		value: func(global launcher.GlobalSettings) string { return orDash(global.OfflineUsername) },
		set: func(global *launcher.GlobalSettings, value string) error {
			global.OfflineUsername = textValue(value)
			return nil
		},
	},
	{
		key:   "resolution",
		names: []string{"resolution", "window-size"},
		label: "shell.set.resolution",
		arg:   "<1920x1080>",
		value: func(global launcher.GlobalSettings) string { return fmt.Sprintf("%dx%d", global.Width, global.Height) },
		set: func(global *launcher.GlobalSettings, value string) error {
			width, height, ok := parseResolution(value)
			if !ok {
				return fmt.Errorf(output.Translate("shell.badresolution"), value)
			}
			global.Width, global.Height = width, height
			return nil
		},
	},
}

// languageCode maps a typed language onto the code kept in the settings file. An empty code means
// "follow the system language".
// languageAliases maps the spellings of every language that are neither its code nor its menu
// name - the English and Chinese names a user is likely to type instead.
var languageAliases = map[string]string{
	"chinese":            "zh",
	"simplified chinese": "zh",
	"中文":                 "zh",
	"汉语":                 "zh",
	"简体":                 "zh",
	"english":            "en",
	"french":             "fr",
	"français":           "fr",
	"russian":            "ru",
	"русский":            "ru",
	"spanish":            "es",
	"español":            "es",
	"german":             "de",
	"deutsch":            "de",
}

// languageCode resolves a typed value to the code of a selectable language. It accepts the empty
// value and its "follow the system" spellings, the code itself, the name shown in the menu, and
// the common aliases above. It reports false when nothing matches.
func languageCode(value string) (string, bool) {
	switch strings.ToLower(value) {
	case "", "system", "auto", "follow", "default":
		return "", true
	}
	if code, ok := languageAliases[strings.ToLower(value)]; ok {
		return code, true
	}
	for _, candidate := range output.Languages() {
		if candidate.Code == "" {
			continue
		}
		if strings.EqualFold(value, candidate.Code) || strings.EqualFold(value, candidate.Name) {
			return candidate.Code, true
		}
	}
	return "", false
}

// textValue maps a lone dash onto an empty value, the same way the prompts clear a setting.
func textValue(value string) string {
	if value == "-" {
		return ""
	}
	return value
}

// findGlobalSetting looks an option up by any of its accepted spellings.
func findGlobalSetting(name string) (globalSetting, bool) {
	for _, entry := range globalSettings {
		for _, candidate := range entry.names {
			if strings.EqualFold(name, candidate) {
				return entry, true
			}
		}
	}
	return globalSetting{}, false
}

// globalSettingNames lists the canonical spelling of every option, in menu order.
func globalSettingNames() string {
	names := make([]string, 0, len(globalSettings))
	for _, entry := range globalSettings {
		names = append(names, entry.names[0])
	}
	return strings.Join(names, " | ")
}

// settingsCmd dispatches the "settings" verb.
//
// On its own it opens the same page as "set global". With an option it reads or writes that one
// setting, so that a single value can be changed without walking the menu:
//
//	settings language zh
//	settings memory-max 8192
func (s *shell) settingsCmd(rest []string) {
	if len(rest) == 0 {
		s.setGlobal()
		return
	}

	entry, ok := findGlobalSetting(rest[0])
	if !ok {
		output.Error(output.Translate("shell.settings.unknown"), rest[0])
		output.Info(output.Translate("shell.settings.list"), globalSettingNames())
		return
	}

	global, err := launcher.LoadGlobalSettings()
	if err != nil {
		output.Error("%s", err)
		return
	}

	label := output.Translate(entry.label)
	if len(rest) == 1 {
		// Without a value the option is only reported, together with how to set it.
		output.Info(output.Translate("shell.settings.show"), label, entry.value(global))
		output.Info(output.Translate("shell.usage"), "settings "+entry.names[0]+" "+entry.arg)
		return
	}

	if err := entry.set(&global, strings.Join(rest[1:], " ")); err != nil {
		output.Error("%s", err)
		return
	}
	if err := global.Save(); err != nil {
		output.Error("%s", err)
		return
	}
	if entry.key == "language" {
		// The language switch would otherwise be reported in the language that was just replaced.
		s.applyLanguage(global.Language)
		return
	}
	output.Success(output.Translate("shell.set.saved"), launcher.GlobalSettingsPath())
}

// parseResolution accepts "1920 1080" or "1920x1080".
func parseResolution(answer string) (int, int, bool) {
	fields := strings.FieldsFunc(answer, func(r rune) bool {
		return r == 'x' || r == 'X' || r == ' ' || r == '*' || r == ','
	})
	if len(fields) != 2 {
		return 0, 0, false
	}
	width, err := strconv.Atoi(fields[0])
	if err != nil {
		return 0, 0, false
	}
	height, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, 0, false
	}
	return width, height, true
}

// orDash renders an empty value for display.
func orDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}
