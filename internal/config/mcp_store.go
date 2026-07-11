package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// LoadManagedMCP 读取由 CLI 管理的用户级 MCP server 列表。
func LoadManagedMCP(home string) ([]MCPServer, error) {
	cfg := Default()
	if err := loadConfigFile(filepath.Join(home, "mcp.toml"), &cfg, true, false); err != nil {
		return nil, err
	}
	return dedupeMCPServers(cfg.MCPServers), nil
}

// SaveManagedMCP 原子写入用户级 MCP server 列表，不改动主配置文件。
func SaveManagedMCP(home string, servers []MCPServer) error {
	if err := os.MkdirAll(home, 0o700); err != nil {
		return err
	}
	var out strings.Builder
	for index, server := range servers {
		if strings.TrimSpace(server.Name) == "" {
			return fmt.Errorf("MCP server name is required")
		}
		if index > 0 {
			out.WriteByte('\n')
		}
		out.WriteString("[[mcp_servers]]\n")
		writeMCPString(&out, "name", server.Name)
		writeMCPString(&out, "command", server.Command)
		writeMCPArray(&out, "args", server.Args)
		writeMCPString(&out, "url", server.URL)
		writeMCPString(&out, "bearer_token_env_var", server.BearerTokenEnvVar)
		writeMCPArray(&out, "http_headers", server.HTTPHeaders)
		writeMCPArray(&out, "enabled_tools", server.EnabledTools)
		writeMCPArray(&out, "disabled_tools", server.DisabledTools)
	}
	path := filepath.Join(home, "mcp.toml")
	tmp, err := os.CreateTemp(home, ".mcp-*.toml")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(out.String()); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func writeMCPString(out *strings.Builder, key, value string) {
	if value != "" {
		fmt.Fprintf(out, "%s = %s\n", key, strconv.Quote(value))
	}
}

func writeMCPArray(out *strings.Builder, key string, values []string) {
	if len(values) == 0 {
		return
	}
	quoted := make([]string, len(values))
	for index, value := range values {
		quoted[index] = strconv.Quote(value)
	}
	fmt.Fprintf(out, "%s = [%s]\n", key, strings.Join(quoted, ", "))
}
