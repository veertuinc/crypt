package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/veertuinc/crypt/internal/anka"
)

// ensureClaudeWorkspaceTrust marks guest directories as trusted in
// ~/.claude.json so Claude Code skips the workspace trust dialog. Claude walks
// parent paths when checking trust, so trusting the home directory and Anka's
// shared-files root covers mounted project paths too.
func ensureClaudeWorkspaceTrust(ctx context.Context, conn sshConn, user, guestDir string) error {
	paths := claudeTrustPaths(user, guestDir)
	pathsJSON, err := json.Marshal(paths)
	if err != nil {
		return err
	}

	script := fmt.Sprintf(`python3 - <<'PY'
import json, os
paths = %s
config_path = os.path.expanduser("~/.claude.json")
data = {}
if os.path.isfile(config_path):
    with open(config_path) as f:
        data = json.load(f)
projects = data.setdefault("projects", {})
for path in paths:
    entry = projects.setdefault(path, {})
    entry["hasTrustDialogAccepted"] = True
    entry["hasTrustDialogHooksAccepted"] = True
os.makedirs(os.path.dirname(config_path), exist_ok=True)
with open(config_path, "w") as f:
    json.dump(data, f, indent=2)
    f.write("\n")
PY`, pathsJSON)

	return runSSHScript(ctx, conn, script)
}

func claudeTrustPaths(user, guestDir string) []string {
	home := filepath.Join("/Users", user)
	seen := map[string]bool{home: true, anka.SharedFilesRoot: true}
	paths := []string{home, anka.SharedFilesRoot}
	if guestDir != "" && !seen[guestDir] {
		paths = append(paths, guestDir)
	}
	return paths
}
