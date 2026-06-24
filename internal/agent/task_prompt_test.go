package agent

import "testing"

func TestHasTaskPrompt(t *testing.T) {
	tests := []struct {
		args []string
		want bool
	}{
		{nil, false},
		{[]string{"--reasoning-effort", "high"}, false},
		{[]string{"fix the test"}, true},
		{[]string{"--model", "opus", "fix the test"}, true},
	}
	for _, tc := range tests {
		if got := HasTaskPrompt(tc.args); got != tc.want {
			t.Errorf("HasTaskPrompt(%#v) = %v, want %v", tc.args, got, tc.want)
		}
	}
}
