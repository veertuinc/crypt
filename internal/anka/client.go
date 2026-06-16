// Package anka is a thin binding over the `anka` command-line tool. It shells
// out to the binary and parses Anka's machine-readable JSON envelope so the
// rest of Crypt can treat VM operations as ordinary Go calls.
package anka

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// defaultBinary is the executable looked up on PATH when none is provided.
const defaultBinary = "anka"

// Client invokes the Anka CLI.
type Client struct {
	binary string
}

// New returns a Client that drives the `anka` binary found on PATH.
func New() *Client {
	return &Client{binary: defaultBinary}
}

// envelope models Anka's `--machine-readable` response wrapper.
type envelope struct {
	Status        string          `json:"status"`
	Message       string          `json:"message"`
	ExceptionType string          `json:"exception_type"`
	Body          json.RawMessage `json:"body"`
}

// Installed reports whether the Anka CLI is available on PATH.
func (c *Client) Installed() bool {
	_, err := exec.LookPath(c.binary)
	return err == nil
}

// Version describes the installed Anka virtualization version.
type Version struct {
	Major int
	Minor int
	Patch int
	Raw   string
}

// AtLeast reports whether the version is >= major.minor.
func (v Version) AtLeast(major, minor int) bool {
	if v.Major != major {
		return v.Major > major
	}
	return v.Minor >= minor
}

func (v Version) String() string { return v.Raw }

var versionPattern = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`)

// Version parses the installed Anka version from `anka --version`.
func (c *Client) Version(ctx context.Context) (Version, error) {
	out, err := c.capture(ctx, "--version")
	if err != nil {
		return Version{}, err
	}
	raw := string(bytes.TrimSpace(out))
	match := versionPattern.FindStringSubmatch(raw)
	if match == nil {
		return Version{Raw: raw}, fmt.Errorf("could not parse Anka version from %q", raw)
	}
	major, _ := strconv.Atoi(match[1])
	minor, _ := strconv.Atoi(match[2])
	patch, _ := strconv.Atoi(match[3])
	return Version{Major: major, Minor: minor, Patch: patch, Raw: raw}, nil
}

// vmEntry is one row of `anka list` output.
type vmEntry struct {
	Name   string `json:"name"`
	UUID   string `json:"uuid"`
	Status string `json:"status"`
}

// List returns every VM in the local Anka library.
func (c *Client) List(ctx context.Context) ([]vmEntry, error) {
	body, err := c.machineReadable(ctx, "list")
	if err != nil {
		return nil, err
	}
	var entries []vmEntry
	if err := json.Unmarshal(body, &entries); err != nil {
		return nil, fmt.Errorf("parsing VM list: %w", err)
	}
	return entries, nil
}

// Exists reports whether a VM (template or clone) with the given name exists in
// the local library.
func (c *Client) Exists(ctx context.Context, name string) (bool, error) {
	entries, err := c.List(ctx)
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if entry.Name == name || entry.UUID == name {
			return true, nil
		}
	}
	return false, nil
}

// machineReadable runs an anka subcommand with `--machine-readable`, validates
// the envelope status, and returns the raw body.
func (c *Client) machineReadable(ctx context.Context, args ...string) (json.RawMessage, error) {
	out, err := c.capture(ctx, append([]string{"--machine-readable"}, args...)...)
	if err != nil {
		return nil, err
	}
	var env envelope
	if err := json.Unmarshal(out, &env); err != nil {
		return nil, fmt.Errorf("parsing anka response: %w", err)
	}
	if env.Status != "OK" {
		return nil, fmt.Errorf("anka %s: %s", strings.Join(args, " "), ankaError(env))
	}
	return env.Body, nil
}

// capture invokes the anka binary and returns its stdout. On failure it reports
// the exit error annotated with whatever the command wrote to stderr (falling
// back to stdout when stderr is empty). All other anka calls funnel through here.
func (c *Client) capture(ctx context.Context, args ...string) ([]byte, error) {
	proc := exec.CommandContext(ctx, c.binary, args...)
	var out, errOut bytes.Buffer
	proc.Stdout = &out
	proc.Stderr = &errOut

	if runErr := proc.Run(); runErr != nil {
		detail := bytes.TrimSpace(errOut.Bytes())
		if len(detail) == 0 {
			detail = bytes.TrimSpace(out.Bytes())
		}
		invocation := strings.Join(args, " ")
		if len(detail) == 0 {
			return nil, fmt.Errorf("anka %s: %w", invocation, runErr)
		}
		return nil, fmt.Errorf("anka %s: %w (%s)", invocation, runErr, detail)
	}
	return out.Bytes(), nil
}

// run executes a state-changing anka subcommand whose stdout is irrelevant.
func (c *Client) run(ctx context.Context, args ...string) error {
	_, err := c.capture(ctx, args...)
	return err
}

func ankaError(env envelope) string {
	if env.Message != "" {
		return env.Message
	}
	if env.ExceptionType != "" {
		return env.ExceptionType
	}
	return env.Status
}
