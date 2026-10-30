package workflow

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/Volkiaa/ControlProof/internal/model"
	"github.com/Volkiaa/ControlProof/internal/workspace"
	"go.yaml.in/yaml/v4"
)

var ErrCycle = errors.New("inline cycle")

type inliner struct {
	ws     *workspace.Workspace
	limits Limits
	stack  []string
	errs   []error
}

func Inline(ws *workspace.Workspace, wf *model.Workflow, limits Limits) error {
	if limits.MaxInlineDepth <= 0 {
		limits.MaxInlineDepth = DefaultLimits.MaxInlineDepth
	}
	in := &inliner{ws: ws, limits: limits, stack: []string{wf.Path}}
	in.workflow(wf)
	return errors.Join(in.errs...)
}

func (in *inliner) workflow(wf *model.Workflow) {
	for i := range wf.Jobs {
		in.job(&wf.Jobs[i])
	}
}

func (in *inliner) job(job *model.Job) {
	for i := range job.Steps {
		in.step(&job.Steps[i])
	}
	if job.Uses != nil && isLocalRef(job.Uses.Value) {
		in.workflowCall(job)
	}
}

func (in *inliner) step(step *model.Step) {
	if step.Uses != nil && isLocalRef(step.Uses.Value) {
		in.actionCall(step)
	}
}

func (in *inliner) workflowCall(job *model.Job) {
	local := &model.LocalWorkflow{}
	job.Callee = local

	caller := in.stack[len(in.stack)-1]
	file, err := in.ws.ReadFile(strings.TrimPrefix(job.Uses.Value, "./"))
	if err != nil {
		local.Unresolved = err.Error()
		in.errs = append(in.errs, fmt.Errorf("%s: job %s calls %s: %w", caller, job.ID.Value, job.Uses.Value, err))
		return
	}
	local.Path = file.Path

	if err := in.cycleError(file.Path); err != nil {
		local.Unresolved = "cycle"
		in.errs = append(in.errs, err)
		return
	}
	if err := in.depthError(file.Path); err != nil {
		local.Unresolved = "inline depth exceeded"
		in.errs = append(in.errs, err)
		return
	}

	callee, err := Parse(file, in.limits)
	if err != nil {
		local.Unresolved = err.Error()
		in.errs = append(in.errs, fmt.Errorf("%s: job %s calls %s: %w", caller, job.ID.Value, job.Uses.Value, err))
		return
	}
	local.Workflow = callee

	in.stack = append(in.stack, file.Path)
	in.workflow(callee)
	in.stack = in.stack[:len(in.stack)-1]
}

func (in *inliner) actionCall(step *model.Step) {
	local := &model.LocalAction{}
	step.Local = local

	file, err := in.readActionMetadata(strings.TrimPrefix(step.Uses.Value, "./"))
	if errors.Is(err, fs.ErrNotExist) {
		local.Unresolved = err.Error()
		return
	}
	if err != nil {
		in.unresolvedAction(local, err)
		return
	}
	local.Path = file.Path

	if err := in.cycleError(file.Path); err != nil {
		local.Unresolved = "cycle"
		in.errs = append(in.errs, err)
		return
	}
	if err := in.depthError(file.Path); err != nil {
		local.Unresolved = "inline depth exceeded"
		in.errs = append(in.errs, err)
		return
	}

	using, steps, err := parseActionMetadata(file, in.limits)
	if err != nil {
		in.unresolvedAction(local, err)
		return
	}
	local.Using = using

	switch {
	case using == "composite":
		if steps == nil {
			in.unresolvedAction(local, fmt.Errorf("%s: composite action has no runs.steps", file.Path))
			return
		}
		in.stack = append(in.stack, file.Path)
		converted, convErr := in.compositeSteps(file.Path, steps)
		if convErr != nil {
			in.unresolvedAction(local, convErr)
		} else {
			local.Steps = converted
			for i := range local.Steps {
				in.step(&local.Steps[i])
			}
		}
		in.stack = in.stack[:len(in.stack)-1]
	case using == "node12", using == "node16", using == "node20", using == "node24", using == "docker":
	default:
		in.unresolvedAction(local, fmt.Errorf("%s: unknown runs.using %q", file.Path, using))
	}
}

func (in *inliner) unresolvedAction(local *model.LocalAction, err error) {
	local.Unresolved = err.Error()
	in.errs = append(in.errs, err)
}

func (in *inliner) readActionMetadata(dir string) (workspace.File, error) {
	file, err := in.ws.ReadFile(path.Join(dir, "action.yml"))
	if err == nil {
		return file, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return workspace.File{}, err
	}

	file, err = in.ws.ReadFile(path.Join(dir, "action.yaml"))
	if err == nil {
		return file, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return workspace.File{}, err
	}
	return workspace.File{}, fmt.Errorf("%s: action metadata not found: %w", path.Join(dir, "action.yml"), fs.ErrNotExist)
}

func (in *inliner) cycleError(file string) error {
	start := -1
	for i, entry := range in.stack {
		if entry == file {
			start = i
			break
		}
	}
	if start < 0 {
		return nil
	}
	chain := append(append([]string{}, in.stack[start:]...), file)
	return fmt.Errorf("%s: %w: %s", file, ErrCycle, strings.Join(chain, " -> "))
}

func (in *inliner) depthError(next string) error {
	if len(in.stack) <= in.limits.MaxInlineDepth {
		return nil
	}
	return fmt.Errorf("%s: %w: inline depth exceeds %d", next, ErrTooComplex, in.limits.MaxInlineDepth)
}

func isLocalRef(value string) bool {
	return strings.HasPrefix(value, "./")
}

func parseActionMetadata(file workspace.File, limits Limits) (string, *yaml.Node, error) {
	if err := checkComplexity(file.Content, limits); err != nil {
		return "", nil, fmt.Errorf("%s: %w", file.Path, err)
	}

	var root yaml.Node
	if err := yaml.Unmarshal(file.Content, &root); err != nil {
		return "", nil, fmt.Errorf("%s: %w", file.Path, err)
	}

	doc := &root
	if doc.Kind == yaml.DocumentNode {
		if len(doc.Content) == 0 {
			return "", nil, fmt.Errorf("%s: empty action metadata", file.Path)
		}
		doc = derefNode(doc.Content[0])
	}

	runs := mappingValue(doc, "runs")
	if runs == nil || runs.Kind != yaml.MappingNode {
		return "", nil, fmt.Errorf("%s: runs must be a mapping", file.Path)
	}
	using, err := scalarString(file.Path, "runs.using", mappingValue(runs, "using"))
	if err != nil {
		return "", nil, err
	}
	return using, mappingValue(runs, "steps"), nil
}

func (in *inliner) compositeSteps(file string, node *yaml.Node) ([]model.Step, error) {
	node = derefNode(node)
	if node == nil || node.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("%s:%d: runs.steps must be a sequence", file, nodeLine(node))
	}

	c := &converter{path: file}
	steps := make([]model.Step, 0, len(node.Content))
	for i, item := range node.Content {
		item = derefNode(item)
		if item == nil || item.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("%s:%d: composite step %d must be a mapping", file, nodeLine(item), i)
		}

		step := model.Step{Index: i, Pos: c.posNode(item)}
		var err error
		if step.ID, err = c.nodeStr("id", mappingValue(item, "id")); err != nil {
			return nil, err
		}
		if step.Name, err = c.nodeStr("name", mappingValue(item, "name")); err != nil {
			return nil, err
		}
		if step.If, err = c.nodeStr("if", mappingValue(item, "if")); err != nil {
			return nil, err
		}
		if step.If != nil && !strings.Contains(step.If.Value, "${{") {
			step.If.Exprs = conditionExprs(step.If.Value, step.If.Pos)
		}
		if step.Uses, err = c.nodeStr("uses", mappingValue(item, "uses")); err != nil {
			return nil, err
		}
		if step.Run, err = c.nodeStr("run", mappingValue(item, "run")); err != nil {
			return nil, err
		}
		if step.Shell, err = c.nodeStr("shell", mappingValue(item, "shell")); err != nil {
			return nil, err
		}
		if step.WorkingDirectory, err = c.nodeStr("working-directory", mappingValue(item, "working-directory")); err != nil {
			return nil, err
		}
		if step.With, err = c.nodeKVs("with", mappingValue(item, "with")); err != nil {
			return nil, err
		}
		if step.Env, err = c.nodeKVs("env", mappingValue(item, "env")); err != nil {
			return nil, err
		}
		steps = append(steps, step)
	}
	return steps, nil
}

func (c *converter) posNode(node *yaml.Node) model.Pos {
	pos := model.Pos{File: c.path}
	node = derefNode(node)
	if node != nil {
		pos.Line = node.Line
		pos.Col = node.Column
	}
	return pos
}

func (c *converter) nodeStr(field string, node *yaml.Node) (*model.Str, error) {
	node = derefNode(node)
	if node == nil {
		return nil, nil
	}
	value, err := scalarString(c.path, field, node)
	if err != nil {
		return nil, err
	}
	out := &model.Str{Value: value, Pos: c.posNode(node)}
	out.Exprs = c.exprsAt(value, out.Pos)
	return out, nil
}

func (c *converter) nodeKVs(field string, node *yaml.Node) ([]model.KV, error) {
	node = derefNode(node)
	if node == nil {
		return nil, nil
	}
	if node.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s:%d: %s must be a mapping", c.path, node.Line, field)
	}

	out := make([]model.KV, 0, len(node.Content)/2)
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := derefNode(node.Content[i])
		if key == nil || key.Kind != yaml.ScalarNode {
			return nil, fmt.Errorf("%s:%d: %s key must be a string", c.path, nodeLine(key), field)
		}
		value, err := c.nodeStr(field+"."+key.Value, node.Content[i+1])
		if err != nil {
			return nil, err
		}
		if value == nil {
			return nil, fmt.Errorf("%s:%d: %s.%s must be a string", c.path, nodeLine(node.Content[i+1]), field, key.Value)
		}
		out = append(out, model.KV{Key: key.Value, Value: *value})
	}
	sortKVs(out)
	return out, nil
}

func mappingValue(node *yaml.Node, key string) *yaml.Node {
	node = derefNode(node)
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		k := derefNode(node.Content[i])
		if k != nil && k.Kind == yaml.ScalarNode && k.Value == key {
			return derefNode(node.Content[i+1])
		}
	}
	return nil
}

func scalarString(file, field string, node *yaml.Node) (string, error) {
	node = derefNode(node)
	if node == nil {
		return "", fmt.Errorf("%s: %s must be a string", file, field)
	}
	if node.Kind != yaml.ScalarNode {
		return "", fmt.Errorf("%s:%d: %s must be a string", file, node.Line, field)
	}
	return node.Value, nil
}

func derefNode(node *yaml.Node) *yaml.Node {
	for node != nil && node.Kind == yaml.AliasNode {
		node = node.Alias
	}
	return node
}

func nodeLine(node *yaml.Node) int {
	node = derefNode(node)
	if node == nil {
		return 0
	}
	return node.Line
}
