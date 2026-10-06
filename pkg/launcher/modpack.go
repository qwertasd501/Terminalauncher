package launcher

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/qwertasd501/Terminalauncher/internal/meta"
	"github.com/qwertasd501/Terminalauncher/internal/network"
	env "github.com/qwertasd501/Terminalauncher/pkg"
)

// ModpackMarker is written into an instance installed from a modpack, so that the shell can list
// which instances came from one.
const ModpackMarker = "modpack.json"

// A MrpackIndex is the modrinth.index.json of a .mrpack file, the format Modrinth uses to describe
// a modpack: the files to download plus the loader the pack runs on.
type MrpackIndex struct {
	FormatVersion int               `json:"formatVersion"`
	Game          string            `json:"game"`
	VersionID     string            `json:"versionId"`
	Name          string            `json:"name"`
	Summary       string            `json:"summary"`
	Files         []MrpackFile      `json:"files"`
	Dependencies  map[string]string `json:"dependencies"`
}

// A MrpackFile is one file a modpack needs.
type MrpackFile struct {
	Path      string            `json:"path"`
	Hashes    map[string]string `json:"hashes"`
	Env       map[string]string `json:"env"`
	Downloads []string          `json:"downloads"`
	FileSize  int64             `json:"fileSize"`
}

// A ModpackMarkerFile records where an instance came from.
type ModpackMarkerFile struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	ProjectID string `json:"project_id,omitempty"`
	Summary   string `json:"summary,omitempty"`
}

// ReadMrpack reads the index of a .mrpack archive.
func ReadMrpack(archive string) (MrpackIndex, error) {
	r, err := zip.OpenReader(archive)
	if err != nil {
		return MrpackIndex{}, fmt.Errorf("open modpack: %w", err)
	}
	defer r.Close()

	file := zipEntry(r.File, "modrinth.index.json")
	if file == nil {
		return MrpackIndex{}, fmt.Errorf("not a Modrinth modpack: no modrinth.index.json")
	}
	data, err := readZipFile(file)
	if err != nil {
		return MrpackIndex{}, err
	}

	var index MrpackIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return MrpackIndex{}, fmt.Errorf("decode modpack index: %w", err)
	}
	if index.Dependencies["minecraft"] == "" {
		return MrpackIndex{}, fmt.Errorf("modpack does not name a Minecraft version")
	}
	return index, nil
}

// Loader returns the mod loader and its version, as named by the pack's dependencies.
func (index MrpackIndex) Loader() (meta.Loader, string) {
	for dependency, loader := range map[string]meta.Loader{
		"neoforge":      meta.LoaderNeoForge,
		"forge":         meta.LoaderForge,
		"fabric-loader": meta.LoaderFabric,
		"quilt-loader":  meta.LoaderQuilt,
	} {
		if version := index.Dependencies[dependency]; version != "" {
			return loader, version
		}
	}
	return meta.LoaderVanilla, ""
}

// An InstallProgress reports how a modpack installation is going.
type InstallProgress struct {
	Downloaded int
	Total      int
}

// InstallModpack creates an instance from a .mrpack archive.
//
// The instance is created first so that a failure part way through leaves something the shell can
// list and delete, rather than a directory nothing knows about. Overrides are unpacked before the
// downloads, because they are small and describe how the pack is meant to be played.
func InstallModpack(archive string, index MrpackIndex, name string, progress func(InstallProgress)) (Instance, error) {
	loader, loaderVersion := index.Loader()

	inst, err := CreateInstance(InstanceOptions{
		Name:          name,
		GameVersion:   index.Dependencies["minecraft"],
		Loader:        loader,
		LoaderVersion: loaderVersion,
	})
	if err != nil {
		if loaderVersion == "" {
			return Instance{}, err
		}
		// A pack pinned to a loader build that no longer resolves still installs, on the latest one.
		inst, err = CreateInstance(InstanceOptions{
			Name:        name,
			GameVersion: index.Dependencies["minecraft"],
			Loader:      loader,
		})
		if err != nil {
			return Instance{}, err
		}
	}

	if err := extractOverrides(archive, inst.Dir()); err != nil {
		return inst, err
	}

	files, err := modpackDownloads(inst.Dir(), index)
	if err != nil {
		return inst, err
	}

	total := len(files)
	if progress != nil {
		progress(InstallProgress{Total: total})
	}

	// The downloads run in parallel, so the batch is drained to the end even when one file fails:
	// returning early would leave the progress bar short of the total.
	finished := 0
	for result := range network.StartDownloadEntries(files) {
		finished++
		if progress != nil {
			progress(InstallProgress{Downloaded: finished, Total: total})
		}
		if result != nil && err == nil {
			err = fmt.Errorf("download modpack files: %w", result)
		}
	}
	if err != nil {
		return inst, err
	}

	inst.Config.Description = index.Summary
	if err := inst.WriteConfig(); err != nil {
		return inst, err
	}
	if err := WriteModpackMarker(inst, ModpackMarkerFile{
		Name:    index.Name,
		Version: index.VersionID,
		Summary: index.Summary,
	}); err != nil {
		return inst, err
	}
	return inst, nil
}

// modpackDownloads turns the index into download entries, skipping files the server needs but a
// client does not, and anything that would land outside the instance directory.
func modpackDownloads(dir string, index MrpackIndex) ([]network.DownloadEntry, error) {
	files := make([]network.DownloadEntry, 0, len(index.Files))
	for _, file := range index.Files {
		if file.Env["client"] == "unsupported" {
			continue
		}
		if len(file.Downloads) == 0 {
			continue
		}

		target, err := safeJoin(dir, file.Path)
		if err != nil {
			return nil, err
		}
		files = append(files, network.DownloadEntry{
			URL:  file.Downloads[0],
			Path: target,
			Sha1: file.Hashes["sha1"],
		})
	}
	return files, nil
}

// extractOverrides unpacks the overrides trees of a modpack into the instance directory.
//
// Both "overrides" and "client-overrides" are applied; a client installation wants both, and the
// client tree is meant to win.
func extractOverrides(archive, dir string) error {
	r, err := zip.OpenReader(archive)
	if err != nil {
		return fmt.Errorf("open modpack: %w", err)
	}
	defer r.Close()

	for _, tree := range []string{"overrides/", "client-overrides/"} {
		for _, file := range r.File {
			if !strings.HasPrefix(strings.ToLower(file.Name), tree) {
				continue
			}
			relative := path.Clean(file.Name[len(tree):])
			if relative == "." || relative == "" {
				continue
			}

			target, err := safeJoin(dir, relative)
			if err != nil {
				return err
			}
			if file.FileInfo().IsDir() {
				if err := os.MkdirAll(target, 0755); err != nil {
					return err
				}
				continue
			}
			if err := writeZipFile(file, target); err != nil {
				return err
			}
		}
	}
	return nil
}

// writeZipFile extracts one entry, creating its parent directory.
func writeZipFile(file *zip.File, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}

	in, err := file.Open()
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(target)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// safeJoin joins a path taken from an archive onto dir, refusing anything that escapes it.
//
// A modpack index is untrusted input: a path such as "../../windows/system32" would otherwise be
// written outside the instance.
func safeJoin(dir, relative string) (string, error) {
	cleaned := path.Clean(strings.ReplaceAll(relative, `\`, "/"))
	if cleaned == "." || path.IsAbs(cleaned) || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("modpack path %q escapes the instance directory", relative)
	}
	return filepath.Join(dir, filepath.FromSlash(cleaned)), nil
}

// WriteModpackMarker records the modpack an instance was installed from.
func WriteModpackMarker(inst Instance, marker ModpackMarkerFile) error {
	data, err := json.MarshalIndent(marker, "", "    ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(inst.Dir(), ModpackMarker), data, 0644)
}

// ReadModpackMarker reads the marker of an instance, reporting whether it has one.
func ReadModpackMarker(inst Instance) (ModpackMarkerFile, bool) {
	data, err := os.ReadFile(filepath.Join(inst.Dir(), ModpackMarker))
	if err != nil {
		return ModpackMarkerFile{}, false
	}
	var marker ModpackMarkerFile
	if err := json.Unmarshal(data, &marker); err != nil {
		return ModpackMarkerFile{}, false
	}
	return marker, true
}

// FetchModpackArchive downloads a modpack archive into the launcher's cache and returns its path.
func FetchModpackArchive(url string) (string, error) {
	name := filepath.Base(strings.SplitN(url, "?", 2)[0])
	if name == "" || name == "." || name == "/" {
		return "", fmt.Errorf("invalid modpack URL %q", url)
	}

	cached := filepath.Join(env.CachesDir, "modpacks", name)
	if info, err := os.Stat(cached); err == nil && info.Size() > 0 {
		return cached, nil
	}
	if err := network.DownloadFile(network.DownloadEntry{URL: url, Path: cached}); err != nil {
		return "", err
	}
	return cached, nil
}
