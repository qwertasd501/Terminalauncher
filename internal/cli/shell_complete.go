package cli

import (
	"os"
	"sort"
	"strings"
	"time"

	"github.com/qwertasd501/Terminalauncher/pkg/auth"
	"github.com/qwertasd501/Terminalauncher/pkg/launcher"
)

// shellVerbs are the command verbs, offered when the line is still empty.
var shellVerbs = []string{
	"list", "select", "deselect", "download", "set", "settings", "create",
	"del", "delete", "rename", "start", "search", "auth",
	"enable", "disable", "import", "update",
	"cd", "pwd", "info", "open", "about", "help", "clear", "exit", "quit",
}

// shellObjects maps a verb to the objects it accepts.
var shellObjects = map[string][]string{
	"list":     {"versions", "users", "game", "fabric", "quilt", "forge", "mods", "resourcepacks", "shaderpacks", "datapacks", "modpacks"},
	"select":   {"versions", "users", "java", "mods", "resourcepacks", "shaderpacks", "datapacks"},
	"create":   {"users", "instance"},
	"del":      {"users", "version", "mods", "resourcepacks", "shaderpacks", "datapacks"},
	"delete":   {"users", "version", "mods", "resourcepacks", "shaderpacks", "datapacks"},
	"download": {"version", "mods", "resourcepacks", "shaderpacks", "datapacks", "modpacks"},
	"set":      {"versions", "global"},
	"rename":   {"version", "mods", "resourcepacks", "shaderpacks", "datapacks"},
	"auth":     {"login", "logout"},
	"enable":   {"mods", "resourcepacks", "shaderpacks", "datapacks"},
	"disable":  {"mods", "resourcepacks", "shaderpacks", "datapacks"},
	"import":   {"mods", "resourcepacks", "shaderpacks", "datapacks"},
	"update":   {"mods", "resourcepacks", "shaderpacks", "datapacks"},
	"info":     {"mods", "resourcepacks", "shaderpacks", "datapacks"},
	"clear":    {"screen", "memory"},
	"open":     {"version", "saves", "mods", "config", "logs", "screenshots", "resourcepacks", "shaderpacks", "datapacks", "root"},
}

// shellFlags maps a verb to the flags it accepts.
var shellFlags = map[string][]string{
	"cd":       {"-y", "--yes"},
	"clear":    {"-a", "--all"},
	"download": {"-v", "--version", "-l", "--loader", "--loader-version", "-w", "--world", "-n", "--name"},
	"create":   {"-v", "--version", "-l", "--loader", "--loader-version", "-n", "--no-browser"},
	"del":      {"-y", "--yes", "-w", "--world"},
	"delete":   {"-y", "--yes", "-w", "--world"},
	"search":   {"-k", "--kind", "-r", "--reverse"},
	"auth":     {"-n", "--no-browser"},
	"list":     {"-w", "--world"},
	"select":   {"-w", "--world"},
	"info":     {"-w", "--world"},
	"enable":   {"-w", "--world"},
	"disable":  {"-w", "--world"},
	"import":   {"-w", "--world"},
	"rename":   {"-w", "--world"},
	"update":   {"-y", "--yes", "-w", "--world"},
	"start": {
		"-u", "--username", "-s", "--server", "-w", "--world",
		"--demo", "--disable-multiplayer", "--disable-chat", "--prepare",
		"--width", "--height", "--jvm", "-a", "--jvm-args", "--min-memory", "--max-memory",
	},
}

// pathFlags are the flags whose value is a filesystem path.
var pathFlags = map[string]bool{
	"--jvm": true,
}

// worldFlags are the flags whose value is a world name.
var worldFlags = map[string]bool{
	"-w": true, "--world": true,
}

// contentArgVerbs are the verbs whose argument is the name of a mod or pack.
var contentArgVerbs = map[string]bool{
	"select":  true,
	"info":    true,
	"enable":  true,
	"disable": true,
	"delete":  true,
	"del":     true,
	"rename":  true,
	"import":  true,
	"update":  true,
}

// instanceArgVerbs are the verbs whose version argument is an instance name.
var instanceArgVerbs = map[string]bool{
	"select": true,
	"set":    true,
	"delete": true,
	"del":    true,
	"rename": true,
}

// complete returns the candidates for the word that ends at the cursor, and the index at which they
// replace the existing text.
func (s *shell) complete(line []rune, cursor int) ([]string, int) {
	if cursor > len(line) {
		cursor = len(line)
	}
	text := string(line[:cursor])

	// The word being completed starts after the last separator.
	start := strings.LastIndexAny(text, " \t") + 1
	word := text[start:]
	before := splitWords(text[:start])

	verb := ""
	if len(before) > 0 {
		verb = strings.ToLower(before[0])
	}
	object := ""
	if len(before) > 1 {
		object = strings.ToLower(before[1])
	}

	last := ""
	if len(before) > 0 {
		last = before[len(before)-1]
	}
	// A flag waiting for a path value.
	if pathFlags[last] {
		return pathCompletions(word), start
	}
	// A flag waiting for a world name.
	if worldFlags[last] {
		return matchPrefix(s.worldNames(), word), start
	}

	switch {
	case verb == "":
		return matchPrefix(shellVerbs, word), start

	case verb == "cd":
		return pathCompletions(word), start

	case strings.HasPrefix(word, "-"):
		return matchPrefix(shellFlags[verb], word), start

	case verb == "import":
		// Import takes paths, but the object still comes first.
		if len(before) == 1 {
			return matchPrefix(shellObjects[verb], word), start
		}
		if _, ok := contentKindOf(object); ok && len(before) >= 2 {
			return pathCompletions(word), start
		}
		return nil, start

	case verb == "start":
		if len(before) == 1 {
			return matchPrefix(s.instanceNames(), word), start
		}
		return nil, start

	case verb == "search":
		if len(before) == 1 {
			return matchPrefix([]string{"game", "versions", "fabric", "quilt", "forge", "mods", "resourcepacks", "shaderpacks", "datapacks", "modpacks"}, word), start
		}
		return nil, start

	case contentArgVerbs[verb] && isContentObject(object):
		// "rename" takes two names; only the first is offered.
		if verb == "rename" && len(before) > 2 {
			return nil, start
		}
		return matchPrefix(s.contentNames(object), word), start

	case len(before) == 1:
		// The object of the command. A few verbs also accept a bare instance name here.
		candidates := append([]string{}, shellObjects[verb]...)
		if instanceArgVerbs[verb] {
			candidates = append(candidates, s.instanceNames()...)
		}
		if verb == "del" || verb == "delete" {
			candidates = append(candidates, accountNames()...)
		}
		return matchPrefix(dedupe(candidates), word), start

	case instanceArgVerbs[verb] && (object == "versions" || object == "version" || object == "instance"):
		// "rename" takes two names; only the first is offered.
		if verb == "rename" && len(before) > 2 {
			return nil, start
		}
		return matchPrefix(s.instanceNames(), word), start

	case (verb == "del" || verb == "delete") && object == "users":
		return matchPrefix(accountNames(), word), start
	}
	return nil, start
}

// isContentObject reports whether a canonical object name is one of the content categories.
func isContentObject(object string) bool {
	_, ok := contentKindOf(object)
	return ok
}

// contentNames lists the files of a category in the current instance, for completion.
func (s *shell) contentNames(object string) []string {
	kind, ok := contentKindOf(object)
	if !ok || s.selected == "" {
		return nil
	}
	inst, err := launcher.FetchInstance(s.selected)
	if err != nil {
		return nil
	}
	dir, err := launcher.ContentDir(inst, kind, s.contentWorld)
	if err != nil {
		return nil
	}
	items, err := launcher.ListContent(dir, kind)
	if err != nil {
		return nil
	}

	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.FileName)
	}
	return names
}

// worldNames lists the worlds of the current instance, for completion.
func (s *shell) worldNames() []string {
	if s.selected == "" {
		return nil
	}
	inst, err := launcher.FetchInstance(s.selected)
	if err != nil {
		return nil
	}
	worlds, err := launcher.Worlds(inst)
	if err != nil {
		return nil
	}
	return worlds
}

// instanceNames lists the installed instances, cached briefly so that repeated tab presses stay
// responsive.
func (s *shell) instanceNames() []string {
	if time.Since(s.namesAt) < 3*time.Second {
		return s.namesCache
	}

	instances, err := launcher.FetchAllInstances()
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(instances))
	for _, inst := range instances {
		names = append(names, inst.Name)
	}

	s.namesCache, s.namesAt = names, time.Now()
	return names
}

// accountNames lists the stored accounts.
func accountNames() []string {
	names := make([]string, 0, len(auth.Store.Accounts))
	for _, account := range auth.Store.Accounts {
		names = append(names, account.Label())
	}
	return names
}

// matchPrefix returns the entries starting with prefix, ignoring case.
func matchPrefix(values []string, prefix string) []string {
	if prefix == "" {
		return dedupe(values)
	}
	lower := strings.ToLower(prefix)
	var out []string
	for _, value := range values {
		if strings.HasPrefix(strings.ToLower(value), lower) {
			out = append(out, value)
		}
	}
	return dedupe(out)
}

// dedupe removes empty and repeated entries, and sorts what is left so that the candidate order is
// stable.
func dedupe(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

// splitWords splits a command line into words, honouring double quotes.
func splitWords(text string) []string {
	var words []string
	var current strings.Builder
	quoted := false

	flush := func() {
		if current.Len() > 0 {
			words = append(words, current.String())
			current.Reset()
		}
	}

	for _, r := range text {
		switch {
		case r == '"':
			quoted = !quoted
		case (r == ' ' || r == '\t') && !quoted:
			flush()
		default:
			current.WriteRune(r)
		}
	}
	flush()
	return words
}

// pathCompletions returns filesystem completions for a partially typed path.
//
// The typed prefix is preserved as it was written, so ".\min" completes to ".\minecraft\".
func pathCompletions(partial string) []string {
	dir, base := "", partial
	if i := strings.LastIndexAny(partial, `/\`); i >= 0 {
		dir, base = partial[:i+1], partial[i+1:]
	}

	// The directory part is only used for reading, so separators and ~ are expanded separately.
	read := dir
	if read == "" {
		read = "."
	}
	if expanded, ok := expandHome(read); ok {
		read = expanded
	}
	if trimmed := strings.TrimRight(read, `/\`); trimmed != "" {
		read = trimmed
		// "D:" means the current directory of that drive, which is not what a typed "D:/" means.
		if len(read) == 2 && read[1] == ':' {
			read += string(os.PathSeparator)
		}
	} else if read != "/" && read != `\` {
		read = string(os.PathSeparator)
	}

	entries, err := os.ReadDir(read)
	if err != nil {
		return nil
	}

	// Directories keep the separator style the user is already typing.
	separator := string(os.PathSeparator)
	if strings.HasSuffix(dir, "/") {
		separator = "/"
	}

	lower := strings.ToLower(base)
	var out []string
	for _, entry := range entries {
		if !strings.HasPrefix(strings.ToLower(entry.Name()), lower) {
			continue
		}
		candidate := dir + entry.Name()
		if entry.IsDir() {
			candidate += separator
		}
		out = append(out, candidate)
	}
	return dedupe(out)
}
