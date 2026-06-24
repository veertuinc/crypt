// Package agent defines the coding agents Crypt knows how to launch and the
// flags each one needs to run unattended ("YOLO" mode) inside a sandbox VM.
package agent

import (
	"fmt"
	"sort"
	"strings"
)

// Agent describes a coding agent that Crypt can run inside an ephemeral VM.
type Agent struct {
	// Name is both the Crypt subcommand and the executable invoked in the VM.
	Name string
	// Summary is a short description shown in `crypt --help`.
	Summary string
	// InjectFlags are prepended to the user-supplied arguments so the agent
	// runs without interactive permission prompts. These are only safe because
	// the agent is confined to a throwaway VM.
	InjectFlags []string
	// TaskInjectFlags are appended to InjectFlags for task prompts only (when
	// the user passes arguments). Use for headless flags that would break the
	// interactive TUI, such as Grok's --output-format plain.
	TaskInjectFlags []string
	// PromptFlag, when set, is inserted before userArgs for task prompts (e.g.
	// Grok's -p for headless single-prompt mode via SSH.
	PromptFlag string
}

// registry is the single source of truth for the supported agents. Adding a
// new agent is a one-line entry here; no new command file is required.
var registry = map[string]Agent{
	"claude": {
		Name:        "claude",
		Summary:     "Run Claude Code in an isolated Anka VM (--dangerously-skip-permissions)",
		InjectFlags: []string{
			"--dangerously-skip-permissions",
			"--settings", `{"skipDangerousModePermissionPrompt":true}`,
		},
	},
	"codex": {
		Name:        "codex",
		Summary:     "Run Codex in an isolated Anka VM (--dangerously-bypass-approvals-and-sandbox)",
		InjectFlags: []string{"--dangerously-bypass-approvals-and-sandbox"},
	},
	"codex-fugu": {
		Name:        "codex-fugu",
		Summary:     "Run Sakana Fugu (codex-fugu) in an isolated Anka VM (--dangerously-bypass-approvals-and-sandbox)",
		InjectFlags: []string{"--dangerously-bypass-approvals-and-sandbox"},
	},
	"grok": {
		Name:            "grok",
		Summary:         "Run Grok Build in an isolated Anka VM (--always-approve, --no-auto-update)",
		InjectFlags:     []string{"--always-approve", "--no-auto-update"},
		TaskInjectFlags: []string{"--output-format", "plain"},
		PromptFlag:      "-p",
	},
	"agent": {
		Name:            "agent",
		Summary:         "Run Grok Build (agent alias) in an isolated Anka VM (--always-approve, --no-auto-update)",
		InjectFlags:     []string{"--always-approve", "--no-auto-update"},
		TaskInjectFlags: []string{"--output-format", "plain"},
		PromptFlag:      "-p",
	},
}

// Get returns the agent registered under name.
func Get(name string) (Agent, error) {
	a, ok := registry[name]
	if !ok {
		return Agent{}, fmt.Errorf("unknown agent %q", name)
	}
	return a, nil
}

// All returns every registered agent, sorted by name for stable output.
func All() []Agent {
	agents := make([]Agent, 0, len(registry))
	for _, a := range registry {
		agents = append(agents, a)
	}
	sort.Slice(agents, func(i, j int) bool { return agents[i].Name < agents[j].Name })
	return agents
}

// HasTaskPrompt reports whether userArgs include a positional task prompt
// rather than only agent flags (e.g. --reasoning-effort high).
func HasTaskPrompt(userArgs []string) bool {
	for i, arg := range userArgs {
		if arg == "--" {
			continue
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		if i > 0 && strings.HasPrefix(userArgs[i-1], "-") {
			continue
		}
		return true
	}
	return false
}

// Command builds the full argument vector to run inside the VM: the agent
// executable, its injected flags, then the user's own arguments.
func (a Agent) Command(userArgs []string) []string {
	inject := a.InjectFlags
	if HasTaskPrompt(userArgs) && len(a.TaskInjectFlags) > 0 {
		inject = append(append([]string{}, inject...), a.TaskInjectFlags...)
	}
	cmd := make([]string, 0, 1+len(inject)+len(userArgs)+1)
	cmd = append(cmd, a.Name)
	cmd = append(cmd, inject...)
	if a.PromptFlag != "" && HasTaskPrompt(userArgs) {
		cmd = append(cmd, a.PromptFlag)
	}
	cmd = append(cmd, userArgs...)
	return cmd
}
