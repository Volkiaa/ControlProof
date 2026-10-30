package main

import (
	"bytes"
	"testing"

	"github.com/Volkiaa/ControlProof/internal/version"
)

func TestRun(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{
			name:       "version",
			args:       []string{"version"},
			wantCode:   exitCompleted,
			wantStdout: "controlproof " + version.Version + "\n",
		},
		{
			name:       "version with extra argument",
			args:       []string{"version", "extra"},
			wantCode:   exitError,
			wantStderr: "controlproof: version takes no arguments\n" + usage,
		},
		{
			name:       "scan is a stub",
			args:       []string{"scan", "."},
			wantCode:   exitError,
			wantStderr: "controlproof: scan is not implemented yet\n",
		},
		{
			name:       "no arguments",
			wantCode:   exitError,
			wantStderr: usage,
		},
		{
			name:       "help",
			args:       []string{"help"},
			wantCode:   exitCompleted,
			wantStdout: usage,
		},
		{
			name:       "short help flag",
			args:       []string{"-h"},
			wantCode:   exitCompleted,
			wantStdout: usage,
		},
		{
			name:       "long help flag",
			args:       []string{"--help"},
			wantCode:   exitCompleted,
			wantStdout: usage,
		},
		{
			name:       "unknown command",
			args:       []string{"bogus"},
			wantCode:   exitError,
			wantStderr: "controlproof: unknown command \"bogus\"\n" + usage,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(tt.args, &stdout, &stderr)

			if code != tt.wantCode {
				t.Errorf("run(%q) = %d, want %d", tt.args, code, tt.wantCode)
			}
			if got := stdout.String(); got != tt.wantStdout {
				t.Errorf("run(%q) stdout = %q, want %q", tt.args, got, tt.wantStdout)
			}
			if got := stderr.String(); got != tt.wantStderr {
				t.Errorf("run(%q) stderr = %q, want %q", tt.args, got, tt.wantStderr)
			}
		})
	}
}
