package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMergeAuthorizedKeyAppendsNewKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "authorized_keys")
	if err := os.WriteFile(path, []byte("ssh-ed25519 existing crypt\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}

	merged, err := mergeAuthorizedKey(path, "ssh-ed25519 new crypt")
	if err != nil {
		t.Fatalf("mergeAuthorizedKey() error: %v", err)
	}
	if !strings.Contains(merged, "existing") || !strings.Contains(merged, "new crypt") {
		t.Fatalf("mergeAuthorizedKey() = %q", merged)
	}
}

func TestMergeAuthorizedKeyIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "authorized_keys")
	key := "ssh-ed25519 crypt"
	if err := os.WriteFile(path, []byte(key+"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}

	merged, err := mergeAuthorizedKey(path, key)
	if err != nil {
		t.Fatalf("mergeAuthorizedKey() error: %v", err)
	}
	if merged != key+"\n" {
		t.Fatalf("mergeAuthorizedKey() = %q, want %q", merged, key+"\n")
	}
}

func TestRemoteCommandChangesDirectory(t *testing.T) {
	got := remoteCommand("/Volumes/My Shared Files/crypt", []string{"grok", "--always-approve"})
	if !strings.Contains(got, "/Volumes/My Shared Files/crypt") {
		t.Fatalf("remoteCommand() = %q, want guestDir in command", got)
	}
	if !strings.Contains(got, "grok") || !strings.Contains(got, "--always-approve") {
		t.Fatalf("remoteCommand() = %q, want exec grok", got)
	}
}

func TestIsMissingGuestPath(t *testing.T) {
	if !isMissingGuestPath(fmt.Errorf("anka cp: no such file or directory")) {
		t.Fatal("expected missing guest path error to match")
	}
	if isMissingGuestPath(nil) {
		t.Fatal("isMissingGuestPath(nil) = true, want false")
	}
}
