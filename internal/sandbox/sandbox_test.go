package sandbox

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"
)

func TestSanitize(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"myproject", "myproject"},
		{"my project", "my-project"},
		{"feature/branch", "feature-branch"},
		{"", "workspace"},
	}
	for _, tc := range cases {
		if got := sanitize(tc.in); got != tc.want {
			t.Errorf("sanitize(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestFirstFreeCloneName(t *testing.T) {
	if got := firstFreeCloneName(map[int]bool{}); got != "crypt-clone-1" {
		t.Fatalf("firstFreeCloneName(empty) = %q, want crypt-clone-1", got)
	}
	if got := firstFreeCloneName(map[int]bool{1: true, 2: true}); got != "crypt-clone-3" {
		t.Fatalf("firstFreeCloneName({1,2}) = %q, want crypt-clone-3", got)
	}
}

func TestResolveNamedClone(t *testing.T) {
	name, err := resolveNamedClone("crypt-base", "crypt-frontend")
	if err != nil {
		t.Fatalf("resolveNamedClone() error: %v", err)
	}
	if name != "crypt-frontend" {
		t.Fatalf("resolveNamedClone() = %q, want crypt-frontend", name)
	}

	if _, err := resolveNamedClone("crypt-base", "crypt-base"); err == nil {
		t.Fatal("expected error when --name matches base VM")
	}
}

func TestSessionRoundTrip(t *testing.T) {
	t.Setenv("CRYPT_SESSION_DIR", t.TempDir())
	dir := t.TempDir()
	if _, ok := loadSession(dir); ok {
		t.Fatal("expected no session before save")
	}
	if err := saveSession(dir, "crypt-clone-7"); err != nil {
		t.Fatalf("saveSession() error: %v", err)
	}
	name, ok := loadSession(dir)
	if !ok || name != "crypt-clone-7" {
		t.Fatalf("loadSession() = (%q, %v), want (crypt-clone-7, true)", name, ok)
	}
	clearSession(dir)
	if _, ok := loadSession(dir); ok {
		t.Fatal("expected session cleared")
	}
}

func TestResolveCloneNameExplicit(t *testing.T) {
	name, err := resolveCloneName(context.Background(), nil, Options{BaseVM: "crypt-base", Name: "crypt-frontend"}, t.TempDir())
	if err != nil {
		t.Fatalf("resolveCloneName() error: %v", err)
	}
	if name != "crypt-frontend" {
		t.Fatalf("resolveCloneName() = %q, want crypt-frontend", name)
	}
}

func TestVMAccessInfoLines(t *testing.T) {
	keyPath := "/Users/dev/Library/Application Support/crypt/keys/crypt-clone-1/id_ed25519"
	got := vmAccessInfoLines("anka", "192.168.64.4", keyPath)
	want := []string{
		"crypt: SSH: ssh -i '/Users/dev/Library/Application Support/crypt/keys/crypt-clone-1/id_ed25519' -o IdentitiesOnly=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR -o ConnectTimeout=5 anka@192.168.64.4",
		"crypt: VNC: open vnc://anka@192.168.64.4",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("vmAccessInfoLines() = %#v, want %#v", got, want)
	}
}

func TestWaitForVMIPRetriesUntilIPIsAvailable(t *testing.T) {
	attempts := 0
	ip, err := waitForVMIP(
		context.Background(),
		func(context.Context) (string, error) {
			attempts++
			if attempts < 3 {
				return "", fmt.Errorf("IP not ready")
			}
			return "192.168.64.4", nil
		},
		time.Second,
		0,
	)
	if err != nil {
		t.Fatalf("waitForVMIP() error: %v", err)
	}
	if ip != "192.168.64.4" {
		t.Fatalf("waitForVMIP() = %q, want 192.168.64.4", ip)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
}

func TestTeardownPlanKeepsVMRunningWhenNotDestroying(t *testing.T) {
	plan := teardownPlan(Options{}, "crypt-clone-1")
	if plan.delete {
		t.Fatal("teardownPlan() delete = true, want false")
	}
	if plan.message != "crypt: kept VM crypt-clone-1 running (crypt destroy when done)" {
		t.Fatalf("teardownPlan() message = %q", plan.message)
	}
}

func TestTeardownPlanDeletesWithoutStopping(t *testing.T) {
	plan := teardownPlan(Options{Destroy: true}, "crypt-clone-1")
	if !plan.delete {
		t.Fatal("teardownPlan() delete = false, want true")
	}
	if plan.message != "crypt: destroying crypt-clone-1" {
		t.Fatalf("teardownPlan() message = %q", plan.message)
	}
}

func TestShouldSuppressLifecycleLogsForReusedRunningClone(t *testing.T) {
	if !shouldSuppressLifecycleLogs(Options{}, true, true) {
		t.Fatal("shouldSuppressLifecycleLogs() = false, want true")
	}
}

func TestShouldNotSuppressLifecycleLogsForFreshStoppedOrDestroyRuns(t *testing.T) {
	cases := []struct {
		name        string
		opts        Options
		cloneExists bool
		running     bool
	}{
		{name: "fresh clone", cloneExists: false, running: false},
		{name: "existing stopped clone", cloneExists: true, running: false},
		{name: "destroy existing running clone", opts: Options{Destroy: true}, cloneExists: true, running: true},
	}
	for _, tc := range cases {
		if shouldSuppressLifecycleLogs(tc.opts, tc.cloneExists, tc.running) {
			t.Fatalf("%s: shouldSuppressLifecycleLogs() = true, want false", tc.name)
		}
	}
}

func TestShouldUnmountAfterRunForMountAddedToRunningKeptVM(t *testing.T) {
	if !shouldUnmountAfterRun(Options{Mount: true}, true) {
		t.Fatal("shouldUnmountAfterRun() = false, want true")
	}
}

func TestShouldNotUnmountAfterRunForFreshDestroyOrUnmountedRuns(t *testing.T) {
	cases := []struct {
		name              string
		opts              Options
		wasRunningAtMount bool
	}{
		{name: "no mount", opts: Options{}, wasRunningAtMount: true},
		{name: "fresh VM mount", opts: Options{Mount: true}, wasRunningAtMount: false},
		{name: "destroy", opts: Options{Mount: true, Destroy: true}, wasRunningAtMount: true},
	}
	for _, tc := range cases {
		if shouldUnmountAfterRun(tc.opts, tc.wasRunningAtMount) {
			t.Fatalf("%s: shouldUnmountAfterRun() = true, want false", tc.name)
		}
	}
}
