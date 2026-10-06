package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/fatih/color"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/qwertasd501/Terminalauncher/internal/cli/output"
	"github.com/qwertasd501/Terminalauncher/internal/concurrent"
	"github.com/qwertasd501/Terminalauncher/internal/meta"
	"github.com/qwertasd501/Terminalauncher/internal/network"
	"github.com/qwertasd501/Terminalauncher/pkg/launcher"
)

// The shell manages the four categories of file an instance holds — mods, resource packs, shader
// packs and data packs — through one command family, plus modpacks, which install as a whole
// instance. The set mirrors PCL's content pages: list, select, enable, disable, delete, rename,
// import, update, download and open.

// contentKindOf maps a canonical object name onto a content kind.
func contentKindOf(object string) (launcher.ContentKind, bool) {
	for _, kind := range launcher.ContentKinds {
		if string(kind) == object {
			return kind, true
		}
	}
	return "", false
}

// contentLabel is the translated name of a category, for messages.
func contentLabel(kind launcher.ContentKind) string {
	return output.Translate("shell.content." + string(kind))
}

// A contentTarget is the instance and directory a content command works on.
type contentTarget struct {
	inst  launcher.Instance
	kind  launcher.ContentKind
	dir   string
	world string
}

// contentTarget resolves the instance and directory for a category, for the shell's selection.
func (s *shell) contentTarget(kind launcher.ContentKind, world string, create bool) (contentTarget, bool) {
	name, ok := s.selectOrPick("")
	if !ok {
		return contentTarget{}, false
	}
	return s.contentTargetFor(name, kind, world, create)
}

// contentTargetFor resolves the instance and directory for a category, for the instance named.
//
// The version page uses this rather than contentTarget: the files it shows belong to the version it
// was opened for, whatever the shell's selection happens to be.
//
// Data packs live inside a world, so a world is resolved (and remembered) for them before the
// directory can be built. The directory is created on demand, because an instance that never had a
// mod has no mods folder yet.
func (s *shell) contentTargetFor(name string, kind launcher.ContentKind, world string, create bool) (contentTarget, bool) {
	inst, err := launcher.FetchInstance(name)
	if err != nil {
		output.Error("%s", err)
		return contentTarget{}, false
	}

	target := contentTarget{inst: inst, kind: kind}
	if kind == launcher.ContentDataPacks {
		world, ok := s.contentWorldFor(inst, world)
		if !ok {
			return contentTarget{}, false
		}
		target.world = world
	}

	dir, err := launcher.ContentDir(inst, kind, world)
	if err != nil {
		output.Error("%s", err)
		return contentTarget{}, false
	}
	if create {
		if err := os.MkdirAll(dir, 0755); err != nil {
			output.Error("%s", err)
			return contentTarget{}, false
		}
	}
	target.dir = dir
	return target, true
}

// contentWorldFor resolves the world a data pack command applies to.
//
// The choice is remembered on the shell, so that a series of data pack commands keeps working on
// the same world without repeating -w.
func (s *shell) contentWorldFor(inst launcher.Instance, world string) (string, bool) {
	worlds, err := launcher.Worlds(inst)
	if err != nil {
		output.Error("%s", err)
		return "", false
	}

	if world != "" {
		for _, candidate := range worlds {
			if strings.EqualFold(candidate, world) {
				s.contentWorld = candidate
				return candidate, true
			}
		}
		output.Error(output.Translate("shell.world.unknown"), world)
		return "", false
	}

	if s.contentWorld != "" {
		for _, candidate := range worlds {
			if candidate == s.contentWorld {
				return candidate, true
			}
		}
	}

	switch len(worlds) {
	case 0:
		output.Error(output.Translate("shell.world.none"))
		return "", false
	case 1:
		s.contentWorld = worlds[0]
		return worlds[0], true
	}

	index, ok := s.pick(output.Translate("shell.world.pick"), worlds, 0)
	if !ok {
		return "", false
	}
	s.contentWorld = worlds[index]
	return worlds[index], true
}

// contentItems lists a directory, reporting read failures as they are found.
func (s *shell) contentItems(target contentTarget) []launcher.ContentItem {
	items, err := launcher.ListContent(target.dir, target.kind)
	if err != nil {
		output.Error("%s", err)
		return nil
	}
	return items
}

// trimDisabled removes the ".disabled" marker from a file name.
func trimDisabled(name string) string {
	if strings.HasSuffix(strings.ToLower(name), launcher.DisabledSuffix) {
		return name[:len(name)-len(launcher.DisabledSuffix)]
	}
	return name
}

// contentEntry renders one item as a line of the picker.
//
// A mod built for another loader is flagged here, which is how a PCL user spots the jar that keeps
// the game from starting.
func contentEntry(inst launcher.Instance, item launcher.ContentItem) string {
	state := output.Translate("shell.content.disabled")
	if item.Enabled {
		state = output.Translate("shell.content.enabled")
	}
	if reason, ok := contentConflict(inst, item); ok {
		state += " (" + reason + ")"
	}

	version := item.Version
	if version == "" {
		version = output.Translate("shell.na")
	}
	return fmt.Sprintf("%-38s %-22s %s", truncate(item.DisplayName(), 38), state, version)
}

// contentConflict reports why an item cannot be used by an instance.
func contentConflict(inst launcher.Instance, item launcher.ContentItem) (string, bool) {
	if item.Kind != launcher.ContentMods {
		return "", false
	}

	// Only a descriptor that names a loader can conflict with the instance's own. A jar we could
	// not identify, and a resource or shader pack that ended up in the mods folder, all say nothing
	// about the loader and must not be reported as being "for another" one.
	switch item.Source {
	case "", "jar", "pack", "shader":
		return "", false
	}

	switch item.Source {
	case "fabric":
		if inst.Loader == meta.LoaderFabric || inst.Loader == meta.LoaderQuilt {
			return "", false
		}
	case "quilt":
		if inst.Loader == meta.LoaderQuilt {
			return "", false
		}
	case "forge":
		if inst.Loader == meta.LoaderForge || inst.Loader == meta.LoaderNeoForge {
			return "", false
		}
	case "neoforge":
		if inst.Loader == meta.LoaderNeoForge {
			return "", false
		}
	case "legacy":
		// The 1.12-era format only ever ran on Forge.
		if inst.Loader == meta.LoaderForge && strings.HasPrefix(inst.GameVersion, "1.12") {
			return "", false
		}
	}
	// The reason ends up in a table cell, so it has to be rendered into a plain string here.
	reason := item.Source
	if reason == "" {
		reason = "?"
	}
	return fmt.Sprintf(output.Translate("shell.content.wrongloader.short"), reason+" -> "+string(inst.Loader)), true
}

// brokenReason renders why a file could not be read.
func brokenReason(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf(output.Translate("shell.content.broken"), err.Error())
}

// truncate shortens a string for a fixed-width column.
func truncate(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	if limit <= 3 {
		return string(runes[:limit])
	}
	return string(runes[:limit-3]) + "..."
}

// pickContent asks the user to select one item with the arrow keys.
func (s *shell) pickContent(inst launcher.Instance, kind launcher.ContentKind, items []launcher.ContentItem, initial int) (int, bool) {
	entries := make([]string, 0, len(items))
	for _, item := range items {
		entries = append(entries, contentEntry(inst, item))
	}
	return s.pick(fmt.Sprintf(output.Translate("shell.content.pick"), contentLabel(kind)), entries, initial)
}

// findContent finds an item by file name or display name, or returns the current selection.
// partialContentMatches returns every item whose file name or display name contains name, ignoring
// case. It backs the fallback that lets "info mods sodium" reach a mod whose display name is longer
// than the word typed.
func partialContentMatches(items []launcher.ContentItem, name string) []launcher.ContentItem {
	needle := strings.ToLower(name)
	var matches []launcher.ContentItem
	for _, item := range items {
		if strings.Contains(strings.ToLower(item.DisplayName()), needle) || strings.Contains(strings.ToLower(item.FileName), needle) {
			matches = append(matches, item)
		}
	}
	return matches
}

func findContent(items []launcher.ContentItem, name, selected string) (launcher.ContentItem, bool) {
	if name == "" {
		name = selected
	}
	if name == "" {
		return launcher.ContentItem{}, false
	}

	// An exact file name, with or without the disabled marker, always wins.
	for _, item := range items {
		if strings.EqualFold(item.FileName, name) || strings.EqualFold(trimDisabled(item.FileName), name) {
			return item, true
		}
	}
	for _, item := range items {
		if strings.EqualFold(item.DisplayName(), name) {
			return item, true
		}
	}
	// A unique partial match is accepted too; an ambiguous one is reported by the caller instead of
	// being guessed at.
	if matches := partialContentMatches(items, name); len(matches) == 1 {
		return matches[0], true
	}
	return launcher.ContentItem{}, false
}

// resolveContent resolves the item a command works on, falling back to the selection and then to
// the arrow key picker.
func (s *shell) resolveContent(target contentTarget, items []launcher.ContentItem, name string) (launcher.ContentItem, bool) {
	if len(items) == 0 {
		output.Info(output.Translate("shell.content.none"), contentLabel(target.kind), target.dir)
		return launcher.ContentItem{}, false
	}

	if item, ok := findContent(items, name, s.selectedContent); ok {
		return item, true
	}
	if name != "" {
		if len(partialContentMatches(items, name)) > 1 {
			output.Error(output.Translate("shell.content.ambiguous"), contentLabel(target.kind), name)
			return launcher.ContentItem{}, false
		}
		output.Error(output.Translate("shell.content.notexist"), contentLabel(target.kind), name)
		return launcher.ContentItem{}, false
	}

	initial := 0
	for i, item := range items {
		if item.FileName == s.selectedContent {
			initial = i
		}
	}
	index, ok := s.pickContent(target.inst, target.kind, items, initial)
	if !ok {
		return launcher.ContentItem{}, false
	}
	s.selectedContent = items[index].FileName
	return items[index], true
}

// ---------------------------------------------------------------------------
// Listing, selecting and describing
// ---------------------------------------------------------------------------

// contentArgs parses the flags shared by the content commands, so that every one of them strips
// them from the positional arguments instead of treating "-w World1" as a search term.
//
// It returns the world given with -w (empty when absent), whether -y was given, and the remaining
// positional arguments. ok is false when a flag was unknown or lacked its value; the error has
// already been reported in that case.
func contentArgs(rest []string, allowWorld, allowYes bool) (world string, yes bool, args []string, ok bool) {
	for i := 0; i < len(rest); i++ {
		flag, inline, hasInline := splitFlag(rest[i])
		switch {
		case allowWorld && (flag == "-w" || flag == "--world"):
			value, err := flagValue(inline, hasInline, rest, &i)
			if err != nil {
				output.Error(output.Translate("shell.missingvalue"), flag)
				return "", false, nil, false
			}
			world = value
		case allowYes && (flag == "-y" || flag == "--yes"):
			yes = true
		case strings.HasPrefix(flag, "-") && flag != "-":
			output.Error(output.Translate("shell.badflag"), flag)
			return "", false, nil, false
		default:
			args = append(args, flag)
		}
	}
	return world, yes, args, true
}

// listContentCmd prints the contents of one category.
func (s *shell) listContentCmd(kind launcher.ContentKind, rest []string) {
	world, _, args, ok := contentArgs(rest, true, false)
	if !ok {
		return
	}
	target, ok := s.contentTarget(kind, world, false)
	if !ok {
		return
	}
	items := s.contentItems(target)

	query := strings.ToLower(strings.Join(args, " "))
	if query != "" {
		filtered := items[:0:0]
		for _, item := range items {
			if strings.Contains(strings.ToLower(item.DisplayName()), query) || strings.Contains(strings.ToLower(item.FileName), query) {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}

	if len(items) == 0 {
		output.Info(output.Translate("shell.content.none"), contentLabel(kind), target.dir)
		return
	}

	active := 0
	t := table.NewWriter()
	t.SetStyle(table.StyleLight)
	t.SetOutputMirror(os.Stdout)
	t.AppendHeader(table.Row{
		"",
		"#",
		output.Translate("shell.content.header.name"),
		output.Translate("shell.content.header.version"),
		output.Translate("shell.content.header.loader"),
		output.Translate("shell.content.header.state"),
		output.Translate("shell.content.header.size"),
	})
	for i, item := range items {
		mark := ""
		if item.FileName == s.selectedContent {
			mark = "*"
		}
		state := output.Translate("shell.content.disabled")
		if item.Enabled {
			state = output.Translate("shell.content.enabled")
			active++
		}
		if reason, conflict := contentConflict(target.inst, item); conflict {
			state = reason
		}
		if item.Err != nil {
			t.AppendRow(table.Row{mark, i, item.DisplayName(), item.Version, item.Source, brokenReason(item.Err), humanSize(item.Size)})
			continue
		}
		t.AppendRow(table.Row{mark, i, item.DisplayName(), item.Version, item.Source, state, humanSize(item.Size)})
	}
	t.Render()

	output.Info(output.Translate("shell.content.count"), active, len(items))
	if kind == launcher.ContentDataPacks {
		output.Info(output.Translate("shell.content.worlddir"), target.world, target.dir)
	}
}

// humanSize renders a file size the way a file manager does.
func humanSize(size int64) string {
	if size <= 0 {
		return "-"
	}
	units := []string{"B", "KiB", "MiB", "GiB"}
	value := float64(size)
	unit := 0
	for value >= 1024 && unit < len(units)-1 {
		value /= 1024
		unit++
	}
	if unit == 0 {
		return fmt.Sprintf("%d %s", size, units[0])
	}
	return fmt.Sprintf("%.1f %s", value, units[unit])
}

// selectContentCmd makes an item the default of the content commands.
func (s *shell) selectContentCmd(kind launcher.ContentKind, rest []string) {
	world, _, args, ok := contentArgs(rest, true, false)
	if !ok {
		return
	}
	target, ok := s.contentTarget(kind, world, false)
	if !ok {
		return
	}
	items := s.contentItems(target)

	name := strings.Join(args, " ")
	item, ok := s.resolveContent(target, items, name)
	if !ok {
		return
	}
	s.selectedContent = item.FileName
	output.Success(output.Translate("shell.content.selected"), color.New(color.Bold).Sprint(item.DisplayName()))
}

// infoContentCmd describes one mod or pack in full, the way PCL's detail pane does.
func (s *shell) infoContentCmd(kind launcher.ContentKind, rest []string) {
	world, _, args, ok := contentArgs(rest, true, false)
	if !ok {
		return
	}

	target, ok := s.contentTarget(kind, world, false)
	if !ok {
		return
	}
	items := s.contentItems(target)
	item, ok := s.resolveContent(target, items, strings.Join(args, " "))
	if !ok {
		return
	}
	s.describeContent(target, item)
}

// describeContent prints everything known about one file. It is shared by the "info" command and by
// the version page, which shows it from the actions of a file.
func (s *shell) describeContent(target contentTarget, item launcher.ContentItem) {
	state := output.Translate("shell.content.disabled")
	if item.Enabled {
		state = output.Translate("shell.content.enabled")
	}
	if reason, conflict := contentConflict(target.inst, item); conflict {
		state = reason
	}

	rows := []table.Row{
		{output.Translate("shell.content.header.name"), item.DisplayName()},
		{output.Translate("shell.content.header.file"), item.FileName},
		{output.Translate("shell.content.header.state"), state},
		{output.Translate("shell.content.header.size"), humanSize(item.Size)},
		{output.Translate("shell.info.dir"), item.Path},
	}
	if item.ID != "" {
		rows = append(rows, table.Row{output.Translate("shell.content.header.id"), item.ID})
	}
	if item.Version != "" {
		rows = append(rows, table.Row{output.Translate("shell.content.header.version"), item.Version})
	}
	if item.Source != "" {
		rows = append(rows, table.Row{output.Translate("shell.content.header.loader"), item.Source})
	}
	if len(item.Authors) > 0 {
		rows = append(rows, table.Row{output.Translate("shell.content.authors"), strings.Join(item.Authors, ", ")})
	}
	if item.PackFormat > 0 {
		rows = append(rows, table.Row{output.Translate("shell.content.packformat"), packFormatLabel(item.PackFormat)})
	}
	if item.Description != "" {
		rows = append(rows, table.Row{output.Translate("shell.set.description"), item.Description})
	}
	if len(item.Depends) > 0 {
		names := make([]string, 0, len(item.Depends))
		for id, version := range item.Depends {
			if version == "" {
				names = append(names, id)
				continue
			}
			names = append(names, id+" "+version)
		}
		sort.Strings(names)
		rows = append(rows, table.Row{output.Translate("shell.content.depends"), strings.Join(names, ", ")})
	}
	if item.Err != nil {
		rows = append(rows, table.Row{output.Translate("shell.content.header.state"), brokenReason(item.Err)})
	}

	t := table.NewWriter()
	t.SetStyle(table.StyleLight)
	t.SetOutputMirror(os.Stdout)
	t.AppendRows(rows)
	t.Render()
}

// packFormats maps a resource or data pack format onto the game versions that use it.
//
// Only the formats that are widely documented are listed; anything newer shows its raw number
// rather than a guess.
var packFormats = map[int]string{
	1:  "1.6.1 - 1.8.9",
	2:  "1.9 - 1.10.2",
	3:  "1.11 - 1.12.2",
	4:  "1.13 - 1.14.4",
	5:  "1.15 - 1.16.1",
	6:  "1.16.2 - 1.16.5",
	7:  "1.17 - 1.17.1",
	8:  "1.18 - 1.18.2",
	9:  "1.19 - 1.19.2",
	12: "1.19.3",
	13: "1.19.4",
	15: "1.20 - 1.20.1",
	18: "1.20.2",
	22: "1.20.3 - 1.20.4",
	32: "1.20.5 - 1.20.6",
	34: "1.21 - 1.21.1",
	42: "1.21.2 - 1.21.3",
	46: "1.21.4",
	55: "1.21.5",
}

// packFormatLabel renders a pack format number with the versions it belongs to.
func packFormatLabel(format int) string {
	if versions, ok := packFormats[format]; ok {
		return fmt.Sprintf("%d (%s)", format, versions)
	}
	return fmt.Sprintf("%d", format)
}

// ---------------------------------------------------------------------------
// Enabling, disabling, deleting and renaming
// ---------------------------------------------------------------------------

// setContentEnabledCmd switches a mod or pack on or off.
func (s *shell) setContentEnabledCmd(kind launcher.ContentKind, enabled bool, rest []string) {
	world, _, args, ok := contentArgs(rest, true, false)
	if !ok {
		return
	}
	target, ok := s.contentTarget(kind, world, false)
	if !ok {
		return
	}
	items := s.contentItems(target)
	item, ok := s.resolveContent(target, items, strings.Join(args, " "))
	if !ok {
		return
	}
	s.toggleContent(item, enabled)
}

// toggleContent switches one file on or off. It is shared by the "enable"/"disable" commands and by
// the version page, and reports whether the file changed.
func (s *shell) toggleContent(item launcher.ContentItem, enabled bool) bool {
	if item.Enabled == enabled {
		output.Info(output.Translate("shell.content.alreadystate"), item.DisplayName(), stateWord(enabled))
		return false
	}
	if err := launcher.SetContentEnabled(&item, enabled); err != nil {
		output.Error("%s", err)
		return false
	}
	if s.selectedContent == trimDisabled(item.FileName) || s.selectedContent == item.FileName {
		s.selectedContent = item.FileName
	}
	if enabled {
		output.Success(output.Translate("shell.content.enabledone"), color.New(color.Bold).Sprint(item.DisplayName()))
		return true
	}
	output.Success(output.Translate("shell.content.disabledone"), color.New(color.Bold).Sprint(item.DisplayName()))
	return true
}

// stateWord renders "enabled" or "disabled".
func stateWord(enabled bool) string {
	if enabled {
		return output.Translate("shell.content.enabled")
	}
	return output.Translate("shell.content.disabled")
}

// deleteContentCmd removes a mod or pack.
func (s *shell) deleteContentCmd(kind launcher.ContentKind, rest []string) {
	world, assumeYes, args, ok := contentArgs(rest, true, true)
	if !ok {
		return
	}

	target, ok := s.contentTarget(kind, world, false)
	if !ok {
		return
	}
	items := s.contentItems(target)
	item, ok := s.resolveContent(target, items, strings.Join(args, " "))
	if !ok {
		return
	}

	if !assumeYes && !s.confirm(output.Translate("shell.content.confirmdelete"), color.New(color.Bold).Sprint(item.DisplayName())) {
		output.Info(output.Translate("delete.abort"))
		return
	}
	s.removeContent(item)
}

// removeContent deletes one file. It is shared by the "delete" command and by the version page, and
// reports whether the file is gone.
func (s *shell) removeContent(item launcher.ContentItem) bool {
	if err := launcher.RemoveContent(item); err != nil {
		output.Error("%s", err)
		return false
	}
	if s.selectedContent == item.FileName {
		s.selectedContent = ""
	}
	output.Success(output.Translate("shell.content.removed"), color.New(color.Bold).Sprint(item.DisplayName()))
	return true
}

// renameContentCmd renames a mod or pack.
func (s *shell) renameContentCmd(kind launcher.ContentKind, rest []string) {
	world, _, args, ok := contentArgs(rest, true, false)
	if !ok {
		return
	}
	target, ok := s.contentTarget(kind, world, false)
	if !ok {
		return
	}
	items := s.contentItems(target)

	var name string
	switch {
	case len(args) >= 2:
		// "rename mods <name> <new name>"
		name = args[0]
		args = args[1:]
	case len(args) == 1 && s.selectedContent != "":
		// "rename mods <new name>", relying on the selection
	default:
		s.usage(mustSyntax("rename version"))
		return
	}

	item, ok := s.resolveContent(target, items, name)
	if !ok {
		return
	}

	newName := strings.Join(args, " ")
	if newName == "" {
		answer, changed, err := s.askText(output.Translate("shell.content.renameto"), item.FileName)
		if err != nil || !changed {
			output.Info(output.Translate("shell.cancelled"))
			return
		}
		newName = answer
	}

	s.renameContent(item, newName)
}

// renameContent renames one file. It is shared by the "rename" command and by the version page, and
// reports whether the file was renamed.
func (s *shell) renameContent(item launcher.ContentItem, newName string) bool {
	if err := launcher.RenameContent(&item, newName); err != nil {
		output.Error("%s", err)
		return false
	}
	if s.selectedContent != "" {
		s.selectedContent = item.FileName
	}
	output.Success(output.Translate("shell.content.renamed"), color.New(color.Bold).Sprint(item.FileName))
	return true
}

// ---------------------------------------------------------------------------
// Importing
// ---------------------------------------------------------------------------

// importContentCmd copies mods or packs into the instance from local files.
func (s *shell) importContentCmd(kind launcher.ContentKind, rest []string) {
	if len(rest) == 0 {
		output.Error(output.Translate("shell.content.importusage"))
		output.Info(output.Translate("shell.usage"), mustSyntax("import mods").syntax)
		return
	}

	world, _, args, ok := contentArgs(rest, true, false)
	if !ok {
		return
	}
	if len(args) == 0 {
		output.Error(output.Translate("shell.content.importusage"))
		output.Info(output.Translate("shell.usage"), mustSyntax("import mods").syntax)
		return
	}

	target, ok := s.contentTarget(kind, world, true)
	if !ok {
		return
	}
	s.importContent(target, args)
}

// importContent copies files or folders into a category. It is shared by the "import" command and by
// the version page, which asks for the paths instead of taking them from the command line.
func (s *shell) importContent(target contentTarget, args []string) {
	// A bare file name is resolved against the game directory, as everywhere else in the shell.
	sources := make([]string, 0, len(args))
	for _, arg := range args {
		absolute, err := resolvePath(arg)
		if err == nil {
			sources = append(sources, absolute)
			continue
		}
		sources = append(sources, arg)
	}

	outcomes := launcher.ImportContent(target.dir, target.kind, sources)
	added := 0
	for _, outcome := range outcomes {
		switch {
		case outcome.Err != nil:
			output.Error(output.Translate("shell.content.importfailed"), outcome.Name, outcome.Err)
		case outcome.Skipped:
			output.Info(output.Translate("shell.content.importskipped"), outcome.Name)
		default:
			added++
			output.Success(output.Translate("shell.content.imported"), color.New(color.Bold).Sprint(outcome.Name))
		}
	}
	if added == 0 && len(outcomes) == 0 {
		output.Info(output.Translate("shell.content.none"), contentLabel(target.kind), filepath.Dir(target.dir))
		return
	}
	if added > 0 {
		output.Info(output.Translate("shell.content.importdone"), added)
	}
}

// ---------------------------------------------------------------------------
// Modrinth: searching, downloading and updating
// ---------------------------------------------------------------------------

// contentLoaders returns the loader categories Modrinth should be filtered by.
//
// Only mods are loader specific: resource packs, shaders and data packs are not.
func contentLoaders(inst launcher.Instance, kind launcher.ContentKind) []string {
	if kind != launcher.ContentMods {
		return nil
	}
	return meta.ModrinthLoaders(inst.Loader)
}

// searchModrinth lists projects matching a query for a category.
func (s *shell) searchModrinth(target contentTarget, query string, limit int) ([]meta.ModrinthProject, error) {
	facets := meta.ModrinthFacets(meta.ModrinthProjectType(string(target.kind)), contentLoaders(target.inst, target.kind))
	result, err := meta.ModrinthSearchProjects(query, facets, target.inst.GameVersion, limit)
	if err != nil {
		return nil, err
	}
	return result.Hits, nil
}

// searchContentCmd prints a table of Modrinth search results.
func (s *shell) searchContentCmd(kind launcher.ContentKind, query string) {
	target, ok := s.contentTarget(kind, "", false)
	if !ok {
		return
	}
	if query == "" {
		output.Error(output.Translate("shell.mr.noquery"))
		return
	}

	hits, err := s.searchModrinth(target, query, 20)
	if err != nil {
		output.Error("%s", err)
		return
	}
	if len(hits) == 0 {
		output.Info(output.Translate("shell.mr.noresults"), query, target.inst.GameVersion)
		return
	}

	t := table.NewWriter()
	t.SetStyle(table.StyleLight)
	t.SetOutputMirror(os.Stdout)
	t.AppendHeader(table.Row{
		"#",
		output.Translate("shell.mr.header.title"),
		output.Translate("shell.mr.header.author"),
		output.Translate("shell.mr.header.downloads"),
	})
	for i, hit := range hits {
		t.AppendRow(table.Row{i, hit.Title, hit.Author, hit.Downloads})
	}
	t.Render()

	output.Tip(output.Translate("shell.mr.tip"), kind, kind)
}

// searchModpacksCmd lists the Modrinth modpacks matching a query.
//
// Modpacks name the game version they were built for themselves, so the search is not filtered by
// the instance the way the other categories are.
func (s *shell) searchModpacksCmd(query string) {
	if query == "" {
		output.Error(output.Translate("shell.mr.noquery"))
		return
	}

	result, err := meta.ModrinthSearchProjects(query, meta.ModrinthFacets(meta.ModrinthModpack, nil), "", 20)
	if err != nil {
		output.Error("%s", err)
		return
	}
	if len(result.Hits) == 0 {
		output.Info(output.Translate("shell.mr.noresults"), query, output.Translate("shell.content.modpacks"))
		return
	}

	t := table.NewWriter()
	t.SetStyle(table.StyleLight)
	t.SetOutputMirror(os.Stdout)
	t.AppendHeader(table.Row{
		"#",
		output.Translate("shell.mr.header.title"),
		output.Translate("shell.mr.header.author"),
		output.Translate("shell.mr.header.downloads"),
	})
	for i, hit := range result.Hits {
		t.AppendRow(table.Row{i, hit.Title, hit.Author, hit.Downloads})
	}
	t.Render()

	output.Tip(output.Translate("shell.mr.tip"), "modpacks", output.Translate("shell.content.modpacks"))
}

// pickModrinthProject asks the user to select a search result.
func (s *shell) pickModrinthProject(target contentTarget, query string) (meta.ModrinthProject, bool) {
	hits, err := s.searchModrinth(target, query, 30)
	if err != nil {
		output.Error("%s", err)
		return meta.ModrinthProject{}, false
	}
	if len(hits) == 0 {
		output.Info(output.Translate("shell.mr.noresults"), query, target.inst.GameVersion)
		return meta.ModrinthProject{}, false
	}

	entries := make([]string, 0, len(hits))
	for _, hit := range hits {
		entries = append(entries, fmt.Sprintf("%-38s %-16s %9d", truncate(hit.Title, 38), truncate(hit.Author, 16), hit.Downloads))
	}
	index, ok := s.pick(output.Translate("shell.mr.pickproject"), entries, 0)
	if !ok {
		return meta.ModrinthProject{}, false
	}
	return hits[index], true
}

// pickModrinthVersion asks the user to select one release of a project, newest first.
func (s *shell) pickModrinthVersion(target contentTarget, project meta.ModrinthProject) (meta.ModrinthVersion, bool) {
	versions, err := meta.ModrinthProjectVersions(project.ProjectID, []string{target.inst.GameVersion}, contentLoaders(target.inst, target.kind))
	if err != nil {
		output.Error("%s", err)
		return meta.ModrinthVersion{}, false
	}
	if len(versions) == 0 {
		output.Error(output.Translate("shell.mr.noversions"), kindWord(target.kind), project.Title, target.inst.GameVersion)
		return meta.ModrinthVersion{}, false
	}

	entries := make([]string, 0, len(versions))
	for _, version := range versions {
		entries = append(entries, fmt.Sprintf("%-30s %-10s %s", truncate(version.VersionNumber, 30), version.VersionType, shortDate(version.DatePublished)))
	}
	index, ok := s.pickStar(output.Translate("shell.mr.pickversion"), entries, 0)
	if !ok {
		return meta.ModrinthVersion{}, false
	}
	return versions[index], true
}

// kindWord is the singular of a category, for messages about one project.
func kindWord(kind launcher.ContentKind) string {
	switch kind {
	case launcher.ContentMods:
		return output.Translate("shell.content.mod")
	case launcher.ContentResourcePacks:
		return output.Translate("shell.content.resourcepack")
	case launcher.ContentShaderPacks:
		return output.Translate("shell.content.shaderpack")
	case launcher.ContentDataPacks:
		return output.Translate("shell.content.datapack")
	}
	return string(kind)
}

// shortDate trims an ISO timestamp to its date.
func shortDate(value string) string {
	if len(value) >= 10 {
		return value[:10]
	}
	return value
}

// downloadContentCmd searches Modrinth and installs the chosen file.
func (s *shell) downloadContentCmd(kind launcher.ContentKind, rest []string) {
	world, _, args, ok := contentArgs(rest, true, false)
	if !ok {
		return
	}
	target, ok := s.contentTarget(kind, world, true)
	if !ok {
		return
	}
	s.downloadContent(target, strings.Join(args, " "))
}

// downloadContent installs one project from Modrinth. It is shared by the "download" command and by
// the version page, which leaves the search term to the prompt.
func (s *shell) downloadContent(target contentTarget, query string) {
	if target.kind == launcher.ContentDataPacks {
		output.Info(output.Translate("shell.pack.datapacknote"))
	}

	if query == "" {
		answer, _, err := s.askText(output.Translate("shell.mr.query"), "")
		if err != nil || answer == "" {
			output.Info(output.Translate("shell.cancelled"))
			return
		}
		query = answer
	}

	project, ok := s.pickModrinthProject(target, query)
	if !ok {
		return
	}
	version, ok := s.pickModrinthVersion(target, project)
	if !ok {
		return
	}

	file, ok := version.PrimaryFile()
	if !ok {
		output.Error(output.Translate("shell.mr.nofiles"), project.Title)
		return
	}
	if err := s.installModrinthFile(target.dir, file, false); err != nil {
		output.Error(output.Translate("shell.mr.failed"), err)
		return
	}
	output.Success(output.Translate("shell.mr.installed"), color.New(color.Bold).Sprint(file.Filename))
}

// installModrinthFile downloads one Modrinth file into a directory.
//
// disabled appends the disabled marker when the file replaces a switched off one, so that updating
// a disabled mod does not silently switch it back on.
func (s *shell) installModrinthFile(dir string, file meta.ModrinthFile, disabled bool) error {
	output.Info(output.Translate("shell.mr.downloading"), file.Filename, humanSize(file.Size))

	path := filepath.Join(dir, file.Filename)
	if err := network.DownloadFile(network.DownloadEntry{
		URL:  file.URL,
		Path: path,
		Sha1: file.Hashes["sha1"],
	}); err != nil {
		return err
	}
	if disabled {
		if err := os.Rename(path, path+launcher.DisabledSuffix); err != nil {
			return err
		}
	}
	return nil
}

// updateContent looks for newer releases of every installed file of a category.
//
// Matching is by name, because a jar does not record which Modrinth project it came from: the mod
// id and display name are compared against the project slug and title, and a file that matches
// nothing is reported as such rather than guessed at.
func (s *shell) updateContent(target contentTarget, name string, assumeYes bool) {
	items := s.contentItems(target)
	if len(items) == 0 {
		output.Info(output.Translate("shell.content.none"), contentLabel(target.kind), target.dir)
		return
	}
	if name != "" {
		item, ok := s.resolveContent(target, items, name)
		if !ok {
			return
		}
		items = []launcher.ContentItem{item}
	}

	type pending struct {
		item    launcher.ContentItem
		version meta.ModrinthVersion
		file    meta.ModrinthFile
	}
	type checked struct {
		row    [3]string
		update *pending
	}

	// Every file needs a search and a version listing, so a large instance would otherwise spend
	// minutes waiting on one request after another. The lookups run in parallel and the table is
	// built afterwards, in instance order, so the output is unchanged.
	anonymous := output.Translate("shell.update.unknown")
	upToDate := output.Translate("shell.update.uptodate")
	checks := concurrent.Map(items, modrinthThreads(), func(_ int, item launcher.ContentItem) checked {
		unknown := checked{row: [3]string{item.DisplayName(), item.Version, anonymous}}

		project, ok := s.matchModrinthProject(target, item)
		if !ok {
			return unknown
		}
		versions, err := meta.ModrinthProjectVersions(project.ProjectID, []string{target.inst.GameVersion}, contentLoaders(target.inst, target.kind))
		if err != nil || len(versions) == 0 {
			return unknown
		}
		newest := versions[0]
		if !isNewerVersion(item.Version, newest.VersionNumber, target.inst.GameVersion) {
			return checked{row: [3]string{item.DisplayName(), item.Version, upToDate}}
		}
		file, ok := newest.PrimaryFile()
		if !ok {
			return unknown
		}
		return checked{
			row:    [3]string{item.DisplayName(), item.Version, newest.VersionNumber},
			update: &pending{item: item, version: newest, file: file},
		}
	})

	rows := make([][3]string, 0, len(items))
	var updates []pending
	for _, check := range checks {
		rows = append(rows, check.row)
		if check.update != nil {
			updates = append(updates, *check.update)
		}
	}

	t := table.NewWriter()
	t.SetStyle(table.StyleLight)
	t.SetOutputMirror(os.Stdout)
	t.AppendHeader(table.Row{
		output.Translate("shell.content.header.name"),
		output.Translate("shell.update.header.current"),
		output.Translate("shell.update.header.status"),
	})
	for _, row := range rows {
		t.AppendRow(table.Row{row[0], row[1], row[2]})
	}
	t.Render()

	if len(updates) == 0 {
		output.Info(output.Translate("shell.update.none"))
		return
	}
	if !assumeYes && !s.confirm(output.Translate("shell.update.confirm"), len(updates), contentLabel(target.kind)) {
		output.Info(output.Translate("delete.abort"))
		return
	}

	// The files are independent, so they are fetched in parallel; the messages are printed under a
	// lock, which keeps a line from being cut in half by another result.
	var printed sync.Mutex
	failed := 0
	concurrent.Do(len(updates), network.MaxConcurrentDownloads(), func(i int) {
		update := updates[i]
		err := s.installModrinthFile(target.dir, update.file, !update.item.Enabled)

		// The new file has its own name, so the old one is removed afterwards.
		if err == nil && update.file.Filename != update.item.FileName {
			_ = launcher.RemoveContent(update.item)
		}

		printed.Lock()
		defer printed.Unlock()
		if err != nil {
			failed++
			output.Error(output.Translate("shell.mr.failed"), err)
			return
		}
		output.Success(output.Translate("shell.update.done"), color.New(color.Bold).Sprint(update.file.Filename))
	})
	if failed > 0 {
		output.Warning(output.Translate("shell.update.failed"), failed, len(updates))
	}
}

// updateContentCmd is the "update" command, which resolves the instance and the category first.
func (s *shell) updateContentCmd(kind launcher.ContentKind, rest []string) {
	world, assumeYes, args, ok := contentArgs(rest, true, true)
	if !ok {
		return
	}
	target, ok := s.contentTarget(kind, world, false)
	if !ok {
		return
	}
	s.updateContent(target, strings.Join(args, " "), assumeYes)
}

// modrinthThreads is how many Modrinth lookups run at once.
//
// It is deliberately lower than the download thread count: Modrinth rate limits the API by address,
// so a bulk lookup has to stay well below the number of files a version downloads at once.
func modrinthThreads() int {
	threads := network.MaxConcurrentDownloads()
	if threads > 8 {
		threads = 8
	}
	return threads
}

// matchModrinthProject finds the Modrinth project an installed file came from.
//
// Only a confident match is accepted: the mod id has to equal the project slug, or the display
// name the project title, so that "appleskin" never updates "applecore".
func (s *shell) matchModrinthProject(target contentTarget, item launcher.ContentItem) (meta.ModrinthProject, bool) {
	query := item.ID
	if query == "" {
		query = item.Name
	}
	if query == "" {
		return meta.ModrinthProject{}, false
	}

	hits, err := s.searchModrinth(target, query, 10)
	if err != nil {
		return meta.ModrinthProject{}, false
	}

	best, bestScore := meta.ModrinthProject{}, 0
	for _, hit := range hits {
		score := 0
		switch {
		case item.ID != "" && strings.EqualFold(hit.Slug, item.ID):
			score = 3
		case item.Name != "" && strings.EqualFold(hit.Title, item.Name):
			score = 2
		case item.ID != "" && slugify(hit.Title) == item.ID:
			score = 2
		case item.Name != "" && strings.EqualFold(hit.Slug, slugify(item.Name)):
			score = 1
		}
		if score > bestScore {
			best, bestScore = hit, score
		}
	}
	return best, bestScore >= 2
}

// slugify turns a display name into the shape a project slug usually has.
func slugify(name string) string {
	var b strings.Builder
	previousDash := true
	for _, r := range strings.ToLower(name) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			previousDash = false
		default:
			if !previousDash {
				b.WriteByte('-')
				previousDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// versionPattern finds the numeric part of a version string.
var versionPattern = regexp.MustCompile(`\d+(?:\.\d+)+`)

// versionKey extracts the version from a string that also carries other numbers.
//
// A version number such as "mc1.20.1-0.5.13-fabric" holds the game version as well, so anything
// that matches the game version is passed over in favour of the longest remaining candidate.
func versionKey(value, gameVersion string) string {
	best := ""
	for _, candidate := range versionPattern.FindAllString(value, -1) {
		if gameVersion != "" && strings.HasPrefix(gameVersion, candidate) {
			continue
		}
		if len(candidate) > len(best) {
			best = candidate
		}
	}
	if best == "" {
		return strings.TrimSpace(value)
	}
	return best
}

// isNewerVersion reports whether the available version differs from the installed one.
//
// Versions are declared by each mod in its own style, so this compares the numeric part and treats
// a difference as an update rather than trying to order release candidates.
func isNewerVersion(installed, available, gameVersion string) bool {
	if available == "" {
		return false
	}
	if installed == "" {
		return true
	}
	return versionKey(installed, gameVersion) != versionKey(available, gameVersion)
}

// ---------------------------------------------------------------------------
// Modpacks
// ---------------------------------------------------------------------------

// listModpacksCmd lists the instances installed from a modpack.
func (s *shell) listModpacksCmd() {
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
		output.Translate("shell.info.name"),
		output.Translate("shell.pack.from"),
		output.Translate("shell.pack.version"),
		output.Translate("shell.info.version"),
	})

	found := 0
	for _, inst := range instances {
		marker, ok := launcher.ReadModpackMarker(inst)
		if !ok {
			continue
		}
		found++
		mark := ""
		if inst.Name == s.selected {
			mark = "*"
		}
		t.AppendRow(table.Row{mark, inst.Name, marker.Name, marker.Version, inst.GameVersion})
	}
	t.Render()

	if found == 0 {
		output.Info(output.Translate("shell.pack.none"))
	}
}

// downloadModpackCmd installs a modpack as a new instance.
//
// The argument is either a local .mrpack file or a search term for Modrinth. CurseForge packs are
// not supported, because the CurseForge API needs a key this launcher does not ship.
func (s *shell) downloadModpackCmd(rest []string) {
	name := ""
	var args []string
	for i := 0; i < len(rest); i++ {
		flag, inline, hasInline := splitFlag(rest[i])
		switch flag {
		case "-n", "--name":
			value, err := flagValue(inline, hasInline, rest, &i)
			if err != nil {
				output.Error(output.Translate("shell.missingvalue"), flag)
				return
			}
			name = value
		default:
			if strings.HasPrefix(flag, "-") && flag != "-" {
				output.Error(output.Translate("shell.badflag"), flag)
				return
			}
			args = append(args, flag)
		}
	}

	index, archive, ok := s.modpackSource(strings.Join(args, " "))
	if !ok {
		return
	}

	if name == "" {
		name = sanitizeInstanceName(index.Name)
	}
	if name == "" {
		answer, changed, err := s.askText(output.Translate("shell.pack.newname"), "")
		if err != nil || !changed {
			output.Info(output.Translate("shell.cancelled"))
			return
		}
		name = answer
	}
	name = s.freeInstanceName(name)

	output.Info(output.Translate("shell.pack.installing"), color.New(color.Bold).Sprint(index.Name))
	inst, err := launcher.InstallModpack(archive, index, name, s.modpackProgress)
	if err != nil {
		output.Error(output.Translate("shell.pack.failed"), err)
		return
	}
	output.Success(output.Translate("shell.pack.done"), index.Name, inst.Name)
	s.selected = inst.Name
	s.selectedContent, s.contentWorld = "", ""
}

// modpackSource resolves the modpack to install, either from disk or from Modrinth.
func (s *shell) modpackSource(source string) (launcher.MrpackIndex, string, bool) {
	if source != "" {
		if absolute, err := resolvePath(source); err == nil {
			if info, err := os.Stat(absolute); err == nil && !info.IsDir() {
				index, err := launcher.ReadMrpack(absolute)
				if err != nil {
					output.Error(output.Translate("shell.pack.badarchive"), absolute)
					output.Info("%s", err)
					return launcher.MrpackIndex{}, "", false
				}
				return index, absolute, true
			}
		}
	}

	// A local file that could not be read is not a search term; anything else is.
	if source == "" {
		answer, changed, err := s.askText(output.Translate("shell.mr.query"), "")
		if err != nil || !changed {
			output.Info(output.Translate("shell.cancelled"))
			return launcher.MrpackIndex{}, "", false
		}
		source = answer
	}

	// Modpacks carry their own game and loader versions, so the search is not filtered by them.
	facets := meta.ModrinthFacets(meta.ModrinthModpack, nil)
	result, err := meta.ModrinthSearchProjects(source, facets, "", 30)
	if err != nil {
		output.Error("%s", err)
		return launcher.MrpackIndex{}, "", false
	}
	if len(result.Hits) == 0 {
		output.Info(output.Translate("shell.mr.noresults"), source, output.Translate("shell.content.modpacks"))
		return launcher.MrpackIndex{}, "", false
	}

	entries := make([]string, 0, len(result.Hits))
	for _, hit := range result.Hits {
		entries = append(entries, fmt.Sprintf("%-38s %-16s %9d", truncate(hit.Title, 38), truncate(hit.Author, 16), hit.Downloads))
	}
	hitIndex, ok := s.pick(output.Translate("shell.mr.pickproject"), entries, 0)
	if !ok {
		return launcher.MrpackIndex{}, "", false
	}
	project := result.Hits[hitIndex]

	versions, err := meta.ModrinthProjectVersions(project.ProjectID, nil, nil)
	if err != nil {
		output.Error("%s", err)
		return launcher.MrpackIndex{}, "", false
	}
	var candidates []meta.ModrinthVersion
	for _, version := range versions {
		for _, file := range version.Files {
			if strings.HasSuffix(strings.ToLower(file.Filename), ".mrpack") {
				candidates = append(candidates, version)
				break
			}
		}
	}
	if len(candidates) == 0 {
		output.Error(output.Translate("shell.pack.notmrpack"))
		return launcher.MrpackIndex{}, "", false
	}

	entries = entries[:0]
	for _, version := range candidates {
		game := ""
		// Packs name the game version they were built for in their own metadata.
		for _, supported := range version.GameVersions {
			game = supported
			break
		}
		entries = append(entries, fmt.Sprintf("%-30s %-16s %s", truncate(version.VersionNumber, 30), game, shortDate(version.DatePublished)))
	}
	versionIndex, ok := s.pickStar(output.Translate("shell.mr.pickversion"), entries, 0)
	if !ok {
		return launcher.MrpackIndex{}, "", false
	}
	version := candidates[versionIndex]

	var url string
	for _, file := range version.Files {
		if strings.HasSuffix(strings.ToLower(file.Filename), ".mrpack") {
			url = file.URL
			break
		}
	}
	if url == "" {
		output.Error(output.Translate("shell.mr.nofiles"), project.Title)
		return launcher.MrpackIndex{}, "", false
	}

	archive, err := launcher.FetchModpackArchive(url)
	if err != nil {
		output.Error(output.Translate("shell.mr.failed"), err)
		return launcher.MrpackIndex{}, "", false
	}
	index, err := launcher.ReadMrpack(archive)
	if err != nil {
		output.Error(output.Translate("shell.pack.badarchive"), archive)
		output.Info("%s", err)
		return launcher.MrpackIndex{}, "", false
	}
	return index, archive, true
}

// modpackProgress reports how far a modpack installation has got.
//
// One line every ten files keeps a pack of several hundred mods from filling the window, while
// still showing that something is happening.
func (s *shell) modpackProgress(p launcher.InstallProgress) {
	if p.Total == 0 {
		return
	}
	if p.Downloaded == 0 {
		output.Info(output.Translate("shell.pack.progress"), 0, p.Total)
		return
	}
	if p.Downloaded%10 == 0 || p.Downloaded == p.Total {
		output.Info(output.Translate("shell.pack.progress"), p.Downloaded, p.Total)
	}
}

// sanitizeInstanceName turns a modpack name into something usable as a directory name.
func sanitizeInstanceName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch r {
		case '<', '>', ':', '"', '/', '\\', '|', '?', '*':
			continue
		}
		b.WriteRune(r)
	}
	return strings.Trim(strings.TrimSpace(b.String()), ".")
}

// freeInstanceName appends a counter until the name is unused, so that installing a pack twice does
// not fail on the second attempt.
func (s *shell) freeInstanceName(name string) string {
	if !launcher.DoesInstanceExist(name) {
		return name
	}
	for i := 2; i < 100; i++ {
		candidate := fmt.Sprintf("%s (%d)", name, i)
		if !launcher.DoesInstanceExist(candidate) {
			output.Info(output.Translate("shell.pack.nameinuse"), name, candidate)
			return candidate
		}
	}
	return name
}

// mustSyntax returns the help entry of a command, which the help table is guaranteed to hold.
func mustSyntax(prefix string) shellHelpEntry {
	entry, _ := syntaxOf(prefix)
	return entry
}
