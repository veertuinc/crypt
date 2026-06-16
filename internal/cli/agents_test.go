package cli

import (
	"context"
	"testing"

	"github.com/veertuinc/crypt/internal/agent"
	"github.com/veertuinc/crypt/internal/sandbox"
)

func TestInteractiveAgentDoesNotDestroyCloneByDefault(t *testing.T) {
	var got sandbox.Options
	originalRunSandbox := runSandbox
	runSandbox = func(_ context.Context, _ agent.Agent, _ []string, opts sandbox.Options) error {
		got = opts
		return nil
	}
	t.Cleanup(func() { runSandbox = originalRunSandbox })

	cmd := newRootCmd()
	cmd.SetArgs([]string{"claude"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error: %v", err)
	}

	if got.Destroy {
		t.Fatal("interactive agent Destroy = true, want false")
	}
}
