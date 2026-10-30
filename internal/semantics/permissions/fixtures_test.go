package permissions

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

	got := make(map[string][]JobPrivilege)
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
			got[key] = Resolve(wf, config.Default().RepoProfile)
		}
		if err := ws.Close(); err != nil {
			t.Fatalf("%s: close workspace: %v", root, err)
		}
	}

	t.Run("a-spire review apply", func(t *testing.T) {
		apply := fixtureJob(t, got, "a-spire/.github/workflows/review-apply.yml", "apply")
		privilege := apply.Privilege

		if privilege.Token.Source != SourceJob {
			t.Errorf("token source = %q, want %q", privilege.Token.Source, SourceJob)
		}
		want := map[string]string{
			"actions":       AccessRead,
			"issues":        AccessWrite,
			"pull-requests": AccessWrite,
			"contents":      AccessNone,
		}
		for scope, access := range want {
			if privilege.Token.Scopes[scope] != access {
				t.Errorf("scopes[%s] = %q, want %q", scope, privilege.Token.Scopes[scope], access)
			}
		}
		if len(privilege.Checkouts) != 1 {
			t.Fatalf("checkouts = %+v, want one", privilege.Checkouts)
		}
		if privilege.Checkouts[0].PersistCredentials != persistTrue {
			t.Errorf("persist_credentials = %q, want %q", privilege.Checkouts[0].PersistCredentials, persistTrue)
		}
		if privilege.CacheWrite != CacheDefaultBranch {
			t.Errorf("cache_write = %q, want %q", privilege.CacheWrite, CacheDefaultBranch)
		}
	})

	t.Run("g-unbound-selector publish", func(t *testing.T) {
		publish := fixtureJob(t, got, "g-unbound-selector/.github/workflows/publish-site.yml", "publish")
		if publish.Privilege.Token.Scopes["contents"] != AccessWrite {
			t.Errorf("scopes[contents] = %q, want %q", publish.Privilege.Token.Scopes["contents"], AccessWrite)
		}
	})
}

func fixtureJob(t *testing.T, privileges map[string][]JobPrivilege, workflowPath, id string) JobPrivilege {
	t.Helper()
	jobs, ok := privileges[workflowPath]
	if !ok {
		t.Fatalf("fixture workflow %s was not resolved", workflowPath)
	}
	for _, job := range jobs {
		if job.Job == id {
			return job
		}
	}
	t.Fatalf("%s has no job %s", workflowPath, id)
	return JobPrivilege{}
}
