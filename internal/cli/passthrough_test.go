package cli

import (
	"testing"
)

func TestPassthroughAgentArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "agent flags only",
			args: []string{"crypt", "grok", "--mount", ".", "--reasoning-effort", "high"},
			want: []string{"--reasoning-effort", "high"},
		},
		{
			name: "task prompt",
			args: []string{"crypt", "grok", "--mount", ".", "fix the test"},
			want: []string{"fix the test"},
		},
		{
			name: "multiple mounts",
			args: []string{"crypt", "grok", "--mount", ".", "--mount", "/opt/atrium/bin", "--reasoning-effort", "high"},
			want: []string{"--reasoning-effort", "high"},
		},
		{
			name: "root flags before subcommand",
			args: []string{"crypt", "--name", "backend", "grok", "--mount", ".", "--reasoning-effort", "high"},
			want: []string{"--reasoning-effort", "high"},
		},
		{
			name: "env flag stripped",
			args: []string{"crypt", "grok", "--mount", ".", "--env", "ATRIUM_SOCKET=/tmp/sock", "fix the test"},
			want: []string{"fix the test"},
		},
		{
			name: "socket flag stripped",
			args: []string{"crypt", "grok", "--mount", ".", "--socket", "~/.atrium/ipc/stable.sock::ATRIUM_SOCKET"},
			want: nil,
		},
		{
			name: "socket flag stripped with task prompt",
			args: []string{"crypt", "grok", "--mount", ".", "--socket", "~/.atrium/ipc/stable.sock::ATRIUM_SOCKET", "fix the test"},
			want: []string{"fix the test"},
		},
		{
			name: "double dash separator",
			args: []string{"crypt", "grok", "--mount", ".", "--", "--reasoning-effort", "high"},
			want: []string{"--reasoning-effort", "high"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := passthroughAgentArgsFrom("grok", tc.args)
			if len(got) != len(tc.want) {
				t.Fatalf("passthroughAgentArgs() = %#v, want %#v", got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("passthroughAgentArgs()[%d] = %q, want %q (full %#v)", i, got[i], tc.want[i], got)
				}
			}
		})
	}
}
