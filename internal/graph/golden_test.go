package graph

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/Volkiaa/ControlProof/internal/config"
)

var updateGolden = flag.Bool("update", false, "update golden files")

func TestGolden(t *testing.T) {
	for _, name := range []string{"a-spire", "g-unbound-selector"} {
		t.Run(name, func(t *testing.T) {
			root := filepath.Join("..", "..", "testdata", "fixtures", name)
			g := Build(loadWorkspace(t, root), config.Default().RepoProfile)
			got, err := json.MarshalIndent(g, "", "  ")
			if err != nil {
				t.Fatalf("marshal graph: %v", err)
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
				t.Fatalf("graph for %s does not match %s\n--- got ---\n%s\n--- want ---\n%s", name, golden, got, want)
			}
		})
	}
}
