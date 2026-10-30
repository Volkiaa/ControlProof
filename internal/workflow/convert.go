package workflow

import (
	"sort"
	"strings"

	"github.com/Volkiaa/ControlProof/internal/model"
	"github.com/rhysd/actionlint"
)

type converter struct {
	path string
}

func convert(path string, w *actionlint.Workflow) *model.Workflow {
	c := &converter{path: path}
	wf := &model.Workflow{Path: path}

	wf.Name = c.str(w.Name)
	for _, event := range w.On {
		wf.Triggers = append(wf.Triggers, c.trigger(event))
	}
	wf.Permissions = c.permissions(w.Permissions)
	wf.Env = c.env(w.Env)
	wf.DefaultShell, wf.DefaultWorkingDirectory = c.defaults(w.Defaults)

	ids := make([]string, 0, len(w.Jobs))
	for id := range w.Jobs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		wf.Jobs = append(wf.Jobs, c.job(id, w.Jobs[id]))
	}

	return wf
}

func (c *converter) pos(p *actionlint.Pos) model.Pos {
	pos := model.Pos{File: c.path}
	if p != nil {
		pos.Line = p.Line
		pos.Col = p.Col
	}
	return pos
}

func (c *converter) str(s *actionlint.String) *model.Str {
	if s == nil {
		return nil
	}
	out := &model.Str{Value: s.Value, Pos: c.pos(s.Pos)}
	out.Exprs = c.exprs(s.Value, s.Pos)
	return out
}

func (c *converter) ifStr(s *actionlint.String) *model.Str {
	out := c.str(s)
	if out == nil {
		return nil
	}
	if !strings.Contains(out.Value, "${{") {
		out.Exprs = conditionExprs(out.Value, out.Pos)
	}
	return out
}

func (c *converter) strings(values []*actionlint.String) []model.Str {
	out := make([]model.Str, 0, len(values))
	for _, value := range values {
		if value == nil {
			continue
		}
		out = append(out, *c.str(value))
	}
	return out
}

func (c *converter) trigger(event actionlint.Event) model.Trigger {
	tr := model.Trigger{Event: event.EventName(), Pos: model.Pos{File: c.path}}

	switch e := event.(type) {
	case *actionlint.WebhookEvent:
		tr.Pos = c.pos(e.Pos)
		tr.Types = c.strings(e.Types)
		if e.Branches != nil {
			tr.Branches = c.strings(e.Branches.Values)
		}
		if e.BranchesIgnore != nil {
			tr.BranchesIgnore = c.strings(e.BranchesIgnore.Values)
		}
		tr.Workflows = c.strings(e.Workflows)
	case *actionlint.ScheduledEvent:
		tr.Pos = c.pos(e.Pos)
		for _, entry := range e.Schedules {
			if entry != nil && entry.Cron != nil {
				tr.Cron = append(tr.Cron, *c.str(entry.Cron))
			}
		}
	case *actionlint.WorkflowDispatchEvent:
		tr.Pos = c.pos(e.Pos)
		for name := range e.Inputs {
			tr.Inputs = append(tr.Inputs, name)
		}
		sort.Strings(tr.Inputs)
	case *actionlint.WorkflowCallEvent:
		tr.Pos = c.pos(e.Pos)
		for _, input := range e.Inputs {
			if input == nil {
				continue
			}
			name := input.ID
			if name == "" && input.Name != nil {
				name = input.Name.Value
			}
			if name != "" {
				tr.Inputs = append(tr.Inputs, name)
			}
		}
		sort.Strings(tr.Inputs)
	case *actionlint.RepositoryDispatchEvent:
		tr.Pos = c.pos(e.Pos)
		tr.Types = c.strings(e.Types)
	case *actionlint.ImageVersionEvent:
		tr.Pos = c.pos(e.Pos)
	}

	return tr
}

func (c *converter) permissions(p *actionlint.Permissions) *model.Permissions {
	if p == nil {
		return nil
	}
	out := &model.Permissions{Pos: c.pos(p.Pos), All: c.str(p.All)}
	for _, scope := range p.Scopes {
		if scope == nil {
			continue
		}
		out.Scopes = append(out.Scopes, c.kv(c.scopeKey(scope), scope.Value))
	}
	sortKVs(out.Scopes)
	return out
}

func (c *converter) scopeKey(scope *actionlint.PermissionScope) string {
	if scope.Name != nil {
		return scope.Name.Value
	}
	return ""
}

func (c *converter) env(env *actionlint.Env) []model.KV {
	if env == nil || env.Vars == nil {
		return nil
	}
	out := make([]model.KV, 0, len(env.Vars))
	for name, variable := range env.Vars {
		if variable == nil {
			continue
		}
		key := name
		if variable.Name != nil {
			key = variable.Name.Value
		}
		out = append(out, c.kv(key, variable.Value))
	}
	sortKVs(out)
	return out
}

func (c *converter) defaults(defaults *actionlint.Defaults) (*model.Str, *model.Str) {
	if defaults == nil || defaults.Run == nil {
		return nil, nil
	}
	return c.str(defaults.Run.Shell), c.str(defaults.Run.WorkingDirectory)
}

func (c *converter) job(id string, job *actionlint.Job) model.Job {
	out := model.Job{}
	if job.ID != nil {
		out.ID = *c.str(job.ID)
	} else {
		out.ID = model.Str{Value: id, Pos: model.Pos{File: c.path}}
	}
	out.Pos = c.pos(job.Pos)
	out.Name = c.str(job.Name)
	out.If = c.ifStr(job.If)
	out.Needs = c.strings(job.Needs)
	if job.RunsOn != nil {
		out.RunsOn = c.strings(job.RunsOn.Labels)
		if job.RunsOn.LabelsExpr != nil {
			out.RunsOn = append(out.RunsOn, *c.str(job.RunsOn.LabelsExpr))
		}
		out.RunnerGroup = c.str(job.RunsOn.Group)
	}
	out.Permissions = c.permissions(job.Permissions)
	if job.Environment != nil {
		out.Environment = c.str(job.Environment.Name)
	}
	out.Env = c.env(job.Env)
	out.DefaultShell, out.DefaultWorkingDirectory = c.defaults(job.Defaults)
	out.Outputs = c.outputs(job.Outputs)

	for i, step := range job.Steps {
		if step == nil {
			continue
		}
		out.Steps = append(out.Steps, c.step(i, step))
	}

	if call := job.WorkflowCall; call != nil {
		out.Uses = c.str(call.Uses)
		out.With = c.calledInputs(call.Inputs)
		out.Secrets = c.calledSecrets(call.Secrets)
		out.InheritSecrets = call.InheritSecrets
	}

	return out
}

func (c *converter) outputs(outputs map[string]*actionlint.Output) []model.KV {
	out := make([]model.KV, 0, len(outputs))
	for name, output := range outputs {
		if output == nil {
			continue
		}
		key := name
		if output.Name != nil {
			key = output.Name.Value
		}
		out = append(out, c.kv(key, output.Value))
	}
	sortKVs(out)
	return out
}

func (c *converter) calledInputs(inputs map[string]*actionlint.WorkflowCallInput) []model.KV {
	out := make([]model.KV, 0, len(inputs))
	for name, input := range inputs {
		if input == nil {
			continue
		}
		key := name
		if input.Name != nil {
			key = input.Name.Value
		}
		out = append(out, c.kv(key, input.Value))
	}
	sortKVs(out)
	return out
}

func (c *converter) calledSecrets(secrets map[string]*actionlint.WorkflowCallSecret) []model.KV {
	out := make([]model.KV, 0, len(secrets))
	for name, secret := range secrets {
		if secret == nil {
			continue
		}
		key := name
		if secret.Name != nil {
			key = secret.Name.Value
		}
		out = append(out, c.kv(key, secret.Value))
	}
	sortKVs(out)
	return out
}

func (c *converter) step(index int, step *actionlint.Step) model.Step {
	out := model.Step{Index: index}
	out.Pos = c.pos(step.Pos)
	out.ID = c.str(step.ID)
	out.Name = c.str(step.Name)
	out.If = c.ifStr(step.If)
	out.Env = c.env(step.Env)

	switch exec := step.Exec.(type) {
	case *actionlint.ExecRun:
		out.Run = c.str(exec.Run)
		out.Shell = c.str(exec.Shell)
		out.WorkingDirectory = c.str(exec.WorkingDirectory)
	case *actionlint.ExecAction:
		out.Uses = c.str(exec.Uses)
		out.With = c.actionInputs(exec.Inputs)
	}

	return out
}

func (c *converter) actionInputs(inputs map[string]*actionlint.Input) []model.KV {
	out := make([]model.KV, 0, len(inputs))
	for name, input := range inputs {
		if input == nil {
			continue
		}
		key := name
		if input.Name != nil {
			key = input.Name.Value
		}
		out = append(out, c.kv(key, input.Value))
	}
	sortKVs(out)
	return out
}

func (c *converter) kv(key string, value *actionlint.String) model.KV {
	out := model.KV{Key: key}
	if value != nil {
		out.Value = *c.str(value)
	} else {
		out.Value = model.Str{Pos: model.Pos{File: c.path}}
	}
	return out
}

func sortKVs(values []model.KV) {
	sort.SliceStable(values, func(i, j int) bool {
		return values[i].Key < values[j].Key
	})
}
