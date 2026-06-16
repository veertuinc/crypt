// Command crypt runs coding agents inside disposable Anka macOS VMs.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/veertuinc/crypt/internal/cli"
)

func main() {
	// A cancellable context lets VM operations and guest commands stop promptly
	// when the user interrupts (Ctrl-C) or the process is asked to terminate.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := cli.Execute(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "crypt: %v\n", err)
		os.Exit(1)
	}
}
