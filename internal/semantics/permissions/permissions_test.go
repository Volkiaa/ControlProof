package permissions

import (
	"encoding/json"
	"testing"

	"github.com/Volkiaa/ControlProof/internal/config"
	"github.com/Volkiaa/ControlProof/internal/model"
	"github.com/Volkiaa/ControlProof/internal/workflow"
	"github.com/Volkiaa/ControlProof/internal/workspace"
)

const testPath = ".github/workflows/wf.yml"

func parseWorkflow(t *testing.T, content string) *model.Workflow {
	t.Helper()
	wf, err := workflow.Parse(workspace.File{Path: testPath, Content: []byte(content)}, workflow.DefaultLimits)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	if len(wf.Diagnostics) != 0 {
		t.Fatalf("Parse() diagnostics: %+v", wf.Diagnostics)
	}
	return wf
}

func resolveOne(t *testing.T, content string, profile config.RepoProfile) Privilege {
	t.Helper()
	jobs := Resolve(parseWorkflow(t, content), profile)
	if len(jobs) != 1 {
		t.Fatalf("jobs = %d, want 1", len(jobs))
	}
	return jobs[0].Privilege
}

func allNone() map[string]string {
	scopes := make(map[string]string, len(Scopes))
	for _, scope := range Scopes {
		scopes[scope] = AccessNone
	}
	return scopes
}

func profileWriteScopes() map[string]string {
	scopes := allWith(AccessWrite)
	scopes["id-token"] = AccessNone
	return scopes
}

func allWith(access string) map[string]string {
	scopes := make(map[string]string, len(Scopes))
	for _, scope := range Scopes {
		scopes[scope] = access
	}
	return scopes
}

func TestTokenPrecedence(t *testing.T) {
	jobPermissions := &model.Permissions{All: &model.Str{Value: "read-all"}, Pos: model.Pos{File: testPath, Line: 10, Col: 5}}
	workflowPermissions := &model.Permissions{All: &model.Str{Value: "write-all"}, Pos: model.Pos{File: testPath, Line: 3, Col: 1}}
	writeProfile := config.RepoProfile{RepositoryDefaultTokenPermissions: config.TokenWrite}

	tests := []struct {
		name       string
		job        *model.Permissions
		workflow   *model.Permissions
		profile    config.RepoProfile
		wantSource string
		wantScopes map[string]string
		wantPos    *model.Pos
	}{
		{
			name:       "job over workflow and profile",
			job:        jobPermissions,
			workflow:   workflowPermissions,
			profile:    writeProfile,
			wantSource: SourceJob,
			wantScopes: allWith(AccessRead),
			wantPos:    &jobPermissions.Pos,
		},
		{
			name:       "workflow over profile",
			workflow:   workflowPermissions,
			profile:    writeProfile,
			wantSource: SourceWorkflow,
			wantScopes: allWith(AccessWrite),
			wantPos:    &workflowPermissions.Pos,
		},
		{
			name:       "profile",
			profile:    writeProfile,
			wantSource: SourceRepoProfile,
			wantScopes: profileWriteScopes(),
		},
		{
			name:       "unknown default",
			profile:    config.Default().RepoProfile,
			wantSource: SourceUnknownDefault,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Resolve(&model.Workflow{
				Path:        testPath,
				Permissions: tt.workflow,
				Jobs:        []model.Job{{ID: model.Str{Value: "build"}, Permissions: tt.job}},
			}, tt.profile)[0].Privilege.Token

			if got.Source != tt.wantSource {
				t.Fatalf("source = %q, want %q", got.Source, tt.wantSource)
			}
			if tt.wantScopes == nil {
				if got.Scopes != nil {
					t.Fatalf("scopes = %v, want nil", got.Scopes)
				}
			} else if !equalScopes(got.Scopes, tt.wantScopes) {
				t.Fatalf("scopes = %v, want %v", got.Scopes, tt.wantScopes)
			}
			switch {
			case tt.wantPos == nil && got.Pos != nil:
				t.Fatalf("pos = %+v, want nil", got.Pos)
			case tt.wantPos != nil && (got.Pos == nil || *got.Pos != *tt.wantPos):
				t.Fatalf("pos = %+v, want %+v", got.Pos, tt.wantPos)
			}
		})
	}
}

func TestExplicitPermissions(t *testing.T) {
	tests := []struct {
		name        string
		permissions *model.Permissions
		want        map[string]string
	}{
		{
			name:        "read all",
			permissions: &model.Permissions{All: &model.Str{Value: "read-all"}},
			want:        allWith(AccessRead),
		},
		{
			name:        "write all",
			permissions: &model.Permissions{All: &model.Str{Value: "write-all"}},
			want:        allWith(AccessWrite),
		},
		{
			name:        "empty mapping",
			permissions: &model.Permissions{},
			want:        allNone(),
		},
		{
			name: "partial mapping",
			permissions: &model.Permissions{Scopes: []model.KV{
				{Key: "contents", Value: model.Str{Value: AccessRead}},
				{Key: "issues", Value: model.Str{Value: AccessWrite}},
			}},
			want: map[string]string{"contents": AccessRead, "issues": AccessWrite},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Resolve(&model.Workflow{
				Path: testPath,
				Jobs: []model.Job{{ID: model.Str{Value: "build"}, Permissions: tt.permissions}},
			}, config.Default().RepoProfile)[0].Privilege.Token

			if got.Source != SourceJob {
				t.Fatalf("source = %q, want %q", got.Source, SourceJob)
			}
			if len(got.Scopes) != len(Scopes) {
				t.Fatalf("scopes = %v, want every scope listed", got.Scopes)
			}
			for _, scope := range Scopes {
				want, ok := tt.want[scope]
				if !ok {
					want = AccessNone
				}
				if got.Scopes[scope] != want {
					t.Errorf("scopes[%s] = %q, want %q", scope, got.Scopes[scope], want)
				}
			}
		})
	}
}

func TestExplicitEmptyMappingDiffersFromMissingKey(t *testing.T) {
	empty := resolveOne(t, "on: push\npermissions: {}\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo hi\n", config.Default().RepoProfile).Token
	missing := resolveOne(t, "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo hi\n", config.Default().RepoProfile).Token

	if empty.Source != SourceWorkflow || len(empty.Scopes) != len(Scopes) {
		t.Fatalf("empty mapping token = %+v, want workflow source with every scope", empty)
	}
	for _, scope := range Scopes {
		if empty.Scopes[scope] != AccessNone {
			t.Errorf("empty mapping scopes[%s] = %q, want none", scope, empty.Scopes[scope])
		}
	}
	if missing.Source != SourceUnknownDefault || missing.Scopes != nil {
		t.Fatalf("missing key token = %+v, want unknown-default with nil scopes", missing)
	}
}

func TestRepositoryDefaultTokenPermissions(t *testing.T) {
	tests := []struct {
		name    string
		profile string
		want    map[string]string
	}{
		{
			name:    "write",
			profile: config.TokenWrite,
			want:    profileWriteScopes(),
		},
		{
			name:    "read",
			profile: config.TokenRead,
			want: func() map[string]string {
				scopes := allNone()
				scopes["contents"] = AccessRead
				scopes["packages"] = AccessRead
				return scopes
			}(),
		},
		{
			name:    "unknown",
			profile: config.Unknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			privilege := resolveOne(t, "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo hi\n", config.RepoProfile{RepositoryDefaultTokenPermissions: tt.profile})

			if tt.profile == config.Unknown {
				if privilege.Token.Source != SourceUnknownDefault || privilege.Token.Scopes != nil {
					t.Fatalf("token = %+v, want unknown-default with nil scopes", privilege.Token)
				}
				if !hasPrerequisite(privilege, prerequisiteRepositoryDefaultToken) {
					t.Fatalf("prerequisites = %+v, want %s", privilege.Prerequisites, prerequisiteRepositoryDefaultToken)
				}
				return
			}

			if privilege.Token.Source != SourceRepoProfile {
				t.Fatalf("source = %q, want %q", privilege.Token.Source, SourceRepoProfile)
			}
			if !equalScopes(privilege.Token.Scopes, tt.want) {
				t.Fatalf("scopes = %v, want %v", privilege.Token.Scopes, tt.want)
			}
			if hasPrerequisite(privilege, prerequisiteRepositoryDefaultToken) {
				t.Errorf("prerequisites = %+v, want no %s once the profile resolves it", privilege.Prerequisites, prerequisiteRepositoryDefaultToken)
			}
		})
	}
}

func TestIDTokenWrite(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		wantWrite bool
	}{
		{"write", AccessWrite, true},
		{"read", AccessRead, false},
		{"none", AccessNone, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			privilege := Resolve(&model.Workflow{
				Path: testPath,
				Jobs: []model.Job{{
					ID: model.Str{Value: "publish"},
					Permissions: &model.Permissions{Scopes: []model.KV{
						{Key: "id-token", Value: model.Str{Value: tt.value}},
					}},
				}},
			}, config.Default().RepoProfile)[0].Privilege

			if privilege.IDTokenWrite != tt.wantWrite {
				t.Fatalf("id_token_write = %t, want %t", privilege.IDTokenWrite, tt.wantWrite)
			}
			if got := hasPrerequisite(privilege, prerequisiteOIDCTrust); got != tt.wantWrite {
				t.Fatalf("prerequisite %s = %t, want %t", prerequisiteOIDCTrust, got, tt.wantWrite)
			}
		})
	}
}

func TestSecrets(t *testing.T) {
	wf := parseWorkflow(t, `on: push
permissions: {}
jobs:
  build:
    runs-on: ubuntu-latest
    if: secrets.deploy_key != ''
    env:
      ALPHA: ${{ secrets.ALPHA }}
    steps:
      - env:
          BETA: ${{ secrets.BETA }}
        run: echo ${{ secrets.GAMMA }} ${{ secrets.GITHUB_TOKEN }} ${{ secrets.Gamma }}
      - uses: actions/upload-artifact@v7
        with:
          name: ${{ secrets.artifact_name }}
`)
	got := Resolve(wf, config.Default().RepoProfile)[0].Privilege

	want := []Ref{
		{Name: "alpha", Pos: model.Pos{File: testPath, Line: 8, Col: 14}},
		{Name: "artifact_name", Pos: model.Pos{File: testPath, Line: 15, Col: 17}},
		{Name: "beta", Pos: model.Pos{File: testPath, Line: 11, Col: 17}},
		{Name: "deploy_key", Pos: model.Pos{File: testPath, Line: 6, Col: 9}},
		{Name: "gamma", Pos: model.Pos{File: testPath, Line: 12, Col: 19}},
		{Name: "gamma", Pos: model.Pos{File: testPath, Line: 12, Col: 68}},
	}
	if len(got.Secrets) != len(want) {
		t.Fatalf("secrets = %+v, want %+v", got.Secrets, want)
	}
	for i := range want {
		if got.Secrets[i] != want[i] {
			t.Errorf("secrets[%d] = %+v, want %+v", i, got.Secrets[i], want[i])
		}
	}
}

func TestSecretsInLocalSteps(t *testing.T) {
	expr := func(name string, line int) *model.Str {
		return &model.Str{Exprs: []model.Expr{{Refs: []string{"secrets." + name}, Pos: model.Pos{File: testPath, Line: line, Col: 7}}}}
	}
	wf := &model.Workflow{
		Path: testPath,
		Jobs: []model.Job{{
			ID: model.Str{Value: "build"},
			Steps: []model.Step{
				{Run: expr("top_secret", 10)},
				{Local: &model.LocalAction{Steps: []model.Step{{Run: expr("local_secret", 20)}}}},
			},
		}},
	}
	got := Resolve(wf, config.Default().RepoProfile)[0].Privilege.Secrets
	if len(got) != 2 || got[0].Name != "local_secret" || got[1].Name != "top_secret" {
		t.Fatalf("secrets = %+v, want local_secret then top_secret", got)
	}
	if got[0].Pos.Line != 20 || got[1].Pos.Line != 10 {
		t.Fatalf("secrets = %+v, want positions from the local and top step", got)
	}
}

func TestInheritSecrets(t *testing.T) {
	wf := parseWorkflow(t, `on: push
jobs:
  call:
    uses: octo/repo/.github/workflows/reusable.yml@v1
    secrets: inherit
`)
	if !Resolve(wf, config.Default().RepoProfile)[0].Privilege.InheritSecrets {
		t.Fatal("inherit_secrets = false, want true")
	}
}

func TestCheckouts(t *testing.T) {
	wf := parseWorkflow(t, `on: push
permissions: {}
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: Actions/Checkout@v7
      - uses: actions/checkout@v7
        with:
          persist-credentials: false
      - uses: actions/checkout@v7
        with:
          persist-credentials: ${{ inputs.persist }}
      - uses: actions/upload-artifact@v7
`)
	got := Resolve(wf, config.Default().RepoProfile)[0].Privilege.Checkouts

	want := []string{persistTrue, persistFalse, persistUnknown}
	if len(got) != len(want) {
		t.Fatalf("checkouts = %+v, want %d", got, len(want))
	}
	for i := range want {
		if got[i].PersistCredentials != want[i] {
			t.Errorf("checkouts[%d].persist_credentials = %q, want %q", i, got[i].PersistCredentials, want[i])
		}
	}
	if got[0].Pos.Line != 7 || got[1].Pos.Line != 8 || got[2].Pos.Line != 11 {
		t.Errorf("checkout positions = %+v, want lines 7, 8, 11", got)
	}
}

func TestCheckoutInLocalAction(t *testing.T) {
	wf := &model.Workflow{
		Path: testPath,
		Jobs: []model.Job{{
			ID: model.Str{Value: "build"},
			Steps: []model.Step{{
				Local: &model.LocalAction{Steps: []model.Step{{
					Uses: &model.Str{Value: "actions/checkout@v7"},
					Pos:  model.Pos{File: ".github/actions/setup/action.yml", Line: 4, Col: 9},
				}}},
			}},
		}},
	}
	got := Resolve(wf, config.Default().RepoProfile)[0].Privilege.Checkouts
	if len(got) != 1 || got[0].PersistCredentials != persistTrue || got[0].Pos.File != ".github/actions/setup/action.yml" {
		t.Fatalf("checkouts = %+v, want one persisted checkout from the local action", got)
	}
}

func TestEnvironment(t *testing.T) {
	wf := parseWorkflow(t, `on: push
permissions: {}
jobs:
  deploy:
    environment: ${{ inputs.target }}
    runs-on: ubuntu-latest
    steps:
      - run: echo deploy
`)
	got := Resolve(wf, config.Default().RepoProfile)[0].Privilege

	if got.Environment == nil {
		t.Fatal("environment = nil, want the raw name")
	}
	if got.Environment.Name != "${{ inputs.target }}" {
		t.Errorf("environment name = %q, want the raw expression", got.Environment.Name)
	}
	if got.Environment.Protection != "unknown" {
		t.Errorf("environment protection = %q, want unknown", got.Environment.Protection)
	}

	var found bool
	for _, prerequisite := range got.Prerequisites {
		if prerequisite.Setting != prerequisiteEnvironmentProtection {
			continue
		}
		found = true
		if prerequisite.Description != "environment ${{ inputs.target }} protection rules (required reviewers, branch policy) are unknown offline" {
			t.Errorf("description = %q", prerequisite.Description)
		}
		if prerequisite.Pos == nil || *prerequisite.Pos != got.Environment.Pos {
			t.Errorf("pos = %+v, want the environment key position %+v", prerequisite.Pos, got.Environment.Pos)
		}
	}
	if !found {
		t.Fatalf("prerequisites = %+v, want %s", got.Prerequisites, prerequisiteEnvironmentProtection)
	}
}

func TestAppTokens(t *testing.T) {
	wf := parseWorkflow(t, `on: push
permissions: {}
jobs:
  token:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/create-github-app-token@v1
      - uses: TibDex/GitHub-App-Token@v2
      - uses: peter-murray/workflow-application-token-action@v4
      - uses: actions/checkout@v7
      - uses: actions/github-script@v9
`)
	got := Resolve(wf, config.Default().RepoProfile)[0].Privilege.AppTokens

	want := []struct {
		name string
		line int
	}{
		{"actions/create-github-app-token", 7},
		{"TibDex/GitHub-App-Token", 8},
		{"peter-murray/workflow-application-token-action", 9},
	}
	if len(got) != len(want) {
		t.Fatalf("app_tokens = %+v, want %d", got, len(want))
	}
	for i := range want {
		if got[i].Name != want[i].name || got[i].Pos.Line != want[i].line {
			t.Errorf("app_tokens[%d] = %+v, want %s on line %d", i, got[i], want[i].name, want[i].line)
		}
	}
}

func TestRunner(t *testing.T) {
	expression := model.Str{Value: "${{ matrix.os }}", Exprs: []model.Expr{{Raw: "matrix.os", Refs: []string{"matrix.os"}}}}
	tests := []struct {
		name   string
		runsOn []model.Str
		group  *model.Str
		want   string
	}{
		{name: "self hosted label", runsOn: []model.Str{{Value: "self-hosted"}, {Value: "linux"}}, want: RunnerSelfHosted},
		{name: "ubuntu", runsOn: []model.Str{{Value: "ubuntu-latest"}}, want: RunnerGitHubHosted},
		{name: "windows", runsOn: []model.Str{{Value: "windows-2022"}}, want: RunnerGitHubHosted},
		{name: "macos", runsOn: []model.Str{{Value: "macos-14"}}, want: RunnerGitHubHosted},
		{name: "expression", runsOn: []model.Str{expression}, want: RunnerUnknown},
		{name: "runner group", runsOn: []model.Str{{Value: "ubuntu-latest"}}, group: &model.Str{Value: "large"}, want: RunnerUnknown},
		{name: "other label", runsOn: []model.Str{{Value: "linux"}}, want: RunnerUnknown},
		{name: "no labels", want: RunnerUnknown},
		{name: "self hosted beside expression", runsOn: []model.Str{expression, {Value: "self-hosted"}}, want: RunnerSelfHosted},
		{name: "expression beside ubuntu", runsOn: []model.Str{{Value: "ubuntu-latest"}, expression}, want: RunnerUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Resolve(&model.Workflow{
				Path: testPath,
				Jobs: []model.Job{{ID: model.Str{Value: "build"}, RunsOn: tt.runsOn, RunnerGroup: tt.group}},
			}, config.Default().RepoProfile)[0].Privilege.Runner
			if got != tt.want {
				t.Fatalf("runner = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCacheWrite(t *testing.T) {
	tests := []struct {
		name   string
		events []string
		want   string
	}{
		{name: "workflow run", events: []string{"workflow_run"}, want: CacheDefaultBranch},
		{name: "pull request target", events: []string{"pull_request_target"}, want: CacheDefaultBranch},
		{name: "issue comment", events: []string{"issue_comment"}, want: CacheDefaultBranch},
		{name: "issues", events: []string{"issues"}, want: CacheDefaultBranch},
		{name: "schedule", events: []string{"schedule"}, want: CacheDefaultBranch},
		{name: "schedule with pull request", events: []string{"pull_request", "schedule"}, want: CacheDefaultBranch},
		{name: "pull request", events: []string{"pull_request"}, want: CachePRScoped},
		{name: "pull request review", events: []string{"pull_request_review"}, want: CachePRScoped},
		{name: "pull request review comment", events: []string{"pull_request_review_comment"}, want: CachePRScoped},
		{name: "pull request variants", events: []string{"pull_request", "pull_request_review_comment"}, want: CachePRScoped},
		{name: "push", events: []string{"push"}, want: CacheUnknown},
		{name: "pull request with push", events: []string{"pull_request", "push"}, want: CacheUnknown},
		{name: "no triggers", want: CacheUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			triggers := make([]model.Trigger, 0, len(tt.events))
			for _, event := range tt.events {
				triggers = append(triggers, model.Trigger{Event: event})
			}
			wf := &model.Workflow{
				Path:        testPath,
				Triggers:    triggers,
				Permissions: &model.Permissions{},
				Jobs:        []model.Job{{ID: model.Str{Value: "build"}}},
			}
			got := Resolve(wf, config.Default().RepoProfile)[0].Privilege.CacheWrite
			if got != tt.want {
				t.Fatalf("cache_write = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCalleeTokenFallsBackToCaller(t *testing.T) {
	callerPermissions := &model.Permissions{Scopes: []model.KV{
		{Key: "contents", Value: model.Str{Value: AccessRead}},
		{Key: "id-token", Value: model.Str{Value: AccessWrite}},
	}}
	wf := &model.Workflow{
		Path: testPath,
		Jobs: []model.Job{{
			ID:          model.Str{Value: "call"},
			Permissions: callerPermissions,
			Callee: &model.LocalWorkflow{Workflow: &model.Workflow{
				Path: ".github/workflows/callee.yml",
				Jobs: []model.Job{
					{ID: model.Str{Value: "inherit"}},
					{ID: model.Str{Value: "override"}, Permissions: &model.Permissions{Scopes: []model.KV{{Key: "issues", Value: model.Str{Value: AccessWrite}}}}},
				},
			}},
		}},
	}
	got := Resolve(wf, config.Default().RepoProfile)
	if len(got) != 1 || len(got[0].CalleeJobs) != 2 {
		t.Fatalf("jobs = %+v, want one caller with two callee jobs", got)
	}

	inherit := got[0].CalleeJobs[0]
	if inherit.Privilege.Token.Source != SourceCaller {
		t.Errorf("inherit source = %q, want %q", inherit.Privilege.Token.Source, SourceCaller)
	}
	if inherit.Privilege.Token.Scopes["contents"] != AccessRead || inherit.Privilege.Token.Scopes["id-token"] != AccessWrite {
		t.Errorf("inherit scopes = %v, want the caller scopes", inherit.Privilege.Token.Scopes)
	}
	if !inherit.Privilege.IDTokenWrite || !hasPrerequisite(inherit.Privilege, prerequisiteOIDCTrust) {
		t.Errorf("inherit privilege = %+v, want inherited id-token write with the OIDC prerequisite", inherit.Privilege)
	}
	if inherit.Privilege.Token.Pos == nil || *inherit.Privilege.Token.Pos != callerPermissions.Pos {
		t.Errorf("inherit pos = %+v, want the caller permissions key", inherit.Privilege.Token.Pos)
	}

	override := got[0].CalleeJobs[1]
	if override.Privilege.Token.Source != SourceJob || override.Privilege.Token.Scopes["issues"] != AccessWrite {
		t.Errorf("override token = %+v, want its own job permissions", override.Privilege.Token)
	}
}

func TestCalleeKeepsUnknownDefaultFromCaller(t *testing.T) {
	wf := &model.Workflow{
		Path: testPath,
		Jobs: []model.Job{{
			ID: model.Str{Value: "call"},
			Callee: &model.LocalWorkflow{Workflow: &model.Workflow{
				Path: ".github/workflows/callee.yml",
				Jobs: []model.Job{{ID: model.Str{Value: "inherit"}}},
			}},
		}},
	}
	got := Resolve(wf, config.Default().RepoProfile)
	inherit := got[0].CalleeJobs[0].Privilege
	if inherit.Token.Source != SourceUnknownDefault || inherit.Token.Scopes != nil {
		t.Errorf("inherit token = %+v, want unknown-default with no scopes", inherit.Token)
	}
	if !hasPrerequisite(inherit, prerequisiteRepositoryDefaultToken) {
		t.Errorf("inherit prerequisites = %+v, want %s", inherit.Prerequisites, prerequisiteRepositoryDefaultToken)
	}
}

func TestResolveOrderAndDeterminism(t *testing.T) {
	wf := parseWorkflow(t, `on:
  pull_request:
  push:
permissions:
  contents: read
jobs:
  alpha:
    runs-on: ubuntu-latest
    environment: production
    permissions:
      id-token: write
    steps:
      - run: echo ${{ secrets.BETA }} ${{ secrets.ALPHA }}
  beta:
    runs-on: self-hosted
    steps:
      - uses: actions/checkout@v7
  gamma:
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
`)
	jobs := Resolve(wf, config.Default().RepoProfile)
	names := make([]string, 0, len(jobs))
	for _, job := range jobs {
		names = append(names, job.Job)
	}
	if len(names) != 3 || names[0] != "alpha" || names[1] != "beta" || names[2] != "gamma" {
		t.Fatalf("jobs = %v, want [alpha beta gamma]", names)
	}

	alpha := jobs[0].Privilege.Prerequisites
	settings := make([]string, 0, len(alpha))
	for _, prerequisite := range alpha {
		settings = append(settings, prerequisite.Setting)
	}
	want := []string{prerequisiteEnvironmentProtection, prerequisiteOIDCTrust}
	if len(settings) != len(want) {
		t.Fatalf("prerequisites = %v, want %v", settings, want)
	}
	for i := range want {
		if settings[i] != want[i] {
			t.Fatalf("prerequisites = %v, want %v", settings, want)
		}
	}

	first, err := json.Marshal(Resolve(wf, config.Default().RepoProfile))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	second, err := json.Marshal(Resolve(wf, config.Default().RepoProfile))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(first) != string(second) {
		t.Fatalf("Resolve is not deterministic:\n%s\n%s", first, second)
	}
}

func equalScopes(got, want map[string]string) bool {
	if len(got) != len(want) {
		return false
	}
	for scope, value := range want {
		if got[scope] != value {
			return false
		}
	}
	return true
}

func hasPrerequisite(privilege Privilege, setting string) bool {
	for _, prerequisite := range privilege.Prerequisites {
		if prerequisite.Setting == setting {
			return true
		}
	}
	return false
}
