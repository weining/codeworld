package capability

import (
	"context"
	"encoding/json"

	"codeworld/internal/codexplugin"
	"codeworld/internal/config"
	"codeworld/internal/mcp"
	"codeworld/internal/plugin"
	"codeworld/internal/skill"
	"codeworld/internal/tools"
	"codeworld/internal/workspace"
)

type Options struct {
	Root           string
	Workspace      workspace.Workspace
	PluginsEnabled bool
	MCPServers     []config.MCPServer
}

type Loaded struct {
	Tools        []tools.Tool
	Skills       []skill.Skill
	SkillContext string
	MCPClients   []*mcp.Client
}

// Load 加载外部或项目内配置，并把原始数据转换为内部结构。
func Load(ctx context.Context, opts Options) (Loaded, error) {
	var loaded Loaded
	projectSkills, err := skill.LoadProject(opts.Root)
	if err != nil {
		return Loaded{}, err
	}
	loaded.Skills = projectSkills

	codexPlugins, err := codexplugin.LoadProject(opts.Root)
	if err != nil {
		return Loaded{}, err
	}
	mcpServers := append([]config.MCPServer{}, opts.MCPServers...)
	for _, plugin := range codexPlugins {
		loaded.Skills = append(loaded.Skills, plugin.Skills...)
		mcpServers = append(mcpServers, plugin.MCPServers...)
	}
	loaded.SkillContext = skill.Index(loaded.Skills)

	pluginTools, err := plugin.LoadManifests(opts.Root, opts.PluginsEnabled)
	if err != nil {
		return Loaded{}, err
	}
	for _, spec := range pluginTools {
		loaded.Tools = append(loaded.Tools, tools.NewPluginTool(opts.Workspace, spec))
	}

	mcpTools, clients, err := loadMCPTools(ctx, mcpServers)
	if err != nil {
		CloseClients(loaded.MCPClients)
		return Loaded{}, err
	}
	loaded.Tools = append(loaded.Tools, mcpTools...)
	loaded.MCPClients = clients
	return loaded, nil
}

// CloseClients 释放持有的资源，避免后台进程或句柄泄漏。
func CloseClients(clients []*mcp.Client) {
	for _, client := range clients {
		_ = client.Close()
	}
}

// loadMCPTools 加载外部或项目内配置，并把原始数据转换为内部结构。
func loadMCPTools(ctx context.Context, servers []config.MCPServer) ([]tools.Tool, []*mcp.Client, error) {
	var out []tools.Tool
	clients := make([]*mcp.Client, 0, len(servers))
	for _, server := range servers {
		var client interface {
			Initialize(context.Context) error
			ListTools(context.Context) ([]mcp.Tool, error)
			CallTool(context.Context, string, json.RawMessage) (mcp.CallToolResult, error)
		}
		if server.URL != "" {
			client = mcp.NewHTTPClient(mcp.ServerConfig{Name: server.Name, URL: server.URL, BearerTokenEnvVar: server.BearerTokenEnvVar, HTTPHeaders: server.HTTPHeaders})
		} else {
			stdioClient, err := mcp.StartStdio(ctx, mcp.ServerConfig{Name: server.Name, Command: server.Command, Args: server.Args})
			if err != nil {
				CloseClients(clients)
				return nil, nil, err
			}
			clients = append(clients, stdioClient)
			client = stdioClient
		}
		if err := client.Initialize(ctx); err != nil {
			CloseClients(clients)
			return nil, nil, err
		}
		listed, err := client.ListTools(ctx)
		if err != nil {
			CloseClients(clients)
			return nil, nil, err
		}
		for _, spec := range listed {
			if !mcpToolEnabled(server, spec.Name) {
				continue
			}
			out = append(out, tools.NewMCPTool(server.Name, spec, client))
		}
	}
	return out, clients, nil
}

// mcpToolEnabled 应用 allow/deny 列表；deny 优先于 allow。
func mcpToolEnabled(server config.MCPServer, name string) bool {
	for _, disabled := range server.DisabledTools {
		if disabled == name {
			return false
		}
	}
	if len(server.EnabledTools) == 0 {
		return true
	}
	for _, enabled := range server.EnabledTools {
		if enabled == name {
			return true
		}
	}
	return false
}
