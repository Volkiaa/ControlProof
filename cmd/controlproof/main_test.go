package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
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

type graphOutput struct {
	Schema       string `json:"schema"`
	PolicySHA256 string `json:"policy_sha256"`
	Graph        struct {
		Nodes []struct {
			ID   string `json:"id"`
			Kind string `json:"kind"`
		} `json:"nodes"`
		Edges []struct {
			From       string            `json:"from"`
			To         string            `json:"to"`
			Kind       string            `json:"kind"`
			Confidence string            `json:"confidence"`
			Attrs      map[string]string `json:"attrs"`
		} `json:"edges"`
	} `json:"graph"`
}

func TestDebugGraph(t *testing.T) {
	aSpire := filepath.Join("..", "..", "testdata", "fixtures", "a-spire")

	t.Run("valid workflows", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := run([]string{"debug", "graph", aSpire}, &stdout, &stderr)
		if code != exitCompleted {
			t.Fatalf("run() = %d, want %d; stderr: %s", code, exitCompleted, stderr.String())
		}
		if stderr.Len() != 0 {
			t.Fatalf("stderr = %q, want empty", stderr.String())
		}

		var output graphOutput
		if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
			t.Fatalf("stdout is not JSON: %v\n%s", err, stdout.String())
		}
		if output.Schema != "controlproof-graph/0" {
			t.Fatalf("schema = %q, want controlproof-graph/0", output.Schema)
		}
		if output.PolicySHA256 != "" {
			t.Fatalf("policy_sha256 = %q, want empty without --policy", output.PolicySHA256)
		}
		if len(output.Graph.Nodes) == 0 || len(output.Graph.Edges) == 0 {
			t.Fatalf("graph is empty: %+v", output.Graph)
		}

		found := false
		for _, edge := range output.Graph.Edges {
			if edge.Kind == "DOWNLOADS" && edge.To == "artifact:.github/workflows/review-capture.yml#review" {
				found = true
				if edge.Confidence != "high" || edge.Attrs["content"] != "arbitrary" || edge.Attrs["run_id_binding"] != "workflow-run-event" {
					t.Fatalf("download edge = %+v attrs %v, want high/arbitrary/workflow-run-event", edge, edge.Attrs)
				}
			}
		}
		if !found {
			t.Fatal("graph has no DOWNLOADS edge to the review capture artifact")
		}
	})

	t.Run("invalid policy", func(t *testing.T) {
		policy := filepath.Join(t.TempDir(), "bad.yaml")
		if err := os.WriteFile(policy, []byte("schema_version: 2\n"), 0o644); err != nil {
			t.Fatalf("write policy: %v", err)
		}

		var stdout, stderr bytes.Buffer
		code := run([]string{"debug", "graph", "--policy", policy, aSpire}, &stdout, &stderr)
		if code != exitError {
			t.Fatalf("run() = %d, want %d", code, exitError)
		}
		if stdout.Len() != 0 {
			t.Fatalf("stdout = %q, want empty", stdout.String())
		}
		if !strings.Contains(stderr.String(), policy) {
			t.Fatalf("stderr = %q, want it to name %s", stderr.String(), policy)
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

type semanticsOutput struct {
	Schema       string                   `json:"schema"`
	PolicySHA256 string                   `json:"policy_sha256"`
	Workflows    []semanticsWorkflowEntry `json:"workflows"`
}

type semanticsWorkflowEntry struct {
	Path     string `json:"path"`
	Triggers struct {
		Workflow string `json:"workflow"`
		Name     string `json:"name"`
		Trust    string `json:"trust"`
		Triggers []struct {
			Event           string `json:"event"`
			Modeled         bool   `json:"modeled"`
			AttackerControl string `json:"attacker_control"`
		} `json:"triggers"`
	} `json:"triggers"`
	Jobs []semanticsJob `json:"jobs"`
}

type semanticsJob struct {
	Job       string `json:"job"`
	Privilege struct {
		Token struct {
			Source string            `json:"source"`
			Scopes map[string]string `json:"scopes"`
		} `json:"token"`
		Prerequisites []struct {
			Setting string `json:"setting"`
		} `json:"prerequisites"`
	} `json:"privilege"`
}

func findSemanticsJob(t *testing.T, output semanticsOutput, path, id string) semanticsJob {
	t.Helper()
	for _, wf := range output.Workflows {
		if wf.Path != path {
			continue
		}
		for _, job := range wf.Jobs {
			if job.Job == id {
				return job
			}
		}
		t.Fatalf("%s has no job %s", path, id)
	}
	t.Fatalf("output has no workflow %s", path)
	return semanticsJob{}
}

func hasSemanticsPrerequisite(job semanticsJob, setting string) bool {
	for _, prerequisite := range job.Privilege.Prerequisites {
		if prerequisite.Setting == setting {
			return true
		}
	}
	return false
}

func readSemantics(t *testing.T, stdout []byte) semanticsOutput {
	t.Helper()
	var output semanticsOutput
	if err := json.Unmarshal(stdout, &output); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	return output
}

func TestDebugSemantics(t *testing.T) {
	aSpire := filepath.Join("..", "..", "testdata", "fixtures", "a-spire")

	t.Run("valid workflows", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := run([]string{"debug", "semantics", aSpire}, &stdout, &stderr)
		if code != exitCompleted {
			t.Fatalf("run() = %d, want %d; stderr: %s", code, exitCompleted, stderr.String())
		}
		if stderr.Len() != 0 {
			t.Fatalf("stderr = %q, want empty", stderr.String())
		}

		output := readSemantics(t, stdout.Bytes())
		if output.Schema != "controlproof-semantics/0" {
			t.Fatalf("schema = %q, want controlproof-semantics/0", output.Schema)
		}
		if output.PolicySHA256 != "" {
			t.Fatalf("policy_sha256 = %q, want empty without --policy", output.PolicySHA256)
		}
		if len(output.Workflows) != 2 {
			t.Fatalf("workflows = %+v, want two", output.Workflows)
		}

		trust := make(map[string]string)
		for _, wf := range output.Workflows {
			trust[wf.Path] = wf.Triggers.Trust
		}
		if got := trust[".github/workflows/review-capture.yml"]; got != "fork-definition-controlled" {
			t.Errorf("review-capture trust = %q, want fork-definition-controlled", got)
		}
		if got := trust[".github/workflows/review-apply.yml"]; got != "trusted-definition" {
			t.Errorf("review-apply trust = %q, want trusted-definition", got)
		}
	})

	t.Run("policy", func(t *testing.T) {
		policy := filepath.Join(t.TempDir(), "controlproof.yaml")
		content := "schema_version: 1\nrepo_profile:\n  fork_pr_workflow_approval: none\n"
		if err := os.WriteFile(policy, []byte(content), 0o644); err != nil {
			t.Fatalf("write policy: %v", err)
		}

		var stdout, stderr bytes.Buffer
		code := run([]string{"debug", "semantics", "--policy", policy, aSpire}, &stdout, &stderr)
		if code != exitCompleted {
			t.Fatalf("run() = %d, want %d; stderr: %s", code, exitCompleted, stderr.String())
		}

		sum := sha256.Sum256([]byte(content))
		output := readSemantics(t, stdout.Bytes())
		if want := hex.EncodeToString(sum[:]); output.PolicySHA256 != want {
			t.Fatalf("policy_sha256 = %q, want %q", output.PolicySHA256, want)
		}
		classified := false
		for _, wf := range output.Workflows {
			classified = classified || wf.Triggers.Trust == "fork-definition-controlled"
		}
		if !classified {
			t.Fatal("no fork-definition-controlled workflow, want the approval prerequisite to apply")
		}
		if bytes.Contains(stdout.Bytes(), []byte("fork_pr_workflow_approval")) {
			t.Error("output lists fork_pr_workflow_approval despite approval: none")
		}
	})

	t.Run("invalid policy", func(t *testing.T) {
		policy := filepath.Join(t.TempDir(), "bad.yaml")
		if err := os.WriteFile(policy, []byte("schema_version: 2\n"), 0o644); err != nil {
			t.Fatalf("write policy: %v", err)
		}

		var stdout, stderr bytes.Buffer
		code := run([]string{"debug", "semantics", "--policy", policy, aSpire}, &stdout, &stderr)
		if code != exitError {
			t.Fatalf("run() = %d, want %d", code, exitError)
		}
		if stdout.Len() != 0 {
			t.Fatalf("stdout = %q, want empty", stdout.String())
		}
		if !strings.Contains(stderr.String(), policy) {
			t.Fatalf("stderr = %q, want it to name %s", stderr.String(), policy)
		}
	})

	t.Run("missing policy", func(t *testing.T) {
		policy := filepath.Join(t.TempDir(), "missing.yaml")

		var stdout, stderr bytes.Buffer
		code := run([]string{"debug", "semantics", "--policy", policy}, &stdout, &stderr)
		if code != exitError {
			t.Fatalf("run() = %d, want %d", code, exitError)
		}
		if stdout.Len() != 0 {
			t.Fatalf("stdout = %q, want empty", stdout.String())
		}
		if !strings.Contains(stderr.String(), policy) {
			t.Fatalf("stderr = %q, want it to name %s", stderr.String(), policy)
		}
	})

	t.Run("resolved repository default token permissions", func(t *testing.T) {
		root := t.TempDir()
		writeWorkflow(t, root, "consumer.yml", "on:\n  workflow_run:\n    workflows: [Build]\n    types: [completed]\njobs:\n  consume:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo hi\n")

		var stdout, stderr bytes.Buffer
		if code := run([]string{"debug", "semantics", root}, &stdout, &stderr); code != exitCompleted {
			t.Fatalf("run() = %d, want %d; stderr: %s", code, exitCompleted, stderr.String())
		}
		unknown := findSemanticsJob(t, readSemantics(t, stdout.Bytes()), ".github/workflows/consumer.yml", "consume")
		if unknown.Privilege.Token.Source != "unknown-default" {
			t.Fatalf("token source = %q, want unknown-default", unknown.Privilege.Token.Source)
		}
		if unknown.Privilege.Token.Scopes != nil {
			t.Fatalf("token scopes = %v, want none", unknown.Privilege.Token.Scopes)
		}
		if !hasSemanticsPrerequisite(unknown, "repository_default_token_permissions") {
			t.Fatalf("prerequisites = %+v, want repository_default_token_permissions", unknown.Privilege.Prerequisites)
		}

		policy := filepath.Join(t.TempDir(), "controlproof.yaml")
		content := "schema_version: 1\nrepo_profile:\n  repository_default_token_permissions: write\n"
		if err := os.WriteFile(policy, []byte(content), 0o644); err != nil {
			t.Fatalf("write policy: %v", err)
		}

		stdout.Reset()
		stderr.Reset()
		if code := run([]string{"debug", "semantics", "--policy", policy, root}, &stdout, &stderr); code != exitCompleted {
			t.Fatalf("run() = %d, want %d; stderr: %s", code, exitCompleted, stderr.String())
		}
		resolved := findSemanticsJob(t, readSemantics(t, stdout.Bytes()), ".github/workflows/consumer.yml", "consume")
		if resolved.Privilege.Token.Source != "repo_profile" {
			t.Fatalf("token source = %q, want repo_profile", resolved.Privilege.Token.Source)
		}
		if resolved.Privilege.Token.Scopes["contents"] != "write" || resolved.Privilege.Token.Scopes["id-token"] != "none" {
			t.Fatalf("token scopes = %v, want contents write and id-token none", resolved.Privilege.Token.Scopes)
		}
		if hasSemanticsPrerequisite(resolved, "repository_default_token_permissions") {
			t.Fatalf("prerequisites = %+v, want no repository_default_token_permissions once resolved", resolved.Privilege.Prerequisites)
		}
	})

	t.Run("malformed workflow", func(t *testing.T) {
		root := t.TempDir()
		writeWorkflow(t, root, "broken.yml", "on: [push\njobs: {}\n")

		var stdout, stderr bytes.Buffer
		code := run([]string{"debug", "semantics", root}, &stdout, &stderr)
		if code != exitError {
			t.Fatalf("run() = %d, want %d", code, exitError)
		}
		if stdout.Len() == 0 {
			t.Fatal("stdout is empty, want partial output before the error")
		}
		if !strings.Contains(stderr.String(), ".github/workflows/broken.yml") {
			t.Fatalf("stderr = %q, want it to name the malformed workflow", stderr.String())
		}
	})
}
