package triggers

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

			got, err := json.MarshalIndent(Classify(wf, config.Default().RepoProfile), "", "  ")
			if err != nil {
				t.Fatalf("marshal classification: %v", err)
			}
			got = append(got, '\n')

			golden := strings.TrimSuffix(input, ".yml") + ".json"
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
				t.Fatalf("classification for %s does not match %s\n--- got ---\n%s\n--- want ---\n%s", input, golden, got, want)
			}
		})
	}
}
