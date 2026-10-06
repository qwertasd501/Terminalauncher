package meta

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/telecter/cmd-launcher/internal/network"
)

// Modrinth is the source the shell installs mods, resource packs, shader packs, data packs and
// modpacks from. It needs no API key, unlike CurseForge, which is why it is the only one wired in.
const modrinthAPI = "https://api.modrinth.com/v2"

// modrinthUserAgent identifies the launcher. Modrinth rejects requests without a user agent that
// names the application.
const modrinthUserAgent = "telecter/cmd-launcher (github.com/telecter/cmd-launcher)"

// Modrinth project types, as used by the search facets.
const (
	ModrinthMod          = "mod"
	ModrinthResourcePack = "resourcepack"
	ModrinthShader       = "shader"
	ModrinthDataPack     = "datapack"
	ModrinthModpack      = "modpack"
)

// A ModrinthProject is one search result.
type ModrinthProject struct {
	ProjectID   string   `json:"project_id"`
	Slug        string   `json:"slug"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Author      string   `json:"author"`
	ProjectType string   `json:"project_type"`
	Downloads   int      `json:"downloads"`
	Follows     int      `json:"follows"`
	Categories  []string `json:"categories"`
	Versions    []string `json:"versions"`
	IconURL     string   `json:"icon_url"`
	Updated     string   `json:"date_modified"`
}

// A ModrinthSearch is a page of search results.
type ModrinthSearch struct {
	Hits   []ModrinthProject `json:"hits"`
	Offset int               `json:"offset"`
	Limit  int               `json:"limit"`
	Total  int               `json:"total_hits"`
}

// A ModrinthVersion is one downloadable release of a project.
type ModrinthVersion struct {
	ID            string               `json:"id"`
	ProjectID     string               `json:"project_id"`
	Name          string               `json:"name"`
	VersionNumber string               `json:"version_number"`
	VersionType   string               `json:"version_type"`
	GameVersions  []string             `json:"game_versions"`
	Loaders       []string             `json:"loaders"`
	DatePublished string               `json:"date_published"`
	Files         []ModrinthFile       `json:"files"`
	Dependencies  []ModrinthDependency `json:"dependencies"`
}

// PrimaryFile returns the file to download, preferring the one Modrinth marks as primary.
func (version ModrinthVersion) PrimaryFile() (ModrinthFile, bool) {
	for _, file := range version.Files {
		if file.Primary {
			return file, true
		}
	}
	if len(version.Files) > 0 {
		return version.Files[0], true
	}
	return ModrinthFile{}, false
}

// A ModrinthFile is one download of a version.
type ModrinthFile struct {
	Filename string            `json:"filename"`
	URL      string            `json:"url"`
	Size     int64             `json:"size"`
	Primary  bool              `json:"primary"`
	Hashes   map[string]string `json:"hashes"`
}

// A ModrinthDependency is a project or version a version depends on.
type ModrinthDependency struct {
	VersionID      string `json:"version_id"`
	ProjectID      string `json:"project_id"`
	DependencyType string `json:"dependency_type"`
}

// ModrinthFacets builds the facet groups of a search for one project type.
//
// Facets are ANDed between groups and ORed inside a group, which is what lets a search ask for "a
// mod that runs on this game version with either of these loaders" without also matching every
// other mod the loader happens to support.
func ModrinthFacets(projectType string, loaders []string) [][]string {
	var groups [][]string
	if projectType != "" {
		groups = append(groups, []string{"project_type:" + projectType})
	}
	if len(loaders) > 0 {
		group := make([]string, 0, len(loaders))
		for _, loader := range loaders {
			if loader != "" {
				group = append(group, "categories:"+loader)
			}
		}
		if len(group) > 0 {
			groups = append(groups, group)
		}
	}
	return groups
}

// ModrinthSearchProjects queries the search API.
func ModrinthSearchProjects(query string, facets [][]string, gameVersion string, limit int) (ModrinthSearch, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	groups := make([][]string, 0, len(facets)+1)
	groups = append(groups, facets...)
	if gameVersion != "" {
		groups = append(groups, []string{"versions:" + gameVersion})
	}

	values := url.Values{}
	values.Set("query", query)
	values.Set("limit", strconv.Itoa(limit))
	values.Set("index", "relevance")
	if len(groups) > 0 {
		encoded, err := json.Marshal(groups)
		if err != nil {
			return ModrinthSearch{}, err
		}
		values.Set("facets", string(encoded))
	}

	var result ModrinthSearch
	if err := modrinthGet(modrinthAPI+"/search?"+values.Encode(), &result); err != nil {
		return ModrinthSearch{}, fmt.Errorf("search Modrinth: %w", err)
	}
	return result, nil
}

// ModrinthProjectVersions lists a project's versions, optionally filtered by game version and
// loaders. Results come newest first.
func ModrinthProjectVersions(project string, gameVersions, loaders []string) ([]ModrinthVersion, error) {
	values := url.Values{}
	if len(gameVersions) > 0 {
		encoded, err := json.Marshal(gameVersions)
		if err != nil {
			return nil, err
		}
		values.Set("game_versions", string(encoded))
	}
	if len(loaders) > 0 {
		encoded, err := json.Marshal(loaders)
		if err != nil {
			return nil, err
		}
		values.Set("loaders", string(encoded))
	}

	endpoint := fmt.Sprintf("%s/project/%s/version", modrinthAPI, url.PathEscape(project))
	if query := values.Encode(); query != "" {
		endpoint += "?" + query
	}

	var versions []ModrinthVersion
	if err := modrinthGet(endpoint, &versions); err != nil {
		return nil, fmt.Errorf("list versions of %s: %w", project, err)
	}
	return versions, nil
}

// FetchModrinthProject looks a project up by its id or slug.
func FetchModrinthProject(idOrSlug string) (ModrinthProject, error) {
	var project ModrinthProject
	if err := modrinthGet(fmt.Sprintf("%s/project/%s", modrinthAPI, url.PathEscape(idOrSlug)), &project); err != nil {
		return ModrinthProject{}, err
	}
	return project, nil
}

// modrinthAttempts is how often one API call is made before its failure is reported.
const modrinthAttempts = 4

// Rate limit and server error handling. Modrinth limits an IP address to a few hundred requests a
// minute, and a bulk lookup of an installed mod list makes a burst of them, so being told to slow
// down must not be reported as "this mod could not be matched".
const (
	modrinthDefaultWait = time.Second
	modrinthMaxWait     = 10 * time.Second
	modrinthServerWait  = 750 * time.Millisecond
)

// modrinthGet performs a GET request against the Modrinth API and decodes its JSON body.
//
// A "slow down" or a server error is retried after the delay the API asks for; a plain error, such
// as a project that does not exist, is returned straight away.
func modrinthGet(endpoint string, target any) error {
	var lastErr error
	for attempt := 0; attempt < modrinthAttempts; attempt++ {
		wait, err := modrinthGetOnce(endpoint, target)
		if err == nil {
			return nil
		}
		lastErr = err
		if wait <= 0 || attempt == modrinthAttempts-1 {
			return err
		}
		time.Sleep(wait)
	}
	return lastErr
}

// modrinthGetOnce performs one API call, returning how long to wait before repeating it. A wait of
// zero means the error is final.
func modrinthGetOnce(endpoint string, target any) (time.Duration, error) {
	request, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, err
	}
	request.Header.Set("User-Agent", modrinthUserAgent)
	request.Header.Set("Accept", "application/json")

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if err := network.CheckResponse(response); err != nil {
		return modrinthRetryWait(response), err
	}
	return 0, json.NewDecoder(response.Body).Decode(target)
}

// modrinthRetryWait returns how long to wait before repeating a rejected call, or zero when
// repeating it is pointless.
func modrinthRetryWait(response *http.Response) time.Duration {
	switch {
	case response.StatusCode == http.StatusTooManyRequests:
		if seconds, err := strconv.Atoi(response.Header.Get("Retry-After")); err == nil && seconds > 0 {
			wait := time.Duration(seconds) * time.Second
			if wait > modrinthMaxWait {
				wait = modrinthMaxWait
			}
			return wait
		}
		return modrinthDefaultWait
	case response.StatusCode >= 500:
		return modrinthServerWait
	}
	return 0
}

// ModrinthLoaders lists the loader categories a shell content kind is compatible with.
//
// Quilt runs Fabric mods, so a Quilt instance offers both; everything else accepts only its own
// loader, which is what PCL's download page does.
func ModrinthLoaders(loader Loader) []string {
	switch loader {
	case LoaderFabric:
		return []string{"fabric"}
	case LoaderQuilt:
		return []string{"quilt", "fabric"}
	case LoaderForge:
		return []string{"forge"}
	case LoaderNeoForge:
		return []string{"neoforge"}
	}
	return nil
}

// ModrinthProjectType maps a shell category onto the Modrinth project type used in a search.
func ModrinthProjectType(kind string) string {
	switch strings.ToLower(kind) {
	case "mods":
		return ModrinthMod
	case "resourcepacks":
		return ModrinthResourcePack
	case "shaderpacks":
		return ModrinthShader
	case "datapacks":
		return ModrinthDataPack
	case "modpacks":
		return ModrinthModpack
	}
	return ""
}
