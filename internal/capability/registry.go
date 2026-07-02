package capability

import (
	"context"

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

func CloseClients(clients []*mcp.Client) {
	for _, client := range clients {
		_ = client.Close()
	}
}

func loadMCPTools(ctx context.Context, servers []config.MCPServer) ([]tools.Tool, []*mcp.Client, error) {
	var out []tools.Tool
	clients := make([]*mcp.Client, 0, len(servers))
	for _, server := range servers {
		client, err := mcp.StartStdio(ctx, mcp.ServerConfig{Name: server.Name, Command: server.Command, Args: server.Args})
		if err != nil {
			CloseClients(clients)
			return nil, nil, err
		}
		clients = append(clients, client)
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
			out = append(out, tools.NewMCPTool(server.Name, spec, client))
		}
	}
	return out, clients, nil
}
