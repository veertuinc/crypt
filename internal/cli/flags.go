package cli

import (
	"github.com/spf13/cobra"
	"github.com/veertuinc/crypt/internal/sandbox"
)

// config holds flags shared across the crypt command tree.
type config struct {
	name    string
	baseVM  string
	cpu     uint32
	memory  uint32
	mountPaths []string
	noLocal bool
	destroy bool
}

func (c config) runOptions() sandbox.Options {
	return sandbox.Options{
		BaseVM:  c.baseVM,
		Name:    c.name,
		CPU:     c.cpu,
		Memory:  c.memory,
		MountPaths: c.mountPaths,
		NoLocal: c.noLocal,
		Destroy: c.destroy,
	}
}

func (c *config) bindPersistentFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().StringVar(&c.name, "name", "", "clone VM name (default: crypt-clone-N, reused per directory)")
	cmd.PersistentFlags().StringVar(&c.baseVM, "vm", defaultBaseVM, "base Anka VM to clone from on first use")
}

func (c *config) bindRunFlags(cmd *cobra.Command) {
	cmd.Flags().Uint32Var(&c.cpu, "cpu", 0, "override vCPU core count (0 = use the VM's setting)")
	cmd.Flags().Uint32Var(&c.memory, "memory", 0, "override RAM in megabytes (0 = use the VM's setting)")
	cmd.Flags().StringArrayVar(&c.mountPaths, "mount", nil, "host directory to share with the VM (repeatable; pass . for the current directory; optional :folder_name for the name under /Volumes/My Shared Files)")
	cmd.Flags().BoolVar(&c.noLocal, "no-local", false, "block VM-to-VM and VM-to-host network on the clone (Anka Enterprise)")
	cmd.Flags().BoolVar(&c.destroy, "destroy", false, "delete the clone when the run ends (default: keep until crypt destroy)")

	// Stop parsing at the first positional and let unrecognized flags fall
	// through so they reach the agent.
	cmd.Flags().SetInterspersed(false)
	cmd.FParseErrWhitelist.UnknownFlags = true
}
