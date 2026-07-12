package codexplugin

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLocalMarketplaceInstallAndRemove(t *testing.T) {
	home := t.TempDir()
	marketplaceRoot := filepath.Join(t.TempDir(), "demo-market")
	pluginRoot := filepath.Join(marketplaceRoot, "plugins", "reviewer")
	if err := os.MkdirAll(filepath.Join(pluginRoot, ".codex-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginRoot, ".codex-plugin", "plugin.json"), []byte(`{"name":"reviewer","version":"1.2.3"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(pluginRoot, "skills", "review"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginRoot, "skills", "review", "SKILL.md"), []byte("# Review\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	marketplace, err := AddLocalMarketplace(home, marketplaceRoot)
	if err != nil {
		t.Fatal(err)
	}
	available, err := ListAvailable(home, marketplace.Name)
	if err != nil {
		t.Fatal(err)
	}
	if len(available) != 1 || available[0].PluginID != "reviewer@demo-market" || available[0].Installed {
		t.Fatalf("available = %#v", available)
	}
	installed, err := InstallPlugin(home, "reviewer@demo-market", "")
	if err != nil {
		t.Fatal(err)
	}
	if installed.Version != "1.2.3" || !installed.Enabled {
		t.Fatalf("installed = %#v", installed)
	}
	loaded, err := LoadInstalled(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || len(loaded[0].Skills) != 1 {
		t.Fatalf("loaded = %#v", loaded)
	}
	if _, err := RemovePlugin(home, "reviewer", "demo-market"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(installed.Path); !os.IsNotExist(err) {
		t.Fatalf("plugin cache remains: %v", err)
	}
	if err := RemoveMarketplace(home, "demo-market"); err != nil {
		t.Fatal(err)
	}
}

func TestMarketplaceRejectsRemovalWithInstalledPlugin(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(t.TempDir(), "market")
	manifestDir := filepath.Join(root, "plugins", "demo", ".codex-plugin")
	if err := os.MkdirAll(manifestDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(manifestDir, "plugin.json"), []byte(`{"name":"demo"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := AddLocalMarketplace(home, root); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallPlugin(home, "demo@market", ""); err != nil {
		t.Fatal(err)
	}
	if err := RemoveMarketplace(home, "market"); err == nil {
		t.Fatal("removed marketplace with installed plugin")
	}
}

func TestNormalizeGitSource(t *testing.T) {
	tests := []struct {
		source, url, name, ref string
	}{
		{"owner/repo", "https://github.com/owner/repo.git", "repo", ""},
		{"owner/repo@main", "https://github.com/owner/repo.git", "repo", "main"},
		{"https://example.com/team/tools.git", "https://example.com/team/tools.git", "tools", ""},
		{"git@example.com:team/tools.git", "git@example.com:team/tools.git", "tools", ""},
	}
	for _, test := range tests {
		gotURL, gotName, gotRef, err := normalizeGitSource(test.source)
		if err != nil {
			t.Fatalf("normalizeGitSource(%q): %v", test.source, err)
		}
		if gotURL != test.url || gotName != test.name || gotRef != test.ref {
			t.Fatalf("normalizeGitSource(%q) = %q, %q, %q", test.source, gotURL, gotName, gotRef)
		}
	}
}
