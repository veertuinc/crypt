package sandbox

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"
)

// socketSpec describes one host UNIX socket forwarded into the guest over SSH.
type socketSpec struct {
	// hostPath is the absolute path of the socket on the host that the
	// host-side ssh client connects to.
	hostPath string
	// guestPath is the absolute path of the listener sshd creates in the guest.
	guestPath string
	// envVar, when set, is exported in the guest as envVar=guestPath so the
	// agent can find the forwarded socket.
	envVar string
}

// resolveSocketSpecs parses every --socket value into a socketSpec. user is the
// guest account used to build the default guest path when one is not given.
func resolveSocketSpecs(specs []string, user string) ([]socketSpec, error) {
	if len(specs) == 0 {
		return nil, nil
	}
	resolved := make([]socketSpec, 0, len(specs))
	for _, raw := range specs {
		spec, err := parseSocketSpec(raw, user)
		if err != nil {
			return nil, err
		}
		resolved = append(resolved, spec)
	}
	return resolved, nil
}

// parseSocketSpec parses HOSTPATH[:GUESTPATH[:ENVVAR]] into a socketSpec.
func parseSocketSpec(raw, user string) (socketSpec, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return socketSpec{}, fmt.Errorf("--socket requires a host socket path")
	}
	if strings.HasPrefix(raw, "-") {
		return socketSpec{}, fmt.Errorf("--socket path %q looks like a flag; pass the socket explicitly (e.g. --socket ~/.atrium/ipc/stable.sock)", raw)
	}

	fields := strings.SplitN(raw, ":", 3)
	hostRaw := strings.TrimSpace(fields[0])
	if hostRaw == "" {
		return socketSpec{}, fmt.Errorf("--socket %q requires a host socket path", raw)
	}

	hostRaw, err := expandHome(hostRaw)
	if err != nil {
		return socketSpec{}, fmt.Errorf("resolving socket host path %q: %w", raw, err)
	}
	hostPath, err := filepath.Abs(hostRaw)
	if err != nil {
		return socketSpec{}, fmt.Errorf("resolving socket host path %q: %w", raw, err)
	}

	guestPath := ""
	if len(fields) >= 2 {
		guestPath = strings.TrimSpace(fields[1])
	}
	if guestPath == "" {
		guestPath = defaultGuestSocketPath(user, hostPath)
	}
	if !path.IsAbs(guestPath) {
		return socketSpec{}, fmt.Errorf("--socket %q: guest path %q must be absolute", raw, guestPath)
	}

	envVar := ""
	if len(fields) == 3 {
		envVar = strings.TrimSpace(fields[2])
		if envVar != "" && !guestEnvKeyPattern.MatchString(envVar) {
			return socketSpec{}, fmt.Errorf("--socket %q: env var %q must match [A-Za-z_][A-Za-z0-9_]*", raw, envVar)
		}
	}

	return socketSpec{hostPath: hostPath, guestPath: guestPath, envVar: envVar}, nil
}

// defaultGuestSocketPath places forwarded sockets under the guest user's home
// so the parent directory is always writable by sshd.
func defaultGuestSocketPath(user, hostPath string) string {
	return path.Join("/Users", user, ".crypt", "sockets", filepath.Base(hostPath))
}

// sshForwardArgs builds the `-R guestPath:hostPath` arguments that establish
// UNIX-domain remote forwarding for each socket.
func sshForwardArgs(specs []socketSpec) []string {
	if len(specs) == 0 {
		return nil
	}
	args := make([]string, 0, len(specs)*2)
	for _, spec := range specs {
		args = append(args, "-R", spec.guestPath+":"+spec.hostPath)
	}
	return args
}

// socketEnvExports returns ENVVAR=GUESTPATH pairs for specs that bind an env var,
// suitable for merging with the guest environment.
func socketEnvExports(specs []socketSpec) []string {
	var exports []string
	for _, spec := range specs {
		if spec.envVar != "" {
			exports = append(exports, spec.envVar+"="+spec.guestPath)
		}
	}
	return exports
}

// socketGuestPaths returns the unique guest socket paths that sshd will bind
// for remote forwarding.
func socketGuestPaths(specs []socketSpec) []string {
	seen := make(map[string]bool)
	var paths []string
	for _, spec := range specs {
		if spec.guestPath == "" || seen[spec.guestPath] {
			continue
		}
		seen[spec.guestPath] = true
		paths = append(paths, spec.guestPath)
	}
	return paths
}

// socketGuestDirs returns the unique parent directories that must exist in the
// guest before sshd binds the forwarded socket listeners.
func socketGuestDirs(specs []socketSpec) []string {
	seen := make(map[string]bool)
	var dirs []string
	for _, spec := range specs {
		dir := path.Dir(spec.guestPath)
		if dir == "" || dir == "." || dir == "/" || seen[dir] {
			continue
		}
		seen[dir] = true
		dirs = append(dirs, dir)
	}
	return dirs
}
