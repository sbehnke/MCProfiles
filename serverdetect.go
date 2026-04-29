package main

import (
	"archive/zip"
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var paperMCVersionPattern = regexp.MustCompile(`MC:\s*(\d+\.\d+(?:\.\d+)?)`)

// ServerJarInfo is what we can learn from inspecting a server jar.
type ServerJarInfo struct {
	GameVersion string // e.g. "1.21.4"
	Loader      string // "fabric", "quilt", "forge", "neoforge", "paper", "vanilla"
}

var mcVersionInFilename = regexp.MustCompile(`(\d+\.\d+(?:\.\d+)?)`)

// DetectServerJar inspects a server jar at `path` and returns best-effort version + loader.
// Missing fields are left empty; it never returns an error for a readable jar that simply
// lacks metadata — callers should check info.GameVersion / info.Loader.
func DetectServerJar(path string) (ServerJarInfo, error) {
	var info ServerJarInfo

	r, err := zip.OpenReader(path)
	if err != nil {
		return info, err
	}
	defer r.Close()

	files := make(map[string]*zip.File, len(r.File))
	for _, f := range r.File {
		files[f.Name] = f
	}

	// Fabric / Quilt server launcher: install.properties at root.
	if f := files["install.properties"]; f != nil {
		props := readZipProps(f)
		if v := props["game-version"]; v != "" {
			info.GameVersion = v
		}
		switch {
		case props["quilt-loader-version"] != "":
			info.Loader = "quilt"
		case props["fabric-loader-version"] != "":
			info.Loader = "fabric"
		}
	}

	// Vanilla / Paper / Purpur: version.json at root with {"id":"1.21.4"}.
	if info.GameVersion == "" {
		if f := files["version.json"]; f != nil {
			if id := readZipVersionID(f); id != "" {
				info.GameVersion = id
			}
		}
	}

	// Paper-specific marker.
	if info.Loader == "" {
		if _, ok := files["META-INF/versions.list"]; ok {
			info.Loader = "paper"
		} else if _, ok := files["patch.properties"]; ok {
			info.Loader = "paper"
		}
	}

	// Fall back to filename heuristics for loader + version.
	base := strings.ToLower(filepath.Base(path))
	if info.Loader == "" {
		switch {
		case strings.Contains(base, "neoforge"):
			info.Loader = "neoforge"
		case strings.Contains(base, "forge"):
			info.Loader = "forge"
		case strings.Contains(base, "fabric"):
			info.Loader = "fabric"
		case strings.Contains(base, "quilt"):
			info.Loader = "quilt"
		case strings.Contains(base, "paper") || strings.Contains(base, "purpur"):
			info.Loader = "paper"
		case strings.HasPrefix(base, "minecraft_server") || strings.HasPrefix(base, "server"):
			info.Loader = "vanilla"
		}
	}
	if info.GameVersion == "" {
		if m := mcVersionInFilename.FindString(base); m != "" {
			info.GameVersion = m
		}
	}

	return info, nil
}

// DetectGameVersionFromDataDir scans a server's data directory (the parent of
// mods/) for the Minecraft version. Useful when the server jar isn't a
// configured path — e.g. itzg/minecraft-server downloads the jar at runtime
// into the mounted data volume.
func DetectGameVersionFromDataDir(dataDir string) string {
	if dataDir == "" {
		return ""
	}

	if v := readPaperVersionHistory(filepath.Join(dataDir, "version_history.json")); v != "" {
		return v
	}

	entries, err := os.ReadDir(dataDir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".jar") {
			continue
		}
		info, err := DetectServerJar(filepath.Join(dataDir, e.Name()))
		if err == nil && info.GameVersion != "" {
			return info.GameVersion
		}
	}
	return ""
}

// readPaperVersionHistory parses Paper/Purpur/Folia's version_history.json.
// Format: {"currentVersion":"git-Paper-196 (MC: 1.20.1)"}.
func readPaperVersionHistory(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var v struct {
		CurrentVersion string `json:"currentVersion"`
		OldVersion     string `json:"oldVersion"`
	}
	if json.Unmarshal(data, &v) != nil {
		return ""
	}
	s := v.CurrentVersion
	if s == "" {
		s = v.OldVersion
	}
	if m := paperMCVersionPattern.FindStringSubmatch(s); len(m) == 2 {
		return m[1]
	}
	return ""
}

// readZipProps parses a Java .properties file from inside a zip entry.
func readZipProps(f *zip.File) map[string]string {
	rc, err := f.Open()
	if err != nil {
		return nil
	}
	defer rc.Close()

	props := make(map[string]string)
	scanner := bufio.NewScanner(rc)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}
		eq := strings.IndexAny(line, "=:")
		if eq < 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		val := strings.TrimSpace(line[eq+1:])
		props[key] = val
	}
	return props
}

// readZipVersionID extracts the "id" field from a vanilla/Paper version.json entry.
func readZipVersionID(f *zip.File) string {
	rc, err := f.Open()
	if err != nil {
		return ""
	}
	defer rc.Close()

	data, err := io.ReadAll(rc)
	if err != nil {
		return ""
	}
	var v struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return ""
	}
	if v.ID != "" {
		return v.ID
	}
	return v.Name
}
