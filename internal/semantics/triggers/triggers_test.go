package triggers

import (
	"fmt"
	"testing"

	"github.com/Volkiaa/ControlProof/internal/config"
	"github.com/Volkiaa/ControlProof/internal/model"
)

const testPath = ".github/workflows/wf.yml"

func TestClassifyTriggers(t *testing.T) {
	tests := []struct {
		event   string
		modeled bool
		control string
	}{
		{"pull_request", true, "fork-definition"},
		{"pull_request_review", true, "fork-definition"},
		{"pull_request_review_comment", true, "fork-definition"},
		{"pull_request_target", true, "event-fields"},
		{"issue_comment", true, "event-fields"},
		{"issues", true, "event-fields"},
		{"workflow_run", true, "upstream-dependent"},
		{"workflow_call", true, "caller-dependent"},
		{"push", true, "conditional"},
		{"workflow_dispatch", true, "conditional"},
		{"schedule", false, "unmodeled"},
		{"release", false, "unmodeled"},
		{"repository_dispatch", false, "unmodeled"},
	}

	for _, tt := range tests {
		t.Run(tt.event, func(t *testing.T) {
			wf := &model.Workflow{
				Path:     testPath,
				Triggers: []model.Trigger{{Event: tt.event, Pos: model.Pos{File: testPath, Line: 3, Col: 3}}},
			}
			got := Classify(wf, config.Default().RepoProfile)
			if len(got.Triggers) != 1 {
				t.Fatalf("triggers = %+v, want one", got.Triggers)
			}
			trigger := got.Triggers[0]
			if trigger.Event != tt.event {
				t.Errorf("event = %q, want %q", trigger.Event, tt.event)
			}
			if trigger.Modeled != tt.modeled {
				t.Errorf("modeled = %t, want %t", trigger.Modeled, tt.modeled)
			}
			if trigger.AttackerControl != tt.control {
				t.Errorf("attacker_control = %q, want %q", trigger.AttackerControl, tt.control)
			}
			if tt.modeled && len(got.Unmodeled) != 0 {
				t.Errorf("unmodeled = %v, want none", got.Unmodeled)
			}
			if !tt.modeled && (len(got.Unmodeled) != 1 || got.Unmodeled[0] != tt.event) {
				t.Errorf("unmodeled = %v, want [%s]", got.Unmodeled, tt.event)
			}
		})
	}
}

func TestClassifyUnmodeledSortedUnique(t *testing.T) {
	wf := &model.Workflow{
		Path: testPath,
		Triggers: []model.Trigger{
			{Event: "release"},
			{Event: "schedule"},
			{Event: "release"},
			{Event: "push"},
		},
	}
	got := Classify(wf, config.Default().RepoProfile)
	if len(got.Unmodeled) != 2 || got.Unmodeled[0] != "release" || got.Unmodeled[1] != "schedule" {
		t.Fatalf("unmodeled = %v, want [release schedule]", got.Unmodeled)
	}
}

func TestClassifyName(t *testing.T) {
	tests := []struct {
		name   string
		wfName *model.Str
		want   string
	}{
		{"set", &model.Str{Value: "CI", Pos: model.Pos{File: testPath, Line: 1, Col: 7}}, "CI"},
		{"nil", nil, testPath},
		{"empty", &model.Str{Value: "", Pos: model.Pos{File: testPath, Line: 1, Col: 7}}, testPath},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Classify(&model.Workflow{Path: testPath, Name: tt.wfName}, config.Default().RepoProfile)
			if got.Name != tt.want {
				t.Errorf("name = %q, want %q", got.Name, tt.want)
			}
			if got.Workflow != testPath {
				t.Errorf("workflow = %q, want %q", got.Workflow, testPath)
			}
		})
	}
}

func TestClassifyTrust(t *testing.T) {
	headCheckout := checkoutStep(9, exprKV("ref", "github.event.pull_request.head.sha"))
	mergeCheckout := checkoutStep(9, exprKV("ref", "github.event.pull_request.merge_commit_sha"))
	headRefCheckout := checkoutStep(9, exprKV("ref", "github.head_ref"))
	headRepoCheckout := checkoutStep(9, exprKV("repository", "github.event.pull_request.head.repo.full_name"))
	literalPRRefCheckout := checkoutStep(9, model.KV{Key: "ref", Value: model.Str{Value: "refs/pull/42/head"}})
	baseCheckout := checkoutStep(9, exprKV("ref", "github.event.pull_request.base.ref"))
	defaultCheckout := checkoutStep(9, exprKV("ref", "github.event.repository.default_branch"))
	shaCheckout := checkoutStep(9, exprKV("ref", "github.sha"))
	stepsCheckout := checkoutStep(9, exprKV("ref", "steps.pr.outputs.sha"))
	envCheckout := checkoutStep(9, exprKV("ref", "env.TARGET_REF"))
	inputsCheckout := checkoutStep(9, exprKV("repository", "inputs.repository"))
	issueCheckout := checkoutStep(9, exprKV("ref", "github.event.issue.title"))
	commentCheckout := checkoutStep(9, exprKV("ref", "github.event.comment.body"))
	ghCheckout := model.Step{Run: &model.Str{Value: "gh pr checkout $PR"}, Pos: model.Pos{File: testPath, Line: 9, Col: 9}}
	upperCaseCheckout := model.Step{Uses: &model.Str{Value: "Actions/Checkout@v7"}, With: []model.KV{exprKV("ref", "github.event.pull_request.head.sha")}, Pos: model.Pos{File: testPath, Line: 9, Col: 9}}
	needsCheckout := checkoutStep(9, exprKV("ref", "needs.build.outputs.sha"))
	githubRefCheckout := checkoutStep(9, exprKV("ref", "github.ref"))
	mixedRefCheckout := checkoutStep(9, refsKV("ref", "steps.pr.outputs.sha || github.sha", "github.sha", "steps.pr.outputs.sha"))
	workflowRunShaCheckout := checkoutStep(9, exprKV("ref", "github.event.workflow_run.head_sha"))
	baseShaCheckout := checkoutStep(9, exprKV("ref", "github.event.pull_request.base.sha"))
	literalRefCheckout := checkoutStep(9, model.KV{Key: "ref", Value: model.Str{Value: "main"}})
	varsCheckout := checkoutStep(9, exprKV("ref", "vars.TARGET_REF"))
	clientPayloadCheckout := checkoutStep(9, exprKV("ref", "github.event.client_payload.ref"))
	unparsedCheckout := checkoutStep(9, model.KV{Key: "ref", Value: model.Str{
		Value: "${{ steps.",
		Exprs: []model.Expr{{Raw: "steps.", Err: "unexpected end of expression"}},
	}})

	tests := []struct {
		name        string
		events      []string
		steps       []model.Step
		want        Trust
		wantReasons int
	}{
		{"pull_request", []string{"pull_request"}, nil, TrustForkDefinitionControlled, 0},
		{"fork wins over pull_request_target", []string{"pull_request_target", "pull_request_review"}, nil, TrustForkDefinitionControlled, 0},
		{"no triggers", nil, nil, TrustTrustedDefinition, 0},
		{"pull_request_target plain", []string{"pull_request_target"}, []model.Step{checkoutStep(9)}, TrustTrustedDefinition, 0},
		{"pull_request_target head sha", []string{"pull_request_target"}, []model.Step{headCheckout}, TrustPRHeadBuilder, 0},
		{"pull_request_target merge commit", []string{"pull_request_target"}, []model.Step{mergeCheckout}, TrustPRHeadBuilder, 0},
		{"pull_request_target head ref", []string{"pull_request_target"}, []model.Step{headRefCheckout}, TrustPRHeadBuilder, 0},
		{"pull_request_target head repo", []string{"pull_request_target"}, []model.Step{headRepoCheckout}, TrustPRHeadBuilder, 0},
		{"pull_request_target refs pull literal", []string{"pull_request_target"}, []model.Step{literalPRRefCheckout}, TrustPRHeadBuilder, 0},
		{"checkout owner and repo are case insensitive", []string{"pull_request_target"}, []model.Step{upperCaseCheckout}, TrustPRHeadBuilder, 0},
		{"issue_comment gh pr checkout", []string{"issue_comment"}, []model.Step{ghCheckout}, TrustPRHeadBuilder, 0},
		{"issue_comment head sha", []string{"issue_comment"}, []model.Step{headCheckout}, TrustPRHeadBuilder, 0},
		{"pull_request_target base ref", []string{"pull_request_target"}, []model.Step{baseCheckout}, TrustTrustedDefinition, 0},
		{"pull_request_target repository event", []string{"pull_request_target"}, []model.Step{defaultCheckout}, TrustTrustedDefinition, 0},
		{"pull_request_target github sha", []string{"pull_request_target"}, []model.Step{shaCheckout}, TrustTrustedDefinition, 0},
		{"pull_request_target github ref", []string{"pull_request_target"}, []model.Step{githubRefCheckout}, TrustTrustedDefinition, 0},
		{"pull_request_target steps ref", []string{"pull_request_target"}, []model.Step{stepsCheckout}, TrustUnknown, 1},
		{"pull_request_target env ref", []string{"pull_request_target"}, []model.Step{envCheckout}, TrustUnknown, 1},
		{"pull_request_target inputs ref", []string{"pull_request_target"}, []model.Step{inputsCheckout}, TrustUnknown, 1},
		{"pull_request_target needs ref", []string{"pull_request_target"}, []model.Step{needsCheckout}, TrustUnknown, 1},
		{"pull_request_target mixed dynamic and base ref", []string{"pull_request_target"}, []model.Step{mixedRefCheckout}, TrustUnknown, 1},
		{"pull_request_target workflow_run head sha", []string{"pull_request_target"}, []model.Step{workflowRunShaCheckout}, TrustUnknown, 1},
		{"pull_request_target vars ref", []string{"pull_request_target"}, []model.Step{varsCheckout}, TrustUnknown, 1},
		{"pull_request_target client payload ref", []string{"pull_request_target"}, []model.Step{clientPayloadCheckout}, TrustUnknown, 1},
		{"pull_request_target unparsed expression", []string{"pull_request_target"}, []model.Step{unparsedCheckout}, TrustUnknown, 1},
		{"pull_request_target base sha", []string{"pull_request_target"}, []model.Step{baseShaCheckout}, TrustTrustedDefinition, 0},
		{"pull_request_target literal ref", []string{"pull_request_target"}, []model.Step{literalRefCheckout}, TrustTrustedDefinition, 0},
		{"issue_comment issue ref", []string{"issue_comment"}, []model.Step{issueCheckout}, TrustUnknown, 1},
		{"issue_comment comment ref", []string{"issue_comment"}, []model.Step{commentCheckout}, TrustUnknown, 1},
		{"workflow_run steps ref", []string{"workflow_run"}, []model.Step{stepsCheckout}, TrustTrustedDefinition, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wf := workflowWith(tt.events, jobWith(tt.steps...))
			got := Classify(wf, config.Default().RepoProfile)
			if got.Trust != tt.want {
				t.Fatalf("trust = %q, want %q", got.Trust, tt.want)
			}
			if len(got.UnknownReasons) != tt.wantReasons {
				t.Fatalf("unknown_reasons = %v, want %d", got.UnknownReasons, tt.wantReasons)
			}
			if tt.wantReasons > 0 {
				want := fmt.Sprintf("%s:9: checkout ref is dynamic and not provably the base commit", testPath)
				if got.UnknownReasons[0] != want {
					t.Fatalf("unknown_reasons[0] = %q, want %q", got.UnknownReasons[0], want)
				}
			}
		})
	}
}

func TestClassifyPRHeadCheckoutsSorted(t *testing.T) {
	first := model.Step{Uses: &model.Str{Value: "actions/checkout@v7"}, With: []model.KV{exprKV("ref", "github.head_ref")}, Pos: model.Pos{File: testPath, Line: 20, Col: 9}}
	second := model.Step{Uses: &model.Str{Value: "actions/checkout@v7"}, With: []model.KV{exprKV("ref", "github.event.pull_request.head.sha")}, Pos: model.Pos{File: testPath, Line: 8, Col: 9}}
	wf := workflowWith([]string{"pull_request"}, jobWith(second, first))

	got := Classify(wf, config.Default().RepoProfile)
	if len(got.PRHeadCheckouts) != 2 {
		t.Fatalf("pr_head_checkouts = %+v, want two", got.PRHeadCheckouts)
	}
	if got.PRHeadCheckouts[0].Line != 8 || got.PRHeadCheckouts[1].Line != 20 {
		t.Fatalf("pr_head_checkouts = %+v, want sorted by line", got.PRHeadCheckouts)
	}
}

func TestClassifyInlinedCode(t *testing.T) {
	const calleePath = ".github/workflows/reusable.yml"
	callee := &model.Workflow{
		Path:     calleePath,
		Triggers: []model.Trigger{{Event: "pull_request"}},
		Jobs: []model.Job{{
			ID:     model.Str{Value: "inner"},
			RunsOn: []model.Str{{Value: "self-hosted"}},
			Pos:    model.Pos{File: calleePath, Line: 6, Col: 3},
			Steps: []model.Step{{
				Uses: &model.Str{Value: "actions/checkout@v7"},
				With: []model.KV{exprKV("ref", "github.event.pull_request.head.sha")},
				Pos:  model.Pos{File: calleePath, Line: 12, Col: 9},
			}},
		}},
	}
	local := &model.LocalAction{Path: "./.github/actions/checkout", Steps: []model.Step{{
		Run: &model.Str{Value: "gh pr checkout $PR"},
		Pos: model.Pos{File: "./.github/actions/checkout/action.yml", Line: 5, Col: 9},
	}}}

	wf := &model.Workflow{
		Path:     testPath,
		Triggers: []model.Trigger{{Event: "pull_request_target"}},
		Jobs: []model.Job{{
			ID:   model.Str{Value: "call"},
			If:   &model.Str{Value: "github.actor == 'dependabot[bot]'", Pos: model.Pos{File: testPath, Line: 4, Col: 9}},
			Uses: &model.Str{Value: "./.github/workflows/reusable.yml"},
			Callee: &model.LocalWorkflow{
				Path:     calleePath,
				Workflow: callee,
			},
			Steps: []model.Step{{
				Uses:  &model.Str{Value: "./.github/actions/checkout"},
				Local: local,
				Pos:   model.Pos{File: testPath, Line: 20, Col: 9},
			}},
		}},
	}

	got := Classify(wf, config.Default().RepoProfile)
	if got.Trust != TrustPRHeadBuilder {
		t.Fatalf("trust = %q, want %q", got.Trust, TrustPRHeadBuilder)
	}
	if len(got.Triggers) != 1 || got.Triggers[0].Event != "pull_request_target" {
		t.Fatalf("triggers = %+v, want only the caller's event", got.Triggers)
	}
	if len(got.PRHeadCheckouts) != 2 {
		t.Fatalf("pr_head_checkouts = %+v, want the callee step and the local action step", got.PRHeadCheckouts)
	}
	if got.PRHeadCheckouts[0].File != "./.github/actions/checkout/action.yml" || got.PRHeadCheckouts[1].File != calleePath {
		t.Fatalf("pr_head_checkouts = %+v, want the callee and local action positions", got.PRHeadCheckouts)
	}

	gotPrerequisites := prerequisiteKeys(got.Prerequisites)
	wantPrerequisites := []string{
		"dependabot_event: Dependabot-triggered runs are treated as trusted",
		"self_hosted_runner_isolation: self-hosted runner is not shared with less-trusted workflows",
	}
	if fmt.Sprint(gotPrerequisites) != fmt.Sprint(wantPrerequisites) {
		t.Fatalf("prerequisites = %v, want %v", gotPrerequisites, wantPrerequisites)
	}
}

func TestClassifyForkApproval(t *testing.T) {
	tests := []struct {
		approval string
		want     string
	}{
		{config.Unknown, "fork_pr_workflow_approval: fork pull request workflow runs are not held for maintainer approval (repository/organization fork PR approval policy)"},
		{config.ApprovalFirstTimeContributors, "fork_pr_workflow_approval: the attacker is a prior contributor or a maintainer approves the fork run"},
		{config.ApprovalAllOutsideCollaborators, "fork_pr_workflow_approval: a maintainer approves the attacker's fork run"},
		{config.ApprovalNone, ""},
	}

	for _, tt := range tests {
		t.Run(tt.approval, func(t *testing.T) {
			profile := config.Default().RepoProfile
			profile.ForkPRWorkflowApproval = tt.approval
			wf := workflowWith([]string{"pull_request_review"}, jobWith())
			got := prerequisiteKeys(Classify(wf, profile).Prerequisites)
			if tt.want == "" {
				if len(got) != 0 {
					t.Fatalf("prerequisites = %v, want none", got)
				}
				return
			}
			if len(got) != 1 || got[0] != tt.want {
				t.Fatalf("prerequisites = %v, want [%s]", got, tt.want)
			}
		})
	}
}

func TestClassifyPrerequisites(t *testing.T) {
	tests := []struct {
		name string
		wf   *model.Workflow
		want []string
	}{
		{
			name: "push with branch filters",
			wf: &model.Workflow{Path: testPath, Triggers: []model.Trigger{{
				Event:    "push",
				Branches: []model.Str{{Value: "main"}, {Value: "release/**"}},
			}}, Jobs: []model.Job{jobWith()}},
			want: []string{"branch_protection: pushes to main, release/** are restricted to trusted committers"},
		},
		{
			name: "push without branch filters",
			wf:   &model.Workflow{Path: testPath, Triggers: []model.Trigger{{Event: "push"}}, Jobs: []model.Job{jobWith()}},
			want: []string{"branch_protection: pushes to any branch are restricted to trusted committers"},
		},
		{
			name: "workflow_dispatch",
			wf:   &model.Workflow{Path: testPath, Triggers: []model.Trigger{{Event: "workflow_dispatch"}}, Jobs: []model.Job{jobWith()}},
			want: []string{"dispatcher_trust: only trusted users can dispatch this workflow (requires write access)"},
		},
		{
			name: "self-hosted runner",
			wf: &model.Workflow{Path: testPath, Jobs: []model.Job{{
				RunsOn: []model.Str{{Value: "linux"}, {Value: "self-hosted"}},
				Pos:    model.Pos{File: testPath, Line: 4, Col: 3},
				Steps:  []model.Step{checkoutStep(9)},
			}}},
			want: []string{"self_hosted_runner_isolation: self-hosted runner is not shared with less-trusted workflows"},
		},
		{
			name: "self-hosted runners deduplicated",
			wf: &model.Workflow{Path: testPath, Jobs: []model.Job{
				{RunsOn: []model.Str{{Value: "self-hosted"}}, Pos: model.Pos{File: testPath, Line: 4, Col: 3}},
				{RunsOn: []model.Str{{Value: "self-hosted"}}, Pos: model.Pos{File: testPath, Line: 9, Col: 3}},
			}},
			want: []string{"self_hosted_runner_isolation: self-hosted runner is not shared with less-trusted workflows"},
		},
		{
			name: "dependabot job and step conditions deduplicated",
			wf: &model.Workflow{Path: testPath, Jobs: []model.Job{{
				If:    &model.Str{Value: "github.actor == 'dependabot[bot]'", Pos: model.Pos{File: testPath, Line: 4, Col: 9}},
				Steps: []model.Step{{If: &model.Str{Value: "github.actor == 'dependabot[bot]'", Pos: model.Pos{File: testPath, Line: 8, Col: 13}}}},
			}}},
			want: []string{"dependabot_event: Dependabot-triggered runs are treated as trusted"},
		},
		{
			name: "sorted by setting",
			wf: &model.Workflow{
				Path:     testPath,
				Triggers: []model.Trigger{{Event: "workflow_dispatch"}, {Event: "push", Branches: []model.Str{{Value: "main"}}}},
				Jobs: []model.Job{{
					If:     &model.Str{Value: "github.actor == 'dependabot[bot]'", Pos: model.Pos{File: testPath, Line: 4, Col: 9}},
					RunsOn: []model.Str{{Value: "self-hosted"}},
					Pos:    model.Pos{File: testPath, Line: 3, Col: 3},
				}},
			},
			want: []string{
				"branch_protection: pushes to main are restricted to trusted committers",
				"dependabot_event: Dependabot-triggered runs are treated as trusted",
				"dispatcher_trust: only trusted users can dispatch this workflow (requires write access)",
				"self_hosted_runner_isolation: self-hosted runner is not shared with less-trusted workflows",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := prerequisiteKeys(Classify(tt.wf, config.Default().RepoProfile).Prerequisites)
			if fmt.Sprint(got) != fmt.Sprint(tt.want) {
				t.Fatalf("prerequisites = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClassifyEmptyWorkflow(t *testing.T) {
	got := Classify(&model.Workflow{Path: testPath}, config.Default().RepoProfile)
	if got.Name != testPath {
		t.Errorf("name = %q, want %q", got.Name, testPath)
	}
	if got.Trust != TrustTrustedDefinition {
		t.Errorf("trust = %q, want %q", got.Trust, TrustTrustedDefinition)
	}
	if got.Triggers != nil || got.PRHeadCheckouts != nil || got.Unmodeled != nil || got.UnknownReasons != nil || got.Prerequisites != nil {
		t.Errorf("classification = %+v, want empty collections", got)
	}
}

func workflowWith(events []string, jobs ...model.Job) *model.Workflow {
	wf := &model.Workflow{Path: testPath, Jobs: jobs}
	for _, event := range events {
		wf.Triggers = append(wf.Triggers, model.Trigger{Event: event})
	}
	return wf
}

func jobWith(steps ...model.Step) model.Job {
	return model.Job{
		ID:     model.Str{Value: "job"},
		RunsOn: []model.Str{{Value: "ubuntu-latest"}},
		Pos:    model.Pos{File: testPath, Line: 4, Col: 3},
		Steps:  steps,
	}
}

func checkoutStep(line int, with ...model.KV) model.Step {
	return model.Step{
		Uses: &model.Str{Value: "actions/checkout@v7"},
		With: with,
		Pos:  model.Pos{File: testPath, Line: line, Col: 9},
	}
}

func exprKV(key, ref string) model.KV {
	return refsKV(key, ref, ref)
}

func refsKV(key, raw string, refs ...string) model.KV {
	return model.KV{
		Key: key,
		Value: model.Str{
			Value: "${{ " + raw + " }}",
			Pos:   model.Pos{File: testPath, Line: 10, Col: 16},
			Exprs: []model.Expr{{Raw: raw, Refs: refs}},
		},
	}
}

func prerequisiteKeys(prerequisites []model.Prerequisite) []string {
	var keys []string
	for _, prerequisite := range prerequisites {
		keys = append(keys, prerequisite.Setting+": "+prerequisite.Description)
	}
	return keys
}
