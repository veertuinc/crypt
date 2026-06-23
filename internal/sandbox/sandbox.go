// Package sandbox orchestrates a single agent run: it provisions a clone of a
// base VM, mounts the working directory, launches the agent, and deletes the
// clone only when explicitly requested. By default the clone is kept running;
// pass Destroy to delete it immediately after the run.
package sandbox

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/veertuinc/crypt/internal/agent"
	"github.com/veertuinc/crypt/internal/anka"
)

// minMajor / minMinor is the lowest Anka version that supports host directory
// mounts (see Anka 3.9.0 release notes).
const (
	minMajor = 3
	minMinor = 9
)

const (
	// teardownTimeout caps how long cleanup of the ephemeral VM may take.
	teardownTimeout = 2 * time.Minute
	// vmIPWaitTimeout gives Anka time to publish the guest IP after boot.
	vmIPWaitTimeout = 15 * time.Second
	// vmIPProbeInterval is the delay between `anka show <vm> ip` attempts.
	vmIPProbeInterval = 500 * time.Millisecond
)

// Options configures a sandbox run.
type Options struct {
	// BaseVM is the prepared template/VM that clones are made from.
	BaseVM string
	// CPU, when non-zero, overrides the clone's vCPU core count.
	CPU uint32
	// Memory, when non-zero, overrides the clone's RAM size in megabytes.
	Memory uint32
	// Mount shares the host working directory into the guest via anka mount.
	Mount bool
	// NoLocal blocks VM-to-VM and VM-to-host network on the clone (anka network --no-local).
	NoLocal bool
	// Destroy deletes the clone when the run ends instead of keeping it on disk.
	Destroy bool
	// Name pins the run to a specific clone VM. When set and the VM already
	// exists, it is reused instead of cloning again.
	Name string
}

// Run provisions a clone VM, runs the agent inside it, and stops it when done.
func Run(ctx context.Context, ag agent.Agent, userArgs []string, opts Options) error {
	client := anka.New()

	if !client.Installed() {
		return fmt.Errorf("the `anka` CLI was not found on your PATH; install Anka from https://veertu.com/download-anka-build/")
	}

	version, err := client.Version(ctx)
	if err != nil {
		return err
	}
	if opts.Mount && !version.AtLeast(minMajor, minMinor) {
		return fmt.Errorf("Anka %d.%d+ is required for host directory mounts (found %s)", minMajor, minMinor, version)
	}

	exists, err := client.Exists(ctx, opts.BaseVM)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("base VM %q not found; create and prepare it first, e.g. `anka create %s latest`", opts.BaseVM, opts.BaseVM)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("determining working directory: %w", err)
	}
	cwd, err = filepath.Abs(cwd)
	if err != nil {
		return fmt.Errorf("resolving working directory: %w", err)
	}
	dirName := filepath.Base(cwd)

	clone, err := resolveCloneName(ctx, client, opts, cwd)
	if err != nil {
		return err
	}

	cloneExists, err := client.Exists(ctx, clone)
	if err != nil {
		return err
	}

	freshClone := !cloneExists
	running := false
	if cloneExists {
		running, err = vmIsRunning(ctx, client, clone)
		if err != nil {
			return err
		}
	}
	suppressLifecycleLogs := shouldSuppressLifecycleLogs(opts, cloneExists, running)
	if opts.Name == "" && !suppressLifecycleLogs {
		if cloneExists {
			fmt.Fprintf(os.Stderr, "crypt: VM name is %s (reusing existing clone)\n", clone)
		} else {
			fmt.Fprintf(os.Stderr, "crypt: VM name is %s\n", clone)
		}
	} else if cloneExists && !suppressLifecycleLogs {
		fmt.Fprintf(os.Stderr, "crypt: using existing %s\n", clone)
	}
	if !cloneExists {
		fmt.Fprintf(os.Stderr, "crypt: cloning %s -> %s\n", opts.BaseVM, clone)
		if err := client.Clone(ctx, opts.BaseVM, clone); err != nil {
			return fmt.Errorf("cloning base VM: %w", err)
		}
	}

	// Guarantee teardown exactly once, even on interrupt or panic.
	var teardownOnce sync.Once
	teardown := func() {
		teardownOnce.Do(func() {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), teardownTimeout)
			defer cancel()

			plan := teardownPlan(opts, clone)
			if !suppressLifecycleLogs {
				fmt.Fprintln(os.Stderr, plan.message)
			}
			if plan.delete {
				if err := client.Delete(cleanupCtx, clone); err != nil {
					fmt.Fprintf(os.Stderr, "crypt: warning: failed to delete %s: %v\n", clone, err)
				} else {
					removeSSHKey(clone)
				}
				if opts.Name == "" {
					clearSession(cwd)
				}
			}
		})
	}
	defer teardown()

	if freshClone && opts.NoLocal {
		fmt.Fprintf(os.Stderr, "crypt: blocking VM-to-host network access (--no-local)\n")
		if err := client.SetNetworkNoLocal(ctx, clone); err != nil {
			return fmt.Errorf("blocking VM-to-host network access: %w", err)
		}
	}

	if freshClone && opts.CPU > 0 {
		if err := client.SetCPU(ctx, clone, opts.CPU); err != nil {
			return fmt.Errorf("setting CPU count: %w", err)
		}
	}
	if freshClone && opts.Memory > 0 {
		if err := client.SetRAM(ctx, clone, opts.Memory); err != nil {
			return fmt.Errorf("setting memory: %w", err)
		}
	}

	startedVM := false
	if !running {
		if !suppressLifecycleLogs {
			fmt.Fprintf(os.Stderr, "crypt: starting %s\n", clone)
		}
		if err := client.Start(ctx, clone); err != nil {
			return fmt.Errorf("starting VM: %w", err)
		}
		startedVM = true
	} else if !suppressLifecycleLogs {
		fmt.Fprintf(os.Stderr, "crypt: %s is already running\n", clone)
	}

	if startedVM {
		ip, err := waitForVMIP(ctx, func(ctx context.Context) (string, error) {
			return client.ShowIP(ctx, clone)
		}, vmIPWaitTimeout, vmIPProbeInterval)
		if err != nil {
			fmt.Fprintf(os.Stderr, "crypt: warning: could not read VM IP for %s: %v\n", clone, err)
		} else {
			keyPath, _, keyErr := ensureSSHKey(clone)
			if keyErr != nil {
				fmt.Fprintf(os.Stderr, "crypt: warning: could not resolve SSH key: %v\n", keyErr)
			}
			for _, line := range vmAccessInfoLines(sshUser(), ip, keyPath) {
				fmt.Fprintln(os.Stderr, line)
			}
		}
	}

	var guestDir string
	if opts.Mount {
		alreadyMounted, err := client.HasMount(ctx, clone, cwd)
		if err != nil {
			return fmt.Errorf("checking mounts: %w", err)
		}
		mountedThisRun := false
		if alreadyMounted {
			if !suppressLifecycleLogs {
				fmt.Fprintf(os.Stderr, "crypt: %s is already mounted\n", cwd)
			}
		} else {
			if !suppressLifecycleLogs {
				fmt.Fprintf(os.Stderr, "crypt: mounting %s\n", cwd)
			}
			if err := client.Mount(ctx, clone, cwd); err != nil {
				return fmt.Errorf("mounting working directory: %w", err)
			}
			mountedThisRun = true
		}
		if mountedThisRun && shouldUnmountAfterRun(opts, running) {
			defer func() {
				cleanupCtx, cancel := context.WithTimeout(context.Background(), teardownTimeout)
				defer cancel()
				if err := client.Unmount(cleanupCtx, clone, dirName); err != nil && !suppressLifecycleLogs {
					fmt.Fprintf(os.Stderr, "crypt: warning: failed to unmount %s from %s: %v\n", cwd, clone, err)
				}
			}()
		}
		guestDir = path.Join(anka.SharedFilesRoot, dirName)
	}

	conn, err := prepareSSH(ctx, client, clone, suppressLifecycleLogs)
	if err != nil {
		return err
	}

	if ag.Name == "claude" {
		if err := ensureClaudeWorkspaceTrust(ctx, conn, conn.user, guestDir); err != nil {
			return fmt.Errorf("preparing claude workspace trust: %w", err)
		}
	}

	if !suppressLifecycleLogs {
		if guestDir != "" {
			fmt.Fprintf(os.Stderr, "crypt: launching %s in %s\n", ag.Name, guestDir)
		} else {
			fmt.Fprintf(os.Stderr, "crypt: launching %s\n", ag.Name)
		}
	}

	runErr := runAgentSSH(ctx, conn, guestDir, ag, userArgs)
	if runErr != nil && !suppressLifecycleLogs {
		// A non-zero agent exit is surfaced but is not a Crypt failure.
		fmt.Fprintf(os.Stderr, "crypt: %s exited: %v\n", ag.Name, runErr)
	}

	return nil
}

// CloneName resolves the kept clone VM for the current working directory.
func CloneName(ctx context.Context, baseVM, name string) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("determining working directory: %w", err)
	}
	cwd, err = filepath.Abs(cwd)
	if err != nil {
		return "", fmt.Errorf("resolving working directory: %w", err)
	}
	if name != "" {
		return resolveNamedClone(baseVM, name)
	}
	if session, ok := loadSession(cwd); ok {
		client := anka.New()
		exists, err := client.Exists(ctx, session)
		if err != nil {
			return "", err
		}
		if exists {
			return session, nil
		}
		clearSession(cwd)
	}
	return "", fmt.Errorf("no kept crypt VM for this directory; use --name or run a task prompt here first")
}

// resolveCloneName picks the clone VM for this run.
func resolveCloneName(ctx context.Context, client *anka.Client, opts Options, cwd string) (string, error) {
	if opts.Name != "" {
		return resolveNamedClone(opts.BaseVM, opts.Name)
	}
	if session, ok := loadSession(cwd); ok {
		exists, err := client.Exists(ctx, session)
		if err != nil {
			return "", err
		}
		if exists {
			return session, nil
		}
		clearSession(cwd)
	}
	name, err := nextCloneName(ctx, client)
	if err != nil {
		return "", err
	}
	if err := saveSession(cwd, name); err != nil {
		return "", fmt.Errorf("recording clone session: %w", err)
	}
	return name, nil
}

func resolveNamedClone(baseVM, name string) (string, error) {
	name = sanitize(strings.TrimSpace(name))
	if name == "" {
		return "", fmt.Errorf("invalid --name %q", name)
	}
	if name == baseVM {
		return "", fmt.Errorf("--name %q cannot match the base VM %q", name, baseVM)
	}
	return name, nil
}

// nextCloneName returns the lowest unused crypt-clone-N name in the local VM library.
func nextCloneName(ctx context.Context, client *anka.Client) (string, error) {
	entries, err := client.List(ctx)
	if err != nil {
		return "", err
	}
	used := make(map[int]bool)
	for _, entry := range entries {
		var n int
		if _, err := fmt.Sscanf(entry.Name, cloneNamePrefix+"%d", &n); err == nil && n > 0 {
			used[n] = true
		}
	}
	return firstFreeCloneName(used), nil
}

func firstFreeCloneName(used map[int]bool) string {
	for n := 1; ; n++ {
		if !used[n] {
			return fmt.Sprintf("%s%d", cloneNamePrefix, n)
		}
	}
}

func vmIsRunning(ctx context.Context, client *anka.Client, vm string) (bool, error) {
	info, err := client.Show(ctx, vm)
	if err != nil {
		return false, err
	}
	switch strings.ToLower(info.Status) {
	case "running", "started", "booted":
		return true, nil
	default:
		return false, nil
	}
}

func vmAccessInfoLines(user, ip, keyPath string) []string {
	return []string{
		fmt.Sprintf("crypt: SSH: %s", sshAccessCommand(user, ip, keyPath)),
		fmt.Sprintf("crypt: VNC: open vnc://%s@%s", user, ip),
	}
}

type vmTeardownPlan struct {
	message string
	delete  bool
}

func teardownPlan(opts Options, clone string) vmTeardownPlan {
	if opts.Destroy {
		return vmTeardownPlan{
			message: fmt.Sprintf("crypt: destroying %s", clone),
			delete:  true,
		}
	}
	return vmTeardownPlan{
		message: fmt.Sprintf("crypt: kept VM %s running (crypt destroy when done)", clone),
	}
}

func shouldSuppressLifecycleLogs(opts Options, cloneExists, running bool) bool {
	return cloneExists && running && !opts.Destroy
}

func shouldUnmountAfterRun(opts Options, wasRunningAtMount bool) bool {
	return opts.Mount && wasRunningAtMount && !opts.Destroy
}

func waitForVMIP(ctx context.Context, lookup func(context.Context) (string, error), timeout, interval time.Duration) (string, error) {
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var lastErr error
	for {
		ip, err := lookup(waitCtx)
		if strings.TrimSpace(ip) != "" && err == nil {
			return strings.TrimSpace(ip), nil
		}
		if err != nil {
			lastErr = err
		} else {
			lastErr = fmt.Errorf("Anka has not reported an IP yet")
		}

		if interval <= 0 {
			select {
			case <-waitCtx.Done():
				return "", fmt.Errorf("waiting for VM IP: %w", lastErr)
			default:
			}
			continue
		}

		timer := time.NewTimer(interval)
		select {
		case <-waitCtx.Done():
			timer.Stop()
			return "", fmt.Errorf("waiting for VM IP: %w", lastErr)
		case <-timer.C:
		}
	}
}

var unsafeNameChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// sanitize reduces an arbitrary directory name to characters that are safe in
// an Anka VM name.
func sanitize(name string) string {
	cleaned := unsafeNameChars.ReplaceAllString(name, "-")
	cleaned = strings.Trim(cleaned, "-")
	if cleaned == "" {
		return "workspace"
	}
	return cleaned
}

// guestEnvPrefix exports environment variables that tell agents they are
// running inside Crypt's isolated VM, so safety prompts can be skipped.
const guestEnvPrefix = "export IS_SANDBOX=1; "

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
