package sandbox

import (
	"testing"
)

func TestInteractiveSSHArgsPlaceTTYFlagAfterOptions(t *testing.T) {
	keyPath := "/tmp/crypt-key"
	got := sshOptions(keyPath)
	got = append(got, "-t")
	got = append(got, "anka@192.168.64.1", "true")

	want := []string{
		"-i", keyPath,
		"-o", "IdentitiesOnly=yes",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "LogLevel=ERROR",
		"-o", "ConnectTimeout=5",
		"-t",
		"anka@192.168.64.1", "true",
	}
	if len(got) != len(want) {
		t.Fatalf("len(got)=%d want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("args[%d] = %q, want %q\nfull got: %#v", i, got[i], want[i], got)
		}
	}
}
