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

// errSSHAuthFailed means sshd accepted a connection but rejected Crypt's public key.
var errSSHAuthFailed = errors.New("ssh public key not authorized")

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

	keyPath, pubKey, err := ensureSSHKey(vm)
	if err != nil {
		return sshConn{}, fmt.Errorf("preparing crypt SSH key: %w", err)
	}

	ip, err := client.IP(ctx, vm)
	if err != nil {
		return sshConn{}, fmt.Errorf("resolving VM IP: %w", err)
	}

	conn := sshConn{keyPath: keyPath, user: user, ip: ip}

	logSSHProgress(suppressLifecycleLogs, "authorizing SSH key in %s via anka run", vm)
	if err := bootstrapAuthorizedKey(ctx, client, vm, user, pubKey); err != nil {
		return sshConn{}, fmt.Errorf("authorizing SSH key in %s via anka run: %w", vm, err)
	}

	if err := waitForSSH(ctx, keyPath, user, ip, suppressLifecycleLogs, fmt.Sprintf("waiting for SSH on %s", userAtHost(user, ip))); err != nil {
		return sshConn{}, err
	}

	if err := ensureAuthorizedKey(ctx, conn, pubKey); err != nil {
		return sshConn{}, fmt.Errorf("authorizing SSH key in %s: %w", vm, err)
	}

	return conn, nil
}

func logSSHProgress(suppress bool, format string, args ...any) {
	if suppress {
		return
	}
	fmt.Fprintf(os.Stderr, "crypt: "+format+"\n", args...)
}

// runAgentSSH launches the agent inside the guest over SSH. Interactive runs
// allocate a TTY; task prompts run headlessly without forwarding host stdin.
func runAgentSSH(ctx context.Context, conn sshConn, guestDir string, ag agent.Agent, userArgs []string) error {
	argv := ag.Command(userArgs)
	return runSSH(ctx, conn, remoteCommand(guestDir, argv), !agent.HasTaskPrompt(userArgs))
}

func runSSH(ctx context.Context, conn sshConn, remoteCommand string, interactive bool) error {
	args := sshOptions(conn.keyPath)
	if interactive {
		args = append(args, "-t")
	}
	args = append(args, userAtHost(conn.user, conn.ip), remoteCommand)

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

// ensureSSHKey returns the SSH key pair dedicated to vm, generating it on first use.
func ensureSSHKey(vm string) (keyPath, pubKey string, err error) {
	dir, err := sshKeyDir(vm)
	if err != nil {
		return "", "", err
	}
	keyPath = filepath.Join(dir, "id_ed25519")
	pubPath := keyPath + ".pub"

	if _, statErr := os.Stat(keyPath); errors.Is(statErr, fs.ErrNotExist) {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", "", err
		}
		gen := exec.Command("ssh-keygen", "-t", "ed25519", "-N", "", "-C", "crypt-"+vm, "-f", keyPath)
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

func sshKeyDir(vm string) (string, error) {
	root, err := sshKeysRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, vm), nil
}

func sshKeysRoot() (string, error) {
	if dir := os.Getenv("CRYPT_KEYS_DIR"); dir != "" {
		return dir, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "crypt", "keys"), nil
}

// removeSSHKey deletes the SSH key pair stored for vm.
func removeSSHKey(vm string) {
	dir, err := sshKeyDir(vm)
	if err != nil {
		return
	}
	_ = os.RemoveAll(dir)
}

// bootstrapAuthorizedKey appends Crypt's public key to authorized_keys via
// anka run when key-based SSH is not yet available.
func bootstrapAuthorizedKey(ctx context.Context, client *anka.Client, vm, user, pubKey string) error {
	return client.Run(ctx, vm, "zsh", "-lc", authorizedKeyScript(user, pubKey))
}

// ensureAuthorizedKey appends Crypt's public key to authorized_keys over SSH.
func ensureAuthorizedKey(ctx context.Context, conn sshConn, pubKey string) error {
	return runSSHScript(ctx, conn, authorizedKeyScript(conn.user, pubKey))
}

func authorizedKeyScript(user, pubKey string) string {
	home := "/Users/" + user
	sshDir := home + "/.ssh"
	authFile := sshDir + "/authorized_keys"
	quotedKey := shellQuote(pubKey)

	return strings.Join([]string{
		"set -e",
		"mkdir -p " + sshDir,
		"chmod 700 " + sshDir,
		"touch " + authFile,
		"chmod 600 " + authFile,
		"grep -qxF " + quotedKey + " " + authFile + " || printf '%s\\n' " + quotedKey + " >> " + authFile,
		"chown -R " + user + " " + sshDir + " 2>/dev/null || true",
	}, "; ")
}

// waitForSSH blocks until an SSH connection to the guest succeeds or the
// readiness timeout elapses.
func waitForSSH(ctx context.Context, keyPath, user, ip string, suppress bool, waitingMsg string) error {
	deadline := time.Now().Add(sshReadyTimeout)
	logSSHProgress(suppress, "%s", waitingMsg)
	lastProgress := time.Now()

	for {
		probeArgs := append(sshOptions(keyPath),
			"-o", "BatchMode=yes",
			userAtHost(user, ip), "true",
		)
		probe := exec.CommandContext(ctx, "ssh", probeArgs...)
		var errBuf bytes.Buffer
		probe.Stderr = &errBuf
		if err := probe.Run(); err == nil {
			return nil
		}
		if isSSHAuthFailure(errBuf.String()) {
			return errSSHAuthFailed
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for SSH on %s; enable Remote Login (sshd) in the base VM", ip)
		}
		if !suppress && time.Since(lastProgress) >= 15*time.Second {
			logSSHProgress(suppress, "still %s", waitingMsg)
			lastProgress = time.Now()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(sshProbeInterval):
		}
	}
}

func isSSHAuthFailure(stderr string) bool {
	msg := strings.ToLower(stderr)
	return strings.Contains(msg, "permission denied") ||
		strings.Contains(msg, "publickey")
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

// sshAccessCommand is a copy-pasteable ssh invocation using Crypt's dedicated key.
func sshAccessCommand(user, ip, keyPath string) string {
	if keyPath == "" {
		return "ssh " + userAtHost(user, ip)
	}
	return strings.Join([]string{
		"ssh",
		"-i", shellQuote(keyPath),
		"-o", "IdentitiesOnly=yes",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "LogLevel=ERROR",
		"-o", "ConnectTimeout=5",
		userAtHost(user, ip),
	}, " ")
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
