package sandbox

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseSocketSpec(t *testing.T) {
	t.Setenv("HOME", "/Users/tester")

	cases := []struct {
		name      string
		raw       string
		user      string
		want      socketSpec
		wantError bool
	}{
		{
			name: "host only uses default guest path",
			raw:  "/tmp/atrium.sock",
			user: "anka",
			want: socketSpec{
				hostPath:  "/tmp/atrium.sock",
				guestPath: "/Users/anka/.crypt/sockets/atrium.sock",
			},
		},
		{
			name: "explicit guest path",
			raw:  "/tmp/atrium.sock:/Users/anka/.atrium/ipc/stable.sock",
			user: "anka",
			want: socketSpec{
				hostPath:  "/tmp/atrium.sock",
				guestPath: "/Users/anka/.atrium/ipc/stable.sock",
			},
		},
		{
			name: "guest path and env var",
			raw:  "/tmp/atrium.sock:/Users/anka/.atrium/ipc/stable.sock:ATRIUM_SOCKET",
			user: "anka",
			want: socketSpec{
				hostPath:  "/tmp/atrium.sock",
				guestPath: "/Users/anka/.atrium/ipc/stable.sock",
				envVar:    "ATRIUM_SOCKET",
			},
		},
		{
			name: "empty guest path with env var keeps default",
			raw:  "/tmp/atrium.sock::ATRIUM_SOCKET",
			user: "anka",
			want: socketSpec{
				hostPath:  "/tmp/atrium.sock",
				guestPath: "/Users/anka/.crypt/sockets/atrium.sock",
				envVar:    "ATRIUM_SOCKET",
			},
		},
		{
			name: "home expansion",
			raw:  "~/.atrium/ipc/stable.sock::SOCK",
			user: "anka",
			want: socketSpec{
				hostPath:  "/Users/tester/.atrium/ipc/stable.sock",
				guestPath: "/Users/anka/.crypt/sockets/stable.sock",
				envVar:    "SOCK",
			},
		},
		{name: "empty", raw: "", user: "anka", wantError: true},
		{name: "looks like flag", raw: "--foo", user: "anka", wantError: true},
		{name: "missing host path", raw: ":/guest.sock", user: "anka", wantError: true},
		{name: "relative guest path", raw: "/tmp/a.sock:guest.sock", user: "anka", wantError: true},
		{name: "bad env var", raw: "/tmp/a.sock:/g.sock:1BAD", user: "anka", wantError: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseSocketSpec(tc.raw, tc.user)
			if tc.wantError {
				if err == nil {
					t.Fatalf("parseSocketSpec(%q) expected error, got %+v", tc.raw, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseSocketSpec(%q) error: %v", tc.raw, err)
			}
			if got != tc.want {
				t.Fatalf("parseSocketSpec(%q) = %+v, want %+v", tc.raw, got, tc.want)
			}
		})
	}
}

func TestParseSocketSpecRelativeHostBecomesAbsolute(t *testing.T) {
	got, err := parseSocketSpec("rel.sock", "anka")
	if err != nil {
		t.Fatalf("parseSocketSpec error: %v", err)
	}
	if !filepath.IsAbs(got.hostPath) {
		t.Fatalf("host path %q is not absolute", got.hostPath)
	}
}

func TestSSHForwardArgs(t *testing.T) {
	specs := []socketSpec{
		{hostPath: "/tmp/a.sock", guestPath: "/Users/anka/a.sock"},
		{hostPath: "/tmp/b.sock", guestPath: "/Users/anka/b.sock", envVar: "B"},
	}
	got := sshForwardArgs(specs)
	want := []string{
		"-R", "/Users/anka/a.sock:/tmp/a.sock",
		"-R", "/Users/anka/b.sock:/tmp/b.sock",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sshForwardArgs() = %v, want %v", got, want)
	}
	if sshForwardArgs(nil) != nil {
		t.Fatal("sshForwardArgs(nil) should be nil")
	}
}

func TestSocketEnvExports(t *testing.T) {
	specs := []socketSpec{
		{hostPath: "/tmp/a.sock", guestPath: "/Users/anka/a.sock"},
		{hostPath: "/tmp/b.sock", guestPath: "/Users/anka/b.sock", envVar: "ATRIUM_SOCKET"},
	}
	got := socketEnvExports(specs)
	want := []string{"ATRIUM_SOCKET=/Users/anka/b.sock"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("socketEnvExports() = %v, want %v", got, want)
	}
}

func TestSocketGuestDirs(t *testing.T) {
	specs := []socketSpec{
		{guestPath: "/Users/anka/.atrium/ipc/stable.sock"},
		{guestPath: "/Users/anka/.atrium/ipc/other.sock"},
		{guestPath: "/Users/anka/.crypt/sockets/x.sock"},
	}
	got := socketGuestDirs(specs)
	want := []string{"/Users/anka/.atrium/ipc", "/Users/anka/.crypt/sockets"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("socketGuestDirs() = %v, want %v", got, want)
	}
}
