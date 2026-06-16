package anka

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeAnka writes an executable shell script standing in for the `anka` binary
// and returns a Client wired to it. The script body has access to the invocation
// arguments via "$@".
func fakeAnka(t *testing.T, scriptBody string) *Client {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake anka binary relies on a POSIX shell")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "anka")
	script := "#!/bin/sh\n" + scriptBody + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing fake anka: %v", err)
	}
	return &Client{binary: path}
}

func TestVersionParsing(t *testing.T) {
	c := fakeAnka(t, `echo "3.9.1 (build 12345)"`)

	v, err := c.Version(context.Background())
	if err != nil {
		t.Fatalf("Version() error: %v", err)
	}
	if v.Major != 3 || v.Minor != 9 || v.Patch != 1 {
		t.Errorf("parsed %d.%d.%d, want 3.9.1", v.Major, v.Minor, v.Patch)
	}
	if !strings.Contains(v.String(), "3.9.1") {
		t.Errorf("Raw = %q, want it to contain 3.9.1", v.String())
	}
}

func TestVersionUnparseable(t *testing.T) {
	c := fakeAnka(t, `echo "not a version"`)

	if _, err := c.Version(context.Background()); err == nil {
		t.Fatal("expected error parsing bogus version, got nil")
	}
}

func TestAtLeast(t *testing.T) {
	cases := []struct {
		v            Version
		major, minor int
		want         bool
	}{
		{Version{Major: 3, Minor: 9}, 3, 9, true},
		{Version{Major: 3, Minor: 10}, 3, 9, true},
		{Version{Major: 4, Minor: 0}, 3, 9, true},
		{Version{Major: 3, Minor: 8}, 3, 9, false},
		{Version{Major: 2, Minor: 99}, 3, 9, false},
	}
	for _, tc := range cases {
		if got := tc.v.AtLeast(tc.major, tc.minor); got != tc.want {
			t.Errorf("%d.%d AtLeast(%d,%d) = %v, want %v",
				tc.v.Major, tc.v.Minor, tc.major, tc.minor, got, tc.want)
		}
	}
}

func TestExistsFindsByNameAndUUID(t *testing.T) {
	body := `[{"name":"crypt-base","uuid":"abc-123","status":"stopped"}]`
	c := fakeAnka(t, `echo '{"status":"OK","body":`+body+`}'`)

	for _, name := range []string{"crypt-base", "abc-123"} {
		ok, err := c.Exists(context.Background(), name)
		if err != nil {
			t.Fatalf("Exists(%q) error: %v", name, err)
		}
		if !ok {
			t.Errorf("Exists(%q) = false, want true", name)
		}
	}

	ok, err := c.Exists(context.Background(), "missing")
	if err != nil {
		t.Fatalf("Exists(missing) error: %v", err)
	}
	if ok {
		t.Error("Exists(missing) = true, want false")
	}
}

func TestMachineReadableNonOKStatus(t *testing.T) {
	c := fakeAnka(t, `echo '{"status":"ERROR","message":"boom"}'`)

	_, err := c.Exists(context.Background(), "whatever")
	if err == nil {
		t.Fatal("expected error from ERROR status, got nil")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("error = %v, want it to mention the anka message", err)
	}
}

func TestCaptureSurfacesStderr(t *testing.T) {
	c := fakeAnka(t, `echo "detailed failure" >&2; exit 1`)

	_, err := c.capture(context.Background(), "list")
	if err == nil {
		t.Fatal("expected error from non-zero exit, got nil")
	}
	if !strings.Contains(err.Error(), "detailed failure") {
		t.Errorf("error = %v, want it to include stderr detail", err)
	}
}

func TestInstalled(t *testing.T) {
	c := fakeAnka(t, `exit 0`)
	if !c.Installed() {
		t.Error("Installed() = false for an existing binary, want true")
	}

	missing := &Client{binary: filepath.Join(t.TempDir(), "nope")}
	if missing.Installed() {
		t.Error("Installed() = true for a missing binary, want false")
	}
}

func TestAnkaError(t *testing.T) {
	cases := []struct {
		env  envelope
		want string
	}{
		{envelope{Message: "msg", ExceptionType: "Exc", Status: "ERROR"}, "msg"},
		{envelope{ExceptionType: "Exc", Status: "ERROR"}, "Exc"},
		{envelope{Status: "ERROR"}, "ERROR"},
	}
	for _, tc := range cases {
		if got := ankaError(tc.env); got != tc.want {
			t.Errorf("ankaError(%+v) = %q, want %q", tc.env, got, tc.want)
		}
	}
}
