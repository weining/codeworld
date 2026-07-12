package codexplugin

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type Marketplace struct {
	Name       string   `json:"name"`
	SourceType string   `json:"source_type"`
	Source     string   `json:"source"`
	Root       string   `json:"root"`
	Ref        string   `json:"ref,omitempty"`
	Sparse     []string `json:"sparse,omitempty"`
}

type InstalledPlugin struct {
	PluginID        string `json:"plugin_id"`
	Name            string `json:"name"`
	MarketplaceName string `json:"marketplace_name"`
	Version         string `json:"version,omitempty"`
	Enabled         bool   `json:"enabled"`
	Path            string `json:"path"`
}

type AvailablePlugin struct {
	PluginID        string `json:"plugin_id"`
	Name            string `json:"name"`
	MarketplaceName string `json:"marketplace_name"`
	Version         string `json:"version,omitempty"`
	Description     string `json:"description,omitempty"`
	Path            string `json:"path"`
	Installed       bool   `json:"installed"`
	Enabled         bool   `json:"enabled"`
}

type State struct {
	Marketplaces []Marketplace     `json:"marketplaces,omitempty"`
	Installed    []InstalledPlugin `json:"installed,omitempty"`
}

type publicManifest struct {
	Name        string `json:"name"`
	Version     string `json:"version,omitempty"`
	Description string `json:"description,omitempty"`
}

func statePath(home string) string { return filepath.Join(home, "plugins", "state.json") }

// LoadState 读取 Codeworld 管理的 marketplace 与插件安装状态。
func LoadState(home string) (State, error) {
	data, err := os.ReadFile(statePath(home))
	if err != nil {
		if os.IsNotExist(err) {
			return State{}, nil
		}
		return State{}, err
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return State{}, err
	}
	return state, nil
}

// SaveState 原子保存 plugin 生命周期状态。
func SaveState(home string, state State) error {
	dir := filepath.Dir(statePath(home))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	sort.Slice(state.Marketplaces, func(i, j int) bool { return state.Marketplaces[i].Name < state.Marketplaces[j].Name })
	sort.Slice(state.Installed, func(i, j int) bool { return state.Installed[i].PluginID < state.Installed[j].PluginID })
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(dir, ".state-*.json")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, statePath(home))
}

// AddLocalMarketplace 注册一个本地 marketplace，名称取目录名。
func AddLocalMarketplace(home, source string) (Marketplace, error) {
	abs, err := filepath.Abs(source)
	if err != nil {
		return Marketplace{}, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return Marketplace{}, fmt.Errorf("resolve local marketplace source: %w", err)
	}
	if !info.IsDir() {
		return Marketplace{}, fmt.Errorf("marketplace source %q is not a directory", source)
	}
	name := filepath.Base(filepath.Clean(abs))
	if err := validateName(name); err != nil {
		return Marketplace{}, err
	}
	marketplace := Marketplace{Name: name, SourceType: "local", Source: abs, Root: abs}
	state, err := LoadState(home)
	if err != nil {
		return Marketplace{}, err
	}
	for _, existing := range state.Marketplaces {
		if existing.Name == name {
			return Marketplace{}, fmt.Errorf("marketplace %q already exists", name)
		}
	}
	state.Marketplaces = append(state.Marketplaces, marketplace)
	return marketplace, SaveState(home, state)
}

// AddMarketplace 注册本地路径或 Git marketplace；Git source 会保存为可升级快照。
func AddMarketplace(home, source, ref string, sparse []string) (Marketplace, error) {
	if info, err := os.Stat(source); err == nil && info.IsDir() {
		if ref != "" || len(sparse) > 0 {
			return Marketplace{}, fmt.Errorf("--ref and --sparse only apply to Git marketplaces")
		}
		return AddLocalMarketplace(home, source)
	}
	gitURL, name, inlineRef, err := normalizeGitSource(source)
	if err != nil {
		return Marketplace{}, err
	}
	if ref != "" && inlineRef != "" && ref != inlineRef {
		return Marketplace{}, fmt.Errorf("Git ref in source conflicts with --ref")
	}
	if ref == "" {
		ref = inlineRef
	}
	state, err := LoadState(home)
	if err != nil {
		return Marketplace{}, err
	}
	for _, existing := range state.Marketplaces {
		if existing.Name == name {
			return Marketplace{}, fmt.Errorf("marketplace %q already exists", name)
		}
	}
	root := filepath.Join(home, "plugins", "marketplaces", name)
	if err := os.MkdirAll(filepath.Dir(root), 0o700); err != nil {
		return Marketplace{}, err
	}
	args := []string{"clone", "--depth", "1"}
	if ref != "" {
		args = append(args, "--branch", ref)
	}
	args = append(args, gitURL, root)
	if output, err := exec.Command("git", args...).CombinedOutput(); err != nil {
		return Marketplace{}, fmt.Errorf("clone marketplace: %w: %s", err, strings.TrimSpace(string(output)))
	}
	if len(sparse) > 0 {
		sparseArgs := append([]string{"-C", root, "sparse-checkout", "set", "--no-cone", "--"}, sparse...)
		if output, err := exec.Command("git", sparseArgs...).CombinedOutput(); err != nil {
			_ = os.RemoveAll(root)
			return Marketplace{}, fmt.Errorf("configure sparse checkout: %w: %s", err, strings.TrimSpace(string(output)))
		}
	}
	marketplace := Marketplace{Name: name, SourceType: "git", Source: gitURL, Root: root, Ref: ref, Sparse: append([]string(nil), sparse...)}
	state.Marketplaces = append(state.Marketplaces, marketplace)
	if err := SaveState(home, state); err != nil {
		_ = os.RemoveAll(root)
		return Marketplace{}, err
	}
	return marketplace, nil
}

// UpgradeMarketplaces 对指定或全部 Git marketplace 执行快进更新。
func UpgradeMarketplaces(home, name string) ([]Marketplace, error) {
	state, err := LoadState(home)
	if err != nil {
		return nil, err
	}
	var upgraded []Marketplace
	found := name == ""
	for _, marketplace := range state.Marketplaces {
		if name != "" && marketplace.Name != name {
			continue
		}
		found = true
		if marketplace.SourceType != "git" {
			if name != "" {
				return nil, fmt.Errorf("marketplace %q is local and cannot be upgraded", name)
			}
			continue
		}
		args := []string{"-C", marketplace.Root, "pull", "--ff-only"}
		if output, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			return nil, fmt.Errorf("upgrade marketplace %q: %w: %s", marketplace.Name, err, strings.TrimSpace(string(output)))
		}
		upgraded = append(upgraded, marketplace)
	}
	if !found {
		return nil, fmt.Errorf("marketplace %q not found", name)
	}
	return upgraded, nil
}

// RemoveMarketplace 删除 marketplace 配置；仍有安装项时拒绝删除。
func RemoveMarketplace(home, name string) error {
	state, err := LoadState(home)
	if err != nil {
		return err
	}
	for _, plugin := range state.Installed {
		if plugin.MarketplaceName == name {
			return fmt.Errorf("marketplace %q still has installed plugin %q", name, plugin.PluginID)
		}
	}
	found := false
	var removed Marketplace
	filtered := state.Marketplaces[:0]
	for _, marketplace := range state.Marketplaces {
		if marketplace.Name == name {
			found = true
			removed = marketplace
			continue
		}
		filtered = append(filtered, marketplace)
	}
	if !found {
		return fmt.Errorf("marketplace %q not found", name)
	}
	state.Marketplaces = filtered
	if err := SaveState(home, state); err != nil {
		return err
	}
	if removed.SourceType == "git" {
		return os.RemoveAll(removed.Root)
	}
	return nil
}

// ListAvailable 枚举 marketplace 的 plugins 目录并合并安装状态。
func ListAvailable(home, marketplaceFilter string) ([]AvailablePlugin, error) {
	state, err := LoadState(home)
	if err != nil {
		return nil, err
	}
	installed := make(map[string]InstalledPlugin, len(state.Installed))
	for _, plugin := range state.Installed {
		installed[plugin.PluginID] = plugin
	}
	var out []AvailablePlugin
	for _, marketplace := range state.Marketplaces {
		if marketplaceFilter != "" && marketplace.Name != marketplaceFilter {
			continue
		}
		entries, err := os.ReadDir(filepath.Join(marketplace.Root, "plugins"))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			path := filepath.Join(marketplace.Root, "plugins", entry.Name())
			manifest, err := readPublicManifest(path)
			if err != nil {
				return nil, err
			}
			if manifest.Name != entry.Name() {
				return nil, fmt.Errorf("plugin manifest name %q does not match directory %q", manifest.Name, entry.Name())
			}
			if manifest.Version != "" {
				if err := validateName(manifest.Version); err != nil {
					return nil, fmt.Errorf("plugin %q has invalid version: %w", manifest.Name, err)
				}
			}
			id := manifest.Name + "@" + marketplace.Name
			entry := AvailablePlugin{PluginID: id, Name: manifest.Name, MarketplaceName: marketplace.Name, Version: manifest.Version, Description: manifest.Description, Path: path}
			if current, ok := installed[id]; ok {
				entry.Installed, entry.Enabled = true, current.Enabled
			}
			out = append(out, entry)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PluginID < out[j].PluginID })
	return out, nil
}

// InstallPlugin 将 marketplace plugin 复制到用户缓存并启用。
func InstallPlugin(home, selector, marketplaceFlag string) (InstalledPlugin, error) {
	name, marketplace, err := parseSelector(selector, marketplaceFlag)
	if err != nil {
		return InstalledPlugin{}, err
	}
	available, err := ListAvailable(home, marketplace)
	if err != nil {
		return InstalledPlugin{}, err
	}
	id := name + "@" + marketplace
	var selected *AvailablePlugin
	for index := range available {
		if available[index].PluginID == id {
			selected = &available[index]
			break
		}
	}
	if selected == nil {
		return InstalledPlugin{}, fmt.Errorf("plugin %q not found", id)
	}
	destination := filepath.Join(home, "plugins", "cache", marketplace, name, selected.Version)
	if selected.Version == "" {
		destination = filepath.Join(home, "plugins", "cache", marketplace, name, "current")
	}
	if err := os.RemoveAll(destination); err != nil {
		return InstalledPlugin{}, err
	}
	if err := copyTree(selected.Path, destination); err != nil {
		return InstalledPlugin{}, err
	}
	installed := InstalledPlugin{PluginID: id, Name: name, MarketplaceName: marketplace, Version: selected.Version, Enabled: true, Path: destination}
	state, err := LoadState(home)
	if err != nil {
		return InstalledPlugin{}, err
	}
	replaced := false
	for index := range state.Installed {
		if state.Installed[index].PluginID == id {
			state.Installed[index], replaced = installed, true
		}
	}
	if !replaced {
		state.Installed = append(state.Installed, installed)
	}
	return installed, SaveState(home, state)
}

// RemovePlugin 删除插件安装记录和缓存。
func RemovePlugin(home, selector, marketplaceFlag string) (InstalledPlugin, error) {
	name, marketplace, err := parseSelector(selector, marketplaceFlag)
	if err != nil {
		return InstalledPlugin{}, err
	}
	id := name + "@" + marketplace
	state, err := LoadState(home)
	if err != nil {
		return InstalledPlugin{}, err
	}
	var removed InstalledPlugin
	filtered := state.Installed[:0]
	for _, plugin := range state.Installed {
		if plugin.PluginID == id {
			removed = plugin
			continue
		}
		filtered = append(filtered, plugin)
	}
	if removed.PluginID == "" {
		return InstalledPlugin{}, fmt.Errorf("plugin %q is not installed", id)
	}
	cacheRoot := filepath.Join(home, "plugins", "cache")
	rel, err := filepath.Rel(cacheRoot, removed.Path)
	if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return InstalledPlugin{}, fmt.Errorf("plugin %q has invalid cache path", id)
	}
	if err := os.RemoveAll(removed.Path); err != nil {
		return InstalledPlugin{}, err
	}
	state.Installed = filtered
	return removed, SaveState(home, state)
}

func readPublicManifest(pluginPath string) (publicManifest, error) {
	data, err := os.ReadFile(filepath.Join(pluginPath, ".codex-plugin", "plugin.json"))
	if err != nil {
		return publicManifest{}, err
	}
	var manifest publicManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return publicManifest{}, err
	}
	if err := validateName(manifest.Name); err != nil {
		return publicManifest{}, err
	}
	return manifest, nil
}

func parseSelector(selector, marketplaceFlag string) (string, string, error) {
	name, marketplace, hasMarketplace := strings.Cut(selector, "@")
	if hasMarketplace && marketplaceFlag != "" && marketplace != marketplaceFlag {
		return "", "", fmt.Errorf("marketplace in selector conflicts with --marketplace")
	}
	if !hasMarketplace {
		marketplace = marketplaceFlag
	}
	if err := validateName(name); err != nil {
		return "", "", err
	}
	if err := validateName(marketplace); err != nil {
		return "", "", fmt.Errorf("marketplace is required: %w", err)
	}
	return name, marketplace, nil
}

func validateName(name string) error {
	if name == "" || name == "." || name == ".." || filepath.Base(name) != name || strings.ContainsAny(name, `/\\@`) {
		return fmt.Errorf("invalid name %q", name)
	}
	return nil
}

func normalizeGitSource(source string) (string, string, string, error) {
	ref := ""
	if strings.Count(source, "@") == 1 && !strings.HasPrefix(source, "git@") {
		var hasRef bool
		source, ref, hasRef = strings.Cut(source, "@")
		if !hasRef || ref == "" {
			return "", "", "", fmt.Errorf("invalid Git marketplace source")
		}
	}
	gitURL := source
	if !strings.Contains(source, "://") && !strings.HasPrefix(source, "git@") {
		parts := strings.Split(source, "/")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return "", "", "", fmt.Errorf("invalid marketplace source %q", source)
		}
		gitURL = "https://github.com/" + source + ".git"
	}
	pathPart := source
	if index := strings.LastIndexAny(pathPart, "/:"); index >= 0 {
		pathPart = pathPart[index+1:]
	}
	name := strings.TrimSuffix(pathPart, ".git")
	if err := validateName(name); err != nil {
		return "", "", "", err
	}
	return gitURL, name, ref, nil
}

func copyTree(source, destination string) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, rel)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("plugin contains unsupported symlink %q", rel)
		}
		if entry.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		return copyFile(path, target, info.Mode().Perm())
	})
}

func copyFile(source, destination string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}
