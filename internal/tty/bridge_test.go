package tty

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRunAttachedCompletesWithoutForwardingStdin(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test relies on a POSIX shell")
	}

	// An open pipe mimics a TTY waiting for input: a child that reads stdin
	// would block forever if RunAttached wired os.Stdin through.
	readEnd, writeEnd, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe() error: %v", err)
	}
	defer readEnd.Close()

	oldStdin := os.Stdin
	os.Stdin = readEnd
	t.Cleanup(func() {
		os.Stdin = oldStdin
		_ = writeEnd.Close()
	})

	done := make(chan struct{})
	var (
		output string
		runErr error
	)
	go func() {
		cmd := exec.Command("sh", "-c", "if read -r _; then echo read; else echo eof; fi")
		output, runErr = RunAttached(cmd)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunAttached blocked; task runs must not forward host stdin")
	}

	if runErr != nil {
		t.Fatalf("RunAttached() error: %v", runErr)
	}
	if !strings.Contains(output, "eof") {
		t.Fatalf("output = %q, want eof (child stdin should be closed immediately)", output)
	}
}

func TestRunAttachedCapturesOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test relies on a POSIX shell")
	}

	cmd := exec.Command("sh", "-c", "echo hello-task")
	output, err := RunAttached(cmd)
	if err != nil {
		t.Fatalf("RunAttached() error: %v", err)
	}
	if !strings.Contains(output, "hello-task") {
		t.Fatalf("output = %q, want hello-task", output)
	}
}
