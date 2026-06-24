package sandbox

import "testing"

func TestFilterRulesMayBlockHostSSH(t *testing.T) {
	tests := []struct {
		rules string
		want  bool
	}{
		{"", false},
		{"pass in from 10.0.0.1", false},
		{"pass in from any port 22\npass out to any\nblock in from any port 80", false},
		{"pass in from any port 22\nblock local\nblock any", true},
		{"block local", true},
		{"block in from any port 22", true},
		{"block any", true},
	}
	for _, tc := range tests {
		if got := filterRulesMayBlockHostSSH(tc.rules); got != tc.want {
			t.Fatalf("filterRulesMayBlockHostSSH(%q) = %v, want %v", tc.rules, got, tc.want)
		}
	}
}
