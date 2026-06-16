package sandbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/veertuinc/crypt/internal/agent"
	"github.com/veertuinc/crypt/internal/anka"
)

const (
	// defaultSSHUser is the macOS account Crypt logs into on the clone. Anka
	// base VMs conventionally ship with an "anka" user.
	defaultSSHUser = "anka"
	// sshReadyTimeout caps how long we wait for the guest's sshd to come up
	// after the VM starts.
	sshReadyTimeout = 90 * time.Second
	// sshProbeInterval is the delay between connection attempts while waiting.
	sshProbeInterval = 2 * time.Second
)

// sshUser is the account Crypt connects as, overridable via CRYPT_SSH_USER.
func sshUser() string {
	if user := strings.TrimSpace(os.Getenv("CRYPT_SSH_USER")); user != "" {
		return user
	}
	return defaultSSHUser
}

// runInteractiveSSH authorizes Crypt's SSH key in the clone, then opens an
// interactive agent session over SSH. ssh -t allocates a real PTY in the guest,
// which anka run cannot do, so agents like Claude Code stay in interactive mode.
func runInteractiveSSH(ctx context.Context, client *anka.Client, vm, guestDir string, ag agent.Agent, suppressLifecycleLogs bool) error {
	user := sshUser()

	keyPath, pubKey, err := ensureSSHKey()
	if err != nil {
		return fmt.Errorf("preparing crypt SSH key: %w", err)
	}
	if err := installAuthorizedKey(ctx, client, vm, user, pubKey); err != nil {
		return fmt.Errorf("authorizing SSH key in %s: %w", vm, err)
	}

	ip, err := client.IP(ctx, vm)
	if err != nil {
		return fmt.Errorf("resolving VM IP: %w", err)
	}

	if !suppressLifecycleLogs {
		fmt.Fprintf(os.Stderr, "crypt: connecting to %s@%s\n", user, ip)
	}
	if err := waitForSSH(ctx, keyPath, user, ip); err != nil {
		return err
	}

	args := append(sshOptions(keyPath), "-t", user+"@"+ip, remoteAgentCommand(guestDir, ag))
	cmd := exec.CommandContext(ctx, "ssh", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// ensureSSHKey returns the path to Crypt's dedicated SSH private key and the
// matching public key, generating the pair on first use.
func ensureSSHKey() (keyPath, pubKey string, err error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", "", err
	}
	dir := filepath.Join(base, "crypt")
	keyPath = filepath.Join(dir, "id_ed25519")
	pubPath := keyPath + ".pub"

	if _, statErr := os.Stat(keyPath); errors.Is(statErr, fs.ErrNotExist) {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", "", err
		}
		gen := exec.Command("ssh-keygen", "-t", "ed25519", "-N", "", "-C", "crypt", "-f", keyPath)
		if out, genErr := gen.CombinedOutput(); genErr != nil {
			return "", "", fmt.Errorf("ssh-keygen: %w (%s)", genErr, strings.TrimSpace(string(out)))
		}
	} else if statErr != nil {
		return "", "", statErr
	}

	pub, err := os.ReadFile(pubPath)
	if err != nil {
		return "", "", err
	}
	return keyPath, strings.TrimSpace(string(pub)), nil
}

// installAuthorizedKey appends Crypt's public key to the guest user's
// authorized_keys (idempotently) using anka run, which works before sshd is up.
func installAuthorizedKey(ctx context.Context, client *anka.Client, vm, user, pubKey string) error {
	home := "/Users/" + user
	sshDir := home + "/.ssh"
	authFile := sshDir + "/authorized_keys"
	quotedKey := shellQuote(pubKey)

	script := strings.Join([]string{
		"set -e",
		"mkdir -p " + sshDir,
		"chmod 700 " + sshDir,
		"touch " + authFile,
		"chmod 600 " + authFile,
		"grep -qxF " + quotedKey + " " + authFile + " || printf '%s\\n' " + quotedKey + " >> " + authFile,
		"chown -R " + user + " " + sshDir + " 2>/dev/null || true",
	}, "; ")

	cmd := client.RunCommand(ctx, vm, "", []string{"zsh", "-lc", script})
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		if detail := strings.TrimSpace(errBuf.String()); detail != "" {
			return fmt.Errorf("%w (%s)", err, detail)
		}
		return err
	}
	return nil
}

// waitForSSH blocks until an SSH connection to the guest succeeds or the
// readiness timeout elapses.
func waitForSSH(ctx context.Context, keyPath, user, ip string) error {
	deadline := time.Now().Add(sshReadyTimeout)
	probeArgs := append(sshOptions(keyPath), user+"@"+ip, "true")

	for {
		probe := exec.CommandContext(ctx, "ssh", probeArgs...)
		if err := probe.Run(); err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for SSH on %s; enable Remote Login (sshd) in the base VM", ip)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(sshProbeInterval):
		}
	}
}

// sshOptions are the connection flags shared by the readiness probe and the
// interactive session. The clone is ephemeral and its IP is reused across
// clones, so host-key checking is disabled deliberately.
func sshOptions(keyPath string) []string {
	return []string{
		"-i", keyPath,
		"-o", "IdentitiesOnly=yes",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "LogLevel=ERROR",
		"-o", "ConnectTimeout=5",
	}
}

// remoteAgentCommand builds the single command string SSH runs in the guest: a
// login shell (for the user's PATH) that cd's into the mounted directory and
// execs the agent. Interactive runs carry no user args.
func remoteAgentCommand(guestDir string, ag agent.Agent) string {
	argv := ag.Command(nil)
	quoted := make([]string, len(argv))
	for i, token := range argv {
		quoted[i] = shellQuote(token)
	}
	inner := guestEnvPrefix + "exec " + strings.Join(quoted, " ")
	if guestDir != "" {
		inner = "cd " + shellQuote(guestDir) + " && " + inner
	}
	return "zsh -lc " + shellQuote(inner)
}
