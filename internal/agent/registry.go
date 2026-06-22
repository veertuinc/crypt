// Package agent defines the coding agents Crypt knows how to launch and the
// flags each one needs to run unattended ("YOLO" mode) inside a sandbox VM.
package agent

import (
	"fmt"
	"sort"
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
		Name:        "grok",
		Summary:     "Run Grok Build in an isolated Anka VM (--always-approve)",
		InjectFlags: []string{"--always-approve"},
	},
	"agent": {
		Name:        "agent",
		Summary:     "Run Grok Build (agent alias) in an isolated Anka VM (--always-approve)",
		InjectFlags: []string{"--always-approve"},
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

// Command builds the full argument vector to run inside the VM: the agent
// executable, its injected flags, then the user's own arguments.
func (a Agent) Command(userArgs []string) []string {
	cmd := make([]string, 0, 1+len(a.InjectFlags)+len(userArgs))
	cmd = append(cmd, a.Name)
	cmd = append(cmd, a.InjectFlags...)
	cmd = append(cmd, userArgs...)
	return cmd
}
