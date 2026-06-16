package sandbox

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const cloneNamePrefix = "crypt-clone-"

// loadSession returns the clone VM pinned to cwd by a prior run in this directory.
func loadSession(cwd string) (string, bool) {
	data, err := os.ReadFile(sessionFile(cwd))
	if err != nil {
		return "", false
	}
	name := strings.TrimSpace(string(data))
	if name == "" {
		return "", false
	}
	return name, true
}

func saveSession(cwd, name string) error {
	path := sessionFile(cwd)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(name+"\n"), 0o600)
}

func clearSession(cwd string) {
	_ = os.Remove(sessionFile(cwd))
}

func sessionFile(cwd string) string {
	sum := sha256.Sum256([]byte(cwd))
	return filepath.Join(sessionDir(), hex.EncodeToString(sum[:])+".name")
}

// ClearSessionForCWD removes the clone session for the current working directory.
func ClearSessionForCWD() error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("determining working directory: %w", err)
	}
	cwd, err = filepath.Abs(cwd)
	if err != nil {
		return fmt.Errorf("resolving working directory: %w", err)
	}
	clearSession(cwd)
	return nil
}

func sessionDir() string {
	if dir := os.Getenv("CRYPT_SESSION_DIR"); dir != "" {
		return dir
	}
	base, err := os.UserConfigDir()
	if err != nil {
		base = os.TempDir()
	}
	return filepath.Join(base, "crypt", "sessions")
}
