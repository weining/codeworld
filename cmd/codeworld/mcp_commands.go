package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strings"

	"codeworld/internal/config"
	"codeworld/internal/mcp"
)

const mcpUsage = "usage: codeworld mcp <list|get|add|remove|login|logout>"

func runMCPCommand(out io.Writer, root string, global globalOptions, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("%s", mcpUsage)
	}
	switch args[0] {
	case "list":
		return listMCPServers(out, root, global, args[1:])
	case "get":
		return getMCPServer(out, root, global, args[1:])
	case "add":
		return addMCPServer(out, args[1:])
	case "remove":
		return removeMCPServer(out, args[1:])
	case "login":
		return loginMCPServer(out, root, global, args[1:])
	case "logout":
		return logoutMCPServer(out, root, args[1:])
	default:
		return fmt.Errorf("%s", mcpUsage)
	}
}

func loginMCPServer(out io.Writer, root string, global globalOptions, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: codeworld mcp login <name> [--scopes <scope,scope>]")
	}
	name := args[0]
	var scopes []string
	for index := 1; index < len(args); index++ {
		if args[index] != "--scopes" || index+1 >= len(args) {
			return fmt.Errorf("unknown MCP login option %s", args[index])
		}
		for _, scope := range strings.Split(args[index+1], ",") {
			if scope = strings.TrimSpace(scope); scope != "" {
				scopes = append(scopes, scope)
			}
		}
		index++
	}
	cfg, err := config.LoadWithOptions(root, config.LoadOptions{Profile: global.Profile, Overrides: global.Config})
	if err != nil {
		return err
	}
	var server *config.MCPServer
	for index := range cfg.MCPServers {
		if cfg.MCPServers[index].Name == name {
			server = &cfg.MCPServers[index]
			break
		}
	}
	if server == nil {
		return fmt.Errorf("MCP server %q not found", name)
	}
	if server.URL == "" {
		return fmt.Errorf("MCP server %q uses stdio and does not support OAuth login", name)
	}
	token, err := mcp.LoginOAuth(context.Background(), mcp.OAuthLoginOptions{
		ServerName: name, ServerURL: server.URL, Scopes: scopes,
		CallbackURL: cfg.MCPOAuthCallbackURL, CallbackPort: cfg.MCPOAuthCallbackPort,
		NotifyURL: func(target string) { _, _ = fmt.Fprintf(out, "Open this URL to authorize %s:\n%s\n", name, target) },
	})
	if err != nil {
		return err
	}
	if err := mcp.SaveOAuthToken(root, token); err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "logged in to MCP server %s\n", name)
	return err
}

func logoutMCPServer(out io.Writer, root string, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: codeworld mcp logout <name>")
	}
	if err := mcp.DeleteOAuthToken(root, args[0]); err != nil {
		return err
	}
	_, err := fmt.Fprintf(out, "logged out of MCP server %s\n", args[0])
	return err
}

func listMCPServers(out io.Writer, root string, global globalOptions, args []string) error {
	jsonOutput, err := parseJSONFlag(args)
	if err != nil {
		return err
	}
	cfg, err := config.LoadWithOptions(root, config.LoadOptions{Profile: global.Profile, Overrides: global.Config})
	if err != nil {
		return err
	}
	servers := append([]config.MCPServer(nil), cfg.MCPServers...)
	sort.Slice(servers, func(i, j int) bool { return servers[i].Name < servers[j].Name })
	if jsonOutput {
		return writeJSON(out, servers)
	}
	if len(servers) == 0 {
		_, err = fmt.Fprintln(out, "no MCP servers configured")
		return err
	}
	for _, server := range servers {
		transport := "stdio"
		target := strings.Join(append([]string{server.Command}, server.Args...), " ")
		if server.URL != "" {
			transport, target = "http", server.URL
		}
		if _, err := fmt.Fprintf(out, "%s %s %s\n", server.Name, transport, strings.TrimSpace(target)); err != nil {
			return err
		}
	}
	return nil
}

func getMCPServer(out io.Writer, root string, global globalOptions, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: codeworld mcp get <name> [--json]")
	}
	name := args[0]
	jsonOutput, err := parseJSONFlag(args[1:])
	if err != nil {
		return err
	}
	cfg, err := config.LoadWithOptions(root, config.LoadOptions{Profile: global.Profile, Overrides: global.Config})
	if err != nil {
		return err
	}
	for _, server := range cfg.MCPServers {
		if server.Name != name {
			continue
		}
		if jsonOutput {
			return writeJSON(out, server)
		}
		return writeJSON(out, server)
	}
	return fmt.Errorf("MCP server %q not found", name)
}

func addMCPServer(out io.Writer, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: codeworld mcp add <name> (--url <url> | -- <command...>)")
	}
	server := config.MCPServer{Name: strings.TrimSpace(args[0])}
	if server.Name == "" || strings.ContainsAny(server.Name, " \t/\\") {
		return fmt.Errorf("invalid MCP server name %q", server.Name)
	}
	remaining := args[1:]
	if remaining[0] == "--url" {
		if len(remaining) < 2 {
			return fmt.Errorf("--url requires a value")
		}
		server.URL = remaining[1]
		parsed, err := url.Parse(server.URL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return fmt.Errorf("invalid MCP URL %q", server.URL)
		}
		for index := 2; index < len(remaining); index++ {
			switch remaining[index] {
			case "--bearer-token-env-var":
				if index+1 >= len(remaining) {
					return fmt.Errorf("--bearer-token-env-var requires a value")
				}
				server.BearerTokenEnvVar = remaining[index+1]
				index++
			case "--header":
				if index+1 >= len(remaining) {
					return fmt.Errorf("--header requires a value")
				}
				server.HTTPHeaders = append(server.HTTPHeaders, remaining[index+1])
				index++
			default:
				return fmt.Errorf("unknown MCP add option %s", remaining[index])
			}
		}
	} else {
		if remaining[0] != "--" || len(remaining) < 2 {
			return fmt.Errorf("usage: codeworld mcp add <name> -- <command...>")
		}
		server.Command = remaining[1]
		server.Args = append([]string(nil), remaining[2:]...)
	}
	home, err := config.Home("")
	if err != nil {
		return err
	}
	servers, err := config.LoadManagedMCP(home)
	if err != nil {
		return err
	}
	replaced := false
	for index := range servers {
		if servers[index].Name == server.Name {
			servers[index], replaced = server, true
		}
	}
	if !replaced {
		servers = append(servers, server)
	}
	if err := config.SaveManagedMCP(home, servers); err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "%s MCP server %s\n", map[bool]string{true: "updated", false: "added"}[replaced], server.Name)
	return err
}

func removeMCPServer(out io.Writer, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: codeworld mcp remove <name>")
	}
	home, err := config.Home("")
	if err != nil {
		return err
	}
	servers, err := config.LoadManagedMCP(home)
	if err != nil {
		return err
	}
	filtered := servers[:0]
	found := false
	for _, server := range servers {
		if server.Name == args[0] {
			found = true
			continue
		}
		filtered = append(filtered, server)
	}
	if !found {
		return fmt.Errorf("managed MCP server %q not found", args[0])
	}
	if err := config.SaveManagedMCP(home, filtered); err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "removed MCP server %s\n", args[0])
	return err
}

func parseJSONFlag(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	if len(args) == 1 && args[0] == "--json" {
		return true, nil
	}
	return false, fmt.Errorf("unknown option %s", args[0])
}

func writeJSON(out io.Writer, value any) error {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
