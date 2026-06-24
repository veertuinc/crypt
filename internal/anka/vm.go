package anka

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"path/filepath"
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

// MountEntry is one row of `anka mount <vm>` output.
type MountEntry struct {
	FSID            int    `json:"fsid"`
	HostPath        string `json:"host_path"`
	GuestFolderName string `json:"guest_folder_name"`
	GuestPath       string `json:"guest_path"`
}

// ListMounts returns host directories currently shared with a running VM.
func (c *Client) ListMounts(ctx context.Context, vm string) ([]MountEntry, error) {
	body, err := c.machineReadable(ctx, "mount", vm)
	if err != nil {
		return nil, err
	}
	var entries []MountEntry
	if err := json.Unmarshal(body, &entries); err != nil {
		return nil, fmt.Errorf("parsing mount list: %w", err)
	}
	return entries, nil
}

// HasMount reports whether hostPath is already shared with the VM.
func (c *Client) HasMount(ctx context.Context, vm, hostPath string) (bool, error) {
	entries, err := c.ListMounts(ctx, vm)
	if err != nil {
		return false, err
	}
	hostPath = filepath.Clean(hostPath)
	for _, entry := range entries {
		if filepath.Clean(entry.HostPath) == hostPath {
			return true, nil
		}
	}
	return false, nil
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

// Run executes a command inside a running VM via `anka run`.
func (c *Client) Run(ctx context.Context, vm string, args ...string) error {
	return c.run(ctx, append([]string{"run", vm}, args...)...)
}

// IP returns the guest's IP address for SSH access.
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
