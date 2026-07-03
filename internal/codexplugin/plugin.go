package codexplugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"codeworld/internal/config"
	"codeworld/internal/skill"
)

type Plugin struct {
	Name       string
	Path       string
	Skills     []skill.Skill
	MCPServers []config.MCPServer
}

type manifest struct {
	Name string `json:"name"`
}

type mcpFile struct {
	MCPServers []config.MCPServer `json:"mcp_servers"`
}

// LoadProject 加载外部或项目内配置，并把原始数据转换为内部结构。
func LoadProject(root string) ([]Plugin, error) {
	// 当前只加载项目内声明的 Codex plugin 子集：skills 和 .mcp.json；不执行任意安装脚本。
	pluginsRoot := filepath.Join(root, ".codeworld", "codex-plugins")
	entries, err := os.ReadDir(pluginsRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var plugins []Plugin
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		plugin, err := loadPlugin(filepath.Join(pluginsRoot, entry.Name()), entry.Name())
		if err != nil {
			return nil, err
		}
		plugins = append(plugins, plugin)
	}
	sort.Slice(plugins, func(i, j int) bool { return plugins[i].Name < plugins[j].Name })
	return plugins, nil
}

// loadPlugin 加载外部或项目内配置，并把原始数据转换为内部结构。
func loadPlugin(path, dirName string) (Plugin, error) {
	manifest, err := readManifest(filepath.Join(path, ".codex-plugin", "plugin.json"))
	if err != nil {
		return Plugin{}, err
	}
	// plugin 名称必须和目录一致，避免 manifest 伪造路径或覆盖其他 plugin 命名空间。
	if manifest.Name == "" || filepath.Base(manifest.Name) != manifest.Name || strings.Contains(manifest.Name, "\\") || manifest.Name != dirName {
		return Plugin{}, fmt.Errorf("invalid codex plugin name %q", manifest.Name)
	}
	plugin := Plugin{Name: manifest.Name, Path: path}
	skills, err := loadSkills(path, manifest.Name)
	if err != nil {
		return Plugin{}, err
	}
	plugin.Skills = skills
	servers, err := loadMCP(path, manifest.Name)
	if err != nil {
		return Plugin{}, err
	}
	plugin.MCPServers = servers
	return plugin, nil
}

// readManifest 读取外部输入，并保持调用方可处理的错误语义。
func readManifest(path string) (manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return manifest{}, err
	}
	var out manifest
	if err := json.Unmarshal(data, &out); err != nil {
		return manifest{}, err
	}
	return out, nil
}

// loadSkills 加载外部或项目内配置，并把原始数据转换为内部结构。
func loadSkills(pluginPath, pluginName string) ([]skill.Skill, error) {
	skillsRoot := filepath.Join(pluginPath, "skills")
	entries, err := os.ReadDir(skillsRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []skill.Skill
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(skillsRoot, entry.Name(), "SKILL.md")
		loaded, err := skill.LoadFile(entry.Name(), path, "codex-plugin:"+pluginName)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		out = append(out, loaded)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// loadMCP 加载外部或项目内配置，并把原始数据转换为内部结构。
func loadMCP(pluginPath, pluginName string) ([]config.MCPServer, error) {
	path := filepath.Join(pluginPath, ".mcp.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var parsed mcpFile
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, err
	}
	servers := make([]config.MCPServer, 0, len(parsed.MCPServers))
	for _, server := range parsed.MCPServers {
		if server.Name == "" || server.Command == "" {
			return nil, fmt.Errorf("codex plugin %q has invalid mcp server %q", pluginName, server.Name)
		}
		// MCP server 名称加 plugin 前缀，避免不同 plugin 的同名 server 冲突。
		server.Name = pluginName + "." + server.Name
		servers = append(servers, server)
	}
	return servers, nil
}
