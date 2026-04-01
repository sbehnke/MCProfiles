package main

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	neturl "net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const modrinthAPI = "https://api.modrinth.com/v2"
const searchResultLimit = 10

var modrinthHTTPClient = &http.Client{
	Timeout: 30 * time.Second,
}

var trailingVersionPattern = regexp.MustCompile(`(?:^|[+\-\s_])(mc)?(\d+\.\d+(?:\.\d+)?)$`)

func isRetryableHTTPError(err error) bool {
	if err == nil {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) && (netErr.Timeout() || netErr.Temporary()) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "tls handshake timeout") ||
		strings.Contains(msg, "timeout awaiting response headers") ||
		strings.Contains(msg, "connection reset by peer") ||
		strings.Contains(msg, "unexpected eof")
}

func isRetryableStatus(code int) bool {
	switch code {
	case http.StatusRequestTimeout, http.StatusTooManyRequests,
		http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func doRequestWithRetry(req *http.Request) (*http.Response, error) {
	const maxAttempts = 3

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		cloned := req.Clone(req.Context())
		if req.GetBody != nil {
			body, err := req.GetBody()
			if err != nil {
				return nil, err
			}
			cloned.Body = body
		}

		resp, err := modrinthHTTPClient.Do(cloned)
		if err == nil {
			if !isRetryableStatus(resp.StatusCode) || attempt == maxAttempts {
				return resp, nil
			}
			resp.Body.Close()
			lastErr = fmt.Errorf("temporary HTTP %d", resp.StatusCode)
		} else {
			if !isRetryableHTTPError(err) || attempt == maxAttempts {
				return nil, err
			}
			lastErr = err
		}

		time.Sleep(time.Duration(attempt) * 750 * time.Millisecond)
	}

	return nil, lastErr
}

// ModrinthVersion represents a version returned by the Modrinth API.
type ModrinthVersion struct {
	ID            string                `json:"id"`
	ProjectID     string                `json:"project_id"`
	Name          string                `json:"name"`
	VersionNumber string                `json:"version_number"`
	GameVersions  []string              `json:"game_versions"`
	Loaders       []string              `json:"loaders"`
	Dependencies  []ModrinthDependency  `json:"dependencies"`
	Files         []ModrinthVersionFile `json:"files"`
}

// ModrinthDependency represents a dependency of a version.
type ModrinthDependency struct {
	ProjectID      string `json:"project_id"`
	VersionID      string `json:"version_id"`
	DependencyType string `json:"dependency_type"` // required, optional, incompatible, embedded
}

// ModrinthVersionFile represents a file within a version.
type ModrinthVersionFile struct {
	Hashes   map[string]string `json:"hashes"`
	URL      string            `json:"url"`
	Filename string            `json:"filename"`
	Primary  bool              `json:"primary"`
}

// ModrinthProject represents basic project info.
type ModrinthProject struct {
	ID          string `json:"id"`
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Downloads   int    `json:"downloads"`
}

// ModrinthSearchResult represents a hit from the search API.
type ModrinthSearchResult struct {
	ProjectID   string   `json:"project_id"`
	Slug        string   `json:"slug"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Downloads   int      `json:"downloads"`
	IconURL     string   `json:"icon_url"`
	Categories  []string `json:"categories"`
}

// knownLoaders is the set of Modrinth category names that are mod loaders.
var knownLoaders = map[string]bool{
	"fabric": true, "forge": true, "neoforge": true, "quilt": true,
	"liteloader": true, "modloader": true, "rift": true,
}

// Loaders returns just the loader categories from a search result.
func (r ModrinthSearchResult) Loaders() []string {
	var loaders []string
	for _, c := range r.Categories {
		if knownLoaders[c] {
			loaders = append(loaders, c)
		}
	}
	return loaders
}

// ModrinthSearchResponse represents the search API response.
type ModrinthSearchResponse struct {
	Hits      []ModrinthSearchResult `json:"hits"`
	TotalHits int                    `json:"total_hits"`
}

func searchProjects(query string, facets string) (*ModrinthSearchResponse, error) {
	endpoint := fmt.Sprintf("/search?query=%s&limit=%d&facets=%s",
		neturl.QueryEscape(query), searchResultLimit, neturl.QueryEscape(facets))

	data, err := modrinthGet(endpoint)
	if err != nil {
		return nil, err
	}

	var resp ModrinthSearchResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ModInfo holds local and remote info about an installed mod.
type ModInfo struct {
	Filename       string
	SHA1           string
	Found          bool // found on Modrinth
	ProjectID      string
	ProjectTitle   string
	CurrentVersion string
	LatestVersion  string
	HasUpdate      bool
	UpdateURL      string // download URL for the latest version
	UpdateFilename string // filename for the latest version
	InstalledPath  string // full path to the installed jar
	Dependencies   []ModrinthDependency
	JarMeta        *JarModMeta // metadata parsed from inside the jar
}

// hashJarFile computes the SHA-1 hash of a file.
func hashJarFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha1.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// ScannedMod holds the hash, filename, and full path of a scanned jar.
type ScannedMod struct {
	Filename string
	Path     string
}

// ScanModsFolder finds all .jar files in a directory and computes their SHA-1 hashes.
func ScanModsFolder(dir string) (map[string]ScannedMod, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	hashes := make(map[string]ScannedMod) // hash -> ScannedMod
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".jar") {
			continue
		}
		fullPath := filepath.Join(dir, e.Name())
		h, err := hashJarFile(fullPath)
		if err != nil {
			continue
		}
		hashes[h] = ScannedMod{Filename: e.Name(), Path: fullPath}
	}
	return hashes, nil
}

// modrinthPost performs a POST request to the Modrinth API.
func modrinthPost(endpoint string, body any) ([]byte, error) {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", modrinthAPI+endpoint, strings.NewReader(string(jsonBody)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "MCProfiles/1.0 (https://github.com/sbehnke/MCProfiles)")

	resp, err := doRequestWithRetry(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("modrinth API returned %d: %s", resp.StatusCode, string(data))
	}
	return data, nil
}

// modrinthGet performs a GET request to the Modrinth API.
func modrinthGet(endpoint string) ([]byte, error) {
	req, err := http.NewRequest("GET", modrinthAPI+endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "MCProfiles/1.0 (https://github.com/sbehnke/MCProfiles)")

	resp, err := doRequestWithRetry(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("modrinth API returned %d: %s", resp.StatusCode, string(data))
	}
	return data, nil
}

// LookupVersionsByHash looks up multiple mods by their SHA-1 file hashes.
// Returns a map of hash -> ModrinthVersion.
func LookupVersionsByHash(hashes []string) (map[string]*ModrinthVersion, error) {
	body := map[string]any{
		"hashes":    hashes,
		"algorithm": "sha1",
	}
	data, err := modrinthPost("/version_files", body)
	if err != nil {
		return nil, err
	}

	var result map[string]*ModrinthVersion
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// CheckUpdates checks for updates for mods identified by their SHA-1 hashes.
// Returns a map of hash -> latest ModrinthVersion.
func CheckUpdates(hashes []string, loaders []string, gameVersions []string) (map[string]*ModrinthVersion, error) {
	body := map[string]any{
		"hashes":        hashes,
		"algorithm":     "sha1",
		"loaders":       loaders,
		"game_versions": gameVersions,
	}
	data, err := modrinthPost("/version_files/update", body)
	if err != nil {
		return nil, err
	}

	var result map[string]*ModrinthVersion
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func versionSupportsGameVersion(v *ModrinthVersion, gameVersion string) bool {
	if v == nil || gameVersion == "" {
		return true
	}
	for _, gv := range v.GameVersions {
		if gv == gameVersion {
			return true
		}
	}
	return false
}

func versionLabelMatchesGameVersion(v *ModrinthVersion, gameVersion string) bool {
	if v == nil || gameVersion == "" {
		return true
	}

	checks := []string{v.VersionNumber, v.Name}
	for _, s := range checks {
		matches := trailingVersionPattern.FindStringSubmatch(strings.ToLower(s))
		if len(matches) == 3 {
			return matches[2] == strings.ToLower(gameVersion)
		}
	}
	return true
}

func pickBestVersion(versions []*ModrinthVersion, gameVersion string) *ModrinthVersion {
	var fallback *ModrinthVersion
	for _, v := range versions {
		if !versionSupportsGameVersion(v, gameVersion) {
			continue
		}
		if versionLabelMatchesGameVersion(v, gameVersion) {
			return v
		}
		if fallback == nil {
			fallback = v
		}
	}
	return fallback
}

// LookupProjects fetches project info for multiple project IDs.
func LookupProjects(ids []string) (map[string]*ModrinthProject, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	idsJSON, _ := json.Marshal(ids)
	data, err := modrinthGet("/projects?ids=" + neturl.QueryEscape(string(idsJSON)))
	if err != nil {
		return nil, err
	}

	var projects []*ModrinthProject
	if err := json.Unmarshal(data, &projects); err != nil {
		return nil, err
	}

	result := make(map[string]*ModrinthProject)
	for _, p := range projects {
		result[p.ID] = p
	}
	return result, nil
}

// LookupProjectBySlug tries to find a project on Modrinth by slug.
// Returns nil if not found.
func LookupProjectBySlug(slug string) *ModrinthProject {
	data, err := modrinthGet("/project/" + slug)
	if err != nil {
		return nil
	}
	var proj ModrinthProject
	if err := json.Unmarshal(data, &proj); err != nil {
		return nil
	}
	return &proj
}

// CheckMods scans a mods folder and returns info about each mod including
// update availability and dependencies.
func CheckMods(modsDir string, gameVersion string) ([]ModInfo, error) {
	hashToMod, err := ScanModsFolder(modsDir)
	if err != nil {
		return nil, fmt.Errorf("scanning mods folder: %w", err)
	}

	if len(hashToMod) == 0 {
		return nil, nil
	}

	hashes := make([]string, 0, len(hashToMod))
	for h := range hashToMod {
		hashes = append(hashes, h)
	}

	// Parse jar metadata for all mods
	jarMetas := make(map[string]*JarModMeta) // hash -> JarModMeta
	for hash, scanned := range hashToMod {
		if meta, err := ParseJarMeta(scanned.Path); err == nil && meta != nil {
			jarMetas[hash] = meta
		}
	}

	// Look up all hashes on Modrinth
	versions, err := LookupVersionsByHash(hashes)
	if err != nil {
		return nil, fmt.Errorf("looking up mods: %w", err)
	}

	// Determine loaders from the identified mods (Modrinth + jar metadata)
	loaderSet := make(map[string]bool)
	for _, v := range versions {
		for _, l := range v.Loaders {
			loaderSet[l] = true
		}
	}
	for _, m := range jarMetas {
		if m.Loader != "" {
			loaderSet[m.Loader] = true
		}
	}
	loaders := make([]string, 0, len(loaderSet))
	for l := range loaderSet {
		loaders = append(loaders, l)
	}
	if len(loaders) == 0 {
		loaders = []string{"fabric", "forge", "neoforge", "quilt"}
	}

	// Collect project IDs for title lookup
	projectIDs := make(map[string]bool)
	for _, v := range versions {
		projectIDs[v.ProjectID] = true
	}
	for _, v := range versions {
		for _, dep := range v.Dependencies {
			if dep.DependencyType == "required" && dep.ProjectID != "" {
				projectIDs[dep.ProjectID] = true
			}
		}
	}

	ids := make([]string, 0, len(projectIDs))
	for id := range projectIDs {
		ids = append(ids, id)
	}
	projects, err := LookupProjects(ids)
	if err != nil {
		projects = nil
	}

	// Build results
	var results []ModInfo
	for hash, scanned := range hashToMod {
		info := ModInfo{
			Filename:      scanned.Filename,
			SHA1:          hash,
			InstalledPath: scanned.Path,
			JarMeta:       jarMetas[hash],
		}

		v, found := versions[hash]
		if !found {
			// Hash not on Modrinth — try slug-based lookup using jar metadata
			if info.JarMeta != nil {
				info.ProjectTitle = info.JarMeta.Name
				info.CurrentVersion = info.JarMeta.Version

				if proj := LookupProjectBySlug(info.JarMeta.ID); proj != nil {
					info.Found = true
					info.ProjectID = proj.ID
					info.ProjectTitle = proj.Title

					// Check for latest version
					slugVersions, err := GetProjectVersions(proj.ID, loaders, []string{gameVersion})
					if err == nil && len(slugVersions) > 0 {
						latest := pickBestVersion(slugVersions, gameVersion)
						if latest != nil {
							info.LatestVersion = latest.VersionNumber
							info.HasUpdate = latest.VersionNumber != info.JarMeta.Version
							if info.HasUpdate {
								info.UpdateURL, info.UpdateFilename = primaryFileURL(latest)
							}
						}
						// Collect dependencies from the latest version
						for _, dep := range latest.Dependencies {
							if dep.DependencyType == "required" {
								info.Dependencies = append(info.Dependencies, dep)
							}
						}
					}

					// Add to projects map for slug lookup
					if projects == nil {
						projects = make(map[string]*ModrinthProject)
					}
					projects[proj.ID] = proj

					results = append(results, info)
					continue
				}
			}
			info.Found = false
			results = append(results, info)
			continue
		}

		info.Found = true
		info.ProjectID = v.ProjectID
		info.CurrentVersion = v.VersionNumber

		if projects != nil {
			if p, ok := projects[v.ProjectID]; ok {
				info.ProjectTitle = p.Title
			}
		}

		// Check for required dependencies
		for _, dep := range v.Dependencies {
			if dep.DependencyType == "required" {
				info.Dependencies = append(info.Dependencies, dep)
			}
		}

		// Check for updates using this mod's current loader branch.
		if gameVersion != "" {
			modLoaders := v.Loaders
			if len(modLoaders) == 0 && info.JarMeta != nil && info.JarMeta.Loader != "" {
				modLoaders = []string{info.JarMeta.Loader}
			}
			latestVersions, updateErr := GetProjectVersions(v.ProjectID, modLoaders, []string{gameVersion})
			latest := pickBestVersion(latestVersions, gameVersion)
			if updateErr == nil && latest != nil {
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

// primaryFileURL returns the URL and filename of the primary file in a version.
func primaryFileURL(v *ModrinthVersion) (string, string) {
	for _, f := range v.Files {
		if f.Primary {
			return f.URL, f.Filename
		}
	}
	// Fallback to first file
	if len(v.Files) > 0 {
		return v.Files[0].URL, v.Files[0].Filename
	}
	return "", ""
}

// DownloadMod downloads a mod from the given URL and saves it to the mods directory,
// removing the old jar file. Returns the path to the new file.
func DownloadMod(mod ModInfo) (string, error) {
	if mod.UpdateURL == "" {
		return "", fmt.Errorf("no download URL available")
	}

	req, err := http.NewRequest("GET", mod.UpdateURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "MCProfiles/1.0 (https://github.com/sbehnke/MCProfiles)")

	resp, err := doRequestWithRetry(req)
	if err != nil {
		return "", fmt.Errorf("downloading: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download returned %d", resp.StatusCode)
	}

	// Write to a temp file in the same directory, then rename
	modsDir := filepath.Dir(mod.InstalledPath)
	newPath := filepath.Join(modsDir, mod.UpdateFilename)
	tmpPath := newPath + ".tmp"

	out, err := os.Create(tmpPath)
	if err != nil {
		return "", fmt.Errorf("creating file: %w", err)
	}

	_, err = io.Copy(out, resp.Body)
	out.Close()
	if err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("writing file: %w", err)
	}

	// Remove old jar
	if err := os.Remove(mod.InstalledPath); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("removing old jar: %w", err)
	}

	// Rename temp to final
	if err := os.Rename(tmpPath, newPath); err != nil {
		return "", fmt.Errorf("renaming: %w", err)
	}

	return newPath, nil
}

// FindMissingDependencies returns dependencies that are not installed.
// It checks both Modrinth-reported dependencies and jar metadata dependencies.
func FindMissingDependencies(mods []ModInfo, projects map[string]*ModrinthProject) []MissingDep {
	// Track installed mods by Modrinth project ID and jar mod ID
	installedProjects := make(map[string]bool)
	installedModIDs := make(map[string]bool)
	for _, m := range mods {
		if m.Found {
			installedProjects[m.ProjectID] = true
		}
		if m.JarMeta != nil {
			installedModIDs[m.JarMeta.ID] = true
		}
	}

	seen := make(map[string]bool)
	var missing []MissingDep

	for _, m := range mods {
		requiredBy := m.ProjectTitle
		if requiredBy == "" {
			requiredBy = m.Filename
		}

		// Check Modrinth dependencies
		for _, dep := range m.Dependencies {
			if dep.ProjectID != "" && !installedProjects[dep.ProjectID] && !seen[dep.ProjectID] {
				seen[dep.ProjectID] = true
				title := dep.ProjectID
				if projects != nil {
					if p, ok := projects[dep.ProjectID]; ok {
						title = p.Title
					}
				}
				missing = append(missing, MissingDep{
					ProjectID:    dep.ProjectID,
					VersionID:    dep.VersionID,
					ProjectTitle: title,
					RequiredBy:   requiredBy,
				})
			}
		}

		// Check jar metadata dependencies (for mods not on Modrinth)
		if m.JarMeta != nil && !m.Found {
			for _, dep := range m.JarMeta.Dependencies {
				if dep.Required && !installedModIDs[dep.ModID] && !seen["jar:"+dep.ModID] {
					seen["jar:"+dep.ModID] = true
					missing = append(missing, MissingDep{
						ProjectID:    dep.ModID,
						ProjectTitle: dep.ModID,
						RequiredBy:   requiredBy,
					})
				}
			}
		}
	}
	return missing
}

// InstalledModMap builds a map of Modrinth project ID -> installed file path
// for all mods in a directory. Also includes jar metadata ID mappings.
func InstalledModMap(modsDir string) map[string]string {
	installed := make(map[string]string) // projectID -> path

	hashToMod, err := ScanModsFolder(modsDir)
	if err != nil || len(hashToMod) == 0 {
		return installed
	}

	hashes := make([]string, 0, len(hashToMod))
	for h := range hashToMod {
		hashes = append(hashes, h)
	}

	versions, err := LookupVersionsByHash(hashes)
	if err != nil {
		return installed
	}

	for hash, v := range versions {
		if scanned, ok := hashToMod[hash]; ok {
			installed[v.ProjectID] = scanned.Path
		}
	}

	// Also try slug-based lookup for mods not found by hash
	for hash, scanned := range hashToMod {
		if _, found := versions[hash]; found {
			continue
		}
		meta, err := ParseJarMeta(scanned.Path)
		if err != nil || meta == nil {
			continue
		}
		if proj := LookupProjectBySlug(meta.ID); proj != nil {
			installed[proj.ID] = scanned.Path
		}
	}

	return installed
}

// InstalledShaderMap builds a map of Modrinth project ID -> installed file path
// for all shaders in a directory.
func InstalledShaderMap(shadersDir string) map[string]string {
	installed := make(map[string]string)

	hashToFile, err := ScanShadersFolder(shadersDir)
	if err != nil || len(hashToFile) == 0 {
		return installed
	}

	hashes := make([]string, 0, len(hashToFile))
	for h := range hashToFile {
		hashes = append(hashes, h)
	}

	versions, err := LookupVersionsByHash(hashes)
	if err != nil {
		return installed
	}

	for hash, v := range versions {
		if scanned, ok := hashToFile[hash]; ok {
			installed[v.ProjectID] = scanned.Path
		}
	}

	// Slug-based fallback from filename
	for hash, scanned := range hashToFile {
		if _, found := versions[hash]; found {
			continue
		}
		name, _ := parseShaderFilename(scanned.Filename)
		slug := strings.ToLower(strings.ReplaceAll(name, " ", "-"))
		if proj := LookupProjectBySlug(slug); proj != nil {
			installed[proj.ID] = scanned.Path
		}
	}

	return installed
}

// MissingDep represents a required dependency that is not installed.
type MissingDep struct {
	ProjectID    string
	VersionID    string
	ProjectTitle string
	RequiredBy   string
}

// SearchMods searches Modrinth for mods matching the query.
func SearchMods(query string, gameVersion string, loader string) (*ModrinthSearchResponse, error) {
	baseFacets := `[["project_type:mod"]]`
	facets := baseFacets
	if gameVersion != "" || loader != "" {
		parts := []string{`"project_type:mod"`}
		if gameVersion != "" {
			parts = append(parts, fmt.Sprintf(`"versions:%s"`, gameVersion))
		}
		if loader != "" {
			parts = append(parts, fmt.Sprintf(`"categories:%s"`, loader))
		}
		// Each facet in its own array = AND logic.
		facets = "["
		for i, p := range parts {
			if i > 0 {
				facets += ","
			}
			facets += "[" + p + "]"
		}
		facets += "]"
	}

	resp, err := searchProjects(query, facets)
	if err != nil {
		return nil, err
	}
	if len(resp.Hits) == 0 && facets != baseFacets {
		return searchProjects(query, baseFacets)
	}
	if gameVersion != "" {
		loaders := []string{}
		if loader != "" {
			loaders = []string{loader}
		}
		resp.Hits = filterSearchHitsByCompatibility(resp.Hits, loaders, []string{gameVersion})
	}
	return resp, nil
}

// SearchShaders searches Modrinth for shader packs matching the query.
func SearchShaders(query string, gameVersion string) (*ModrinthSearchResponse, error) {
	baseFacets := `[["project_type:shader"]]`
	parts := []string{`"project_type:shader"`}
	if gameVersion != "" {
		parts = append(parts, fmt.Sprintf(`"versions:%s"`, gameVersion))
	}
	facets := "["
	for i, p := range parts {
		if i > 0 {
			facets += ","
		}
		facets += "[" + p + "]"
	}
	facets += "]"

	resp, err := searchProjects(query, facets)
	if err != nil {
		return nil, err
	}
	if len(resp.Hits) == 0 && facets != baseFacets {
		return searchProjects(query, baseFacets)
	}
	if gameVersion != "" {
		resp.Hits = filterSearchHitsByCompatibility(resp.Hits, nil, []string{gameVersion})
	}
	return resp, nil
}

func filterSearchHitsByCompatibility(hits []ModrinthSearchResult, loaders []string, gameVersions []string) []ModrinthSearchResult {
	if len(hits) == 0 {
		return hits
	}

	filtered := make([]ModrinthSearchResult, 0, len(hits))
	for _, hit := range hits {
		versions, err := GetProjectVersions(hit.ProjectID, loaders, gameVersions)
		if err != nil || len(versions) == 0 {
			continue
		}
		best := pickBestVersion(versions, firstGameVersion(gameVersions))
		if best == nil {
			continue
		}
		filtered = append(filtered, hit)
	}
	return filtered
}

// GetProjectVersions returns versions for a project, optionally filtered.
func GetProjectVersions(projectID string, loaders []string, gameVersions []string) ([]*ModrinthVersion, error) {
	url := fmt.Sprintf("/project/%s/version", projectID)

	params := []string{}
	if len(loaders) > 0 {
		j, _ := json.Marshal(loaders)
		params = append(params, "loaders="+neturl.QueryEscape(string(j)))
	}
	if len(gameVersions) > 0 {
		j, _ := json.Marshal(gameVersions)
		params = append(params, "game_versions="+neturl.QueryEscape(string(j)))
	}
	if len(params) > 0 {
		url += "?" + strings.Join(params, "&")
	}

	data, err := modrinthGet(url)
	if err != nil {
		return nil, err
	}

	var versions []*ModrinthVersion
	if err := json.Unmarshal(data, &versions); err != nil {
		return nil, err
	}
	return versions, nil
}

// GetVersion fetches a specific Modrinth version by version ID.
func GetVersion(versionID string) (*ModrinthVersion, error) {
	data, err := modrinthGet("/version/" + versionID)
	if err != nil {
		return nil, err
	}

	var version ModrinthVersion
	if err := json.Unmarshal(data, &version); err != nil {
		return nil, err
	}
	return &version, nil
}

// InstallModFromVersion downloads a mod version file to the mods directory.
// Returns the path to the downloaded file.
func InstallModFromVersion(version *ModrinthVersion, modsDir string) (string, error) {
	dlURL, filename := primaryFileURL(version)
	if dlURL == "" {
		return "", fmt.Errorf("no download URL available")
	}
	return downloadToDir(dlURL, filename, modsDir)
}

func installVersionWithDeps(version *ModrinthVersion, modsDir string, loaders []string, gameVersions []string) ([]string, error) {
	path, err := InstallModFromVersion(version, modsDir)
	if err != nil {
		return nil, fmt.Errorf("downloading mod: %w", err)
	}
	installed := []string{filepath.Base(path)}

	// Install required dependencies
	for _, dep := range version.Dependencies {
		if dep.DependencyType != "required" || dep.ProjectID == "" {
			continue
		}

		// Check if already installed
		alreadyInstalled := false
		entries, _ := os.ReadDir(modsDir)
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".jar") {
				continue
			}
			jarPath := filepath.Join(modsDir, e.Name())
			h, err := hashJarFile(jarPath)
			if err != nil {
				continue
			}
			lookup, err := LookupVersionsByHash([]string{h})
			if err != nil {
				continue
			}
			if v, ok := lookup[h]; ok && v.ProjectID == dep.ProjectID {
				alreadyInstalled = true
				break
			}
		}
		if alreadyInstalled {
			continue
		}

		var depVersion *ModrinthVersion
		if dep.VersionID != "" {
			depVersion, err = GetVersion(dep.VersionID)
		} else {
			var depVersions []*ModrinthVersion
			depVersions, err = GetProjectVersions(dep.ProjectID, loaders, gameVersions)
			if err == nil && len(depVersions) > 0 {
				depVersion = pickBestVersion(depVersions, firstGameVersion(gameVersions))
			}
		}
		if err != nil || depVersion == nil {
			continue
		}

		depPath, err := InstallModFromVersion(depVersion, modsDir)
		if err != nil {
			continue
		}
		installed = append(installed, filepath.Base(depPath))
	}

	return installed, nil
}

// InstallModWithDeps installs a mod and its required dependencies.
// Returns a list of installed filenames and any error.
func InstallModWithDeps(projectID string, modsDir string, loaders []string, gameVersions []string) ([]string, error) {
	versions, err := GetProjectVersions(projectID, loaders, gameVersions)
	if err != nil {
		return nil, fmt.Errorf("fetching versions: %w", err)
	}
	version := pickBestVersion(versions, firstGameVersion(gameVersions))
	if version == nil {
		return nil, fmt.Errorf("no compatible versions found")
	}

	return installVersionWithDeps(version, modsDir, loaders, gameVersions)
}

func firstGameVersion(gameVersions []string) string {
	if len(gameVersions) == 0 {
		return ""
	}
	return gameVersions[0]
}

// InstallMissingDependency installs a missing dependency, preferring the exact
// Modrinth version when one is specified.
func InstallMissingDependency(dep MissingDep, modsDir string, loaders []string, gameVersions []string) ([]string, error) {
	if dep.VersionID != "" {
		version, err := GetVersion(dep.VersionID)
		if err != nil {
			return nil, fmt.Errorf("fetching dependency version: %w", err)
		}
		return installVersionWithDeps(version, modsDir, loaders, gameVersions)
	}
	if dep.ProjectID == "" {
		return nil, fmt.Errorf("dependency is not available on Modrinth")
	}
	return InstallModWithDeps(dep.ProjectID, modsDir, loaders, gameVersions)
}

// UninstallMod removes a mod jar file.
func UninstallMod(path string) error {
	return os.Remove(path)
}

// downloadToDir downloads a URL to a directory with the given filename.
func downloadToDir(dlURL, filename, dir string) (string, error) {
	req, err := http.NewRequest("GET", dlURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "MCProfiles/1.0 (https://github.com/sbehnke/MCProfiles)")

	resp, err := doRequestWithRetry(req)
	if err != nil {
		return "", fmt.Errorf("downloading: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download returned %d", resp.StatusCode)
	}

	destPath := filepath.Join(dir, filename)
	tmpPath := destPath + ".tmp"

	out, err := os.Create(tmpPath)
	if err != nil {
		return "", fmt.Errorf("creating file: %w", err)
	}

	_, err = io.Copy(out, resp.Body)
	out.Close()
	if err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("writing file: %w", err)
	}

	if err := os.Rename(tmpPath, destPath); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("renaming: %w", err)
	}

	return destPath, nil
}

// ProjectURL returns the Modrinth web page URL for a project.
func ProjectURL(slug string) string {
	return "https://modrinth.com/mod/" + slug
}
