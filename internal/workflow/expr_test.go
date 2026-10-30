package workflow

import (
	"reflect"
	"testing"

	"github.com/Volkiaa/ControlProof/internal/workspace"
)

func TestExpressionExtraction(t *testing.T) {
	tests := []struct {
		name     string
		run      string
		wantRaw  []string
		wantRefs [][]string
	}{
		{
			name:     "closing braces inside a string literal",
			run:      "echo ${{ contains('}}', github.event.pull_request.title) }}",
			wantRaw:  []string{"contains('}}', github.event.pull_request.title)"},
			wantRefs: [][]string{{"github.event.pull_request.title"}},
		},
		{
			name:     "dynamic index expression",
			run:      "echo ${{ env[github.event.pull_request.title] }}",
			wantRaw:  []string{"env[github.event.pull_request.title]"},
			wantRefs: [][]string{{"env.*", "github.event.pull_request.title"}},
		},
		{
			name:     "two expressions in one string",
			run:      "echo ${{ github.head_ref }} ${{ steps.a.outputs.b }}",
			wantRaw:  []string{"github.head_ref", "steps.a.outputs.b"},
			wantRefs: [][]string{{"github.head_ref"}, {"steps.a.outputs.b"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := "on: push\njobs:\n  j:\n    runs-on: ubuntu-latest\n    steps:\n      - run: \"" + tt.run + "\"\n"
			wf, err := Parse(workspace.File{Path: "w.yml", Content: []byte(content)}, DefaultLimits)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			exprs := wf.Jobs[0].Steps[0].Run.Exprs
			var raws []string
			var refs [][]string
			for _, e := range exprs {
				if e.Err != "" {
					t.Fatalf("expression %q has error %q", e.Raw, e.Err)
				}
				raws = append(raws, e.Raw)
				refs = append(refs, e.Refs)
			}
			if !reflect.DeepEqual(raws, tt.wantRaw) {
				t.Errorf("raw = %q, want %q", raws, tt.wantRaw)
			}
			if !reflect.DeepEqual(refs, tt.wantRefs) {
				t.Errorf("refs = %q, want %q", refs, tt.wantRefs)
			}
		})
	}
}
