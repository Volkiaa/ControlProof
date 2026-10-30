package summaries

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Volkiaa/ControlProof/internal/model"
	"github.com/Volkiaa/ControlProof/internal/workflow"
	"github.com/Volkiaa/ControlProof/internal/workspace"
)

func TestFixtures(t *testing.T) {
	roots, err := filepath.Glob(filepath.Join("..", "..", "testdata", "fixtures", "*"))
	if err != nil {
		t.Fatalf("glob fixtures: %v", err)
	}

	recognized := 0
	for _, root := range roots {
		info, err := os.Stat(root)
		if err != nil {
			t.Fatalf("stat %s: %v", root, err)
		}
		if !info.IsDir() {
			continue
		}

		ws, err := workspace.Open(root, workspace.DefaultLimits)
		if err != nil {
			t.Fatalf("open %s: %v", root, err)
		}
		files, err := ws.Workflows()
		if err != nil {
			t.Fatalf("%s: list workflows: %v", root, err)
		}
		for _, file := range files {
			wf, err := workflow.Parse(file, workflow.DefaultLimits)
			if err != nil {
				t.Fatalf("%s: parse %s: %v", root, file.Path, err)
			}
			if len(wf.Diagnostics) != 0 {
				t.Fatalf("%s: parse %s diagnostics: %+v", root, file.Path, wf.Diagnostics)
			}
			if err := workflow.Inline(ws, wf, workflow.DefaultLimits); err != nil {
				t.Fatalf("%s: inline %s: %v", root, file.Path, err)
			}
			for _, step := range usesSteps(wf) {
				recognized++
				if got := Recognize(step); got.Opaque {
					t.Errorf("%s:%d %s is opaque (%s)", step.Pos.File, step.Pos.Line, step.Uses.Value, got.OpaqueReason)
				}
			}
		}
		if err := ws.Close(); err != nil {
			t.Fatalf("%s: close workspace: %v", root, err)
		}
	}

	if recognized == 0 {
		t.Fatal("no fixture steps with uses were recognized")
	}
}

func usesSteps(wf *model.Workflow) []model.Step {
	var out []model.Step
	for _, job := range wf.Jobs {
		for _, step := range job.Steps {
			if step.Uses != nil {
				out = append(out, step)
			}
		}
		if job.Callee != nil && job.Callee.Workflow != nil {
			out = append(out, usesSteps(job.Callee.Workflow)...)
		}
	}
	return out
}
