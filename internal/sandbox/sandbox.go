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
	// MountPaths lists host directories to share into the guest via anka mount.
	MountPaths []string
	// GuestEnv lists KEY=VALUE pairs exported in the guest before the agent starts.
	GuestEnv []string
	// Sockets lists host UNIX sockets to forward into the guest over SSH.
	// Each entry is HOSTPATH[:GUESTPATH[:ENVVAR]].
	Sockets []string
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
	if len(opts.MountPaths) > 0 && !version.AtLeast(minMajor, minMinor) {
		return fmt.Errorf("Anka %d.%d+ is required for host directory mounts (found %s)", minMajor, minMinor, version)
	}
	if _, err := guestEnvExports(opts.GuestEnv); err != nil {
		return err
	}
	socketSpecs, err := resolveSocketSpecs(opts.Sockets, sshUser())
	if err != nil {
		return err
	}

	exists, err := client.Exists(ctx, opts.BaseVM)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("base VM %q not found; pull or create and prepare it first, e.g. `anka pull ... veertu/getting-started-templates` then `anka clone getting-started-templates %s` or create a new VM with `anka create %s latest`", opts.BaseVM, opts.BaseVM, opts.BaseVM)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("determining working directory: %w", err)
	}
	cwd, err = filepath.Abs(cwd)
	if err != nil {
		return fmt.Errorf("resolving working directory: %w", err)
	}
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
		fmt.Fprintf(os.Stderr, "crypt: blocking VM-to-host and VM-to-VM network access (--no-local)\n")
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

	mountSpecs, err := resolveMountSpecs(opts.MountPaths)
	if err != nil {
		return err
	}

	var guestDir string
	if len(mountSpecs) > 0 {
		var mountedThisRun []mountSpec
		for _, spec := range mountSpecs {
			alreadyMounted, err := client.HasMount(ctx, clone, spec.hostPath)
			if err != nil {
				return fmt.Errorf("checking mounts: %w", err)
			}
			if alreadyMounted {
				if !suppressLifecycleLogs {
					fmt.Fprintf(os.Stderr, "crypt: %s is already mounted\n", spec.hostPath)
				}
				continue
			}
			if !suppressLifecycleLogs {
				if spec.guestFolderName != "" {
					fmt.Fprintf(os.Stderr, "crypt: mounting %s at %s\n", spec.hostPath, spec.guestWorkDir())
				} else {
					fmt.Fprintf(os.Stderr, "crypt: mounting %s\n", spec.hostPath)
				}
			}
			if err := client.Mount(ctx, clone, spec.ankaArg()); err != nil {
				return fmt.Errorf("mounting %s: %w", spec.hostPath, err)
			}
			mountedThisRun = append(mountedThisRun, spec)
		}
		if len(mountedThisRun) > 0 && shouldUnmountAfterRun(opts, running) {
			defer func() {
				cleanupCtx, cancel := context.WithTimeout(context.Background(), teardownTimeout)
				defer cancel()
				for _, spec := range mountedThisRun {
					if err := client.Unmount(cleanupCtx, clone, spec.unmountRef()); err != nil && !suppressLifecycleLogs {
						fmt.Fprintf(os.Stderr, "crypt: warning: failed to unmount %s from %s: %v\n", spec.hostPath, clone, err)
					}
				}
			}()
		}
		guestDir = mountSpecs[0].guestWorkDir()
	}

	warnIfIPFilterBlocksSSH(ctx, client, clone, suppressLifecycleLogs)

	conn, err := prepareSSH(ctx, client, clone, suppressLifecycleLogs)
	if err != nil {
		return err
	}

	if len(socketSpecs) > 0 {
		if err := ensureGuestSocketDirs(ctx, conn, socketSpecs); err != nil {
			return fmt.Errorf("preparing guest socket directories: %w", err)
		}
		if !suppressLifecycleLogs {
			for _, line := range socketForwardInfoLines(socketSpecs) {
				fmt.Fprintln(os.Stderr, line)
			}
		}
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

	runErr := runAgentSSH(ctx, conn, guestDir, opts.GuestEnv, socketSpecs, ag, userArgs)
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

// socketForwardInfoLines describes each forwarded socket for the run log,
// including the bound env var when one is set.
func socketForwardInfoLines(specs []socketSpec) []string {
	lines := make([]string, 0, len(specs))
	for _, spec := range specs {
		line := fmt.Sprintf("crypt: forwarding host socket %s -> guest %s", spec.hostPath, spec.guestPath)
		if spec.envVar != "" {
			line += fmt.Sprintf(" (%s)", spec.envVar)
		}
		lines = append(lines, line)
	}
	return lines
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
	return len(opts.MountPaths) > 0 && wasRunningAtMount && !opts.Destroy
}

func resolveMountSpecs(paths []string) ([]mountSpec, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	specs := make([]mountSpec, 0, len(paths))
	for _, rawPath := range paths {
		spec, err := parseMountSpec(rawPath)
		if err != nil {
			return nil, err
		}
		specs = append(specs, spec)
	}
	return specs, nil
}

type mountSpec struct {
	hostPath        string
	guestFolderName string
}

func parseMountSpec(rawPath string) (mountSpec, error) {
	rawPath = strings.TrimSpace(rawPath)
	if rawPath == "" {
		return mountSpec{}, fmt.Errorf("--mount requires a host path (pass . for the current directory)")
	}
	if strings.HasPrefix(rawPath, "-") {
		return mountSpec{}, fmt.Errorf("--mount path %q looks like a flag; pass the directory explicitly (e.g. --mount .)", rawPath)
	}

	hostRaw, guestFolderName, hasGuest := strings.Cut(rawPath, ":")
	if hasGuest {
		guestFolderName = strings.TrimSpace(guestFolderName)
		if guestFolderName == "" {
			return mountSpec{}, fmt.Errorf("--mount guest folder name cannot be empty in %q", rawPath)
		}
	}

	hostRaw, err := expandHome(strings.TrimSpace(hostRaw))
	if err != nil {
		return mountSpec{}, fmt.Errorf("resolving mount host path %q: %w", rawPath, err)
	}
	hostPath, err := filepath.Abs(hostRaw)
	if err != nil {
		return mountSpec{}, fmt.Errorf("resolving mount host path %q: %w", rawPath, err)
	}

	if hasGuest {
		if err := validateGuestFolderName(guestFolderName); err != nil {
			return mountSpec{}, fmt.Errorf("--mount %q: %w", rawPath, err)
		}
	}

	return mountSpec{
		hostPath:        hostPath,
		guestFolderName: guestFolderName,
	}, nil
}

func (spec mountSpec) ankaArg() string {
	if spec.guestFolderName == "" {
		return spec.hostPath
	}
	return spec.hostPath + ":" + spec.guestFolderName
}

func (spec mountSpec) guestWorkDir() string {
	folderName := spec.guestFolderName
	if folderName == "" {
		folderName = filepath.Base(spec.hostPath)
	}
	return path.Join(anka.SharedFilesRoot, folderName)
}

func (spec mountSpec) unmountRef() string {
	if spec.guestFolderName != "" {
		return spec.guestFolderName
	}
	return filepath.Base(spec.hostPath)
}

func validateGuestFolderName(name string) error {
	if name == "" {
		return fmt.Errorf("guest folder name cannot be empty")
	}
	if filepath.IsAbs(name) || strings.Contains(name, string(filepath.Separator)) {
		return fmt.Errorf("guest folder name %q must be a single folder name under %s, not a path", name, anka.SharedFilesRoot)
	}
	if name == "." || name == ".." {
		return fmt.Errorf("guest folder name %q is not allowed", name)
	}
	return nil
}

func expandHome(path string) (string, error) {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if path == "~" {
			return home, nil
		}
		return filepath.Join(home, path[2:]), nil
	}
	return path, nil
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

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
