package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Resolution holds game window dimensions.
type Resolution struct {
	Height int `json:"height"`
	Width  int `json:"width"`
}

// Profile represents a single Minecraft launcher profile.
type Profile struct {
	Created       string      `json:"created"`
	Icon          string      `json:"icon"`
	LastUsed      string      `json:"lastUsed"`
	LastVersionId string      `json:"lastVersionId"`
	Name          string      `json:"name"`
	Type          string      `json:"type"`
	GameDir       string      `json:"gameDir,omitempty"`
	JavaDir       string      `json:"javaDir,omitempty"`
	JavaArgs      string      `json:"javaArgs,omitempty"`
	Resolution    *Resolution `json:"resolution,omitempty"`
	LogConfig     string      `json:"logConfig,omitempty"`
	LogConfigXML  *bool       `json:"logConfigIsXML,omitempty"`
}

// LauncherData holds the full launcher_profiles.json, preserving unknown keys.
type LauncherData struct {
	Profiles map[string]*Profile
	Rest     map[string]json.RawMessage
}

func (ld *LauncherData) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	ld.Rest = make(map[string]json.RawMessage)
	for k, v := range raw {
		if k == "profiles" {
			ld.Profiles = make(map[string]*Profile)
			if err := json.Unmarshal(v, &ld.Profiles); err != nil {
				return fmt.Errorf("parsing profiles: %w", err)
			}
		} else {
			ld.Rest[k] = v
		}
	}
	if ld.Profiles == nil {
		ld.Profiles = make(map[string]*Profile)
	}
	return nil
}

func (ld *LauncherData) MarshalJSON() ([]byte, error) {
	combined := make(map[string]any)
	for k, v := range ld.Rest {
		var parsed any
		if err := json.Unmarshal(v, &parsed); err != nil {
			combined[k] = v
		} else {
			combined[k] = parsed
		}
	}
	combined["profiles"] = ld.Profiles
	return json.MarshalIndent(combined, "", "  ")
}

// CandidateProfilePaths returns all known locations where launcher_profiles.json may exist.
func CandidateProfilePaths() []string {
	home, _ := os.UserHomeDir()
	var dirs []string

	switch runtime.GOOS {
	case "darwin":
		dirs = []string{
			filepath.Join(home, "Library", "Application Support", "minecraft"),
		}
	case "windows":
		appdata := os.Getenv("APPDATA")
		localAppData := os.Getenv("LOCALAPPDATA")
		dirs = []string{
			filepath.Join(appdata, ".minecraft"),
		}
		// Microsoft Store / Xbox app location
		if localAppData != "" {
			dirs = append(dirs, filepath.Join(localAppData, "Packages", "Microsoft.4297127D64EC6_8wekyb3d8bbwe", "LocalCache", "Local", "minecraft"))
		}
	default: // linux
		dirs = []string{
			filepath.Join(home, ".minecraft"),
		}
		// Flatpak
		dirs = append(dirs, filepath.Join(home, ".var", "app", "com.mojang.Minecraft", ".minecraft"))
		// Snap
		dirs = append(dirs, filepath.Join(home, "snap", "mc-installer", "current", ".minecraft"))
	}

	filenames := []string{"launcher_profiles.json", "launcher_profiles_microsoft_store.json"}

	var candidates []string
	for _, dir := range dirs {
		for _, fname := range filenames {
			candidates = append(candidates, filepath.Join(dir, fname))
		}
	}
	return candidates
}

// FindExistingProfiles returns all candidate paths that actually exist on disk.
func FindExistingProfiles() []string {
	var found []string
	for _, path := range CandidateProfilePaths() {
		if _, err := os.Stat(path); err == nil {
			found = append(found, path)
		}
	}
	return found
}

// DefaultProfilePath returns the first existing candidate, or the primary default.
func DefaultProfilePath() string {
	found := FindExistingProfiles()
	if len(found) > 0 {
		return found[0]
	}
	// Return the primary default even if it doesn't exist
	candidates := CandidateProfilePaths()
	if len(candidates) > 0 {
		return candidates[0]
	}
	return "launcher_profiles.json"
}

// LoadProfiles reads and parses the launcher profiles file.
func LoadProfiles(path string) (*LauncherData, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var ld LauncherData
	if err := json.Unmarshal(data, &ld); err != nil {
		return nil, err
	}
	return &ld, nil
}

// SaveProfiles writes launcher data back to the file atomically.
func SaveProfiles(path string, ld *LauncherData) error {
	data, err := json.MarshalIndent(ld, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		// Fallback for cross-device rename
		return os.WriteFile(path, data, 0644)
	}
	return nil
}

// ResolveModsFolder determines the effective mods folder for a profile.
// It checks the version JSON for -Dfabric.modsFolder=, the profile's javaArgs,
// and falls back to gameDir/mods or the default .minecraft/mods.
func ResolveModsFolder(prof *Profile, launcherProfilesPath string) string {
	mcDir := filepath.Dir(launcherProfilesPath)

	// Check version JSON for JVM args like -Dfabric.modsFolder=...
	if prof.LastVersionId != "" {
		versionJSON := filepath.Join(mcDir, "versions", prof.LastVersionId, prof.LastVersionId+".json")
		if data, err := os.ReadFile(versionJSON); err == nil {
			var vd struct {
				Arguments struct {
					JVM []json.RawMessage `json:"jvm"`
				} `json:"arguments"`
			}
			if json.Unmarshal(data, &vd) == nil {
				for _, raw := range vd.Arguments.JVM {
					var s string
					if json.Unmarshal(raw, &s) == nil {
						if path, ok := extractModsFolder(s); ok {
							return path
						}
					}
				}
			}
		}
	}

	// Check profile's own javaArgs
	if prof.JavaArgs != "" {
		for _, arg := range splitArgs(prof.JavaArgs) {
			if path, ok := extractModsFolder(arg); ok {
				return path
			}
		}
	}

	// Fall back to gameDir/mods or default
	base := mcDir
	if prof.GameDir != "" {
		base = prof.GameDir
	}
	return filepath.Join(base, "mods")
}

// ResolveShadersFolder determines the shaderpacks folder for a profile.
func ResolveShadersFolder(prof *Profile, launcherProfilesPath string) string {
	mcDir := filepath.Dir(launcherProfilesPath)
	base := mcDir
	if prof.GameDir != "" {
		base = prof.GameDir
	}
	return filepath.Join(base, "shaderpacks")
}

// ResolveGameVersion extracts the base Minecraft version for a profile.
// It checks the version JSON for inheritsFrom, falling back to the version ID itself.
func ResolveGameVersion(prof *Profile, launcherProfilesPath string) string {
	if prof.LastVersionId == "" {
		return ""
	}

	mcDir := filepath.Dir(launcherProfilesPath)
	versionJSON := filepath.Join(mcDir, "versions", prof.LastVersionId, prof.LastVersionId+".json")
	if data, err := os.ReadFile(versionJSON); err == nil {
		var vd struct {
			InheritsFrom string `json:"inheritsFrom"`
		}
		if json.Unmarshal(data, &vd) == nil && vd.InheritsFrom != "" {
			return vd.InheritsFrom
		}
	}

	// If no inheritsFrom, the version ID is the MC version itself
	return prof.LastVersionId
}

// ResolveLoader detects the mod loader for a profile from its version JSON.
// Returns "fabric", "forge", "neoforge", "quilt", or "" if unknown.
func ResolveLoader(prof *Profile, launcherProfilesPath string) string {
	if prof.LastVersionId == "" {
		return ""
	}

	mcDir := filepath.Dir(launcherProfilesPath)
	versionJSON := filepath.Join(mcDir, "versions", prof.LastVersionId, prof.LastVersionId+".json")
	data, err := os.ReadFile(versionJSON)
	if err != nil {
		return ""
	}

	var vd struct {
		MainClass string `json:"mainClass"`
	}
	if json.Unmarshal(data, &vd) != nil {
		return ""
	}

	switch {
	case strings.Contains(vd.MainClass, "fabricmc") || strings.Contains(vd.MainClass, "fabric"):
		return "fabric"
	case strings.Contains(vd.MainClass, "quilt"):
		return "quilt"
	case strings.Contains(vd.MainClass, "neoforge"):
		return "neoforge"
	case strings.Contains(vd.MainClass, "forge"):
		return "forge"
	}
	return ""
}

func extractModsFolder(arg string) (string, bool) {
	const prefix = "-Dfabric.modsFolder="
	if len(arg) > len(prefix) && arg[:len(prefix)] == prefix {
		return arg[len(prefix):], true
	}
	return "", false
}

func splitArgs(s string) []string {
	var args []string
	current := ""
	inQuote := false
	for _, r := range s {
		switch {
		case r == '"':
			inQuote = !inQuote
		case r == ' ' && !inQuote:
			if current != "" {
				args = append(args, current)
				current = ""
			}
		default:
			current += string(r)
		}
	}
	if current != "" {
		args = append(args, current)
	}
	return args
}

// NewProfile creates a profile with sensible defaults.
func NewProfile() *Profile {
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	return &Profile{
		Created:       now,
		LastUsed:      now,
		Name:          "New Profile",
		Type:          "custom",
		Icon:          "Grass",
		LastVersionId: "latest-release",
	}
}
