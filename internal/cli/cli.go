package cli

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/Xuanwo/go-locale"
	"github.com/alecthomas/kong"
	"github.com/fatih/color"
	"github.com/telecter/cmd-launcher/internal/cli/cmd"
	"github.com/telecter/cmd-launcher/internal/cli/output"
	"github.com/telecter/cmd-launcher/internal/meta"
	"github.com/telecter/cmd-launcher/internal/network"
	env "github.com/telecter/cmd-launcher/pkg"
	"github.com/telecter/cmd-launcher/pkg/auth"
	"github.com/telecter/cmd-launcher/pkg/launcher"
	"go.abhg.dev/komplete"
)

const (
	name    = "cmd-launcher"
	version = "1.6.1"
)

type aboutCmd struct{}

func (aboutCmd) Run(ctx *kong.Context) error {
	color.New(color.Bold).Println(name, version)
	color.New(color.Underline).Println(output.Translate("launcher.description"))
	fmt.Println(output.Translate("launcher.copyright"))
	fmt.Println(output.Translate("launcher.license"))
	fmt.Println(output.Translate("launcher.fork"))
	return nil
}

// SettingsCmd edits the launcher-wide settings from the command line, the same options the
// shell's "settings" command offers. It exists so that a one-off change - most importantly the
// language, which decides whether everything else can be read - does not require entering the
// shell first.
type SettingsCmd struct {
	Name  string   `arg:"" optional:"" help:"${settings_arg_name}"`
	Value []string `arg:"" optional:"" help:"${settings_arg_value}"`
}

func (c *SettingsCmd) Run(ctx *kong.Context) error {
	global, err := launcher.LoadGlobalSettings()
	if err != nil {
		return err
	}

	// Without a name, every option is listed with its current value.
	if c.Name == "" {
		for _, entry := range globalSettings {
			output.Info("%s: %s", output.Translate(entry.label), entry.value(global))
		}
		return nil
	}

	entry, ok := findGlobalSetting(c.Name)
	if !ok {
		output.Error(output.Translate("shell.settings.unknown"), c.Name)
		output.Info(output.Translate("shell.settings.list"), globalSettingNames())
		return nil
	}

	// With a name only, the option is reported together with how to change it.
	if len(c.Value) == 0 {
		output.Info(output.Translate("shell.settings.show"), output.Translate(entry.label), entry.value(global))
		output.Info(output.Translate("shell.usage"), "settings "+entry.names[0]+" "+entry.arg)
		return nil
	}

	if err := entry.set(&global, strings.Join(c.Value, " ")); err != nil {
		output.Error("%s", err)
		return nil
	}
	if err := global.Save(); err != nil {
		return err
	}
	if entry.key == "language" {
		// Report the switch in the language that was just selected, like the shell does.
		code := output.SetLangCode(global.Language)
		refreshLabels()
		output.Success(output.Translate("shell.lang.changed"), color.New(color.Bold).Sprint(output.LanguageName(code)))
		return nil
	}
	output.Success(output.Translate("shell.set.saved"), launcher.GlobalSettingsPath())
	return nil
}

type CLI struct {
	Shell       ShellCmd         `cmd:"" help:"${shell}" default:"1"`
	Start       cmd.StartCmd     `cmd:"" help:"${start}"`
	Instance    cmd.InstanceCmd  `cmd:"" help:"${instance}" aliases:"inst"`
	Auth        cmd.AuthCmd      `cmd:"" help:"${auth}"`
	Search      cmd.SearchCmd    `cmd:"" help:"${search}"`
	Settings    SettingsCmd      `cmd:"" help:"${shell.settings}" aliases:"options,prefs"`
	Completions komplete.Command `cmd:"" help:"${completions}"`
	About       aboutCmd         `cmd:"" help:"${about}" aliases:"version"`

	Verbosity string `help:"${arg_verbosity}" enum:"info,extra,debug" default:"info"`
	Dir       string `help:"${arg_dir}" type:"path" placeholder:"PATH"`
	Lang      string `help:"${arg_lang}" placeholder:"LANG"`
	Portable  bool   `help:"${arg_portable}" short:"p"`
	NoColor   bool   `help:"${arg_nocolor}"`
}

func (c *CLI) AfterApply(ctx *kong.Context) error {
	var verbosity int
	switch c.Verbosity {
	case "info":
		verbosity = 0
	case "extra":
		verbosity = 1
	case "debug":
		verbosity = 2
	}
	ctx.Bind(verbosity)

	// Portable mode first: it decides where the account store lives. An explicit --portable flag
	// points the launcher at its own executable directory, and a portable.txt file next to the
	// executable does the same without any flag. --dir still wins for the game directory, so the
	// two can be combined to keep accounts beside the executable but the game elsewhere.
	if c.Portable && !env.Portable {
		env.UsePortable(env.ExecutableDir())
	}
	if env.UsePortableIfPresent() {
		if err := env.EnsureDirs(); err != nil {
			return err
		}
		if err := env.MigrateUserConfig(); err != nil {
			return err
		}
	}
	if c.Dir != "" {
		if err := env.SetDirs(c.Dir); err != nil {
			return err
		}
	}
	// The language and the download thread count are part of the global settings, so they can only be
	// read once the game directory is known: with --dir or portable mode in play, RootDir is no
	// longer the default one. Without a settings file the language detected from the locale stays in
	// place and the built-in download default is kept.
	if settings, err := launcher.LoadGlobalSettings(); err == nil {
		if c.Lang == "" {
			output.SetLangCode(settings.Language)
		}
		network.SetMaxConcurrentDownloads(settings.DownloadThreads)
	}
	if c.Lang != "" {
		// An explicit --lang wins over the settings file, so the UI can be switched even when the
		// stored language is unreadable.
		output.SetLangCode(c.Lang)
	}
	// The wording of the interactive widgets is resolved once, so it has to be handed over after
	// the language is final - also for the commands that never enter the shell.
	refreshLabels()
	if err := auth.ReadFromCache(); err != nil {
		return fmt.Errorf("read auth store: %w", err)
	}
	if c.NoColor {
		color.NoColor = true
	}
	return nil
}

func vars() kong.Vars {
	vars := make(kong.Vars)
	for k, v := range output.Translations() {
		vars[strings.ReplaceAll(k, ".", "_")] = v
	}
	return vars
}

func valueFormatter(value *kong.Value) string {
	if value.Enum != "" {
		return fmt.Sprintf("%s [%s]", value.Help, strings.Join(value.EnumSlice(), ", "))
	}
	return value.Help
}

func groups() kong.Groups {
	return kong.Groups{
		"overrides": output.Translate("start.arg.overrides"),
		"opts":      output.Translate("start.arg.opts"),
	}
}

// tips prints a tip message based on an error, if any are available.
func tips(err error) {
	// General internet connection related issues
	if errors.Is(err, &net.OpError{}) {
		output.Tip(output.Translate("tip.internet"))
	}
	// A cache couldn't be updated from the remote source
	if errors.Is(err, network.ErrNotCached) {
		output.Tip(output.Translate("tip.cache"))
	}
	// Mojang-provided JVM isn't working
	if errors.Is(err, meta.ErrJavaBadSystem) || errors.Is(err, meta.ErrJavaNoVersion) {
		output.Tip(output.Translate("tip.nojvm"))
	}
	// Not logged in
	if errors.Is(err, auth.ErrNoAccount) {
		output.Tip(output.Translate("tip.noaccount"))
	}
}

// Start creates the CLI parser and runs it. It returns an exit handler and code.
func Run() (func(int), int) {
	lang, err := locale.Detect()
	if err == nil {
		output.SetLang(lang)
	}

	parser := kong.Must(&CLI{},
		kong.UsageOnError(),
		kong.Name(name),
		kong.Description(output.Translate("launcher.description")),
		kong.ConfigureHelp(kong.HelpOptions{
			NoExpandSubcommands: true,
			Compact:             true,
		}),
		kong.ValueFormatter(valueFormatter),
		groups(),
		vars(),
	)
	komplete.Run(parser)

	ctx, err := parser.Parse(os.Args[1:])
	if err != nil {
		exitCode := 1
		var parseErr *kong.ParseError
		if errors.As(err, &parseErr) {
			parseErr.Context.PrintUsage(false)
			exitCode = parseErr.ExitCode()
		}
		output.Error("%s", err)
		return parser.Exit, exitCode
	}

	if err := ctx.Run(); err != nil {
		output.Error("%s", err)
		tips(err)
		var coder kong.ExitCoder
		if errors.As(err, &coder) {
			return ctx.Exit, coder.ExitCode()
		}
		return ctx.Exit, 1
	}
	return ctx.Exit, 0
}
