package agent

import (
	"reflect"
	"sort"
	"testing"
)

func TestGetKnownAgent(t *testing.T) {
	a, err := Get("claude")
	if err != nil {
		t.Fatalf("Get(claude) returned error: %v", err)
	}
	if a.Name != "claude" {
		t.Errorf("Name = %q, want %q", a.Name, "claude")
	}
	if len(a.InjectFlags) == 0 {
		t.Error("expected claude to inject at least one flag")
	}
}

func TestGetUnknownAgent(t *testing.T) {
	if _, err := Get("does-not-exist"); err == nil {
		t.Fatal("expected error for unknown agent, got nil")
	}
}

func TestAllIsSortedAndComplete(t *testing.T) {
	agents := All()
	if len(agents) != len(registry) {
		t.Fatalf("All() returned %d agents, want %d", len(agents), len(registry))
	}

	names := make([]string, len(agents))
	for i, a := range agents {
		names[i] = a.Name
	}
	if !sort.StringsAreSorted(names) {
		t.Errorf("All() not sorted by name: %v", names)
	}
}

func TestClaudeInjectFlags(t *testing.T) {
	a, err := Get("claude")
	if err != nil {
		t.Fatalf("Get(claude): %v", err)
	}
	got := a.Command(nil)
	want := []string{
		"claude",
		"--dangerously-skip-permissions",
		"--settings", `{"skipDangerousModePermissionPrompt":true}`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Command(nil) = %v, want %v", got, want)
	}
}

func TestCommandComposition(t *testing.T) {
	a := Agent{Name: "claude", InjectFlags: []string{"--yolo", "--no-prompt"}}

	got := a.Command([]string{"--model", "opus"})
	want := []string{"claude", "--yolo", "--no-prompt", "--model", "opus"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Command() = %v, want %v", got, want)
	}
}

func TestCommandWithNoUserArgs(t *testing.T) {
	a := Agent{Name: "codex", InjectFlags: []string{"--bypass"}}

	got := a.Command(nil)
	want := []string{"codex", "--bypass"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Command(nil) = %v, want %v", got, want)
	}
}

func TestCodexFuguInjectFlags(t *testing.T) {
	a, err := Get("codex-fugu")
	if err != nil {
		t.Fatalf("Get(codex-fugu): %v", err)
	}
	got := a.Command([]string{"fix the test"})
	want := []string{
		"codex-fugu",
		"--dangerously-bypass-approvals-and-sandbox",
		"fix the test",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Command() = %v, want %v", got, want)
	}
}

func TestCommandDoesNotMutateInjectFlags(t *testing.T) {
	a := Agent{Name: "claude", InjectFlags: []string{"--yolo"}}

	_ = a.Command([]string{"a", "b"})
	if !reflect.DeepEqual(a.InjectFlags, []string{"--yolo"}) {
		t.Errorf("InjectFlags mutated by Command(): %v", a.InjectFlags)
	}
}
