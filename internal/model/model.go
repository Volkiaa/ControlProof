package model

type Pos struct {
	File string `json:"file,omitempty"`
	Line int    `json:"line,omitempty"`
	Col  int    `json:"col,omitempty"`
}

type Expr struct {
	Raw  string   `json:"raw"`
	Pos  Pos      `json:"pos"`
	Refs []string `json:"refs,omitempty"`
	Err  string   `json:"err,omitempty"`
}

type Str struct {
	Value string `json:"value"`
	Pos   Pos    `json:"pos"`
	Exprs []Expr `json:"exprs,omitempty"`
}

type Workflow struct {
	Path                    string       `json:"path"`
	Name                    *Str         `json:"name,omitempty"`
	Triggers                []Trigger    `json:"triggers,omitempty"`
	Permissions             *Permissions `json:"permissions"`
	Env                     []KV         `json:"env,omitempty"`
	DefaultShell            *Str         `json:"default_shell,omitempty"`
	DefaultWorkingDirectory *Str         `json:"default_working_directory,omitempty"`
	Jobs                    []Job        `json:"jobs,omitempty"`
	Diagnostics             []Diag       `json:"diagnostics,omitempty"`
}

type Trigger struct {
	Event string `json:"event"`
	Pos
	Types          []Str    `json:"types,omitempty"`
	Branches       []Str    `json:"branches,omitempty"`
	BranchesIgnore []Str    `json:"branches_ignore,omitempty"`
	Workflows      []Str    `json:"workflows,omitempty"`
	Cron           []Str    `json:"cron,omitempty"`
	Inputs         []string `json:"inputs,omitempty"`
}

type Permissions struct {
	All    *Str `json:"all,omitempty"`
	Scopes []KV `json:"scopes,omitempty"`
	Pos
}

type KV struct {
	Key   string `json:"key"`
	Value Str    `json:"value"`
}

type Job struct {
	ID                      Str            `json:"id"`
	Name                    *Str           `json:"name,omitempty"`
	If                      *Str           `json:"if,omitempty"`
	Needs                   []Str          `json:"needs,omitempty"`
	RunsOn                  []Str          `json:"runs_on,omitempty"`
	RunnerGroup             *Str           `json:"runner_group,omitempty"`
	Permissions             *Permissions   `json:"permissions"`
	Environment             *Str           `json:"environment,omitempty"`
	Env                     []KV           `json:"env,omitempty"`
	DefaultShell            *Str           `json:"default_shell,omitempty"`
	DefaultWorkingDirectory *Str           `json:"default_working_directory,omitempty"`
	Outputs                 []KV           `json:"outputs,omitempty"`
	Steps                   []Step         `json:"steps,omitempty"`
	Uses                    *Str           `json:"uses,omitempty"`
	With                    []KV           `json:"with,omitempty"`
	Secrets                 []KV           `json:"secrets,omitempty"`
	InheritSecrets          bool           `json:"inherit_secrets,omitempty"`
	Callee                  *LocalWorkflow `json:"callee,omitempty"`
	Pos
}

type LocalWorkflow struct {
	Path       string    `json:"path,omitempty"`
	Workflow   *Workflow `json:"workflow,omitempty"`
	Unresolved string    `json:"unresolved,omitempty"`
}

type Step struct {
	Index            int          `json:"index"`
	ID               *Str         `json:"id,omitempty"`
	Name             *Str         `json:"name,omitempty"`
	If               *Str         `json:"if,omitempty"`
	Uses             *Str         `json:"uses,omitempty"`
	With             []KV         `json:"with,omitempty"`
	Run              *Str         `json:"run,omitempty"`
	Shell            *Str         `json:"shell,omitempty"`
	WorkingDirectory *Str         `json:"working_directory,omitempty"`
	Env              []KV         `json:"env,omitempty"`
	Local            *LocalAction `json:"local,omitempty"`
	Pos
}

type LocalAction struct {
	Path       string `json:"path,omitempty"`
	Using      string `json:"using,omitempty"`
	Steps      []Step `json:"steps,omitempty"`
	Unresolved string `json:"unresolved,omitempty"`
}

type Prerequisite struct {
	Setting     string `json:"setting"`
	Description string `json:"description"`
	Pos         *Pos   `json:"pos,omitempty"`
}

type Diag struct {
	Pos
	Kind    string `json:"kind"`
	Message string `json:"message"`
}
