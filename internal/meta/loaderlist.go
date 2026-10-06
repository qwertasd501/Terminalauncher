package meta

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/telecter/cmd-launcher/internal/network"
)

// The listing endpoints used to fill the interactive version pickers.
const (
	ForgeMetadataURL      = "https://maven.minecraftforge.net/net/minecraftforge/forge/maven-metadata.xml"
	NeoforgeVersionsURL   = "https://maven.neoforged.net/api/maven/versions/releases/net/neoforged/neoforge"
	NeoforgeLegacyVersURL = "https://maven.neoforged.net/api/maven/versions/releases/net/neoforged/forge"
)

// A listingClient bounds how long a picker waits for a version list.
var listingClient = &http.Client{Timeout: 30 * time.Second}

// A GameVersion is one Minecraft version an instance can be created for.
type GameVersion struct {
	ID   string
	Type string
}

// Label renders the version for a menu, naming anything that is not a regular release.
func (v GameVersion) Label() string {
	if v.Type != "" && v.Type != "release" {
		return fmt.Sprintf("%s  (%s)", v.ID, v.Type)
	}
	return v.ID
}

// A GameVersionList is the set of Minecraft versions, newest first.
type GameVersionList struct {
	Versions    []GameVersion
	Recommended int // Index of the current release
}

// A LoaderVersionList is the set of mod loader versions usable with one game version, newest first.
type LoaderVersionList struct {
	Versions    []string
	Recommended int // Index of the version "latest" would resolve to
}

// FetchGameVersions lists every Minecraft version from the official manifest.
func FetchGameVersions() (GameVersionList, error) {
	manifest, err := FetchVersionManifest()
	if err != nil {
		return GameVersionList{}, fmt.Errorf("retrieve version manifest: %w", err)
	}

	list := GameVersionList{Versions: make([]GameVersion, 0, len(manifest.Versions))}
	for _, version := range manifest.Versions {
		list.Versions = append(list.Versions, GameVersion{ID: version.ID, Type: version.Type})
	}
	list.Recommended = indexOfGameVersion(list.Versions, manifest.Latest.Release)
	return list, nil
}

// FetchLoaderVersions lists the mod loader versions that can be used with a game version.
//
// Vanilla has no loader, so its list is empty.
func FetchLoaderVersions(loader Loader, gameVersion string) (LoaderVersionList, error) {
	var (
		versions []string
		err      error
	)

	switch loader {
	case LoaderVanilla:
		return LoaderVersionList{}, nil
	case LoaderFabric, LoaderQuilt:
		api := Fabric
		if loader == LoaderQuilt {
			api = Quilt
		}
		versions, err = fabricLoaderVersions(api)
	case LoaderForge:
		var all []string
		all, err = fetchMavenVersions(ForgeMetadataURL)
		versions = filterPrefixed(all, gameVersion+"-")
	case LoaderNeoForge:
		versions, err = neoforgeLoaderVersions(gameVersion)
	default:
		err = fmt.Errorf("no version list for loader %s", loader)
	}
	if err != nil {
		return LoaderVersionList{}, err
	}

	sortNewestFirst(versions)
	list := LoaderVersionList{Versions: versions}
	list.Recommended = recommendedLoaderVersion(loader, gameVersion, versions)
	return list, nil
}

// neoforgeLoaderVersions lists NeoForge versions for one game version.
//
// NeoForge changed both its artifact name and its version scheme after 1.20.1, so the two series come
// from different coordinates.
func neoforgeLoaderVersions(gameVersion string) ([]string, error) {
	if gameVersion == "1.20.1" {
		all, err := fetchVersionAPI(NeoforgeLegacyVersURL)
		if err != nil {
			return nil, err
		}
		return filterPrefixed(all, "1.20.1-"), nil
	}

	all, err := fetchVersionAPI(NeoforgeVersionsURL)
	if err != nil {
		return nil, err
	}
	// NeoForge drops the legacy "1." prefix: 1.21.8 becomes 21.8, while 26.3 stays 26.3.
	return filterPrefixed(all, strings.TrimPrefix(gameVersion, "1.")+"."), nil
}

// fabricLoaderVersions lists the loader versions published by a Fabric-style metadata API.
func fabricLoaderVersions(api fabricAPI) ([]string, error) {
	versions, err := api.FetchVersions()
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(versions))
	for _, version := range versions {
		out = append(out, version.Version)
	}
	return out, nil
}

// recommendedLoaderVersion reports which entry is offered by default: the one that the launcher's
// "latest" would resolve to, so that pressing enter keeps the established behaviour.
func recommendedLoaderVersion(loader Loader, gameVersion string, versions []string) int {
	if len(versions) == 0 {
		return 0
	}

	var latest string
	var err error
	switch loader {
	case LoaderForge:
		latest, err = FetchForgeVersion(gameVersion)
	case LoaderNeoForge:
		latest, err = FetchNeoforgeVersion(gameVersion)
	case LoaderFabric, LoaderQuilt:
		api := Fabric
		if loader == LoaderQuilt {
			api = Quilt
		}
		var list FabricVersionList
		list, err = api.FetchVersions()
		if err == nil && len(list) > 0 {
			latest = list[0].Version
		}
	}
	if err != nil || latest == "" {
		return 0
	}

	for i, version := range versions {
		if version == latest {
			return i
		}
	}
	return 0
}

// An xmlMetadata is the subset of a Maven "maven-metadata.xml" that lists an artifact's versions.
type xmlMetadata struct {
	Versioning struct {
		Versions []string `xml:"versions>version"`
	} `xml:"versioning"`
}

// fetchMavenVersions reads every version published for a Maven artifact.
func fetchMavenVersions(url string) ([]string, error) {
	body, err := getBody(url)
	if err != nil {
		return nil, err
	}

	var metadata xmlMetadata
	if err := xml.Unmarshal(body, &metadata); err != nil {
		return nil, fmt.Errorf("parse version list: %w", err)
	}
	return metadata.Versioning.Versions, nil
}

// fetchVersionAPI reads the version list published by the NeoForged Maven API.
func fetchVersionAPI(url string) ([]string, error) {
	body, err := getBody(url)
	if err != nil {
		return nil, err
	}

	var data struct {
		Versions []string `json:"versions"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("parse version list: %w", err)
	}
	return data.Versions, nil
}

// getBody performs a listing request and returns its body.
func getBody(url string) ([]byte, error) {
	resp, err := listingClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if err := network.CheckResponse(resp); err != nil {
		return nil, err
	}
	return io.ReadAll(resp.Body)
}

// filterPrefixed keeps the versions starting with prefix, such as "1.20.1-" or "21.8.".
func filterPrefixed(versions []string, prefix string) []string {
	out := make([]string, 0, len(versions))
	for _, version := range versions {
		if strings.HasPrefix(version, prefix) {
			out = append(out, version)
		}
	}
	return out
}

// sortNewestFirst orders versions so that the newest comes first.
//
// Maven metadata carries no reliable order, so versions are compared by their numeric components
// rather than as strings: 47.4.26 is newer than 47.4.5, which a string comparison would get wrong.
func sortNewestFirst(versions []string) {
	sort.SliceStable(versions, func(i, j int) bool {
		return isNewerVersion(versions[i], versions[j])
	})
}

// isNewerVersion reports whether a is a newer version than b.
func isNewerVersion(a, b string) bool {
	na, nb := versionNumbers(a), versionNumbers(b)
	for i := 0; i < len(na) && i < len(nb); i++ {
		if na[i] != nb[i] {
			return na[i] > nb[i]
		}
	}
	if len(na) != len(nb) {
		return len(na) > len(nb)
	}
	// A finished release outranks a pre-release with the same numbers.
	preA, preB := strings.Contains(a, "-"), strings.Contains(b, "-")
	if preA != preB {
		return !preA
	}
	return a > b
}

// versionNumbers extracts the numeric components of a version, e.g. "1.20.1-47.4.26" becomes
// [1 20 1 47 4 26] and "0.16.10+build.1309" becomes [0 16 10 1309].
func versionNumbers(version string) []int {
	cleaned := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' || r == '.' {
			return r
		}
		return '.'
	}, version)

	var out []int
	for _, field := range strings.Split(cleaned, ".") {
		if field == "" {
			continue
		}
		n, err := strconv.Atoi(field)
		if err != nil {
			continue
		}
		out = append(out, n)
	}
	return out
}

// indexOfGameVersion returns the position of id, or 0 when it is absent.
func indexOfGameVersion(versions []GameVersion, id string) int {
	for i, version := range versions {
		if version.ID == id {
			return i
		}
	}
	return 0
}
