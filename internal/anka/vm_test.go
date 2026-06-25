package anka

import (
	"context"
	"testing"
)

func TestRunInvokesAnka(t *testing.T) {
	c := fakeAnka(t, `
if [ "$1" = run ] && [ "$2" = clone-1 ] && [ "$3" = zsh ] && [ "$4" = -lc ]; then
  exit 0
fi
echo "unexpected args: $@" >&2
exit 1
`)

	if err := c.Run(context.Background(), "clone-1", "zsh", "-lc", "true"); err != nil {
		t.Fatalf("Run() error: %v", err)
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

func TestListMountsParsesMachineReadableOutput(t *testing.T) {
	body := `[{"fsid":1,"host_path":"/Users/dev/crypt","guest_folder_name":"crypt","guest_path":"/Volumes/My Shared Files/crypt"}]`
	c := fakeAnka(t, `
if [ "$1" = --machine-readable ] && [ "$2" = mount ] && [ "$3" = clone-1 ]; then
  echo '{"status":"OK","body":`+body+`}'
  exit 0
fi
echo "unexpected args: $@" >&2
exit 1
`)

	entries, err := c.ListMounts(context.Background(), "clone-1")
	if err != nil {
		t.Fatalf("ListMounts() error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("ListMounts() len = %d, want 1", len(entries))
	}
	if entries[0].HostPath != "/Users/dev/crypt" {
		t.Errorf("HostPath = %q, want /Users/dev/crypt", entries[0].HostPath)
	}
}

func TestHasMountMatchesHostPath(t *testing.T) {
	body := `[{"fsid":1,"host_path":"/Users/dev/crypt","guest_folder_name":"crypt","guest_path":"/Volumes/My Shared Files/crypt"}]`
	c := fakeAnka(t, `
if [ "$1" = --machine-readable ] && [ "$2" = mount ] && [ "$3" = clone-1 ]; then
  echo '{"status":"OK","body":`+body+`}'
  exit 0
fi
echo "unexpected args: $@" >&2
exit 1
`)

	ok, err := c.HasMount(context.Background(), "clone-1", "/Users/dev/crypt")
	if err != nil {
		t.Fatalf("HasMount(existing) error: %v", err)
	}
	if !ok {
		t.Fatal("HasMount(existing) = false, want true")
	}

	ok, err = c.HasMount(context.Background(), "clone-1", "/Users/dev/other")
	if err != nil {
		t.Fatalf("HasMount(missing) error: %v", err)
	}
	if ok {
		t.Fatal("HasMount(missing) = true, want false")
	}
}

func TestMountInvokesAnkaWithGuestFolderName(t *testing.T) {
	c := fakeAnka(t, `
if [ "$1" = mount ] && [ "$2" = clone-1 ] && [ "$3" = /Users/dev/.grok/skills:/Users/dev/.grok/skills ]; then
  exit 0
fi
echo "unexpected args: $@" >&2
exit 1
`)

	if err := c.Mount(context.Background(), "clone-1", "/Users/dev/.grok/skills:/Users/dev/.grok/skills"); err != nil {
		t.Fatalf("Mount() error: %v", err)
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
