package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/fatih/color"
	"github.com/telecter/cmd-launcher/internal/cli/output"
	"github.com/telecter/cmd-launcher/pkg/launcher"
)

// The module in this file is the mod management page of a version.
//
// A version's settings page carries a row that opens it, so that a version and the files it loads
// are handled in one place, the way PCL's version page does it. It works on the instance the page
// was opened for rather than on the shell's selection, which keeps a look at one version from
// changing the one the commands work on.
//
// The top-level command family stays, because the shell has to keep working without a menu around
// it. Both paths run the same helpers — toggleContent, removeContent, renameContent, describeContent,
// contentItems, importContent, downloadContent and updateContent — so they behave alike.

// contentPage is the page behind a version's mod row: one row per category, each opening the page
// that manages its files.
func (s *shell) contentPage(name string) {
	inst, err := launcher.FetchInstance(name)
	if err != nil {
		output.Error("%s", err)
		return
	}

	items := make([]menuItem, 0, len(launcher.ContentKinds))
	for _, kind := range launcher.ContentKinds {
		items = append(items, menuItem{
			label: contentLabel(kind),
			value: func() string { return s.contentKindRow(inst, kind) },
			edit: func() error {
				s.contentCategoryPage(name, kind)
				return nil
			},
		})
	}
	s.menu(fmt.Sprintf("%s - %s", output.Translate("shell.contentmgr"), name), items)
}

// contentCategoryPage manages one category of one version.
//
// Its rows mirror the folder, so the page is rebuilt whenever an action renamed or removed a file.
func (s *shell) contentCategoryPage(name string, kind launcher.ContentKind) {
	title := fmt.Sprintf("%s - %s", contentLabel(kind), name)
	for {
		target, ok := s.contentTargetFor(name, kind, "", false)
		if !ok {
			return
		}
		items := s.contentItems(target)
		rows := make([]menuItem, 0, len(items)+3)
		for i := range items {
			item := items[i]
			rows = append(rows, menuItem{
				label: item.DisplayName(),
				value: func() string { return contentState(target.inst, item) },
				edit: func() error {
					if s.contentItemPage(target, item) {
						return errMenuReload
					}
					return nil
				},
			})
		}
		rows = append(rows, s.contentExtraRows(target)...)
		if !s.menu(title, rows) {
			return
		}
	}
}

// contentExtraRows are the rows a category page adds below its files.
func (s *shell) contentExtraRows(target contentTarget) []menuItem {
	blank := func() string { return "" }
	return []menuItem{
		{
			label: output.Translate("shell.content.download"),
			value: blank,
			edit: func() error {
				s.downloadContent(target, "")
				return nil
			},
		},
		{
			label: output.Translate("shell.content.import"),
			value: blank,
			edit: func() error {
				answer, changed, err := s.askText(output.Translate("shell.content.importprompt"), "")
				if err != nil || !changed {
					return err
				}
				s.importContent(target, strings.Fields(answer))
				return nil
			},
		},
		{
			label: output.Translate("shell.content.update"),
			value: blank,
			edit: func() error {
				s.updateContent(target, "", false)
				return nil
			},
		},
	}
}

// contentItemPage offers the actions for one file.
//
// It reports whether the list has to be rebuilt, which every action that renames the file on disk
// calls for: a disabled mod is the same file under another name, so the row it came from is stale by
// the time the action returns.
func (s *shell) contentItemPage(target contentTarget, item launcher.ContentItem) bool {
	toggle := menuItem{
		label: output.Translate("shell.content.disable"),
		value: func() string { return stateWord(item.Enabled) },
		edit: func() error {
			if s.toggleContent(item, !item.Enabled) {
				return errMenuReload
			}
			return nil
		},
	}
	if !item.Enabled {
		toggle.label = output.Translate("shell.content.enable")
	}

	rows := []menuItem{
		toggle,
		{
			label: output.Translate("shell.content.rename"),
			value: func() string { return item.FileName },
			edit: func() error {
				answer, changed, err := s.askText(output.Translate("shell.content.renameto"), item.FileName)
				if err != nil || !changed {
					return err
				}
				if s.renameContent(item, answer) {
					return errMenuReload
				}
				return nil
			},
		},
		{
			label: output.Translate("shell.content.details"),
			value: func() string { return orDash(item.Version) },
			edit: func() error {
				s.describeContent(target, item)
				return nil
			},
		},
		{
			label: output.Translate("shell.content.delete"),
			value: func() string { return humanSize(item.Size) },
			edit: func() error {
				if !s.confirm(output.Translate("shell.content.confirmdelete"), color.New(color.Bold).Sprint(item.DisplayName())) {
					output.Info(output.Translate("delete.abort"))
					return nil
				}
				if s.removeContent(item) {
					return errMenuReload
				}
				return nil
			},
		},
	}
	return s.menu(item.DisplayName(), rows)
}

// contentState renders one file as the value column of a category page.
func contentState(inst launcher.Instance, item launcher.ContentItem) string {
	state := output.Translate("shell.content.disabled")
	if item.Enabled {
		state = output.Translate("shell.content.enabled")
	}
	if reason, conflict := contentConflict(inst, item); conflict {
		state = reason
	}
	if item.Version != "" {
		state += "  " + item.Version
	}
	return state
}

// contentKindRow is the value of a category row: how many files it holds, and how many are on.
func (s *shell) contentKindRow(inst launcher.Instance, kind launcher.ContentKind) string {
	world := ""
	if kind == launcher.ContentDataPacks {
		// Data packs live in a world, and the world is only known once it has been resolved.
		world = s.contentWorld
	}
	return summaryOf(inst, kind, world)
}

// contentRow is the value of the version page's mod row.
func (s *shell) contentRow(inst launcher.Instance) string {
	return summaryOf(inst, launcher.ContentMods, "")
}

// summaryOf counts a category and renders it.
func summaryOf(inst launcher.Instance, kind launcher.ContentKind, world string) string {
	total, on, ok := contentCounts(inst, kind, world)
	if !ok {
		return output.Translate("shell.na")
	}
	return fmt.Sprintf(output.Translate("shell.content.summary"), total, on)
}

// contentCounts counts a category without reading its files: parsing a few hundred jars would make
// the version page pause before it appears.
//
// A category that was never used has no folder yet; that is an empty category, not an unreadable
// one. A data pack category without a known world is reported as not countable.
func contentCounts(inst launcher.Instance, kind launcher.ContentKind, world string) (total, on int, ok bool) {
	dir, err := launcher.ContentDir(inst, kind, world)
	if err != nil {
		return 0, 0, false
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, 0, true
		}
		return 0, 0, false
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		total++
		if !strings.HasSuffix(strings.ToLower(entry.Name()), launcher.DisabledSuffix) {
			on++
		}
	}
	return total, on, true
}
