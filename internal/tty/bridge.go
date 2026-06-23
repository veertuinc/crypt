// Package tty bridges a child process to the user's terminal through a pseudo
// terminal so full-screen agent TUIs behave correctly.
package tty

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/creack/pty"
	"golang.org/x/term"
)

// Run starts cmd attached to a PTY, mirrors the controlling terminal's size,
// puts the local stdin into raw mode, and proxies I/O until the command exits.
// It blocks until cmd completes and returns the command's error (if any).
func Run(ctx context.Context, cmd *exec.Cmd) error {
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return err
	}
	defer func() { _ = ptmx.Close() }()

	// Keep the PTY window size in sync with the real terminal.
	resizeCh := make(chan os.Signal, 1)
	signal.Notify(resizeCh, syscall.SIGWINCH)
	defer signal.Stop(resizeCh)
	go func() {
		for range resizeCh {
			_ = pty.InheritSize(os.Stdin, ptmx)
		}
	}()
	resizeCh <- syscall.SIGWINCH // trigger an initial resize

	// Put stdin into raw mode so keystrokes pass straight through to the agent.
	if term.IsTerminal(int(os.Stdin.Fd())) {
		oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
		if err == nil {
			defer func() { _ = term.Restore(int(os.Stdin.Fd()), oldState) }()
		}
	}

	// Proxy stdin into the PTY; copy PTY output to stdout.
	go func() { _, _ = io.Copy(ptmx, os.Stdin) }()
	copyDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(os.Stdout, ptmx)
		close(copyDone)
	}()

	// If the context is cancelled (e.g. SIGINT), tear the child down.
	go func() {
		<-ctx.Done()
		if cmd.Process != nil {
			_ = cmd.Process.Signal(syscall.SIGTERM)
		}
	}()

	waitErr := cmd.Wait()
	<-copyDone
	return waitErr
}

// RunAttached runs cmd with stdout and stderr connected to the calling process.
// Stdin is not forwarded: task-mode agents (e.g. grok -p) treat an open TTY as
// interactive and block after printing the response until stdin closes.
// It returns combined guest stdout/stderr captured while forwarding.
func RunAttached(cmd *exec.Cmd) (string, error) {
	var buf bytes.Buffer
	cmd.Stdout = io.MultiWriter(os.Stdout, &buf)
	cmd.Stderr = io.MultiWriter(os.Stderr, &buf)
	err := cmd.Run()
	return buf.String(), err
}
