package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/veertuinc/crypt/internal/sandbox"
)

func newDestroyCmd(cfg *config) *cobra.Command {
	return &cobra.Command{
		Use:   "destroy",
		Short: "Delete a kept crypt run VM",
		Long:  "Delete a kept crypt run VM. Uses --name when set; otherwise the clone VM kept for the current directory.",
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := sandbox.CloneName(cmd.Context(), cfg.baseVM, cfg.name)
			if err != nil {
				return fmt.Errorf("crypt destroy: %w", err)
			}
			if err := sandbox.Destroy(cmd.Context(), cfg.baseVM, name); err != nil {
				return err
			}
			if cfg.name == "" {
				return sandbox.ClearSessionForCWD()
			}
			return nil
		},
	}
}
