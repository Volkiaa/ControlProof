package permissions

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Volkiaa/ControlProof/internal/config"
	"github.com/Volkiaa/ControlProof/internal/model"
)

const (
	SourceJob            = "job"
	SourceWorkflow       = "workflow"
	SourceRepoProfile    = "repo_profile"
	SourceCaller         = "caller"
	SourceUnknownDefault = "unknown-default"
)

const (
	AccessNone  = "none"
	AccessRead  = "read"
	AccessWrite = "write"
)

const (
	RunnerGitHubHosted = "github-hosted"
	RunnerSelfHosted   = "self-hosted"
	RunnerUnknown      = "unknown"
)

const (
	CacheDefaultBranch = "default-branch"
	CachePRScoped      = "pr-scoped"
	CacheUnknown       = "unknown"
)

const (
	protectionUnknown = "unknown"

	prerequisiteRepositoryDefaultToken = "repository_default_token_permissions"
	prerequisiteOIDCTrust              = "oidc_federation_trust"
	prerequisiteEnvironmentProtection  = "environment_protection"

	persistTrue    = "true"
	persistFalse   = "false"
	persistUnknown = "unknown"
)

var Scopes = []string{
	"actions",
	"attestations",
	"checks",
	"contents",
	"deployments",
	"discussions",
	"id-token",
	"issues",
	"models",
	"packages",
	"pages",
	"pull-requests",
	"repository-projects",
	"security-events",
	"statuses",
}

var defaultBranchCacheEvents = map[string]struct{}{
	"workflow_run":        {},
	"pull_request_target": {},
	"issue_comment":       {},
	"issues":              {},
	"schedule":            {},
}

var prScopedCacheEvents = map[string]struct{}{
	"pull_request":                {},
	"pull_request_review":         {},
	"pull_request_review_comment": {},
}

var appTokenActions = map[string]struct{}{
	"actions/create-github-app-token":                {},
	"tibdex/github-app-token":                        {},
	"peter-murray/workflow-application-token-action": {},
}

type Token struct {
	Source string            `json:"source"`
	Scopes map[string]string `json:"scopes,omitempty"`
	Pos    *model.Pos        `json:"pos,omitempty"`
}

type Ref struct {
	Name string    `json:"name"`
	Pos  model.Pos `json:"pos"`
}

type Checkout struct {
	Pos                model.Pos `json:"pos"`
	PersistCredentials string    `json:"persist_credentials"`
}

type Environment struct {
	Name       string    `json:"name"`
	Pos        model.Pos `json:"pos"`
	Protection string    `json:"protection"`
}

type Privilege struct {
	Token          Token                `json:"token"`
	IDTokenWrite   bool                 `json:"id_token_write"`
	Secrets        []Ref                `json:"secrets,omitempty"`
	InheritSecrets bool                 `json:"inherit_secrets,omitempty"`
	Checkouts      []Checkout           `json:"checkouts,omitempty"`
	Environment    *Environment         `json:"environment,omitempty"`
	AppTokens      []Ref                `json:"app_tokens,omitempty"`
	Runner         string               `json:"runner"`
	CacheWrite     string               `json:"cache_write"`
	Prerequisites  []model.Prerequisite `json:"prerequisites,omitempty"`
}

type JobPrivilege struct {
	Job        string         `json:"job"`
	Pos        model.Pos      `json:"pos"`
	Privilege  Privilege      `json:"privilege"`
	CalleeJobs []JobPrivilege `json:"callee_jobs,omitempty"`
}

func Resolve(wf *model.Workflow, profile config.RepoProfile) []JobPrivilege {
	cache := cacheWrite(wf.Triggers)
	jobs := make([]JobPrivilege, 0, len(wf.Jobs))
	for i := range wf.Jobs {
		jobs = append(jobs, resolveJob(&wf.Jobs[i], wf, profile, cache, nil))
	}
	return jobs
}

func resolveJob(job *model.Job, wf *model.Workflow, profile config.RepoProfile, cache string, inherited *Token) JobPrivilege {
	token := resolveToken(job.Permissions, wf.Permissions, profile, inherited)
	out := JobPrivilege{
		Job: job.ID.Value,
		Pos: job.Pos,
		Privilege: Privilege{
			Token:          token,
			IDTokenWrite:   token.Scopes["id-token"] == AccessWrite,
			InheritSecrets: job.InheritSecrets,
			Environment:    environment(job),
			Runner:         runnerType(job),
			CacheWrite:     cache,
			Secrets:        secrets(job),
			Checkouts:      checkouts(job),
			AppTokens:      appTokens(job),
		},
	}
	out.Privilege.Prerequisites = prerequisites(out.Privilege)
	if job.Callee != nil && job.Callee.Workflow != nil {
		for i := range job.Callee.Workflow.Jobs {
			out.CalleeJobs = append(out.CalleeJobs, resolveJob(&job.Callee.Workflow.Jobs[i], job.Callee.Workflow, profile, cache, &token))
		}
	}
	return out
}

func resolveToken(jobPermissions, workflowPermissions *model.Permissions, profile config.RepoProfile, inherited *Token) Token {
	switch {
	case jobPermissions != nil:
		return Token{Source: SourceJob, Scopes: explicitScopes(jobPermissions), Pos: &jobPermissions.Pos}
	case workflowPermissions != nil:
		return Token{Source: SourceWorkflow, Scopes: explicitScopes(workflowPermissions), Pos: &workflowPermissions.Pos}
	case inherited != nil && inherited.Source == SourceUnknownDefault:
		return *inherited
	case inherited != nil:
		return Token{Source: SourceCaller, Scopes: inherited.Scopes, Pos: inherited.Pos}
	}
	return defaultToken(profile)
}

func explicitScopes(p *model.Permissions) map[string]string {
	out := make(map[string]string, len(Scopes))
	for _, scope := range Scopes {
		out[scope] = AccessNone
	}
	switch {
	case p.All == nil:
		for _, scope := range p.Scopes {
			if _, ok := out[scope.Key]; ok {
				out[scope.Key] = scope.Value.Value
			}
		}
	case p.All.Value == "read-all":
		for _, scope := range Scopes {
			out[scope] = AccessRead
		}
	case p.All.Value == "write-all":
		for _, scope := range Scopes {
			out[scope] = AccessWrite
		}
	}
	return out
}

func defaultToken(profile config.RepoProfile) Token {
	switch profile.RepositoryDefaultTokenPermissions {
	case config.TokenWrite:
		scopes := make(map[string]string, len(Scopes))
		for _, scope := range Scopes {
			scopes[scope] = AccessWrite
		}
		scopes["id-token"] = AccessNone
		return Token{Source: SourceRepoProfile, Scopes: scopes}
	case config.TokenRead:
		scopes := make(map[string]string, len(Scopes))
		for _, scope := range Scopes {
			scopes[scope] = AccessNone
		}
		scopes["contents"] = AccessRead
		scopes["packages"] = AccessRead
		return Token{Source: SourceRepoProfile, Scopes: scopes}
	default:
		return Token{Source: SourceUnknownDefault}
	}
}

func secrets(job *model.Job) []Ref {
	var refs []Ref
	add := func(value *model.Str) {
		if value == nil {
			return
		}
		for _, expr := range value.Exprs {
			for _, ref := range expr.Refs {
				name, ok := secretName(ref)
				if !ok {
					continue
				}
				refs = append(refs, Ref{Name: name, Pos: expr.Pos})
			}
		}
	}

	add(job.If)
	addKVValues(job.Env, add)
	addKVValues(job.With, add)
	addKVValues(job.Secrets, add)
	addKVValues(job.Outputs, add)
	for i := range job.Steps {
		stepSecrets(&job.Steps[i], add)
	}
	return sortRefs(refs)
}

func stepSecrets(step *model.Step, add func(*model.Str)) {
	add(step.If)
	add(step.Run)
	addKVValues(step.Env, add)
	addKVValues(step.With, add)
	if step.Local != nil {
		for i := range step.Local.Steps {
			stepSecrets(&step.Local.Steps[i], add)
		}
	}
}

func addKVValues(values []model.KV, add func(*model.Str)) {
	for i := range values {
		add(&values[i].Value)
	}
}

func secretName(ref string) (string, bool) {
	if !strings.HasPrefix(ref, "secrets.") {
		return "", false
	}
	name := strings.TrimPrefix(ref, "secrets.")
	if name == "" || strings.EqualFold(name, "GITHUB_TOKEN") {
		return "", false
	}
	return name, true
}

func sortRefs(refs []Ref) []Ref {
	sort.SliceStable(refs, func(i, j int) bool {
		if refs[i].Name != refs[j].Name {
			return refs[i].Name < refs[j].Name
		}
		return lessPos(refs[i].Pos, refs[j].Pos)
	})
	unique := refs[:0]
	for i := range refs {
		if len(unique) > 0 && unique[len(unique)-1].Name == refs[i].Name && unique[len(unique)-1].Pos == refs[i].Pos {
			continue
		}
		unique = append(unique, refs[i])
	}
	if len(unique) == 0 {
		return nil
	}
	return unique
}

func checkouts(job *model.Job) []Checkout {
	var out []Checkout
	var walk func(steps []model.Step)
	walk = func(steps []model.Step) {
		for i := range steps {
			step := &steps[i]
			if isAction(step, "actions/checkout") {
				out = append(out, Checkout{Pos: step.Pos, PersistCredentials: persistCredentials(step)})
			}
			if step.Local != nil {
				walk(step.Local.Steps)
			}
		}
	}
	walk(job.Steps)
	return out
}

func persistCredentials(step *model.Step) string {
	for i := range step.With {
		input := &step.With[i]
		if !strings.EqualFold(input.Key, "persist-credentials") {
			continue
		}
		if len(input.Value.Exprs) > 0 {
			return persistUnknown
		}
		switch strings.TrimSpace(input.Value.Value) {
		case persistFalse:
			return persistFalse
		case persistTrue:
			return persistTrue
		default:
			return persistUnknown
		}
	}
	return persistTrue
}

func environment(job *model.Job) *Environment {
	if job.Environment == nil {
		return nil
	}
	return &Environment{Name: job.Environment.Value, Pos: job.Environment.Pos, Protection: protectionUnknown}
}

func appTokens(job *model.Job) []Ref {
	var out []Ref
	var walk func(steps []model.Step)
	walk = func(steps []model.Step) {
		for i := range steps {
			step := &steps[i]
			if name, ok := actionName(step); ok {
				if _, ok := appTokenActions[strings.ToLower(name)]; ok {
					out = append(out, Ref{Name: name, Pos: step.Pos})
				}
			}
			if step.Local != nil {
				walk(step.Local.Steps)
			}
		}
	}
	walk(job.Steps)
	return out
}

func runnerType(job *model.Job) string {
	for i := range job.RunsOn {
		if len(job.RunsOn[i].Exprs) == 0 && strings.Contains(job.RunsOn[i].Value, "self-hosted") {
			return RunnerSelfHosted
		}
	}
	if job.RunnerGroup != nil || len(job.RunsOn) == 0 {
		return RunnerUnknown
	}
	for i := range job.RunsOn {
		if len(job.RunsOn[i].Exprs) > 0 {
			return RunnerUnknown
		}
	}
	first := job.RunsOn[0].Value
	for _, prefix := range []string{"ubuntu-", "windows-", "macos-"} {
		if strings.HasPrefix(first, prefix) {
			return RunnerGitHubHosted
		}
	}
	return RunnerUnknown
}

func cacheWrite(triggers []model.Trigger) string {
	prScoped := len(triggers) > 0
	for i := range triggers {
		if _, ok := defaultBranchCacheEvents[triggers[i].Event]; ok {
			return CacheDefaultBranch
		}
		if _, ok := prScopedCacheEvents[triggers[i].Event]; !ok {
			prScoped = false
		}
	}
	if prScoped {
		return CachePRScoped
	}
	return CacheUnknown
}

func prerequisites(privilege Privilege) []model.Prerequisite {
	var out []model.Prerequisite
	if privilege.Token.Source == SourceUnknownDefault {
		out = append(out, model.Prerequisite{
			Setting:     prerequisiteRepositoryDefaultToken,
			Description: "repository default GITHUB_TOKEN permissions grant the access this path needs",
		})
	}
	if privilege.IDTokenWrite {
		out = append(out, model.Prerequisite{
			Setting:     prerequisiteOIDCTrust,
			Description: "an OIDC federation trust policy grants publish authority to this job",
		})
	}
	if privilege.Environment != nil {
		pos := privilege.Environment.Pos
		out = append(out, model.Prerequisite{
			Setting:     prerequisiteEnvironmentProtection,
			Description: fmt.Sprintf("environment %s protection rules (required reviewers, branch policy) are unknown offline", privilege.Environment.Name),
			Pos:         &pos,
		})
	}
	return sortPrerequisites(out)
}

func sortPrerequisites(prerequisites []model.Prerequisite) []model.Prerequisite {
	sort.SliceStable(prerequisites, func(i, j int) bool {
		if prerequisites[i].Setting != prerequisites[j].Setting {
			return prerequisites[i].Setting < prerequisites[j].Setting
		}
		return prerequisites[i].Description < prerequisites[j].Description
	})
	unique := prerequisites[:0]
	for i := range prerequisites {
		if len(unique) > 0 && unique[len(unique)-1].Setting == prerequisites[i].Setting && unique[len(unique)-1].Description == prerequisites[i].Description {
			continue
		}
		unique = append(unique, prerequisites[i])
	}
	if len(unique) == 0 {
		return nil
	}
	return unique
}

func isAction(step *model.Step, action string) bool {
	name, ok := actionName(step)
	return ok && strings.EqualFold(name, action)
}

func actionName(step *model.Step) (string, bool) {
	if step.Uses == nil {
		return "", false
	}
	uses := strings.TrimSpace(step.Uses.Value)
	at := strings.LastIndexByte(uses, '@')
	if at <= 0 {
		return "", false
	}
	return uses[:at], true
}

func lessPos(a, b model.Pos) bool {
	if a.File != b.File {
		return a.File < b.File
	}
	if a.Line != b.Line {
		return a.Line < b.Line
	}
	return a.Col < b.Col
}
