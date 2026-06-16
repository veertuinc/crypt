package anka

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
)

// SharedFilesRoot is where Anka exposes mounted host directories inside the
// guest. The path is fixed by macOS and cannot be changed.
const SharedFilesRoot = "/Volumes/My Shared Files"

// Clone creates a shadow clone of src named dst. Shadow clones share layers
// with the source and are created near-instantly.
func (c *Client) Clone(ctx context.Context, src, dst string) error {
	return c.run(ctx, "clone", src, dst)
}

// SetCPU sets the number of vCPU cores for a stopped VM.
func (c *Client) SetCPU(ctx context.Context, vm string, cores uint32) error {
	return c.run(ctx, "modify", vm, "cpu", strconv.FormatUint(uint64(cores), 10))
}

// SetRAM sets the RAM size (in megabytes) for a stopped VM.
func (c *Client) SetRAM(ctx context.Context, vm string, megabytes uint32) error {
	return c.run(ctx, "modify", vm, "ram", fmt.Sprintf("%dM", megabytes))
}

// SetNetworkNoLocal blocks VM-to-VM and VM-to-host network communication on the
// VM's network interface (anka modify <vm> network --no-local).
func (c *Client) SetNetworkNoLocal(ctx context.Context, vm string) error {
	return c.run(ctx, "modify", vm, "network", "--no-local")
}

// Start boots a VM, resuming it if suspended.
func (c *Client) Start(ctx context.Context, vm string) error {
	return c.run(ctx, "start", vm)
}

// VMInfo is the subset of `anka show` fields Crypt uses.
type VMInfo struct {
	Name                string `json:"name"`
	Status              string `json:"status"`
	IP                  string `json:"ip"`
	VNCConnectionString string `json:"vnc_connection_string"`
}

// Show returns metadata for a VM from `anka show`.
func (c *Client) Show(ctx context.Context, vm string) (VMInfo, error) {
	body, err := c.machineReadable(ctx, "show", vm)
	if err != nil {
		return VMInfo{}, err
	}
	var info VMInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return VMInfo{}, fmt.Errorf("parsing VM info: %w", err)
	}
	return info, nil
}

// ShowIP returns the guest IP reported by `anka show <vm> ip`.
func (c *Client) ShowIP(ctx context.Context, vm string) (string, error) {
	out, err := c.capture(ctx, "show", vm, "ip")
	if err != nil {
		return "", err
	}
	ip := strings.TrimSpace(string(out))
	if ip == "" {
		return "", fmt.Errorf("Anka has not reported an IP for %q yet; ensure the VM is running", vm)
	}
	return ip, nil
}

// Mount shares a host directory into a running VM. Inside the guest it appears
// under SharedFilesRoot, in a folder named after the host directory.
func (c *Client) Mount(ctx context.Context, vm, hostPath string) error {
	return c.run(ctx, "mount", vm, hostPath)
}

// Unmount removes a mounted directory from a running VM by mount reference
// (fsid, guest folder name, or host path).
func (c *Client) Unmount(ctx context.Context, vm, mountRef string) error {
	return c.run(ctx, "unmount", vm, mountRef)
}

// Stop forcefully shuts a VM down.
func (c *Client) Stop(ctx context.Context, vm string) error {
	return c.run(ctx, "stop", "--force", vm)
}

// Delete removes a VM (and its tags) without prompting for confirmation.
func (c *Client) Delete(ctx context.Context, vm string) error {
	return c.run(ctx, "delete", "--yes", vm)
}

// RunCommand builds (but does not start) the exec.Cmd that runs argv inside vm
// with the given working directory. The caller wires up stdio (typically via a
// PTY) and starts the command.
func (c *Client) RunCommand(ctx context.Context, vm, workdir string, argv []string) *exec.Cmd {
	args := []string{"run"}
	if workdir != "" {
		args = append(args, "--workdir", workdir)
	}
	args = append(args, vm)
	args = append(args, argv...)
	return exec.CommandContext(ctx, c.binary, args...)
}

// IP returns the guest's IP address for SSH access. Anka reports the clone's
// address on the shared network as part of its VNC connection string; we reuse
// that rather than requiring port forwarding.
func (c *Client) IP(ctx context.Context, vm string) (string, error) {
	info, err := c.Show(ctx, vm)
	if err != nil {
		return "", err
	}
	if info.IP != "" {
		return info.IP, nil
	}
	if info.VNCConnectionString != "" {
		hostPort := strings.TrimPrefix(info.VNCConnectionString, "vnc://")
		if host, _, splitErr := net.SplitHostPort(hostPort); splitErr == nil && host != "" {
			return host, nil
		}
	}
	return "", fmt.Errorf("Anka has not reported an IP for %q yet; ensure the VM is running", vm)
}
