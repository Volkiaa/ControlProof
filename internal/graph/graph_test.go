package graph

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Volkiaa/ControlProof/internal/config"
	"github.com/Volkiaa/ControlProof/internal/model"
	"github.com/Volkiaa/ControlProof/internal/workflow"
	"github.com/Volkiaa/ControlProof/internal/workspace"
)

func parseWorkflow(t *testing.T, name, source string) model.Workflow {
	t.Helper()
	wf, err := workflow.Parse(workspace.File{Path: ".github/workflows/" + name, Content: []byte(source)}, workflow.DefaultLimits)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	if len(wf.Diagnostics) != 0 {
		t.Fatalf("parse %s diagnostics: %+v", name, wf.Diagnostics)
	}
	return *wf
}

func buildOne(t *testing.T, name, source string) *Graph {
	t.Helper()
	return Build([]model.Workflow{parseWorkflow(t, name, source)}, config.Default().RepoProfile)
}

func findNode(t *testing.T, g *Graph, id string) Node {
	t.Helper()
	for _, node := range g.Nodes {
		if node.ID == id {
			return node
		}
	}
	t.Fatalf("graph has no node %q", id)
	return Node{}
}

func edges(g *Graph, kind, from, to string) []Edge {
	var out []Edge
	for _, edge := range g.Edges {
		if edge.Kind != kind {
			continue
		}
		if from != "" && edge.From != from {
			continue
		}
		if to != "" && edge.To != to {
			continue
		}
		out = append(out, edge)
	}
	return out
}

func edgeTo(t *testing.T, g *Graph, kind, to string) Edge {
	t.Helper()
	matches := edges(g, kind, "", to)
	if len(matches) != 1 {
		t.Fatalf("edges %s to %q = %d, want 1: %+v", kind, to, len(matches), matches)
	}
	return matches[0]
}

func unsupported(g *Graph, kind string) []Unsupported {
	var out []Unsupported
	for _, item := range g.Unsupported {
		if item.Kind == kind {
			out = append(out, item)
		}
	}
	return out
}

func fileNodeForStep(t *testing.T, g *Graph, stepID string) Node {
	t.Helper()
	extractions := edges(g, edgeExtractsTo, stepID, "")
	if len(extractions) != 1 {
		t.Fatalf("EXTRACTS_TO from %q = %d, want 1", stepID, len(extractions))
	}
	return findNode(t, g, extractions[0].To)
}

func TestTriggers(t *testing.T) {
	g := buildOne(t, "ci.yml", `name: CI
on:
  push:
  pull_request:
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
`)

	workflowNode := findNode(t, g, "workflow:.github/workflows/ci.yml")
	if workflowNode.Attrs["name"] != "CI" || workflowNode.Attrs["trust"] != "fork-definition-controlled" {
		t.Fatalf("workflow attrs = %v, want name CI and fork-definition-controlled", workflowNode.Attrs)
	}

	for _, event := range []string{"push", "pull_request"} {
		id := "trigger:.github/workflows/ci.yml#" + event
		node := findNode(t, g, id)
		if node.Kind != kindTrigger || node.Label != event {
			t.Errorf("trigger node = %+v, want event %q", node, event)
		}
		triggerEdges := edges(g, edgeCanTrigger, id, "workflow:.github/workflows/ci.yml")
		if len(triggerEdges) != 1 {
			t.Fatalf("CAN_TRIGGER edges from %s = %d, want 1", id, len(triggerEdges))
		}
		edge := triggerEdges[0]
		if edge.EvidenceClass != "literal" || edge.Confidence != "high" {
			t.Errorf("trigger edge = %+v, want literal/high", edge)
		}
	}

	job := findNode(t, g, "job:.github/workflows/ci.yml#build")
	if job.Attrs["token_source"] == "" || job.Attrs["cache_write"] == "" {
		t.Fatalf("job attrs = %v, want token_source and cache_write", job.Attrs)
	}
}

func TestUploads(t *testing.T) {
	tests := []struct {
		name    string
		source  string
		content string
		dynamic bool
	}{
		{
			name: "trusted-definition",
			source: `name: Trusted
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/upload-artifact@v7
        with:
          name: dist
          path: "dist/\nbin/app"
`,
			content: "trusted-definition",
		},
		{
			name: "pr-head-builder",
			source: `name: PR head
on: pull_request_target
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
        with:
          ref: ${{ github.event.pull_request.head.sha }}
      - uses: actions/upload-artifact@v7
        with:
          name: dist
          path: out/
`,
			content: "bounded-by-upload-path",
		},
		{
			name: "unknown",
			source: `name: Unknown
on: pull_request_target
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
        with:
          ref: ${{ github.workflow }}
      - uses: actions/upload-artifact@v7
        with:
          name: dist
          path: out/
`,
			content: "unknown",
		},
		{
			name: "dynamic-name",
			source: `name: Dynamic
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/upload-artifact@v7
        with:
          name: ${{ github.run_id }}
          path: out/
`,
			content: "trusted-definition",
			dynamic: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := buildOne(t, "upload.yml", tt.source)
			artifactName := "dist"
			if tt.dynamic {
				artifactName = "*"
			}
			id := "artifact:.github/workflows/upload.yml#" + artifactName
			node := findNode(t, g, id)
			if node.Kind != kindArtifact {
				t.Fatalf("node kind = %q, want Artifact", node.Kind)
			}
			if node.Attrs["content"] != tt.content || node.Attrs["producer"] != ".github/workflows/upload.yml" || node.Attrs["name"] != artifactName {
				t.Fatalf("artifact attrs = %v, want content %q producer path name %q", node.Attrs, tt.content, artifactName)
			}
			producing := edges(g, edgeProduces, "", id)
			if len(producing) != 1 {
				t.Fatalf("PRODUCES edges = %d, want 1", len(producing))
			}
			edge := producing[0]
			if edge.EvidenceClass != "literal" {
				t.Errorf("evidence = %q, want literal", edge.EvidenceClass)
			}
			if tt.dynamic && edge.Confidence != "unknown" {
				t.Errorf("confidence = %q, want unknown", edge.Confidence)
			}
			if !tt.dynamic && edge.Confidence != "high" {
				t.Errorf("confidence = %q, want high", edge.Confidence)
			}
			if edge.Attrs["name"] != artifactName || edge.Attrs["path"] == "" {
				t.Errorf("PRODUCES attrs = %v, want name %q and raw path", edge.Attrs, artifactName)
			}
			if !tt.dynamic && edge.Attrs["path"] != "dist/\nbin/app" && tt.content == "trusted-definition" && tt.name == "trusted-definition" {
				t.Errorf("path = %q, want raw newline-separated value", edge.Attrs["path"])
			}
		})
	}
}

func TestWorkflowRunLinking(t *testing.T) {
	producer := parseWorkflow(t, "capture.yml", `name: Capture review
on: pull_request
jobs:
  capture:
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
`)
	consumer := parseWorkflow(t, "apply.yml", `name: Apply review
on:
  workflow_run:
    workflows: ["Capture review"]
    types: [completed]
jobs:
  apply:
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
`)
	missing := parseWorkflow(t, "missing.yml", `name: Missing
on:
  workflow_run:
    workflows: ["Not in this repository"]
    types: [completed]
jobs:
  apply:
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
`)

	g := Build([]model.Workflow{consumer, producer, missing}, config.Default().RepoProfile)
	consumerID := "workflow:.github/workflows/apply.yml"
	producerID := "workflow:.github/workflows/capture.yml"
	syntheticID := "workflow:fork-added:Capture review"

	real := edges(g, edgeCanTrigger, producerID, consumerID)
	if len(real) != 1 {
		t.Fatalf("real CAN_TRIGGER edges = %d, want 1", len(real))
	}
	if real[0].EvidenceClass != "documented_semantics" || real[0].Confidence != "candidate" || real[0].Attrs["match"] != "workflow-name" || real[0].Attrs["name"] != "Capture review" {
		t.Fatalf("real edge = %+v, want documented_semantics candidate workflow-name match", real[0])
	}

	synthetic := edges(g, edgeCanTrigger, syntheticID, consumerID)
	if len(synthetic) != 1 {
		t.Fatalf("synthetic CAN_TRIGGER edges = %d, want 1", len(synthetic))
	}
	if synthetic[0].Attrs["prerequisite"] != "fork_pr_workflow_approval" {
		t.Fatalf("synthetic edge attrs = %v, want fork_pr_workflow_approval", synthetic[0].Attrs)
	}
	node := findNode(t, g, syntheticID)
	if node.Attrs["synthetic"] != "true" || node.Attrs["trust"] != "fork-definition-controlled" || node.Attrs["name"] != "Capture review" {
		t.Fatalf("synthetic node attrs = %v", node.Attrs)
	}

	missingSynthetic := edges(g, edgeCanTrigger, "workflow:fork-added:Not in this repository", "workflow:.github/workflows/missing.yml")
	if len(missingSynthetic) != 1 {
		t.Fatalf("missing workflow synthetic edges = %d, want 1", len(missingSynthetic))
	}
	if len(edges(g, edgeCanTrigger, "", "workflow:.github/workflows/missing.yml")) != 2 {
		t.Fatalf("missing workflow CAN_TRIGGER edges = %d, want trigger plus synthetic", len(edges(g, edgeCanTrigger, "", "workflow:.github/workflows/missing.yml")))
	}
}

func TestActionsDownloadSelectors(t *testing.T) {
	t.Run("same-run", func(t *testing.T) {
		g := buildOne(t, "same.yml", `name: Same
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/upload-artifact@v7
        with:
          name: dist
          path: out/
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/download-artifact@v8
        with:
          name: dist
`)
		to := "artifact:.github/workflows/same.yml#dist"
		edge := edgeTo(t, g, edgeDownloads, to)
		if edge.From != "step:.github/workflows/same.yml#deploy#0" {
			t.Fatalf("edge from = %q", edge.From)
		}
		if edge.Attrs["run_id_binding"] != "same-run" || edge.Confidence != "high" || edge.Attrs["content"] != "trusted-definition" {
			t.Fatalf("edge = %+v, want same-run/high/trusted-definition", edge)
		}
	})

	t.Run("other run-id", func(t *testing.T) {
		g := buildOne(t, "other.yml", `name: Other
on: push
jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/download-artifact@v8
        with:
          name: report
          run-id: "12345"
`)
		edge := edgeTo(t, g, edgeDownloads, "artifact:unknown#report")
		if edge.Confidence != "unknown" || edge.Attrs["run_id_binding"] != "other" || edge.Attrs["content"] != "unknown" {
			t.Fatalf("edge = %+v, want unknown/other/unknown", edge)
		}
		if len(unsupported(g, "dynamic-selector")) != 1 {
			t.Fatalf("dynamic-selector unsupported = %+v, want one", unsupported(g, "dynamic-selector"))
		}
	})

	t.Run("cross repository", func(t *testing.T) {
		g := buildOne(t, "cross.yml", `name: Cross
on: push
jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/download-artifact@v8
        with:
          name: report
          repository: another/repository
`)
		items := unsupported(g, "cross-repository")
		if len(items) != 1 || !strings.Contains(items[0].Reason, "§3.2") {
			t.Fatalf("cross-repository unsupported = %+v, want one mentioning §3.2", items)
		}
		if len(edges(g, edgeDownloads, "", "")) != 0 {
			t.Fatalf("DOWNLOADS edges = %d, want none for cross-repository", len(edges(g, edgeDownloads, "", "")))
		}
	})

	t.Run("upload in the same job is not a same-run candidate", func(t *testing.T) {
		g := buildOne(t, "onejob.yml", `name: One job
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/upload-artifact@v7
        with:
          name: dist
          path: out/
      - uses: actions/download-artifact@v8
        with:
          name: dist
`)
		if len(edges(g, edgeDownloads, "", "")) != 0 {
			t.Fatalf("DOWNLOADS edges = %d, want none within the same job", len(edges(g, edgeDownloads, "", "")))
		}
	})
}

func TestDawidd6Download(t *testing.T) {
	producer := parseWorkflow(t, "build-site.yml", `name: Build site
on: pull_request
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/upload-artifact@v7
        with:
          name: site
          path: _site/
`)

	tests := []struct {
		name              string
		source            string
		binding           string
		confidence        string
		allowForks        string
		unsupported       string
		unsupportedReason string
	}{
		{
			name: "latest-run",
			source: `name: Publish
on: push
jobs:
  publish:
    runs-on: ubuntu-latest
    steps:
      - uses: dawidd6/action-download-artifact@v27
        with:
          workflow: build-site.yml
          name: site
          allow_forks: true
`,
			binding:    "latest-run",
			confidence: "candidate",
			allowForks: "true",
		},
		{
			name: "workflow-run-event",
			source: `name: Publish
on: workflow_run
jobs:
  publish:
    runs-on: ubuntu-latest
    steps:
      - uses: dawidd6/action-download-artifact@v27
        with:
          workflow: Build site
          name: site
          run_id: ${{ github.event.workflow_run.id }}
`,
			binding:    "workflow-run-event",
			confidence: "high",
			allowForks: "false",
		},
		{
			name: "other run-id",
			source: `name: Publish
on: push
jobs:
  publish:
    runs-on: ubuntu-latest
    steps:
      - uses: dawidd6/action-download-artifact@v27
        with:
          workflow: build-site.yml
          name: site
          run_id: "12345"
`,
			binding:    "other",
			confidence: "candidate",
			allowForks: "false",
		},
		{
			name: "missing workflow",
			source: `name: Publish
on: push
jobs:
  publish:
    runs-on: ubuntu-latest
    steps:
      - uses: dawidd6/action-download-artifact@v27
        with:
          name: site
`,
			unsupported: "dynamic-selector",
		},
		{
			name: "dynamic workflow",
			source: `name: Publish
on: push
jobs:
  publish:
    runs-on: ubuntu-latest
    steps:
      - uses: dawidd6/action-download-artifact@v27
        with:
          workflow: ${{ github.event.repository.name }}
          name: site
`,
			unsupported: "dynamic-selector",
		},
		{
			name: "unknown literal workflow",
			source: `name: Publish
on: push
jobs:
  publish:
    runs-on: ubuntu-latest
    steps:
      - uses: dawidd6/action-download-artifact@v27
        with:
          workflow: missing.yml
          name: site
`,
			unsupported:       "dynamic-selector",
			unsupportedReason: "no workflow in the repository matches missing.yml",
		},
		{
			name: "cross repository",
			source: `name: Publish
on: push
jobs:
  publish:
    runs-on: ubuntu-latest
    steps:
      - uses: dawidd6/action-download-artifact@v27
        with:
          workflow: build-site.yml
          name: site
          repo: another/repository
`,
			unsupported: "cross-repository",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := Build([]model.Workflow{producer, parseWorkflow(t, "publish.yml", tt.source)}, config.Default().RepoProfile)
			if tt.unsupported != "" {
				items := unsupported(g, tt.unsupported)
				if len(items) == 0 {
					t.Fatalf("unsupported = %+v, want %s", g.Unsupported, tt.unsupported)
				}
				if tt.unsupportedReason != "" {
					found := false
					for _, item := range items {
						if item.Reason == tt.unsupportedReason {
							found = true
						}
					}
					if !found {
						t.Fatalf("unsupported = %+v, want reason %q", items, tt.unsupportedReason)
					}
				}
			}
			if tt.unsupported == "cross-repository" {
				if len(edges(g, edgeDownloads, "", "")) != 0 {
					t.Fatalf("DOWNLOADS edges = %d, want none", len(edges(g, edgeDownloads, "", "")))
				}
				return
			}
			if tt.unsupported == "dynamic-selector" {
				return
			}
			edge := edgeTo(t, g, edgeDownloads, "artifact:.github/workflows/build-site.yml#site")
			if edge.Attrs["run_id_binding"] != tt.binding || edge.Confidence != tt.confidence || edge.Attrs["allow_forks"] != tt.allowForks || edge.Attrs["content"] != "arbitrary" {
				t.Fatalf("edge attrs = %v confidence %q, want binding %q confidence %q allow_forks %q content arbitrary", edge.Attrs, edge.Confidence, tt.binding, tt.confidence, tt.allowForks)
			}
		})
	}
}

func TestNameMatching(t *testing.T) {
	g := buildOne(t, "match.yml", `name: Match
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/upload-artifact@v7
        with:
          name: pkg-linux
          path: out/
      - uses: actions/upload-artifact@v7
        with:
          name: pkg-mac
          path: out/
      - uses: actions/upload-artifact@v7
        with:
          name: other
          path: out/
  literal:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/download-artifact@v8
        with:
          name: pkg-linux
  pattern:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/download-artifact@v8
        with:
          pattern: pkg-*
  all:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/download-artifact@v8
`)

	literal := edges(g, edgeDownloads, "step:.github/workflows/match.yml#literal#0", "")
	if len(literal) != 1 || literal[0].To != "artifact:.github/workflows/match.yml#pkg-linux" || literal[0].EvidenceClass != "literal" {
		t.Fatalf("literal edges = %+v, want exactly pkg-linux with literal evidence", literal)
	}
	pattern := edges(g, edgeDownloads, "step:.github/workflows/match.yml#pattern#0", "")
	if len(pattern) != 2 {
		t.Fatalf("pattern edges = %+v, want pkg-linux and pkg-mac", pattern)
	}
	names := map[string]bool{}
	for _, edge := range pattern {
		names[edge.To] = true
	}
	if !names["artifact:.github/workflows/match.yml#pkg-linux"] || !names["artifact:.github/workflows/match.yml#pkg-mac"] {
		t.Fatalf("pattern edges = %+v, want both pkg artifacts", pattern)
	}
	all := edges(g, edgeDownloads, "step:.github/workflows/match.yml#all#0", "")
	if len(all) != 3 {
		t.Fatalf("all edges = %+v, want three artifacts", all)
	}
}

func TestAttackerChosenArtifact(t *testing.T) {
	producer := parseWorkflow(t, "capture.yml", `name: Capture review
on: pull_request
jobs:
  capture:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/upload-artifact@v7
        with:
          name: actual
          path: out/
`)
	consumer := parseWorkflow(t, "apply.yml", `name: Apply review
on:
  workflow_run:
    workflows: ["Capture review"]
    types: [completed]
jobs:
  apply:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/download-artifact@v8
        with:
          name: expected
          run-id: ${{ github.event.workflow_run.id }}
`)
	g := Build([]model.Workflow{producer, consumer}, config.Default().RepoProfile)
	edge := edgeTo(t, g, edgeDownloads, "artifact:.github/workflows/capture.yml#expected")
	if edge.EvidenceClass != "documented_semantics" || edge.Attrs["name_match"] != "attacker-chosen" || edge.Attrs["content"] != "arbitrary" || edge.Confidence != "high" {
		t.Fatalf("attacker-chosen edge = %+v attrs %v, want documented_semantics/attacker-chosen/arbitrary/high", edge, edge.Attrs)
	}
}

func TestDynamicUploadName(t *testing.T) {
	t.Run("same run", func(t *testing.T) {
		g := buildOne(t, "dynamic.yml", `name: Dynamic
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/upload-artifact@v7
        with:
          name: ${{ github.run_id }}
          path: out/
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/download-artifact@v8
        with:
          name: dist
`)
		edge := edgeTo(t, g, edgeDownloads, "artifact:.github/workflows/dynamic.yml#*")
		if edge.Confidence != "unknown" || edge.Attrs["name_match"] != "dynamic-upload-name" || edge.Attrs["content"] != "trusted-definition" {
			t.Fatalf("edge = %+v attrs %v, want unknown/dynamic-upload-name/trusted-definition", edge, edge.Attrs)
		}
	})

	t.Run("workflow run producer", func(t *testing.T) {
		producer := parseWorkflow(t, "producer.yml", `name: Producer
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/upload-artifact@v7
        with:
          name: ${{ github.run_id }}
          path: out/
`)
		consumer := parseWorkflow(t, "consumer.yml", `name: Consumer
on:
  workflow_run:
    workflows: ["Producer"]
    types: [completed]
jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/download-artifact@v8
        with:
          name: dist
          run-id: ${{ github.event.workflow_run.id }}
`)
		g := Build([]model.Workflow{producer, consumer}, config.Default().RepoProfile)
		edge := edgeTo(t, g, edgeDownloads, "artifact:.github/workflows/producer.yml#*")
		if edge.Confidence != "unknown" || edge.Attrs["name_match"] != "dynamic-upload-name" || edge.Attrs["run_id_binding"] != "workflow-run-event" {
			t.Fatalf("edge = %+v attrs %v, want unknown/dynamic-upload-name/workflow-run-event", edge, edge.Attrs)
		}
	})
}

func TestDynamicDownloadSelector(t *testing.T) {
	source := `name: Dynamic selector
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/upload-artifact@v7
        with:
          name: alpha
          path: out/
      - uses: actions/upload-artifact@v7
        with:
          name: beta
          path: out/
  download:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/download-artifact@v8
        with:
          %s: ${{ github.run_id }}
`
	for _, input := range []string{"name", "pattern"} {
		t.Run(input, func(t *testing.T) {
			g := buildOne(t, "selector.yml", strings.Replace(source, "%s", input, 1))
			step := "step:.github/workflows/selector.yml#download#0"
			downloads := edges(g, edgeDownloads, step, "")
			if len(downloads) != 2 {
				t.Fatalf("DOWNLOADS edges = %d, want two candidate artifacts: %+v", len(downloads), downloads)
			}
			for _, edge := range downloads {
				if edge.Confidence != "unknown" || edge.Attrs["name_match"] != "dynamic-selector" {
					t.Errorf("edge = %+v attrs %v, want unknown/dynamic-selector", edge, edge.Attrs)
				}
			}
		})
	}
}

func TestExtraction(t *testing.T) {
	g := buildOne(t, "extract.yml", `name: Extract
on: push
jobs:
  extract:
    runs-on: ubuntu-latest
    steps:
      - name: default actions
        uses: actions/download-artifact@v8
      - name: empty actions
        uses: actions/download-artifact@v8
        with:
          path: ""
      - name: relative
        uses: actions/download-artifact@v8
        with:
          path: downloads/../out
      - name: runner temp
        uses: actions/download-artifact@v8
        with:
          path: ${{ runner.temp }}/review
      - name: github workspace
        uses: actions/download-artifact@v8
        with:
          path: ${{ github.workspace }}/build
      - name: absolute
        uses: actions/download-artifact@v8
        with:
          path: /tmp/artifacts/../out
      - name: dynamic
        uses: actions/download-artifact@v8
        with:
          path: ${{ github.event.inputs.dir }}
      - name: combined expression
        uses: actions/download-artifact@v8
        with:
          path: ${{ runner.temp }}/${{ github.sha }}
      - name: escape
        uses: actions/download-artifact@v8
        with:
          path: ../outside
      - name: runner temp escape
        uses: actions/download-artifact@v8
        with:
          path: ${{ runner.temp }}/../outside
      - name: workspace escape
        uses: actions/download-artifact@v8
        with:
          path: ${{ github.workspace }}/../outside
  dawidd:
    runs-on: ubuntu-latest
    steps:
      - name: dawidd default
        uses: dawidd6/action-download-artifact@v27
        with:
          workflow: extract.yml
      - name: dawidd merge
        uses: dawidd6/action-download-artifact@v27
        with:
          workflow: extract.yml
          merge_multiple: true
`)

	tests := []struct {
		step   string
		dir    string
		layout string
	}{
		{"extract#0", "$GITHUB_WORKSPACE", "per-artifact-subdir"},
		{"extract#1", "$GITHUB_WORKSPACE", "per-artifact-subdir"},
		{"extract#2", "$GITHUB_WORKSPACE/out", "per-artifact-subdir"},
		{"extract#3", "$RUNNER_TEMP/review", "per-artifact-subdir"},
		{"extract#4", "$GITHUB_WORKSPACE/build", "per-artifact-subdir"},
		{"extract#5", "/tmp/out", "per-artifact-subdir"},
		{"extract#6", "UNKNOWN", "per-artifact-subdir"},
		{"extract#7", "UNKNOWN", "per-artifact-subdir"},
		{"extract#8", "UNKNOWN", "per-artifact-subdir"},
		{"extract#9", "UNKNOWN", "per-artifact-subdir"},
		{"extract#10", "UNKNOWN", "per-artifact-subdir"},
		{"dawidd#0", "$GITHUB_WORKSPACE", "per-artifact-subdir"},
		{"dawidd#1", "$GITHUB_WORKSPACE", "flat"},
	}
	for _, tt := range tests {
		stepID := "step:.github/workflows/extract.yml#" + tt.step
		node := fileNodeForStep(t, g, stepID)
		if node.Attrs["dir"] != tt.dir || node.Attrs["layout"] != tt.layout {
			t.Errorf("%s attrs = %v, want dir %q layout %q", tt.step, node.Attrs, tt.dir, tt.layout)
		}
	}

	items := unsupported(g, "dynamic-selector")
	if len(items) != 5 {
		t.Fatalf("dynamic-selector unsupported = %+v, want two dynamic and three escaping paths", items)
	}
	reasons := map[string]string{
		"step:.github/workflows/extract.yml#extract#6":  "dynamic extraction path",
		"step:.github/workflows/extract.yml#extract#7":  "dynamic extraction path",
		"step:.github/workflows/extract.yml#extract#8":  "extraction path escapes the workspace",
		"step:.github/workflows/extract.yml#extract#9":  "extraction path escapes the workspace",
		"step:.github/workflows/extract.yml#extract#10": "extraction path escapes the workspace",
	}
	for step, reason := range reasons {
		node := findNode(t, g, step)
		found := false
		for _, item := range items {
			if item.Reason == reason && node.Pos != nil && item.Pos == *node.Pos {
				found = true
			}
		}
		if !found {
			t.Fatalf("dynamic-selector unsupported = %+v, want %q on %s", items, reason, step)
		}
	}
}

func TestOpaqueDownloader(t *testing.T) {
	tests := []struct {
		name   string
		uses   string
		reason string
	}{
		{name: "unknown action", uses: "some/unknown-download-artifact@v1", reason: "no-summary"},
		{name: "unknown artifact action", uses: "some/unknown-artifact@v1", reason: "no-summary"},
		{name: "unsupported version", uses: "actions/download-artifact@v1", reason: "version-out-of-range"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := buildOne(t, "opaque.yml", `name: Opaque
on: push
jobs:
  download:
    runs-on: ubuntu-latest
    steps:
      - uses: `+tt.uses+`
`)
			items := unsupported(g, "opaque-downloader")
			if len(items) != 1 || !strings.Contains(items[0].Reason, tt.reason) {
				t.Fatalf("opaque-downloader unsupported = %+v, want reason containing %q", items, tt.reason)
			}
		})
	}
}

func TestInlinedCode(t *testing.T) {
	t.Run("local composite action", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, ".github/workflows/main.yml", `name: Main
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: ./.github/actions/upload
`)
		writeFile(t, root, ".github/actions/upload/action.yml", `name: Upload
runs:
  using: composite
  steps:
    - uses: actions/upload-artifact@v7
      with:
        name: local
        path: out/
`)
		workflows := loadWorkspace(t, root)
		g := Build(workflows, config.Default().RepoProfile)

		step := "step:.github/workflows/main.yml#build#0/0"
		findNode(t, g, step)
		edge := edgeTo(t, g, edgeProduces, "artifact:.github/workflows/main.yml#local")
		if edge.From != step {
			t.Fatalf("PRODUCES from = %q, want %q", edge.From, step)
		}
	})

	t.Run("callee workflow", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, ".github/workflows/main.yml", `name: Main
on: push
jobs:
  call:
    uses: ./.github/workflows/reusable.yml
`)
		writeFile(t, root, ".github/workflows/reusable.yml", `name: Reusable
on: workflow_call
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/upload-artifact@v7
        with:
          name: reusable
          path: out/
`)
		workflows := loadWorkspace(t, root)
		var main model.Workflow
		for _, wf := range workflows {
			if wf.Path == ".github/workflows/main.yml" {
				main = wf
			}
		}
		g := Build([]model.Workflow{main}, config.Default().RepoProfile)

		jobID := "job:.github/workflows/main.yml#call/.github/workflows/reusable.yml#build"
		job := findNode(t, g, jobID)
		if job.Attrs["token_source"] == "" || job.Attrs["cache_write"] == "" {
			t.Fatalf("callee job attrs = %v, want token_source and cache_write", job.Attrs)
		}
		step := "step:.github/workflows/main.yml#call/.github/workflows/reusable.yml#build#0"
		findNode(t, g, step)
		edge := edgeTo(t, g, edgeProduces, "artifact:.github/workflows/main.yml#reusable")
		if edge.From != step {
			t.Fatalf("PRODUCES from = %q, want %q", edge.From, step)
		}
	})
}

func TestDeterminism(t *testing.T) {
	workflows := loadWorkspace(t, filepath.Join("..", "..", "testdata", "fixtures", "a-spire"))
	first, err := json.MarshalIndent(Build(workflows, config.Default().RepoProfile), "", "  ")
	if err != nil {
		t.Fatalf("marshal first graph: %v", err)
	}
	second, err := json.MarshalIndent(Build(workflows, config.Default().RepoProfile), "", "  ")
	if err != nil {
		t.Fatalf("marshal second graph: %v", err)
	}
	if string(first) != string(second) {
		t.Fatalf("building twice produced different JSON\n--- first ---\n%s\n--- second ---\n%s", first, second)
	}
}

func writeFile(t *testing.T, root, name, content string) {
	t.Helper()
	file := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatalf("create %s: %v", filepath.Dir(file), err)
	}
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", file, err)
	}
}

func loadWorkspace(t *testing.T, root string) []model.Workflow {
	t.Helper()
	ws, err := workspace.Open(root, workspace.DefaultLimits)
	if err != nil {
		t.Fatalf("open %s: %v", root, err)
	}
	files, err := ws.Workflows()
	if err != nil {
		t.Fatalf("%s: list workflows: %v", root, err)
	}
	workflows := make([]model.Workflow, 0, len(files))
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
		workflows = append(workflows, *wf)
	}
	if err := ws.Close(); err != nil {
		t.Fatalf("%s: close workspace: %v", root, err)
	}
	return workflows
}
