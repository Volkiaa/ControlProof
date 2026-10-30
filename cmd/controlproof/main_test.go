package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

func TestDebugIR(t *testing.T) {
	t.Run("valid workflow", func(t *testing.T) {
		root := t.TempDir()
		writeWorkflow(t, root, "ci.yml", "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo hi\n")

		var stdout, stderr bytes.Buffer
		code := run([]string{"debug", "ir", root}, &stdout, &stderr)
		if code != exitCompleted {
			t.Fatalf("run() = %d, want %d; stderr: %s", code, exitCompleted, stderr.String())
		}
		if stderr.Len() != 0 {
			t.Fatalf("stderr = %q, want empty", stderr.String())
		}

		var output struct {
			Schema    string `json:"schema"`
			Workflows []struct {
				Path string `json:"path"`
			} `json:"workflows"`
		}
		if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
			t.Fatalf("stdout is not JSON: %v\n%s", err, stdout.String())
		}
		if output.Schema != "controlproof-ir/0" {
			t.Fatalf("schema = %q, want controlproof-ir/0", output.Schema)
		}
		if len(output.Workflows) != 1 || output.Workflows[0].Path != ".github/workflows/ci.yml" {
			t.Fatalf("workflows = %+v, want one at .github/workflows/ci.yml", output.Workflows)
		}
	})

	t.Run("malformed workflow", func(t *testing.T) {
		root := t.TempDir()
		writeWorkflow(t, root, "broken.yml", "on: [push\njobs: {}\n")

		var stdout, stderr bytes.Buffer
		code := run([]string{"debug", "ir", root}, &stdout, &stderr)
		if code != exitError {
			t.Fatalf("run() = %d, want %d", code, exitError)
		}
		if !strings.Contains(stderr.String(), ".github/workflows/broken.yml") {
			t.Fatalf("stderr = %q, want it to name the malformed workflow", stderr.String())
		}
	})

	t.Run("invalid subcommand", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := run([]string{"debug", "bogus"}, &stdout, &stderr)
		if code != exitError {
			t.Fatalf("run() = %d, want %d", code, exitError)
		}
		if !strings.Contains(stderr.String(), `unknown debug subcommand "bogus"`) {
			t.Fatalf("stderr = %q, want unknown subcommand error", stderr.String())
		}
	})
}

func writeWorkflow(t *testing.T, root, name, content string) {
	t.Helper()
	dir := filepath.Join(root, ".github", "workflows")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create workflows directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write workflow: %v", err)
	}
}
