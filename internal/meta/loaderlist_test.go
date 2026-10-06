package meta

import (
	"reflect"
	"testing"
)

func TestVersionNumbers(t *testing.T) {
	cases := []struct {
		version string
		want    []int
	}{
		{"1.20.1-47.4.26", []int{1, 20, 1, 47, 4, 26}},
		{"21.1.228", []int{21, 1, 228}},
		{"26.3.0.48-beta", []int{26, 3, 0, 48}},
		{"0.16.10+build.1309", []int{0, 16, 10, 1309}},
		{"1.21", []int{1, 21}},
	}
	for _, c := range cases {
		if got := versionNumbers(c.version); !reflect.DeepEqual(got, c.want) {
			t.Errorf("versionNumbers(%q) = %v, want %v", c.version, got, c.want)
		}
	}
}

func TestIsNewerVersion(t *testing.T) {
	// A string comparison would order "47.4.5" above "47.4.26", which is the bug this guards against.
	cases := []struct {
		a, b string
		want bool
	}{
		{"1.20.1-47.4.26", "1.20.1-47.4.5", true},
		{"1.20.1-47.4.5", "1.20.1-47.4.26", false},
		{"21.1.228", "21.1.99", true},
		{"0.16.10+build.1309", "0.16.10+build.99", true},
		{"20.2.19", "20.2.19-beta", true},
		{"20.2.19-beta", "20.2.19", false},
	}
	for _, c := range cases {
		if got := isNewerVersion(c.a, c.b); got != c.want {
			t.Errorf("isNewerVersion(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestSortNewestFirst(t *testing.T) {
	versions := []string{"1.20.1-47.4.5", "1.20.1-47.4.26", "1.20.1-47.1.106", "1.20.1-47.0.1"}
	sortNewestFirst(versions)

	want := []string{"1.20.1-47.4.26", "1.20.1-47.4.5", "1.20.1-47.1.106", "1.20.1-47.0.1"}
	if !reflect.DeepEqual(versions, want) {
		t.Errorf("sortNewestFirst = %v, want %v", versions, want)
	}
}

func TestFilterPrefixed(t *testing.T) {
	all := []string{"1.21-51.0.33", "1.20.1-47.4.26", "21.1.228", "1.20.1-47.4.25"}

	got := filterPrefixed(all, "1.20.1-")
	want := []string{"1.20.1-47.4.26", "1.20.1-47.4.25"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("filterPrefixed = %v, want %v", got, want)
	}

	// Filtering must not treat "21.1.228" as a match for the "21.8." series.
	if got := filterPrefixed([]string{"21.1.228", "21.8.52"}, "21.8."); !reflect.DeepEqual(got, []string{"21.8.52"}) {
		t.Errorf("filterPrefixed = %v, want [21.8.52]", got)
	}
}

func TestGameVersionLabel(t *testing.T) {
	cases := []struct {
		version GameVersion
		want    string
	}{
		{GameVersion{ID: "26.3", Type: "release"}, "26.3"},
		{GameVersion{ID: "25w31a", Type: "snapshot"}, "25w31a  (snapshot)"},
		{GameVersion{ID: "1.21.8-pre1", Type: "old_beta"}, "1.21.8-pre1  (old_beta)"},
	}
	for _, c := range cases {
		if got := c.version.Label(); got != c.want {
			t.Errorf("Label() = %q, want %q", got, c.want)
		}
	}
}
