package meta

import "testing"

func TestVersionFallback(t *testing.T) {
	got := Version()
	if got == "" {
		t.Error("Version() returned empty string")
	}
}

func TestVersionPrefersLdflag(t *testing.T) {
	original := version
	t.Cleanup(func() { version = original })

	version = "v1.2.3"
	if got := Version(); got != "v1.2.3" {
		t.Errorf("Version() = %q, want %q", got, "v1.2.3")
	}
}

func TestVersionDevLdflag(t *testing.T) {
	original := version
	t.Cleanup(func() { version = original })

	version = devVersion
	if got := Version(); got != devVersion {
		t.Errorf("Version() = %q, want %q", got, devVersion)
	}
}

func TestVersionDevWhenUnset(t *testing.T) {
	original := version
	t.Cleanup(func() { version = original })

	version = ""
	if got := Version(); got != devVersion {
		t.Errorf("Version() = %q, want %q", got, devVersion)
	}
}

func TestCommitPrefersLdflag(t *testing.T) {
	original := commit
	t.Cleanup(func() { commit = original })

	commit = "deadbeef"
	if got := Commit(); got != "deadbeef" {
		t.Errorf("Commit() = %q, want %q", got, "deadbeef")
	}
}

func TestCommitFallbackNonEmpty(t *testing.T) {
	if Commit() == "" {
		t.Error("Commit() returned empty string")
	}
}

func TestShortRevision(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"abcdef1234567890", "abcdef1"},
		{"abc", "abc"},
		{"vabcdef1", "abcdef1"},
	}
	for _, tt := range tests {
		if got := shortRevision(tt.in); got != tt.want {
			t.Errorf("shortRevision(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestDate(t *testing.T) {
	original := date
	t.Cleanup(func() { date = original })

	date = "2026-06-08"
	if got := Date(); got != "2026-06-08" {
		t.Errorf("Date() = %q, want %q", got, "2026-06-08")
	}

	date = ""
	if got := Date(); got != "unknown" {
		t.Errorf("Date() fallback = %q, want %q", got, "unknown")
	}
}
