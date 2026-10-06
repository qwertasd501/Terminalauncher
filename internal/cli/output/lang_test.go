package output

import (
	"regexp"
	"sort"
	"strings"
	"testing"

	"golang.org/x/text/language"
)

// formatVerbs returns the sorted format verbs of a translation, so that two wordings of the same
// message can be compared without caring about the words.
func formatVerbs(text string) string {
	verbs := regexp.MustCompile(`%[a-zA-Z]`).FindAllString(text, -1)
	sort.Strings(verbs)
	return strings.Join(verbs, " ")
}

// TestTranslationsMatchEnglish checks that every language defines the same keys as English, and
// that a translated message keeps the format verbs of the English one. A missing key would make
// the shell print the key itself, and a wrong verb would print something like "%!s(int=0)".
func TestTranslationsMatchEnglish(t *testing.T) {
	others := map[string]translations{"de": de, "zh": zh, "fr": fr, "ru": ru, "es": es}

	for name, other := range others {
		for key, text := range en {
			translated, ok := other[key]
			if !ok {
				t.Errorf("%s: missing translation for %q", name, key)
				continue
			}
			if want, got := formatVerbs(text), formatVerbs(translated); want != got {
				t.Errorf("%s: %q uses verbs %q, but English uses %q", name, key, got, want)
			}
		}
		for key := range other {
			if _, ok := en[key]; !ok {
				t.Errorf("%s: translation %q has no English original", name, key)
			}
		}
	}
}

// TestLanguageRegistry checks the shape of the language list: unique codes, exactly one entry that
// follows the system language, and a menu name for every entry.
func TestLanguageRegistry(t *testing.T) {
	seen := make(map[string]bool)
	following := 0
	for _, candidate := range Languages() {
		if candidate.Name == "" {
			t.Errorf("language %q has no menu name", candidate.Code)
		}
		if seen[candidate.Code] {
			t.Errorf("language code %q is listed twice", candidate.Code)
		}
		seen[candidate.Code] = true
		if candidate.Code == "" {
			following++
		}
	}
	if following != 1 {
		t.Errorf("expected exactly one entry that follows the system language, found %d", following)
	}
}

// TestSetLangCode checks that a stored code selects its translations, that an unknown code falls
// back to the language the system asked for, and that an empty code selects it again.
func TestSetLangCode(t *testing.T) {
	previous := CurrentLanguage()
	t.Cleanup(func() { SetLangCode(previous) })

	SetLang(language.German)
	if got := SetLangCode("zh"); got != "zh" {
		t.Fatalf("SetLangCode(zh) reported %q", got)
	}
	if got := Translate("shell.exit"); got != zh["shell.exit"] {
		t.Errorf("Chinese translations were not applied: %q", got)
	}

	// The other languages added later must select through the same path.
	for _, code := range []string{"fr", "ru", "es"} {
		if got := SetLangCode(code); got != code {
			t.Errorf("SetLangCode(%q) reported %q", code, got)
		}
	}

	// An unknown code must not leave the launcher in the previous language.
	if got := SetLangCode("klingon"); got != "" {
		t.Errorf("SetLangCode(klingon) reported %q, expected the system language", got)
	}
	if got := Translate("shell.exit"); got != de["shell.exit"] {
		t.Errorf("German translations were not restored: %q", got)
	}

	// A locale with a region and a script still selects its base language.
	SetLang(language.MustParse("zh-Hans-CN"))
	if got := Translate("shell.exit"); got != zh["shell.exit"] {
		t.Errorf("zh-Hans-CN did not select Chinese: %q", got)
	}
}

// TestLanguageName checks the menu label of a language, including the system entry, which names the
// language the operating system asked for.
func TestLanguageName(t *testing.T) {
	previous := CurrentLanguage()
	t.Cleanup(func() { SetLangCode(previous) })

	SetLang(language.Chinese)
	if name := LanguageName(""); !strings.Contains(name, "简体中文") {
		t.Errorf("system language is shown as %q, expected the detected language", name)
	}
	if name := LanguageName("de"); name != "Deutsch" {
		t.Errorf("German is shown as %q", name)
	}
	// An unknown code falls back to the system entry rather than showing nothing.
	if name := LanguageName("klingon"); name == "" {
		t.Error("unknown language has no label")
	}
}
