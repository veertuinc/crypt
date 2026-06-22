// Package cli wires the Crypt command tree and translates flags into sandbox
// runs.
package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/veertuinc/crypt/internal/meta"
)

// defaultBaseVM is the VM cloned for each run unless overridden with --vm.
const defaultBaseVM = "crypt-base"

// newRootCmd builds the top-level `crypt` command and its subcommands.
func newRootCmd() *cobra.Command {
	cfg := &config{}

	root := &cobra.Command{
		Use:   "crypt",
		Short: "Run coding agents inside disposable Anka macOS VMs",
		Long: `Crypt runs coding agents (Claude, Codex, ...) inside an Anka macOS VM.
Each run clones a prepared base VM and executes the agent. Clones stay on disk
and remain running between sessions so agent state survives. Pass --destroy to
delete after a run, or run crypt --name NAME destroy later.

Usage:
  crypt claude --mount "fix the UI"              # VM name: project1 (from directory)
  crypt --name backend claude --mount "add an endpoint"
  crypt --name frontend codex --mount -- "refactor the parser"
  crypt codex-fugu --mount "investigate the flaky test"
  crypt --name frontend run -- /bin/bash -l
  crypt destroy                                  # or crypt --name frontend destroy

One-time setup:
  1. Create a base VM (default name "crypt-base"; override with --vm):
       ` + "`anka create crypt-base latest`" + `
  2. Start it, install and authenticate your agent(s), then stop the VM.
     Crypt clones this VM for every run.

Flags:
  --name NAME    Explicit clone VM name. When omitted, Crypt picks crypt-clone-N
                 and reuses that clone for the current directory across runs.
  --mount        Share the current directory with the VM. Required when the agent
                 needs to read or edit project files. Exposes that host path —
                 omit it if you do not need filesystem access.
  --destroy      Delete the clone when the run ends (default: keep until crypt destroy).
  --no-local     Block VM-to-host and VM-to-VM network on the clone (Anka Enterprise).
  --vm NAME      Base VM to clone (default: crypt-base).
  --cpu, --memory   Override vCPU count or RAM (in MB) for this run only.

Crypt injects unattended-mode flags and forwards all other arguments to the
agent. The VM is the safety boundary: without --mount, agents cannot reach your
host filesystem even in "YOLO" mode.`,
		Version:       fmt.Sprintf("%s (commit %s, built %s)", meta.Version(), meta.Commit(), meta.Date()),
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cfg.bindPersistentFlags(root)

	for _, cmd := range newAgentCmds(cfg) {
		root.AddCommand(cmd)
	}
	root.AddCommand(newRunCmd(cfg))
	root.AddCommand(newDestroyCmd(cfg))

	return root
}

// Execute runs the Crypt command tree with the provided context.
func Execute(ctx context.Context) error {
	return newRootCmd().ExecuteContext(ctx)
}
