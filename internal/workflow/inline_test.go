package workflow

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Volkiaa/ControlProof/internal/model"
	"github.com/Volkiaa/ControlProof/internal/workspace"
)

var inlineGoldenCases = []string{
	"I1_composite",
	"I2_nested",
	"I3_opaque",
	"I4_workflow",
	"I9_fixture_j",
}

func TestInlineGolden(t *testing.T) {
	for _, name := range inlineGoldenCases {
		t.Run(name, func(t *testing.T) {
			root := filepath.Join("testdata", "inline", name)
			ws := openInlineWorkspace(t, root)
			defer ws.Close()

			files, err := ws.Workflows()
			if err != nil {
				t.Fatalf("Workflows() error: %v", err)
			}

			output := struct {
				Workflows []model.Workflow `json:"workflows"`
			}{}
			for _, file := range files {
				wf, err := Parse(file, DefaultLimits)
				if err != nil {
					t.Fatalf("Parse(%s) error: %v", file.Path, err)
				}
				if err := Inline(ws, wf, DefaultLimits); err != nil {
					t.Fatalf("Inline(%s) error: %v", file.Path, err)
				}
				output.Workflows = append(output.Workflows, *wf)
			}

			got, err := json.MarshalIndent(output, "", "  ")
			if err != nil {
				t.Fatalf("marshal IR: %v", err)
			}
			got = append(got, '\n')

			golden := filepath.Join(root, "expected.json")
			if *updateGolden {
				if err := os.WriteFile(golden, got, 0o644); err != nil {
					t.Fatalf("write %s: %v", golden, err)
				}
				return
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("read %s: %v (run with -update)", golden, err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("IR for %s does not match %s\n--- got ---\n%s\n--- want ---\n%s", name, golden, got, want)
			}
		})
	}
}

func TestInlineCycle(t *testing.T) {
	ws := openInlineWorkspace(t, filepath.Join("testdata", "inline", "I5_cycle"))
	defer ws.Close()

	wf := parseInlineWorkflow(t, ws, ".github/workflows/a.yml")
	err := Inline(ws, wf, DefaultLimits)
	if !errors.Is(err, ErrCycle) {
		t.Fatalf("Inline() error = %v, want ErrCycle", err)
	}
	chain := ".github/workflows/a.yml -> .github/workflows/b.yml -> .github/workflows/a.yml"
	if !strings.Contains(err.Error(), chain) {
		t.Fatalf("Inline() error %q does not contain chain %q", err, chain)
	}
	callee := wf.Jobs[0].Callee
	if callee == nil || callee.Workflow == nil || len(callee.Workflow.Jobs) != 1 {
		t.Fatalf("callee IR = %+v, want b.yml jobs", callee)
	}
	cyclic := callee.Workflow.Jobs[0].Callee
	if cyclic == nil || cyclic.Unresolved != "cycle" {
		t.Fatalf("cyclic callee = %+v, want Unresolved cycle", cyclic)
	}

	self := parseInlineWorkflow(t, ws, ".github/workflows/self.yml")
	err = Inline(ws, self, DefaultLimits)
	if !errors.Is(err, ErrCycle) {
		t.Fatalf("self-call Inline() error = %v, want ErrCycle", err)
	}
	if !strings.Contains(err.Error(), "self.yml -> .github/workflows/self.yml") {
		t.Fatalf("self-call error %q does not show the chain", err)
	}
}

func TestInlineCompositeCycle(t *testing.T) {
	ws := openInlineWorkspace(t, filepath.Join("testdata", "inline", "I6_action_cycle"))
	defer ws.Close()

	wf := parseInlineWorkflow(t, ws, ".github/workflows/ci.yml")
	err := Inline(ws, wf, DefaultLimits)
	if !errors.Is(err, ErrCycle) {
		t.Fatalf("Inline() error = %v, want ErrCycle", err)
	}

	local := wf.Jobs[0].Steps[0].Local
	if local == nil || len(local.Steps) != 1 {
		t.Fatalf("local action = %+v, want one composite step", local)
	}
	cyclic := local.Steps[0].Local
	if cyclic == nil || cyclic.Unresolved != "cycle" {
		t.Fatalf("cyclic action = %+v, want Unresolved cycle", cyclic)
	}
}

func TestInlineDepth(t *testing.T) {
	ws := openInlineWorkspace(t, filepath.Join("testdata", "inline", "I7_depth"))
	defer ws.Close()

	wf := parseInlineWorkflow(t, ws, ".github/workflows/a1.yml")
	err := Inline(ws, wf, DefaultLimits)
	if !errors.Is(err, ErrTooComplex) {
		t.Fatalf("Inline() error = %v, want ErrTooComplex", err)
	}
	if errors.Is(err, ErrCycle) {
		t.Fatalf("Inline() error = %v, want depth failure rather than cycle", err)
	}
}

func TestInlineBrokenCallee(t *testing.T) {
	ws := openInlineWorkspace(t, filepath.Join("testdata", "inline", "I10_broken_callee"))
	defer ws.Close()

	wf := parseInlineWorkflow(t, ws, ".github/workflows/caller.yml")
	err := Inline(ws, wf, DefaultLimits)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("Inline() error = %v, want ErrInvalid", err)
	}
	callee := wf.Jobs[0].Callee
	if callee == nil || !strings.Contains(callee.Unresolved, "invalid workflow") {
		t.Fatalf("callee = %+v, want a parse error in Unresolved", callee)
	}
}

func TestInlineSymlink(t *testing.T) {
	ws := openInlineWorkspace(t, filepath.Join("testdata", "inline", "I8_symlink"))
	defer ws.Close()

	wf := parseInlineWorkflow(t, ws, ".github/workflows/ci.yml")
	if err := Inline(ws, wf, DefaultLimits); !errors.Is(err, workspace.ErrSymlink) {
		t.Fatalf("Inline() error = %v, want workspace.ErrSymlink", err)
	}
	local := wf.Jobs[0].Steps[0].Local
	if local == nil || !strings.Contains(local.Unresolved, "symlink") {
		t.Fatalf("local action = %+v, want symlink in Unresolved", local)
	}
}

func TestInlineOutsideRoot(t *testing.T) {
	ws := openInlineWorkspace(t, filepath.Join("testdata", "inline", "I8_outside"))
	defer ws.Close()

	wf := parseInlineWorkflow(t, ws, ".github/workflows/ci.yml")
	if err := Inline(ws, wf, DefaultLimits); !errors.Is(err, workspace.ErrOutsideRoot) {
		t.Fatalf("Inline() error = %v, want workspace.ErrOutsideRoot", err)
	}
	local := wf.Jobs[0].Steps[0].Local
	if local == nil || !strings.Contains(local.Unresolved, "path outside workspace root") {
		t.Fatalf("local action = %+v, want outside-root reason", local)
	}
}

func TestInlineMalformedComposite(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		".github/workflows/ci.yml":       "on: push\njobs:\n  j:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: ./.github/actions/bad\n",
		".github/actions/bad/action.yml": "name: bad\nruns:\n  using: composite\n  steps: invalid\n",
	}
	for rel, content := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ws := openInlineWorkspace(t, root)
	defer ws.Close()

	wf := parseInlineWorkflow(t, ws, ".github/workflows/ci.yml")
	err := Inline(ws, wf, DefaultLimits)
	if err == nil || !strings.Contains(err.Error(), "runs.steps must be a sequence") {
		t.Fatalf("Inline() error = %v, want runs.steps error", err)
	}
	if local := wf.Jobs[0].Steps[0].Local; local == nil || local.Unresolved == "" {
		t.Fatalf("local action = %+v, want Unresolved", local)
	}
}

func TestInlineRemoteRefsStayOpaque(t *testing.T) {
	ws := openInlineWorkspace(t, filepath.Join("testdata", "inline", "I1_composite"))
	defer ws.Close()

	wf := parseInlineWorkflow(t, ws, ".github/workflows/ci.yml")
	if err := Inline(ws, wf, DefaultLimits); err != nil {
		t.Fatalf("Inline() error = %v", err)
	}
	for _, step := range wf.Jobs[0].Steps {
		if step.Uses == nil {
			continue
		}
		switch step.Uses.Value {
		case "./.github/actions/echo":
			if step.Local == nil {
				t.Fatal("local action has nil Local")
			}
		case "actions/checkout@v7", "owner/repo/path@v1", "docker://alpine:3":
			if step.Local != nil {
				t.Fatalf("remote ref %q has Local = %+v", step.Uses.Value, step.Local)
			}
		default:
			t.Fatalf("unexpected uses %q", step.Uses.Value)
		}
	}
}

func openInlineWorkspace(t *testing.T, root string) *workspace.Workspace {
	t.Helper()
	ws, err := workspace.Open(root, workspace.DefaultLimits)
	if err != nil {
		t.Fatalf("workspace.Open(%s) error: %v", root, err)
	}
	return ws
}

func parseInlineWorkflow(t *testing.T, ws *workspace.Workspace, path string) *model.Workflow {
	t.Helper()
	file, err := ws.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error: %v", path, err)
	}
	wf, err := Parse(file, DefaultLimits)
	if err != nil {
		t.Fatalf("Parse(%s) error: %v", path, err)
	}
	return wf
}
