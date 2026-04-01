package main

import (
	"archive/zip"
	"encoding/json"
	"io"
	"strings"

	"github.com/BurntSushi/toml"
)

// JarModMeta holds metadata extracted from inside a mod jar file.
type JarModMeta struct {
	ID           string
	Name         string
	Version      string
	Dependencies []JarDependency
	Loader       string // "fabric", "quilt", "forge", "neoforge"
}

// JarDependency represents a dependency declared in a mod's metadata.
type JarDependency struct {
	ModID    string
	Required bool
}

// ParseJarMeta reads mod metadata from a jar file.
// It checks for fabric.mod.json (Fabric/Quilt) and META-INF/mods.toml (Forge/NeoForge).
func ParseJarMeta(jarPath string) (*JarModMeta, error) {
	r, err := zip.OpenReader(jarPath)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	for _, f := range r.File {
		switch f.Name {
		case "fabric.mod.json":
			return parseFabricModJSON(f)
		case "quilt.mod.json":
			return parseQuiltModJSON(f)
		case "META-INF/mods.toml":
			return parseModsToml(f)
		case "META-INF/neoforge.mods.toml":
			return parseModsToml(f)
		}
	}
	return nil, nil // no recognized metadata
}

func parseFabricModJSON(f *zip.File) (*JarModMeta, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, err
	}

	var fmod struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Version string `json:"version"`
		Depends any    `json:"depends"`
	}
	if err := json.Unmarshal(data, &fmod); err != nil {
		return nil, err
	}

	meta := &JarModMeta{
		ID:      fmod.ID,
		Name:    fmod.Name,
		Version: fmod.Version,
		Loader:  "fabric",
	}

	// depends can be map[string]string or map[string][]string or map[string]any
	if deps, ok := fmod.Depends.(map[string]any); ok {
		for modID := range deps {
			if modID == "fabricloader" || modID == "fabric" || modID == "minecraft" || modID == "java" || modID == "fabric-api" || strings.HasPrefix(modID, "fabric-") {
				continue
			}
			meta.Dependencies = append(meta.Dependencies, JarDependency{
				ModID:    modID,
				Required: true,
			})
		}
	}

	return meta, nil
}

func parseQuiltModJSON(f *zip.File) (*JarModMeta, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, err
	}

	var qmod struct {
		QuiltLoader struct {
			ID      string `json:"id"`
			Version string `json:"version"`
			Meta    struct {
				Name string `json:"name"`
			} `json:"metadata"`
			Depends []struct {
				ID       string `json:"id"`
				Optional bool   `json:"optional"`
			} `json:"depends"`
		} `json:"quilt_loader"`
	}
	if err := json.Unmarshal(data, &qmod); err != nil {
		return nil, err
	}

	meta := &JarModMeta{
		ID:      qmod.QuiltLoader.ID,
		Name:    qmod.QuiltLoader.Meta.Name,
		Version: qmod.QuiltLoader.Version,
		Loader:  "quilt",
	}

	for _, dep := range qmod.QuiltLoader.Depends {
		if dep.ID == "quilt_loader" || dep.ID == "minecraft" || dep.ID == "java" {
			continue
		}
		meta.Dependencies = append(meta.Dependencies, JarDependency{
			ModID:    dep.ID,
			Required: !dep.Optional,
		})
	}

	return meta, nil
}

func parseModsToml(f *zip.File) (*JarModMeta, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, err
	}

	var mt struct {
		ModLoader string `toml:"modLoader"`
		Mods      []struct {
			ModID   string `toml:"modId"`
			Version string `toml:"version"`
			Name    string `toml:"displayName"`
		} `toml:"mods"`
		Dependencies map[string][]struct {
			ModID     string `toml:"modId"`
			Mandatory bool   `toml:"mandatory"`
		} `toml:"dependencies"`
	}
	if _, err := toml.Decode(string(data), &mt); err != nil {
		return nil, err
	}

	if len(mt.Mods) == 0 {
		return nil, nil
	}

	mod := mt.Mods[0]
	loader := "forge"
	if strings.Contains(f.Name, "neoforge") || strings.Contains(mt.ModLoader, "neoforge") {
		loader = "neoforge"
	}

	meta := &JarModMeta{
		ID:      mod.ModID,
		Name:    mod.Name,
		Version: mod.Version,
		Loader:  loader,
	}

	if deps, ok := mt.Dependencies[mod.ModID]; ok {
		for _, dep := range deps {
			if dep.ModID == "forge" || dep.ModID == "neoforge" || dep.ModID == "minecraft" {
				continue
			}
			meta.Dependencies = append(meta.Dependencies, JarDependency{
				ModID:    dep.ModID,
				Required: dep.Mandatory,
			})
		}
	}

	return meta, nil
}
