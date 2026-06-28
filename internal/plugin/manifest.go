package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Manifest struct {
	Name  string `json:"name"`
	Tools []Tool `json:"tools"`
}

type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Command     string         `json:"command"`
	Args        []string       `json:"args"`
	InputSchema map[string]any `json:"input_schema"`
	Risk        string         `json:"risk"`
}

func LoadManifests(root string, enabled bool) ([]Tool, error) {
	if !enabled {
		return nil, nil
	}
	pluginsRoot := filepath.Join(root, ".codeworld", "plugins")
	entries, err := os.ReadDir(pluginsRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var tools []Tool
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		manifest, err := loadManifest(filepath.Join(pluginsRoot, entry.Name(), "plugin.json"))
		if err != nil {
			return nil, err
		}
		if err := validateManifest(entry.Name(), manifest); err != nil {
			return nil, err
		}
		tools = append(tools, manifest.Tools...)
	}
	return tools, nil
}

func loadManifest(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func validateManifest(dirName string, manifest Manifest) error {
	if manifest.Name == "" || filepath.Base(manifest.Name) != manifest.Name || strings.Contains(manifest.Name, "\\") || manifest.Name != dirName {
		return fmt.Errorf("invalid plugin name %q", manifest.Name)
	}
	for _, tool := range manifest.Tools {
		if !strings.HasPrefix(tool.Name, manifest.Name+".") {
			return fmt.Errorf("plugin tool %q must use namespace %q", tool.Name, manifest.Name+".")
		}
		if tool.Command == "" {
			return fmt.Errorf("plugin tool %q command is required", tool.Name)
		}
		switch tool.Risk {
		case "read", "write", "execute":
		default:
			return fmt.Errorf("plugin tool %q has invalid risk %q", tool.Name, tool.Risk)
		}
	}
	return nil
}
