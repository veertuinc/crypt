package anka

import (
	"context"
	"reflect"
	"testing"
)

func TestRunCommandWithWorkdir(t *testing.T) {
	c := &Client{binary: "anka"}

	cmd := c.RunCommand(context.Background(), "clone-1", "/work", []string{"zsh", "-lc", "echo hi"})

	want := []string{"anka", "run", "--workdir", "/work", "clone-1", "zsh", "-lc", "echo hi"}
	if !reflect.DeepEqual(cmd.Args, want) {
		t.Errorf("cmd.Args = %v, want %v", cmd.Args, want)
	}
}

func TestRunCommandWithoutWorkdir(t *testing.T) {
	c := &Client{binary: "anka"}

	cmd := c.RunCommand(context.Background(), "clone-1", "", []string{"bash"})

	want := []string{"anka", "run", "clone-1", "bash"}
	if !reflect.DeepEqual(cmd.Args, want) {
		t.Errorf("cmd.Args = %v, want %v", cmd.Args, want)
	}
}

func TestIPFallsBackToVNCHost(t *testing.T) {
	body := `{"name":"clone-1","status":"running","vnc_connection_string":"vnc://192.168.64.234:5900"}`
	c := fakeAnka(t, `echo '{"status":"OK","body":`+body+`}'`)

	ip, err := c.IP(context.Background(), "clone-1")
	if err != nil {
		t.Fatalf("IP() error: %v", err)
	}
	if ip != "192.168.64.234" {
		t.Errorf("IP() = %q, want 192.168.64.234", ip)
	}
}

func TestIPPrefersExplicitField(t *testing.T) {
	body := `{"name":"clone-1","status":"running","ip":"10.0.0.5","vnc_connection_string":"vnc://192.168.64.234:5900"}`
	c := fakeAnka(t, `echo '{"status":"OK","body":`+body+`}'`)

	ip, err := c.IP(context.Background(), "clone-1")
	if err != nil {
		t.Fatalf("IP() error: %v", err)
	}
	if ip != "10.0.0.5" {
		t.Errorf("IP() = %q, want 10.0.0.5", ip)
	}
}

func TestShowIPTrimsAnkaShowIPOutput(t *testing.T) {
	c := fakeAnka(t, `
if [ "$1" = show ] && [ "$2" = clone-1 ] && [ "$3" = ip ]; then
  printf '192.168.64.4\n'
  exit 0
fi
echo "unexpected args: $@" >&2
exit 1
`)

	ip, err := c.ShowIP(context.Background(), "clone-1")
	if err != nil {
		t.Fatalf("ShowIP() error: %v", err)
	}
	if ip != "192.168.64.4" {
		t.Errorf("ShowIP() = %q, want 192.168.64.4", ip)
	}
}

func TestShowParsesVNCConnectionString(t *testing.T) {
	body := `{"name":"crypt-base","status":"running","vnc_connection_string":"vnc://192.168.64.234:5900"}`
	c := fakeAnka(t, `echo '{"status":"OK","body":`+body+`}'`)

	info, err := c.Show(context.Background(), "crypt-base")
	if err != nil {
		t.Fatalf("Show() error: %v", err)
	}
	if info.VNCConnectionString != "vnc://192.168.64.234:5900" {
		t.Errorf("VNCConnectionString = %q, want vnc://192.168.64.234:5900", info.VNCConnectionString)
	}
}

func TestSetNetworkNoLocalInvokesAnka(t *testing.T) {
	c := fakeAnka(t, `
if [ "$1" = modify ] && [ "$2" = clone-1 ] && [ "$3" = network ] && [ "$4" = --no-local ]; then
  exit 0
fi
echo "unexpected args: $@" >&2
exit 1
`)

	if err := c.SetNetworkNoLocal(context.Background(), "clone-1"); err != nil {
		t.Fatalf("SetNetworkNoLocal() error: %v", err)
	}
}

func TestUnmountInvokesAnkaWithMountReference(t *testing.T) {
	c := fakeAnka(t, `
if [ "$1" = unmount ] && [ "$2" = clone-1 ] && [ "$3" = project ]; then
  exit 0
fi
echo "unexpected args: $@" >&2
exit 1
`)

	if err := c.Unmount(context.Background(), "clone-1", "project"); err != nil {
		t.Fatalf("Unmount() error: %v", err)
	}
}
