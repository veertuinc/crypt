package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/veertuinc/crypt/internal/agent"
	"github.com/veertuinc/crypt/internal/sandbox"
)

type sandboxRunner func(context.Context, agent.Agent, []string, sandbox.Options) error

var runSandbox sandboxRunner = sandbox.Run

// newAgentCmds builds one subcommand per registered agent. Each injects that
// agent's unattended-mode flags before the user's arguments.
func newAgentCmds(cfg *config) []*cobra.Command {
	var cmds []*cobra.Command
	for _, ag := range agent.All() {
		ag := ag // capture per iteration
		cmd := &cobra.Command{
			Use:   fmt.Sprintf("%s [flags] [-- %s-args...]", ag.Name, ag.Name),
			Short: ag.Summary,
			RunE: func(cmd *cobra.Command, args []string) error {
				return runSandbox(cmd.Context(), ag, args, cfg.runOptions())
			},
		}
		cfg.bindRunFlags(cmd)
		cmds = append(cmds, cmd)
	}
	return cmds
}

// newRunCmd builds a generic passthrough that runs an arbitrary command inside
// the sandbox VM with no injected flags.
func newRunCmd(cfg *config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run [flags] -- <command> [args...]",
		Short: "Run an arbitrary command inside a disposable Anka VM",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// The first positional is the executable; the rest are its args.
			custom := agent.Agent{Name: args[0]}
			return runSandbox(cmd.Context(), custom, args[1:], cfg.runOptions())
		},
	}
	cfg.bindRunFlags(cmd)
	return cmd
}
