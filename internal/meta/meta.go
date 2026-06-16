// Package meta exposes build-time metadata for the crypt binary.
package meta

import (
	"runtime/debug"
	"strings"
)

const devVersion = "dev"

// These are overridden at release time via -ldflags, e.g.:
//
//	-X github.com/veertuinc/crypt/internal/meta.version=v1.2.3
//	-X github.com/veertuinc/crypt/internal/meta.version=dev
var (
	version = ""
	commit  = ""
	date    = ""
)

// Version returns the release tag when the binary was built from one, otherwise
// "dev". Use Commit() for the git revision on dev builds.
func Version() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if moduleVersion := info.Main.Version; moduleVersion != "" && moduleVersion != "(devel)" {
			return moduleVersion
		}
	}
	return devVersion
}

// Commit returns the git commit the binary was built from, if known.
func Commit() string {
	if commit != "" {
		return commit
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if rev := vcsRevision(info); rev != "" {
			return shortRevision(rev)
		}
	}
	return "unknown"
}

// Date returns the build date the binary was stamped with, if known.
func Date() string {
	if date != "" {
		return date
	}
	return "unknown"
}

func vcsRevision(info *debug.BuildInfo) string {
	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" {
			return setting.Value
		}
	}
	return ""
}

func shortRevision(rev string) string {
	rev = strings.TrimPrefix(rev, "v")
	if len(rev) > 7 {
		return rev[:7]
	}
	return rev
}
