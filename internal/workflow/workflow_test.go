package workflow

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Volkiaa/ControlProof/internal/workspace"
	"github.com/rhysd/actionlint"
)

const validWorkflow = `on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
`

func TestParseYAMLSyntaxError(t *testing.T) {
	path := ".github/workflows/broken.yml"
	wf, err := Parse(workspace.File{Path: path, Content: []byte("on: [push\njobs: {}\n")}, DefaultLimits)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("Parse() error = %v, want ErrInvalid", err)
	}
	if wf == nil {
		t.Fatal("Parse() workflow = nil, want an IR with diagnostics")
	}
	if len(wf.Diagnostics) == 0 {
		t.Fatal("Parse() diagnostics = empty, want a YAML diagnostic")
	}
	if wf.Diagnostics[0].Kind != "syntax-check" {
		t.Fatalf("diag kind = %q, want syntax-check", wf.Diagnostics[0].Kind)
	}
	if wf.Diagnostics[0].File != path {
		t.Fatalf("diag file = %q, want %q", wf.Diagnostics[0].File, path)
	}
}

func TestParseUnknownTopLevelKey(t *testing.T) {
	path := ".github/workflows/unknown.yml"
	content := `name: unknown
on: push
bogus: true
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
`
	wf, err := Parse(workspace.File{Path: path, Content: []byte(content)}, DefaultLimits)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("Parse() error = %v, want ErrInvalid", err)
	}
	if wf == nil {
		t.Fatal("Parse() workflow = nil, want an IR with diagnostics")
	}
	if len(wf.Diagnostics) == 0 {
		t.Fatal("Parse() diagnostics = empty, want an actionlint diagnostic")
	}
	if len(wf.Jobs) != 1 {
		t.Fatalf("Parse() jobs = %d, want the parsed job alongside diagnostics", len(wf.Jobs))
	}
	if wf.Diagnostics[0].File != path {
		t.Fatalf("diag file = %q, want %q", wf.Diagnostics[0].File, path)
	}
}

func TestParseRecursiveAlias(t *testing.T) {
	content := `on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps: &steps
      - run: echo hi
      - *steps
`
	_, err := Parse(workspace.File{Path: ".github/workflows/recursive.yml", Content: []byte(content)}, DefaultLimits)
	if !errors.Is(err, ErrInvalid) && !errors.Is(err, ErrTooComplex) {
		t.Fatalf("Parse() error = %v, want ErrInvalid or ErrTooComplex", err)
	}
}

func TestParseBillionLaughs(t *testing.T) {
	content := billionLaughs(9, 9)
	start := time.Now()
	_, err := Parse(workspace.File{Path: ".github/workflows/bomb.yml", Content: content}, DefaultLimits)
	elapsed := time.Since(start)
	if !errors.Is(err, ErrTooComplex) {
		t.Fatalf("Parse() error = %v, want ErrTooComplex", err)
	}
	if elapsed >= time.Second {
		t.Fatalf("Parse() took %s, want under 1s", elapsed)
	}
}

func TestParseTooLarge(t *testing.T) {
	content := make([]byte, 10<<20)
	_, err := Parse(workspace.File{Path: ".github/workflows/huge.yml", Content: content}, DefaultLimits)
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("Parse() error = %v, want ErrTooLarge", err)
	}
}

func TestParseDeepNesting(t *testing.T) {
	nested := strings.Repeat("[", 200) + "0" + strings.Repeat("]", 200)
	content := "on: push\njobs: {}\nx: " + nested + "\n"
	_, err := Parse(workspace.File{Path: ".github/workflows/deep.yml", Content: []byte(content)}, DefaultLimits)
	if !errors.Is(err, ErrTooComplex) {
		t.Fatalf("Parse() error = %v, want ErrTooComplex", err)
	}
}

func TestParsePanic(t *testing.T) {
	original := parseYAML
	t.Cleanup(func() { parseYAML = original })
	parseYAML = func([]byte) (*actionlint.Workflow, []*actionlint.Error) {
		panic("boom")
	}

	_, err := Parse(workspace.File{Path: ".github/workflows/panic.yml", Content: []byte(validWorkflow)}, DefaultLimits)
	if !errors.Is(err, ErrInternal) {
		t.Fatalf("Parse() error = %v, want ErrInternal", err)
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("Parse() error %q does not include the panic value", err)
	}
}

func TestParseTimeout(t *testing.T) {
	original := parseYAML
	t.Cleanup(func() { parseYAML = original })
	finished := make(chan struct{})
	parseYAML = func([]byte) (*actionlint.Workflow, []*actionlint.Error) {
		time.Sleep(200 * time.Millisecond)
		close(finished)
		return nil, nil
	}

	limits := DefaultLimits
	limits.Timeout = 50 * time.Millisecond
	_, err := Parse(workspace.File{Path: ".github/workflows/slow.yml", Content: []byte(validWorkflow)}, limits)
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("Parse() error = %v, want ErrTimeout", err)
	}
	<-finished
}

func billionLaughs(levels, width int) []byte {
	var b strings.Builder
	b.WriteString("on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    strategy:\n      matrix:\n")
	previous := ""
	for level := 0; level < levels; level++ {
		name := fmt.Sprintf("laugh%d", level)
		fmt.Fprintf(&b, "        %s: &%s [", name, name)
		for i := 0; i < width; i++ {
			if i > 0 {
				b.WriteString(", ")
			}
			if previous == "" {
				b.WriteString("x")
			} else {
				b.WriteString("*" + previous)
			}
		}
		b.WriteString("]\n")
		previous = name
	}
	b.WriteString("    steps:\n      - run: echo hi\n")
	return []byte(b.String())
}
