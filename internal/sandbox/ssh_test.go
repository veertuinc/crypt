package sandbox

import (
	"os"
	"strings"
	"testing"
)

func TestEnsureSSHKeyIsPerVM(t *testing.T) {
	t.Setenv("CRYPT_KEYS_DIR", t.TempDir())

	keyA, pubA, err := ensureSSHKey("crypt-clone-1")
	if err != nil {
		t.Fatalf("ensureSSHKey(clone-1) error: %v", err)
	}
	keyB, pubB, err := ensureSSHKey("crypt-clone-2")
	if err != nil {
		t.Fatalf("ensureSSHKey(clone-2) error: %v", err)
	}
	if keyA == keyB {
		t.Fatalf("ensureSSHKey() paths = %q, want distinct per VM", keyA)
	}
	if pubA == pubB {
		t.Fatal("ensureSSHKey() public keys match, want distinct per VM")
	}

	keyAAgain, pubAAgain, err := ensureSSHKey("crypt-clone-1")
	if err != nil {
		t.Fatalf("ensureSSHKey(clone-1 again) error: %v", err)
	}
	if keyA != keyAAgain || pubA != pubAAgain {
		t.Fatal("ensureSSHKey() should reuse the same key for the same VM")
	}
}

func TestRemoveSSHKey(t *testing.T) {
	t.Setenv("CRYPT_KEYS_DIR", t.TempDir())

	keyPath, _, err := ensureSSHKey("crypt-clone-9")
	if err != nil {
		t.Fatalf("ensureSSHKey() error: %v", err)
	}
	removeSSHKey("crypt-clone-9")
	if _, err := os.Stat(keyPath); !os.IsNotExist(err) {
		t.Fatalf("Stat(%q) after removeSSHKey() = %v, want not exist", keyPath, err)
	}
}

func TestIsSSHAuthFailure(t *testing.T) {
	if !isSSHAuthFailure("anka@192.168.64.4: Permission denied (publickey).") {
		t.Fatal("expected publickey auth failure to match")
	}
	if isSSHAuthFailure("ssh: connect to host 192.168.64.4 port 22: Connection refused") {
		t.Fatal("connection refused should not match auth failure")
	}
}

func TestAuthorizedKeyScriptCreatesAndAppends(t *testing.T) {
	key := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGcrypt crypt"
	script := authorizedKeyScript("anka", key)

	for _, want := range []string{
		"mkdir -p /Users/anka/.ssh",
		"touch /Users/anka/.ssh/authorized_keys",
		"grep -qxF",
		key,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("authorizedKeyScript() missing %q in %q", want, script)
		}
	}
}

func TestRemoteCommandChangesDirectory(t *testing.T) {
	got, err := remoteCommand("/Volumes/My Shared Files/crypt", nil, []string{"grok", "--always-approve"})
	if err != nil {
		t.Fatalf("remoteCommand() error: %v", err)
	}
	if !strings.Contains(got, "/Volumes/My Shared Files/crypt") {
		t.Fatalf("remoteCommand() = %q, want guestDir in command", got)
	}
	if !strings.Contains(got, "grok") || !strings.Contains(got, "--always-approve") {
		t.Fatalf("remoteCommand() = %q, want exec grok", got)
	}
	if !strings.Contains(got, "IS_SANDBOX=1") {
		t.Fatalf("remoteCommand() = %q, want IS_SANDBOX export", got)
	}
}

func TestGuestEnvExports(t *testing.T) {
	got, err := guestEnvExports([]string{"ATRIUM_SOCKET=/tmp/atrium.sock", "FOO=bar=baz"})
	if err != nil {
		t.Fatalf("guestEnvExports() error: %v", err)
	}
	for _, want := range []string{
		"export IS_SANDBOX=1",
		"export ATRIUM_SOCKET='/tmp/atrium.sock'",
		"export FOO='bar=baz'",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("guestEnvExports() = %q, want substring %q", got, want)
		}
	}
}

func TestGuestEnvExportsRejectsInvalid(t *testing.T) {
	cases := []string{"NOEQUALS", "1BAD=ok", "=empty", "BAD-KEY=ok"}
	for _, tc := range cases {
		if _, err := guestEnvExports([]string{tc}); err == nil {
			t.Fatalf("guestEnvExports(%q) error = nil, want error", tc)
		}
	}
}
