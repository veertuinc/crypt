package sandbox

import (
	"reflect"
	"testing"

	"github.com/veertuinc/crypt/internal/anka"
)

func TestClaudeTrustPaths(t *testing.T) {
	got := claudeTrustPaths("anka", "/Volumes/My Shared Files/crypt")
	want := []string{"/Users/anka", anka.SharedFilesRoot, "/Volumes/My Shared Files/crypt"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("claudeTrustPaths() = %v, want %v", got, want)
	}

	got = claudeTrustPaths("anka", "")
	want = []string{"/Users/anka", anka.SharedFilesRoot}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("claudeTrustPaths(empty guestDir) = %v, want %v", got, want)
	}
}
