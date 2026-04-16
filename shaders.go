package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ShaderInfo holds local and remote info about an installed shader pack.
type ShaderInfo struct {
	Filename       string
	SHA1           string
	Path           string
	Found          bool
	ProjectID      string
	ProjectSlug    string
	ProjectTitle   string
	CurrentVersion string
	LatestVersion  string
	HasUpdate      bool
	UpdateURL      string
	UpdateFilename string
}

// versionPattern matches common version patterns in shader filenames.
var versionPattern = regexp.MustCompile(`[_\-\s][vrV]?(\d+\.\d+[\.\d]*)`)

// parseShaderFilename tries to extract a name and version from a shader zip filename.
func parseShaderFilename(filename string) (name, version string) {
	base := strings.TrimSuffix(filename, ".zip")
	base = strings.TrimSuffix(base, ".ZIP")

	loc := versionPattern.FindStringIndex(base)
	if loc == nil {
		return base, ""
	}

	name = base[:loc[0]]
	version = strings.TrimLeft(base[loc[0]:], "_- ")
	return name, version
}

// ScanShadersFolder finds all .zip files in the shaderpacks directory.
func ScanShadersFolder(dir string) (map[string]ScannedMod, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	hashes := make(map[string]ScannedMod)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".zip") {
			continue
		}
		fullPath := filepath.Join(dir, e.Name())
		h, err := hashJarFile(fullPath) // works for any file, not just jars
		if err != nil {
			continue
		}
		hashes[h] = ScannedMod{Filename: e.Name(), Path: fullPath}
	}
	return hashes, nil
}

// CheckShaders scans a shaderpacks folder and returns info about each shader.
func CheckShaders(shadersDir string, gameVersion string) ([]ShaderInfo, error) {
	hashToFile, err := ScanShadersFolder(shadersDir)
	if err != nil {
		return nil, fmt.Errorf("scanning shaderpacks folder: %w", err)
	}

	if len(hashToFile) == 0 {
		return nil, nil
	}

	hashes := make([]string, 0, len(hashToFile))
	for h := range hashToFile {
		hashes = append(hashes, h)
	}

	versions, err := LookupVersionsByHash(hashes)
	if err != nil {
		return nil, fmt.Errorf("looking up shaders: %w", err)
	}

	projectIDs := make(map[string]bool)
	for _, v := range versions {
		projectIDs[v.ProjectID] = true
	}

	foundHashes := make([]string, 0)
	for h := range versions {
		foundHashes = append(foundHashes, h)
	}

	var updates map[string]*ModrinthVersion
	if len(foundHashes) > 0 && gameVersion != "" {
		updates, _ = CheckUpdates(foundHashes, []string{"iris", "optifine"}, []string{gameVersion})
		if updates == nil {
			updates, _ = CheckUpdates(foundHashes, []string{}, []string{gameVersion})
		}
	}

	ids := make([]string, 0, len(projectIDs))
	for id := range projectIDs {
		ids = append(ids, id)
	}
	projects, _ := LookupProjects(ids)

	var results []ShaderInfo
	for hash, scanned := range hashToFile {
		info := ShaderInfo{
			Filename: scanned.Filename,
			SHA1:     hash,
			Path:     scanned.Path,
		}

		parsedName, parsedVersion := parseShaderFilename(scanned.Filename)

		v, found := versions[hash]
		if !found {
			slug := strings.ToLower(strings.ReplaceAll(parsedName, " ", "-"))
			if proj := LookupProjectBySlug(slug); proj != nil {
				info.Found = true
				info.ProjectID = proj.ID
				info.ProjectSlug = proj.Slug
				info.ProjectTitle = proj.Title
				info.CurrentVersion = parsedVersion

				slugVersions, err := GetProjectVersions(proj.ID, []string{}, []string{gameVersion})
				if err == nil && len(slugVersions) > 0 {
					latest := slugVersions[0]
					info.LatestVersion = latest.VersionNumber
					info.HasUpdate = latest.VersionNumber != parsedVersion
					if info.HasUpdate {
						info.UpdateURL, info.UpdateFilename = primaryFileURL(latest)
					}
				}
			} else {
				info.ProjectTitle = parsedName
				info.CurrentVersion = parsedVersion
			}

			results = append(results, info)
			continue
		}

		info.Found = true
		info.ProjectID = v.ProjectID
		info.CurrentVersion = v.VersionNumber

		if projects != nil {
			if p, ok := projects[v.ProjectID]; ok {
				info.ProjectTitle = p.Title
				info.ProjectSlug = p.Slug
			}
		}

		if updates != nil {
			if latest, ok := updates[hash]; ok {
				info.LatestVersion = latest.VersionNumber
				info.HasUpdate = latest.VersionNumber != v.VersionNumber
				if info.HasUpdate {
					info.UpdateURL, info.UpdateFilename = primaryFileURL(latest)
				}
			}
		}

		results = append(results, info)
	}

	return results, nil
}
