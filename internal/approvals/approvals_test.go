package approvals

import "testing"

// TestNormalizeCommandOnlyTrimsSurroundingWhitespace 验证批准不会改写命令语义。
func TestNormalizeCommandOnlyTrimsSurroundingWhitespace(t *testing.T) {
	got := NormalizeCommand("  go   test   ./...  ")
	want := "go   test   ./..."
	if got != want {
		t.Fatalf("NormalizeCommand = %q, want %q", got, want)
	}
}

// TestSetAllowsExactNormalizedShellCommand 验证对应场景的行为，避免后续改动破坏既有约束。
func TestSetAllowsExactNormalizedShellCommand(t *testing.T) {
	set := Set{Commands: []string{"go test ./..."}}
	if !set.Allows(" go test ./... ") {
		t.Fatalf("expected surrounding whitespace to be ignored")
	}
	if set.Allows("go  test ./...") {
		t.Fatalf("different internal whitespace should require approval")
	}
	if set.Allows("go test ./internal/...") {
		t.Fatalf("different command should not be allowed")
	}
}

// TestSetAddNormalizesAndDeduplicatesCommands 验证对应场景的行为，避免后续改动破坏既有约束。
func TestSetAddNormalizesAndDeduplicatesCommands(t *testing.T) {
	var set Set
	set.Add(" go test ./... ")
	set.Add("go test ./...")
	if len(set.Commands) != 1 || set.Commands[0] != "go test ./..." {
		t.Fatalf("commands = %#v, want one normalized command", set.Commands)
	}
}
