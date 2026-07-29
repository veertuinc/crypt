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

func TestNeedsInteractiveSSH(t *testing.T) {
	grok, _ := Get("grok")
	cursor, _ := Get("cursor-agent")
	custom := Agent{Name: "/bin/zsh"}

	tests := []struct {
		name string
		ag   Agent
		args []string
		want bool
	}{
		{"grok interactive", grok, nil, true},
		{"grok task", grok, []string{"fix the test"}, false},
		{"grok flags only", grok, []string{"--reasoning-effort", "high"}, true},
		{"cursor interactive", cursor, nil, true},
		{"cursor task", cursor, []string{"fix the test"}, false},
		{"cursor worker", cursor, []string{"worker", "start"}, false},
		{"cursor login", cursor, []string{"login"}, true},
		{"run script", custom, []string{"-lc", "printenv FOO"}, false},
		{"run login shell", custom, []string{"-l"}, true},
		{"run echo", Agent{Name: "/bin/echo"}, []string{"hi"}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := NeedsInteractiveSSH(tc.ag, tc.args); got != tc.want {
				t.Errorf("NeedsInteractiveSSH() = %v, want %v", got, tc.want)
			}
		})
	}
}
