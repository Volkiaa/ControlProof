package permissions

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/Volkiaa/ControlProof/internal/config"
	"github.com/Volkiaa/ControlProof/internal/workflow"
	"github.com/Volkiaa/ControlProof/internal/workspace"
)

var updateGolden = flag.Bool("update", false, "update golden files")

func TestGolden(t *testing.T) {
	inputs, err := filepath.Glob(filepath.Join("testdata", "golden", "*.yml"))
	if err != nil {
		t.Fatalf("glob golden inputs: %v", err)
	}
	if len(inputs) == 0 {
		t.Fatal("no golden inputs found")
	}
	sort.Strings(inputs)

	for _, input := range inputs {
		name := strings.TrimSuffix(filepath.Base(input), ".yml")
		t.Run(name, func(t *testing.T) {
			content, err := os.ReadFile(input)
			if err != nil {
				t.Fatalf("read %s: %v", input, err)
			}
			wf, err := workflow.Parse(workspace.File{Path: filepath.ToSlash(input), Content: content}, workflow.DefaultLimits)
			if err != nil {
				t.Fatalf("Parse(%s) error: %v", input, err)
			}
			if len(wf.Diagnostics) != 0 {
				t.Fatalf("Parse(%s) diagnostics: %+v", input, wf.Diagnostics)
			}
			checkGolden(t, name, Resolve(wf, config.Default().RepoProfile))
		})
	}
}

func TestGoldenWorkspaces(t *testing.T) {
	dirs, err := filepath.Glob(filepath.Join("testdata", "workspaces", "*"))
	if err != nil {
		t.Fatalf("glob workspace cases: %v", err)
	}
	if len(dirs) == 0 {
		t.Fatal("no workspace cases found")
	}
	sort.Strings(dirs)

	for _, dir := range dirs {
		name := filepath.Base(dir)
		t.Run(name, func(t *testing.T) {
			ws, err := workspace.Open(dir, workspace.DefaultLimits)
			if err != nil {
				t.Fatalf("open %s: %v", dir, err)
			}
			defer ws.Close()

			files, err := ws.Workflows()
			if err != nil {
				t.Fatalf("%s: list workflows: %v", dir, err)
			}

			want := ".github/workflows/" + name + ".yml"
			resolved := false
			for _, file := range files {
				if file.Path != want {
					continue
				}
				wf, err := workflow.Parse(file, workflow.DefaultLimits)
				if err != nil {
					t.Fatalf("parse %s: %v", file.Path, err)
				}
				if len(wf.Diagnostics) != 0 {
					t.Fatalf("parse %s diagnostics: %+v", file.Path, wf.Diagnostics)
				}
				if err := workflow.Inline(ws, wf, workflow.DefaultLimits); err != nil {
					t.Fatalf("inline %s: %v", file.Path, err)
				}
				checkGolden(t, name, Resolve(wf, config.Default().RepoProfile))
				resolved = true
			}
			if !resolved {
				t.Fatalf("%s has no entry workflow %s", dir, want)
			}
		})
	}
}

func checkGolden(t *testing.T, name string, jobs []JobPrivilege) {
	t.Helper()

	got, err := json.MarshalIndent(jobs, "", "  ")
	if err != nil {
		t.Fatalf("marshal privileges: %v", err)
	}
	got = append(got, '\n')

	golden := filepath.Join("testdata", "golden", name+".json")
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
		t.Fatalf("privileges for %s do not match %s\n--- got ---\n%s\n--- want ---\n%s", name, golden, got, want)
	}
}
