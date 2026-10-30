package graph

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Volkiaa/ControlProof/internal/config"
	"github.com/Volkiaa/ControlProof/internal/semantics/triggers"
)

func TestFixtures(t *testing.T) {
	roots, err := filepath.Glob(filepath.Join("..", "..", "testdata", "fixtures", "*"))
	if err != nil {
		t.Fatalf("glob fixtures: %v", err)
	}

	for _, root := range roots {
		info, err := os.Stat(root)
		if err != nil {
			t.Fatalf("stat %s: %v", root, err)
		}
		if !info.IsDir() {
			continue
		}
		name := filepath.Base(root)
		t.Run(name, func(t *testing.T) {
			workflows := loadWorkspace(t, root)
			g := Build(workflows, config.Default().RepoProfile)

			trust := make(map[string]triggers.Trust, len(workflows))
			for i := range workflows {
				trust[workflows[i].Path] = triggers.Classify(&workflows[i], config.Default().RepoProfile).Trust
			}
			for _, node := range g.Nodes {
				if node.Kind != kindArtifact {
					continue
				}
				producer := node.Attrs["producer"]
				if trust[producer] == triggers.TrustForkDefinitionControlled && node.Attrs["content"] != "arbitrary" {
					t.Errorf("artifact %s from fork-controlled %s has content %q, want arbitrary", node.ID, producer, node.Attrs["content"])
				}
			}
		})
	}

	t.Run("a-spire", func(t *testing.T) {
		g := Build(loadWorkspace(t, filepath.Join("..", "..", "testdata", "fixtures", "a-spire")), config.Default().RepoProfile)
		step := "step:.github/workflows/review-apply.yml#apply#1"
		artifact := "artifact:.github/workflows/review-capture.yml#review"

		download := edges(g, edgeDownloads, step, artifact)
		if len(download) != 1 {
			t.Fatalf("DOWNLOADS edges to %s = %d, want 1", artifact, len(download))
		}
		if download[0].Attrs["content"] != "arbitrary" || download[0].Attrs["run_id_binding"] != "workflow-run-event" || download[0].Confidence != "high" {
			t.Fatalf("download edge = %+v attrs %v, want arbitrary/workflow-run-event/high", download[0], download[0].Attrs)
		}

		file := fileNodeForStep(t, g, step)
		if file.Attrs["dir"] != "$GITHUB_WORKSPACE" {
			t.Fatalf("extraction dir = %q, want $GITHUB_WORKSPACE", file.Attrs["dir"])
		}

		consumer := "workflow:.github/workflows/review-apply.yml"
		producer := "workflow:.github/workflows/review-capture.yml"
		if len(edges(g, edgeCanTrigger, producer, consumer)) != 1 {
			t.Fatalf("no candidate CAN_TRIGGER edge from %s to %s", producer, consumer)
		}
		if len(edges(g, edgeCanTrigger, "workflow:fork-added:Capture review", consumer)) != 1 {
			t.Fatalf("no synthetic CAN_TRIGGER edge to %s", consumer)
		}
	})

	t.Run("a-spire-fixed", func(t *testing.T) {
		g := Build(loadWorkspace(t, filepath.Join("..", "..", "testdata", "fixtures", "a-spire-fixed")), config.Default().RepoProfile)
		step := "step:.github/workflows/review-apply.yml#apply#1"
		artifact := "artifact:.github/workflows/review-capture.yml#review"

		if len(edges(g, edgeDownloads, step, artifact)) != 1 {
			t.Fatalf("no DOWNLOADS edge to %s", artifact)
		}
		file := fileNodeForStep(t, g, step)
		if file.Attrs["dir"] != "$RUNNER_TEMP/review" {
			t.Fatalf("extraction dir = %q, want $RUNNER_TEMP/review", file.Attrs["dir"])
		}
	})

	t.Run("g-unbound-selector", func(t *testing.T) {
		g := Build(loadWorkspace(t, filepath.Join("..", "..", "testdata", "fixtures", "g-unbound-selector")), config.Default().RepoProfile)
		step := "step:.github/workflows/publish-site.yml#publish#0"
		artifact := "artifact:.github/workflows/build-site.yml#site"

		download := edges(g, edgeDownloads, step, artifact)
		if len(download) != 1 {
			t.Fatalf("DOWNLOADS edges to %s = %d, want 1", artifact, len(download))
		}
		edge := download[0]
		if edge.Attrs["content"] != "arbitrary" || edge.Attrs["run_id_binding"] != "latest-run" || edge.Attrs["allow_forks"] != "true" || edge.Confidence != "candidate" {
			t.Fatalf("download edge = %+v attrs %v, want arbitrary/latest-run/true/candidate", edge, edge.Attrs)
		}

		file := fileNodeForStep(t, g, step)
		if file.Attrs["dir"] != "$RUNNER_TEMP/site" {
			t.Fatalf("extraction dir = %q, want $RUNNER_TEMP/site", file.Attrs["dir"])
		}
	})
}
