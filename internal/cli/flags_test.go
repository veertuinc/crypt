package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/veertuinc/crypt/internal/agent"
	"github.com/veertuinc/crypt/internal/sandbox"
)

func TestRunOptionsUnlockKeychainFromFlag(t *testing.T) {
	var got sandbox.Options
	original := runSandbox
	runSandbox = func(_ context.Context, _ agent.Agent, _ []string, opts sandbox.Options) error {
		got = opts
		return nil
	}
	t.Cleanup(func() { runSandbox = original })
	t.Setenv("CRYPT_UNLOCK_KEYCHAIN", "")

	cmd := newRootCmd()
	cmd.SetArgs([]string{"run", "--unlock-keychain", "login=admin", "--", "/bin/echo", "ok"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error: %v", err)
	}
	if got.UnlockKeychain != "login=admin" {
		t.Fatalf("UnlockKeychain = %q, want login=admin", got.UnlockKeychain)
	}
}

func TestRunOptionsUnlockKeychainFromEnv(t *testing.T) {
	var got sandbox.Options
	original := runSandbox
	runSandbox = func(_ context.Context, _ agent.Agent, _ []string, opts sandbox.Options) error {
		got = opts
		return nil
	}
	t.Cleanup(func() { runSandbox = original })
	t.Setenv("CRYPT_UNLOCK_KEYCHAIN", "login=from-env")

	cmd := newRootCmd()
	cmd.SetArgs([]string{"run", "--", "/bin/echo", "ok"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error: %v", err)
	}
	if got.UnlockKeychain != "login=from-env" {
		t.Fatalf("UnlockKeychain = %q, want login=from-env", got.UnlockKeychain)
	}
}

func TestRunOptionsUnlockKeychainFlagOverridesEnv(t *testing.T) {
	var got sandbox.Options
	original := runSandbox
	runSandbox = func(_ context.Context, _ agent.Agent, _ []string, opts sandbox.Options) error {
		got = opts
		return nil
	}
	t.Cleanup(func() { runSandbox = original })
	t.Setenv("CRYPT_UNLOCK_KEYCHAIN", "login=from-env")

	cmd := newRootCmd()
	cmd.SetArgs([]string{"--unlock-keychain", "login=from-flag", "run", "--", "/bin/echo", "ok"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error: %v", err)
	}
	if got.UnlockKeychain != "login=from-flag" {
		t.Fatalf("UnlockKeychain = %q, want login=from-flag", got.UnlockKeychain)
	}
}

func TestRootHelpMentionsUnlockKeychain(t *testing.T) {
	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"--unlock-keychain", "NAME=PASSWORD", "CRYPT_UNLOCK_KEYCHAIN"} {
		if !strings.Contains(out, want) {
			t.Fatalf("root help missing %q:\n%s", want, out)
		}
	}
}

func TestPassthroughUnlockKeychainBeforeSubcommand(t *testing.T) {
	got := passthroughAgentArgsFrom("cursor-agent", []string{
		"crypt", "--name", "cursor-worker", "--unlock-keychain", "login=admin",
		"cursor-agent", "worker", "start", "--name", "crypt-vm",
	})
	want := []string{"worker", "start", "--name", "crypt-vm"}
	if len(got) != len(want) {
		t.Fatalf("passthroughAgentArgs() = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("passthroughAgentArgs()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
