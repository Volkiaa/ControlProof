package workflow

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Volkiaa/ControlProof/internal/model"
	"github.com/Volkiaa/ControlProof/internal/workspace"
	"github.com/rhysd/actionlint"
)

type Limits struct {
	MaxBytes int
	MaxNodes int
	MaxDepth int
	Timeout  time.Duration
}

var DefaultLimits = Limits{
	MaxBytes: 1 << 20,
	MaxNodes: 100_000,
	MaxDepth: 64,
	Timeout:  5 * time.Second,
}

var (
	ErrTooLarge   = errors.New("workflow too large")
	ErrTooComplex = errors.New("workflow too complex")
	ErrTimeout    = errors.New("workflow parse timed out")
	ErrInvalid    = errors.New("invalid workflow")
	ErrInternal   = errors.New("internal workflow parser error")
)

type parseFunc func([]byte) (*actionlint.Workflow, []*actionlint.Error)

var parseYAML parseFunc = actionlint.Parse

type actionlintResult struct {
	workflow  *actionlint.Workflow
	errors    []*actionlint.Error
	panicName any
	timedOut  bool
}

func Parse(f workspace.File, limits Limits) (*model.Workflow, error) {
	if len(f.Content) > limits.MaxBytes {
		return nil, fmt.Errorf("%s: size %d exceeds limit %d: %w", f.Path, len(f.Content), limits.MaxBytes, ErrTooLarge)
	}

	if err := checkComplexity(f.Content, limits); err != nil {
		var syntax *yamlSyntaxError
		if errors.As(err, &syntax) {
			wf := &model.Workflow{Path: f.Path, Diagnostics: yamlDiagnostics(f.Path, syntax.err)}
			return wf, fmt.Errorf("%s: %w: %v", f.Path, ErrInvalid, syntax.err)
		}
		return nil, fmt.Errorf("%s: %w", f.Path, err)
	}

	result := runActionlint(f.Content, limits.Timeout, parseYAML)
	if result.timedOut {
		return nil, fmt.Errorf("%s: %w after %s", f.Path, ErrTimeout, limits.Timeout)
	}
	if result.panicName != nil {
		return nil, fmt.Errorf("%s: %w: panic: %v", f.Path, ErrInternal, result.panicName)
	}

	var diagnostics []model.Diag
	for _, e := range result.errors {
		diagnostics = append(diagnostics, model.Diag{
			Pos:     model.Pos{File: f.Path, Line: e.Line, Col: e.Column},
			Kind:    e.Kind,
			Message: e.Message,
		})
	}
	sort.SliceStable(diagnostics, func(i, j int) bool {
		if diagnostics[i].Line != diagnostics[j].Line {
			return diagnostics[i].Line < diagnostics[j].Line
		}
		if diagnostics[i].Col != diagnostics[j].Col {
			return diagnostics[i].Col < diagnostics[j].Col
		}
		if diagnostics[i].Kind != diagnostics[j].Kind {
			return diagnostics[i].Kind < diagnostics[j].Kind
		}
		return diagnostics[i].Message < diagnostics[j].Message
	})
	messages := make([]string, 0, len(diagnostics))
	for _, diag := range diagnostics {
		messages = append(messages, diag.Message)
	}

	if result.workflow == nil {
		wf := &model.Workflow{Path: f.Path, Diagnostics: diagnostics}
		if len(messages) > 0 {
			return wf, fmt.Errorf("%s: %w: %s", f.Path, ErrInvalid, strings.Join(messages, "; "))
		}
		return wf, fmt.Errorf("%s: %w: actionlint returned no workflow", f.Path, ErrInternal)
	}

	wf := convert(f.Path, result.workflow)
	wf.Diagnostics = diagnostics
	if len(messages) > 0 {
		return wf, fmt.Errorf("%s: %w: %s", f.Path, ErrInvalid, strings.Join(messages, "; "))
	}
	return wf, nil
}

func runActionlint(content []byte, timeout time.Duration, parse parseFunc) actionlintResult {
	done := make(chan actionlintResult, 1)
	go func() {
		var result actionlintResult
		defer func() {
			result.panicName = recover()
			done <- result
		}()
		result.workflow, result.errors = parse(content)
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case result := <-done:
		return result
	case <-timer.C:
		return actionlintResult{timedOut: true}
	}
}
