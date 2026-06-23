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

type sshConn struct {
	keyPath string
	user    string
	ip      string
}

// sshUser is the account Crypt connects as, overridable via CRYPT_SSH_USER.
func sshUser() string {
	if user := strings.TrimSpace(os.Getenv("CRYPT_SSH_USER")); user != "" {
		return user
	}
	return defaultSSHUser
}

// prepareSSH waits for sshd, bootstraps Crypt's public key when needed, and
// returns a connection ready for guest command execution over SSH.
func prepareSSH(ctx context.Context, client *anka.Client, vm string, suppressLifecycleLogs bool) (sshConn, error) {
	user := sshUser()

	keyPath, pubKey, err := ensureSSHKey()
	if err != nil {
		return sshConn{}, fmt.Errorf("preparing crypt SSH key: %w", err)
	}

	ip, err := client.IP(ctx, vm)
	if err != nil {
		return sshConn{}, fmt.Errorf("resolving VM IP: %w", err)
	}

	conn := sshConn{keyPath: keyPath, user: user, ip: ip}

	if err := waitForSSH(ctx, keyPath, user, ip); err != nil {
		if bootstrapErr := bootstrapAuthorizedKey(ctx, client, vm, user, pubKey); bootstrapErr != nil {
			return sshConn{}, fmt.Errorf(
				"%w\n\ncrypt: add this public key to %q in crypt-base (~/.ssh/authorized_keys), stop crypt-base, and recreate clones:\n%s",
				err, user, pubKey,
			)
		}
		if err := waitForSSH(ctx, keyPath, user, ip); err != nil {
			return sshConn{}, fmt.Errorf("waiting for SSH after authorizing key: %w", err)
		}
	}

	if err := ensureAuthorizedKey(ctx, conn, pubKey); err != nil {
		return sshConn{}, fmt.Errorf("authorizing SSH key in %s: %w", vm, err)
	}

	if !suppressLifecycleLogs {
		fmt.Fprintf(os.Stderr, "crypt: connecting to %s@%s\n", user, ip)
	}

	return conn, nil
}

// runAgentSSH launches the agent inside the guest over SSH. Interactive runs
// allocate a TTY; task prompts run headlessly without forwarding host stdin.
func runAgentSSH(ctx context.Context, conn sshConn, guestDir string, ag agent.Agent, userArgs []string) error {
	argv := ag.Command(userArgs)
	return runSSH(ctx, conn, remoteCommand(guestDir, argv), len(userArgs) == 0)
}

func runSSH(ctx context.Context, conn sshConn, remoteCommand string, interactive bool) error {
	args := append(sshOptions(conn.keyPath), userAtHost(conn.user, conn.ip), remoteCommand)
	if interactive {
		args = insertSSHFlag(args, "-t")
	}

	cmd := exec.CommandContext(ctx, "ssh", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if interactive {
		cmd.Stdin = os.Stdin
	}
	return cmd.Run()
}

// runSSHScript runs a shell script in the guest and returns combined output.
func runSSHScript(ctx context.Context, conn sshConn, script string) error {
	var errBuf bytes.Buffer
	args := append(sshOptions(conn.keyPath), userAtHost(conn.user, conn.ip), "zsh -lc "+shellQuote(script))
	cmd := exec.CommandContext(ctx, "ssh", args...)
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		if detail := strings.TrimSpace(errBuf.String()); detail != "" {
			return fmt.Errorf("%w (%s)", err, detail)
		}
		return err
	}
	return nil
}

func userAtHost(user, ip string) string {
	return user + "@" + ip
}

func insertSSHFlag(args []string, flag string) []string {
	out := make([]string, 0, len(args)+1)
	out = append(out, args[0])
	out = append(out, flag)
	return append(out, args[1:]...)
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

// bootstrapAuthorizedKey seeds authorized_keys via anka cp when SSH is not yet
// available. Guest command execution always uses SSH; this file copy is only
// for the initial key install.
func bootstrapAuthorizedKey(ctx context.Context, client *anka.Client, vm, user, pubKey string) error {
	sshDir := filepath.Join("/Users", user, ".ssh")
	authFile := filepath.Join(sshDir, "authorized_keys")

	dir, err := os.MkdirTemp("", "crypt-authorized-keys-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)

	localAuth := filepath.Join(dir, "authorized_keys")
	existing := filepath.Join(dir, "existing")
	if err := client.CopyFromGuest(ctx, vm, authFile, existing); err == nil {
		content, readErr := os.ReadFile(existing)
		if readErr != nil {
			return readErr
		}
		if err := os.WriteFile(localAuth, content, 0o600); err != nil {
			return err
		}
	} else if !isMissingGuestPath(err) {
		return err
	} else if err := os.WriteFile(localAuth, nil, 0o600); err != nil {
		return err
	}

	merged, err := mergeAuthorizedKey(localAuth, pubKey)
	if err != nil {
		return err
	}
	if err := os.WriteFile(localAuth, []byte(merged), 0o600); err != nil {
		return err
	}

	if err := client.CopyToGuest(ctx, localAuth, vm, authFile); err != nil {
		return err
	}
	return nil
}

func isMissingGuestPath(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no such file") ||
		strings.Contains(msg, "not found") ||
		strings.Contains(msg, "does not exist")
}

func mergeAuthorizedKey(path, pubKey string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	lines := strings.Split(strings.TrimRight(string(content), "\n"), "\n")
	for _, line := range lines {
		if strings.TrimSpace(line) == pubKey {
			return strings.TrimRight(string(content), "\n") + "\n", nil
		}
	}
	if len(lines) == 1 && lines[0] == "" {
		return pubKey + "\n", nil
	}
	if len(content) > 0 && !strings.HasSuffix(string(content), "\n") {
		return string(content) + "\n" + pubKey + "\n", nil
	}
	return string(content) + pubKey + "\n", nil
}

// ensureAuthorizedKey appends Crypt's public key to authorized_keys over SSH.
func ensureAuthorizedKey(ctx context.Context, conn sshConn, pubKey string) error {
	home := "/Users/" + conn.user
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
		"chown -R " + conn.user + " " + sshDir + " 2>/dev/null || true",
	}, "; ")

	return runSSHScript(ctx, conn, script)
}

// waitForSSH blocks until an SSH connection to the guest succeeds or the
// readiness timeout elapses.
func waitForSSH(ctx context.Context, keyPath, user, ip string) error {
	deadline := time.Now().Add(sshReadyTimeout)
	probeArgs := append(sshOptions(keyPath), userAtHost(user, ip), "true")

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

// sshOptions are the connection flags shared by the readiness probe and guest
// sessions. The clone is ephemeral and its IP is reused across clones, so
// host-key checking is disabled deliberately.
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

// remoteCommand builds the single command string SSH runs in the guest: a
// login shell that cd's into the mounted directory and execs argv.
func remoteCommand(guestDir string, argv []string) string {
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
