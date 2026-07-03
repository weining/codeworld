package approvals

import "testing"

// TestNormalizeCommandCollapsesWhitespace 验证对应场景的行为，避免后续改动破坏既有约束。
func TestNormalizeCommandCollapsesWhitespace(t *testing.T) {
	got := NormalizeCommand("  go   test   ./...  ")
	want := "go test ./..."
	if got != want {
		t.Fatalf("NormalizeCommand = %q, want %q", got, want)
	}
}

// TestSetAllowsExactNormalizedShellCommand 验证对应场景的行为，避免后续改动破坏既有约束。
func TestSetAllowsExactNormalizedShellCommand(t *testing.T) {
	set := Set{Commands: []string{"go test ./..."}}
	if !set.Allows(" go   test ./... ") {
		t.Fatalf("expected normalized command to be allowed")
	}
	if set.Allows("go test ./internal/...") {
		t.Fatalf("different command should not be allowed")
	}
}

// TestSetAddNormalizesAndDeduplicatesCommands 验证对应场景的行为，避免后续改动破坏既有约束。
func TestSetAddNormalizesAndDeduplicatesCommands(t *testing.T) {
	var set Set
	set.Add(" go   test ./... ")
	set.Add("go test ./...")
	if len(set.Commands) != 1 || set.Commands[0] != "go test ./..." {
		t.Fatalf("commands = %#v, want one normalized command", set.Commands)
	}
}
