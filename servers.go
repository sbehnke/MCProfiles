package main

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// Server is a single server entry in the TUI config.
type Server struct {
	Name        string `toml:"name"`
	ModsDir     string `toml:"mods_dir"`
	ServerJar   string `toml:"server_jar,omitempty"`
	GameVersion string `toml:"game_version,omitempty"`
	Loader      string `toml:"loader,omitempty"`
	JavaPath    string `toml:"java_path,omitempty"`
}

// ServersConfig is the on-disk shape of servers.toml.
type ServersConfig struct {
	Servers []Server `toml:"servers"`
}

// ServersConfigPath returns the absolute path to servers.toml.
// Honors XDG_CONFIG_HOME on Linux; uses OS-appropriate config dirs elsewhere.
func ServersConfigPath() string {
	if env := os.Getenv("MCPROFILES_CONFIG"); env != "" {
		return env
	}

	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "mcprofiles", "servers.toml")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "servers.toml"
	}

	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "mcprofiles", "servers.toml")
	case "windows":
		if appdata := os.Getenv("APPDATA"); appdata != "" {
			return filepath.Join(appdata, "mcprofiles", "servers.toml")
		}
		return filepath.Join(home, "AppData", "Roaming", "mcprofiles", "servers.toml")
	default:
		return filepath.Join(home, ".config", "mcprofiles", "servers.toml")
	}
}

// LoadServers reads servers.toml. Returns an empty config (no error) if the file doesn't exist.
func LoadServers() (*ServersConfig, error) {
	path := ServersConfigPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &ServersConfig{}, nil
		}
		return nil, err
	}

	var cfg ServersConfig
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	cfg.Sort()
	return &cfg, nil
}

// SaveServers writes servers.toml atomically, creating parent dirs if needed.
func SaveServers(cfg *ServersConfig) error {
	cfg.Sort()

	path := ServersConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	enc := toml.NewEncoder(f)
	enc.Indent = "  "
	if err := enc.Encode(cfg); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return os.WriteFile(path, mustReadAll(tmp), 0644)
	}
	return nil
}

func mustReadAll(path string) []byte {
	data, _ := os.ReadFile(path)
	return data
}

// Sort orders servers by name (case-insensitive) for stable display.
func (c *ServersConfig) Sort() {
	sort.Slice(c.Servers, func(i, j int) bool {
		return strings.ToLower(c.Servers[i].Name) < strings.ToLower(c.Servers[j].Name)
	})
}

// FindByName returns the index of the first server with the given name, or -1.
func (c *ServersConfig) FindByName(name string) int {
	for i, s := range c.Servers {
		if s.Name == name {
			return i
		}
	}
	return -1
}
