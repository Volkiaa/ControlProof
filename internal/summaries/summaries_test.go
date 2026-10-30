package summaries

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Volkiaa/ControlProof/internal/model"
)

const testPath = ".github/workflows/wf.yml"

func usesStep(uses string, with ...model.KV) model.Step {
	return model.Step{
		Uses: &model.Str{Value: uses, Pos: model.Pos{File: testPath, Line: 10, Col: 14}},
		With: with,
		Pos:  model.Pos{File: testPath, Line: 10, Col: 9},
	}
}

func literalInput(key, value string, line, col int) model.KV {
	return model.KV{Key: key, Value: model.Str{Value: value, Pos: model.Pos{File: testPath, Line: line, Col: col}}}
}

func dynamicInput(key, value, raw string, line, col int) model.KV {
	return model.KV{Key: key, Value: model.Str{
		Value: value,
		Pos:   model.Pos{File: testPath, Line: line, Col: col},
		Exprs: []model.Expr{{Raw: raw, Pos: model.Pos{File: testPath, Line: line, Col: col}}},
	}}
}

func TestRecognizeUses(t *testing.T) {
	tests := []struct {
		name   string
		uses   string
		opaque bool
		reason string
		action string
		kind   string
		major  int
	}{
		{name: "major tag", uses: "actions/checkout@v4", action: "actions/checkout", kind: "checkout", major: 4},
		{name: "major.minor tag", uses: "actions/checkout@v4.2", action: "actions/checkout", kind: "checkout", major: 4},
		{name: "major.minor.patch tag", uses: "actions/checkout@v4.2.1", action: "actions/checkout", kind: "checkout", major: 4},
		{name: "case-insensitive owner and repo", uses: "ACTIONS/Checkout@v7", action: "actions/checkout", kind: "checkout", major: 7},
		{name: "download artifact", uses: "actions/download-artifact@v8", action: "actions/download-artifact", kind: "download-artifact", major: 8},
		{name: "dawidd6", uses: "dawidd6/action-download-artifact@v27", action: "dawidd6/action-download-artifact", kind: "dawidd6-download-artifact", major: 27},
		{
			name:   "commit sha",
			uses:   "actions/checkout@0123456789abcdef0123456789abcdef01234567",
			opaque: true, reason: reasonUnversionedRef, action: "actions/checkout",
		},
		{name: "branch", uses: "actions/checkout@main", opaque: true, reason: reasonUnversionedRef, action: "actions/checkout"},
		{name: "no ref", uses: "actions/checkout", opaque: true, reason: reasonUnversionedRef, action: "actions/checkout"},
		{name: "four part tag", uses: "actions/checkout@v4.2.1.5", opaque: true, reason: reasonUnversionedRef, action: "actions/checkout"},
		{name: "non numeric tag", uses: "actions/checkout@v4x", opaque: true, reason: reasonUnversionedRef, action: "actions/checkout"},
		{name: "uppercase v", uses: "actions/checkout@V4", opaque: true, reason: reasonUnversionedRef, action: "actions/checkout"},
		{name: "local action", uses: "./.github/actions/setup", opaque: true, reason: reasonLocal},
		{name: "docker action", uses: "docker://alpine:3.19", opaque: true, reason: reasonDocker},
		{name: "subpath action", uses: "actions/upload-artifact/merge@v4", opaque: true, reason: reasonNoSummary},
		{name: "unknown action", uses: "some/unknown@v1", opaque: true, reason: reasonNoSummary, action: "some/unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Recognize(usesStep(tt.uses))
			if got.Uses != tt.uses {
				t.Errorf("uses = %q, want %q", got.Uses, tt.uses)
			}
			if got.Opaque != tt.opaque {
				t.Errorf("opaque = %t, want %t", got.Opaque, tt.opaque)
			}
			if got.OpaqueReason != tt.reason {
				t.Errorf("opaque_reason = %q, want %q", got.OpaqueReason, tt.reason)
			}
			if got.Action != tt.action {
				t.Errorf("action = %q, want %q", got.Action, tt.action)
			}
			if got.Kind != tt.kind {
				t.Errorf("kind = %q, want %q", got.Kind, tt.kind)
			}
			if got.Major != tt.major {
				t.Errorf("major = %d, want %d", got.Major, tt.major)
			}
			if tt.opaque && got.Summary != nil {
				t.Errorf("summary = %+v, want none for opaque recognition", got.Summary)
			}
			if !tt.opaque {
				if got.Summary == nil {
					t.Fatal("summary = nil, want a summary")
				}
				if got.Summary.Action != tt.action || got.Summary.Kind != tt.kind {
					t.Errorf("summary = %+v, want action %q kind %q", got.Summary, tt.action, tt.kind)
				}
			}
		})
	}
}

func TestRecognizeWithoutUses(t *testing.T) {
	if got := Recognize(model.Step{}); !reflect.DeepEqual(got, Recognition{}) {
		t.Errorf("Recognize() = %+v, want zero Recognition", got)
	}
	run := model.Step{Run: &model.Str{Value: "echo hi", Pos: model.Pos{File: testPath, Line: 5, Col: 9}}}
	if got := Recognize(run); !reflect.DeepEqual(got, Recognition{}) {
		t.Errorf("Recognize(run step) = %+v, want zero Recognition", got)
	}
}

func TestVersionRange(t *testing.T) {
	tests := []struct {
		uses   string
		opaque bool
	}{
		{uses: "actions/upload-artifact@v4"},
		{uses: "actions/upload-artifact@v7"},
		{uses: "actions/upload-artifact@v3", opaque: true},
		{uses: "actions/upload-artifact@v8", opaque: true},
		{uses: "actions/download-artifact@v4"},
		{uses: "actions/download-artifact@v8"},
		{uses: "actions/download-artifact@v3", opaque: true},
		{uses: "actions/download-artifact@v9", opaque: true},
		{uses: "actions/checkout@v4"},
		{uses: "actions/checkout@v7"},
		{uses: "actions/checkout@v3", opaque: true},
		{uses: "actions/checkout@v8", opaque: true},
		{uses: "actions/github-script@v6"},
		{uses: "actions/github-script@v9"},
		{uses: "actions/github-script@v5", opaque: true},
		{uses: "actions/github-script@v10", opaque: true},
		{uses: "dawidd6/action-download-artifact@v2"},
		{uses: "dawidd6/action-download-artifact@v27"},
		{uses: "dawidd6/action-download-artifact@v1", opaque: true},
		{uses: "dawidd6/action-download-artifact@v28", opaque: true},
	}

	for _, tt := range tests {
		t.Run(tt.uses, func(t *testing.T) {
			step := usesStep(tt.uses, literalInput("name", "x", 11, 15))
			got := Recognize(step)
			if got.Opaque != tt.opaque {
				t.Fatalf("opaque = %t, want %t (reason %q)", got.Opaque, tt.opaque, got.OpaqueReason)
			}
			if tt.opaque {
				if got.OpaqueReason != reasonVersionOutOfRange {
					t.Errorf("opaque_reason = %q, want %q", got.OpaqueReason, reasonVersionOutOfRange)
				}
				if got.Inputs != nil {
					t.Errorf("inputs = %+v, want none for opaque recognition", got.Inputs)
				}
				if got.Summary != nil {
					t.Errorf("summary = %+v, want none for opaque recognition", got.Summary)
				}
			}
		})
	}
}

func TestOpaqueUnversionedRefHasNoInputs(t *testing.T) {
	step := usesStep("actions/download-artifact@0123456789abcdef0123456789abcdef01234567", literalInput("name", "x", 11, 15))
	got := Recognize(step)
	if !got.Opaque || got.OpaqueReason != reasonUnversionedRef {
		t.Fatalf("recognized = %+v, want opaque %q", got, reasonUnversionedRef)
	}
	if got.Inputs != nil || got.Summary != nil {
		t.Errorf("inputs = %+v summary = %+v, want neither", got.Inputs, got.Summary)
	}
}

func TestCaptureInputs(t *testing.T) {
	step := usesStep("actions/checkout@v4",
		dynamicInput("ref", "${{ github.head_ref }}", "github.head_ref", 12, 15),
		literalInput("persist-credentials", "false", 13, 15),
		literalInput("bogus", "ignored", 14, 15),
	)

	got := Recognize(step)
	if got.Opaque {
		t.Fatalf("recognized = %+v, want non-opaque", got)
	}
	want := map[string]Input{
		"ref":                 {Value: "${{ github.head_ref }}", Pos: model.Pos{File: testPath, Line: 12, Col: 15}, Dynamic: true},
		"persist-credentials": {Value: "false", Pos: model.Pos{File: testPath, Line: 13, Col: 15}},
		"repository":          {Value: "${{ github.repository }}", Pos: step.Pos, Default: true},
	}
	if !reflect.DeepEqual(got.Inputs, want) {
		t.Errorf("inputs =\n%+v\nwant\n%+v", got.Inputs, want)
	}
}

func TestCaptureInputsAllSet(t *testing.T) {
	step := usesStep("actions/upload-artifact@v7",
		literalInput("name", "build", 12, 15),
		dynamicInput("path", "dist/${{ github.sha }}", "github.sha", 13, 15),
		literalInput("include-hidden-files", "true", 14, 15),
		literalInput("unknown", "ignored", 15, 15),
	)

	got := Recognize(step)
	want := map[string]Input{
		"name":                 {Value: "build", Pos: model.Pos{File: testPath, Line: 12, Col: 15}},
		"path":                 {Value: "dist/${{ github.sha }}", Pos: model.Pos{File: testPath, Line: 13, Col: 15}, Dynamic: true},
		"include-hidden-files": {Value: "true", Pos: model.Pos{File: testPath, Line: 14, Col: 15}},
	}
	if !reflect.DeepEqual(got.Inputs, want) {
		t.Errorf("inputs =\n%+v\nwant\n%+v", got.Inputs, want)
	}
}

func TestCaptureInputsDefaults(t *testing.T) {
	tests := []struct {
		name  string
		uses  string
		key   string
		value string
	}{
		{name: "upload name", uses: "actions/upload-artifact@v4", key: "name", value: "artifact"},
		{name: "upload hidden files", uses: "actions/upload-artifact@v4", key: "include-hidden-files", value: "false"},
		{name: "download merge-multiple", uses: "actions/download-artifact@v8", key: "merge-multiple", value: "false"},
		{name: "download path", uses: "actions/download-artifact@v8", key: "path", value: ""},
		{name: "download run-id", uses: "actions/download-artifact@v8", key: "run-id", value: "${{ github.run_id }}"},
		{name: "download repository", uses: "actions/download-artifact@v8", key: "repository", value: "${{ github.repository }}"},
		{name: "dawidd6 path", uses: "dawidd6/action-download-artifact@v27", key: "path", value: "./"},
		{name: "dawidd6 conclusion", uses: "dawidd6/action-download-artifact@v27", key: "workflow_conclusion", value: "success"},
		{name: "checkout repository", uses: "actions/checkout@v4", key: "repository", value: "${{ github.repository }}"},
		{name: "checkout credentials", uses: "actions/checkout@v4", key: "persist-credentials", value: "true"},
		{name: "github-script encoding", uses: "actions/github-script@v6", key: "result-encoding", value: "json"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			step := usesStep(tt.uses)
			got := Recognize(step)
			input, ok := got.Inputs[tt.key]
			if !ok {
				t.Fatalf("%s is not captured, inputs = %+v", tt.key, got.Inputs)
			}
			if input.Value != tt.value {
				t.Errorf("%s value = %q, want %q", tt.key, input.Value, tt.value)
			}
			if !input.Default {
				t.Errorf("%s default = false, want true", tt.key)
			}
			if input.Pos != step.Pos {
				t.Errorf("%s pos = %+v, want step pos %+v", tt.key, input.Pos, step.Pos)
			}
		})
	}
}

func TestInputsWithoutDefaultAreOmitted(t *testing.T) {
	got := Recognize(usesStep("actions/checkout@v4"))
	for _, key := range []string{"ref", "path", "sparse-checkout", "token"} {
		if _, ok := got.Inputs[key]; ok {
			t.Errorf("%s is captured, want it omitted: %+v", key, got.Inputs)
		}
	}
}

func TestAllowForksDefault(t *testing.T) {
	tests := []struct {
		uses  string
		value string
	}{
		{uses: "dawidd6/action-download-artifact@v2", value: "true"},
		{uses: "dawidd6/action-download-artifact@v3", value: "true"},
		{uses: "dawidd6/action-download-artifact@v4", value: "false"},
		{uses: "dawidd6/action-download-artifact@v27", value: "false"},
	}

	for _, tt := range tests {
		t.Run(tt.uses, func(t *testing.T) {
			got := Recognize(usesStep(tt.uses))
			input, ok := got.Inputs["allow_forks"]
			if !ok {
				t.Fatalf("allow_forks is not captured, inputs = %+v", got.Inputs)
			}
			if input.Value != tt.value {
				t.Errorf("allow_forks = %q, want %q", input.Value, tt.value)
			}
			if !input.Default {
				t.Error("allow_forks default = false, want true")
			}
		})
	}
}

func TestAll(t *testing.T) {
	first := All()
	second := All()
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("All() is not deterministic:\n%+v\n%+v", first, second)
	}
	if len(first) != 5 {
		t.Errorf("len(All()) = %d, want 5", len(first))
	}

	for i, summary := range first {
		if summary.Action != strings.ToLower(summary.Action) {
			t.Errorf("%s is not lowercase", summary.Action)
		}
		if i > 0 && first[i-1].Action >= summary.Action {
			t.Errorf("All() is not sorted by action: %q before %q", first[i-1].Action, summary.Action)
		}
		if summary.MinMajor > summary.MaxMajor {
			t.Errorf("%s: min_major %d > max_major %d", summary.Action, summary.MinMajor, summary.MaxMajor)
		}
		if summary.Provenance == "" {
			t.Errorf("%s has empty provenance", summary.Action)
		}
		if summary.Kind == "" {
			t.Errorf("%s has empty kind", summary.Action)
		}
		if len(captures[summary.Kind]) == 0 {
			t.Errorf("%s (%s) has no captured inputs", summary.Action, summary.Kind)
		}
	}
}
