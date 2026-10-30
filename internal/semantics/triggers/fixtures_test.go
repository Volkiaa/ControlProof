package triggers

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Volkiaa/ControlProof/internal/config"
	"github.com/Volkiaa/ControlProof/internal/workflow"
	"github.com/Volkiaa/ControlProof/internal/workspace"
)

func TestFixtures(t *testing.T) {
	roots, err := filepath.Glob(filepath.Join("..", "..", "..", "testdata", "fixtures", "*"))
	if err != nil {
		t.Fatalf("glob fixtures: %v", err)
	}

	got := make(map[string]Trust)
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
			key := filepath.Base(root) + "/" + file.Path
			if _, ok := got[key]; ok {
				t.Fatalf("duplicate fixture workflow %s", key)
			}
			got[key] = Classify(wf, config.Default().RepoProfile).Trust
		}
		if err := ws.Close(); err != nil {
			t.Fatalf("%s: close workspace: %v", root, err)
		}
	}

	for key, trust := range got {
		if trust == TrustUnknown {
			t.Errorf("fixture workflow %s is %q, want a resolved trust", key, trust)
		}
	}

	forkControlled := []string{
		"a-spire/.github/workflows/review-capture.yml",
		"b-attest/.github/workflows/format.yml",
		"f-restricted-consumer/.github/workflows/review-capture.yml",
		"g-unbound-selector/.github/workflows/build-site.yml",
	}
	for _, key := range forkControlled {
		trust, ok := got[key]
		if !ok {
			t.Fatalf("fixture workflow %s was not classified", key)
		}
		if trust != TrustForkDefinitionControlled {
			t.Errorf("%s trust = %q, want %q", key, trust, TrustForkDefinitionControlled)
		}
	}

	notForkControlled := []string{
		"a-spire/.github/workflows/review-apply.yml",
		"g-unbound-selector/.github/workflows/publish-site.yml",
	}
	for _, key := range notForkControlled {
		trust, ok := got[key]
		if !ok {
			t.Fatalf("fixture workflow %s was not classified", key)
		}
		if trust == TrustForkDefinitionControlled {
			t.Errorf("%s trust = %q, want it not to be %q", key, trust, TrustForkDefinitionControlled)
		}
	}
}
