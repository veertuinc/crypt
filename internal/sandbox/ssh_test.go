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
	got, err := remoteCommand("/Volumes/My Shared Files/crypt", nil, []string{"grok", "--always-approve"}, "", "")
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

func TestRemoteCommandUnlocksKeychainInSameSession(t *testing.T) {
	got, err := remoteCommand("/work", []string{"FOO=bar"}, []string{"cursor-agent", "worker", "start"}, "login", "admin")
	if err != nil {
		t.Fatalf("remoteCommand() error: %v", err)
	}
	for _, want := range []string{
		"security unlock-keychain",
		"login.keychain-db",
		" && ",
		"exec",
		"cursor-agent",
		"export FOO=",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("remoteCommand() = %q, want substring %q", got, want)
		}
	}
	// Unlock must come before exec, and every step must use && so a failed
	// unlock cannot fall through a ";" to still run the agent.
	unlockAt := strings.Index(got, "security unlock-keychain")
	execAt := strings.Index(got, "exec")
	if unlockAt < 0 || execAt < 0 || unlockAt > execAt {
		t.Fatalf("remoteCommand() = %q, want unlock before exec", got)
	}
	between := got[unlockAt:execAt]
	if strings.Contains(between, "; ") {
		t.Fatalf("remoteCommand() = %q, unlock..exec must be &&-chained (no ;)", got)
	}
}

func TestParseUnlockKeychain(t *testing.T) {
	tests := []struct {
		raw      string
		wantName string
		wantPass string
		wantErr  bool
	}{
		{raw: "login=admin", wantName: "login", wantPass: "admin"},
		{raw: "login=p=ass=word", wantName: "login", wantPass: "p=ass=word"},
		{raw: "login.keychain-db=secret", wantName: "login.keychain-db", wantPass: "secret"},
		{raw: "~/Library/Keychains/login.keychain-db=secret", wantName: "~/Library/Keychains/login.keychain-db", wantPass: "secret"},
		{raw: "/Users/anka/Library/Keychains/login.keychain-db=secret", wantName: "/Users/anka/Library/Keychains/login.keychain-db", wantPass: "secret"},
		{raw: "  login=admin  ", wantName: "login", wantPass: "admin"},
		{raw: "", wantName: "", wantPass: ""},
		{raw: "admin", wantErr: true},
		{raw: "=admin", wantErr: true},
		{raw: "login=", wantErr: true},
		{raw: "-login=admin", wantErr: true},
	}
	for _, tc := range tests {
		name, pass, err := parseUnlockKeychain(tc.raw)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("parseUnlockKeychain(%q) error = nil, want error", tc.raw)
			}
			continue
		}
		if err != nil || name != tc.wantName || pass != tc.wantPass {
			t.Fatalf("parseUnlockKeychain(%q) = %q, %q, %v; want %q, %q, nil",
				tc.raw, name, pass, err, tc.wantName, tc.wantPass)
		}
	}
}

func TestUnlockKeychainScript(t *testing.T) {
	got := unlockKeychainScript("login", `p'ass`)
	if !strings.Contains(got, `login.keychain-db`) {
		t.Fatalf("unlockKeychainScript() = %q, want login.keychain-db", got)
	}
	if !strings.Contains(got, "name="+shellQuote("login")) {
		t.Fatalf("unlockKeychainScript() = %q, want quoted name", got)
	}
	if !strings.Contains(got, "pass="+shellQuote(`p'ass`)) {
		t.Fatalf("unlockKeychainScript() = %q, want quoted password", got)
	}

	got = unlockKeychainScript("~/custom.keychain-db", "secret")
	if !strings.Contains(got, `~*)`) {
		t.Fatalf("unlockKeychainScript(~ path) = %q, want ~ branch", got)
	}

	got = unlockKeychainScript("/tmp/custom.keychain-db", "secret")
	if !strings.Contains(got, `/*)`) {
		t.Fatalf("unlockKeychainScript(abs path) = %q, want /* branch", got)
	}
}

func TestRemoteCommandOmitsUnlockWhenEmpty(t *testing.T) {
	got, err := remoteCommand("", nil, []string{"/bin/echo", "hi"}, "", "")
	if err != nil {
		t.Fatalf("remoteCommand() error: %v", err)
	}
	if strings.Contains(got, "unlock-keychain") {
		t.Fatalf("remoteCommand() = %q, did not want unlock-keychain", got)
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
