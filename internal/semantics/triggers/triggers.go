package triggers

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Volkiaa/ControlProof/internal/config"
	"github.com/Volkiaa/ControlProof/internal/model"
)

type Trust string

const (
	TrustForkDefinitionControlled Trust = "fork-definition-controlled"
	TrustPRHeadBuilder            Trust = "pr-head-builder"
	TrustTrustedDefinition        Trust = "trusted-definition"
	TrustUnknown                  Trust = "unknown"
)

const (
	controlForkDefinition    = "fork-definition"
	controlEventFields       = "event-fields"
	controlUpstreamDependent = "upstream-dependent"
	controlCallerDependent   = "caller-dependent"
	controlConditional       = "conditional"
	controlUnmodeled         = "unmodeled"
)

var attackerControls = map[string]string{
	"pull_request":                controlForkDefinition,
	"pull_request_review":         controlForkDefinition,
	"pull_request_review_comment": controlForkDefinition,
	"pull_request_target":         controlEventFields,
	"issue_comment":               controlEventFields,
	"issues":                      controlEventFields,
	"workflow_run":                controlUpstreamDependent,
	"workflow_call":               controlCallerDependent,
	"push":                        controlConditional,
	"workflow_dispatch":           controlConditional,
}

var prHeadRefs = map[string]struct{}{
	"github.event.pull_request.head.sha":            {},
	"github.event.pull_request.head.ref":            {},
	"github.head_ref":                               {},
	"github.event.pull_request.merge_commit_sha":    {},
	"github.event.pull_request.head.repo.full_name": {},
}

var knownBaseRefs = map[string]struct{}{
	"github.sha":        {},
	"github.ref":        {},
	"github.ref_name":   {},
	"github.base_ref":   {},
	"github.repository": {},
}

var knownBaseRefPrefixes = []string{
	"github.event.repository.",
	"github.event.pull_request.base.",
}

type Trigger struct {
	Event           string    `json:"event"`
	Pos             model.Pos `json:"pos"`
	Modeled         bool      `json:"modeled"`
	AttackerControl string    `json:"attacker_control"`
}

type Classification struct {
	Workflow        string               `json:"workflow"`
	Name            string               `json:"name"`
	Trust           Trust                `json:"trust"`
	Triggers        []Trigger            `json:"triggers"`
	PRHeadCheckouts []model.Pos          `json:"pr_head_checkouts,omitempty"`
	Unmodeled       []string             `json:"unmodeled,omitempty"`
	UnknownReasons  []string             `json:"unknown_reasons,omitempty"`
	Prerequisites   []model.Prerequisite `json:"prerequisites,omitempty"`
}

type checkoutKind int

const (
	checkoutPlain checkoutKind = iota
	checkoutPRHead
	checkoutDynamic
)

type classifier struct {
	prHeadCheckouts  []model.Pos
	dynamicCheckouts []model.Pos
	selfHostedJobs   []model.Pos
	dependabotIfs    []model.Pos
}

func Classify(wf *model.Workflow, profile config.RepoProfile) Classification {
	classification := Classification{
		Workflow: wf.Path,
		Name:     workflowName(wf),
	}

	forkDefinition := false
	pullRequestContext := false
	unmodeled := make(map[string]struct{})
	for _, trigger := range wf.Triggers {
		control, modeled := attackerControls[trigger.Event]
		entry := Trigger{Event: trigger.Event, Pos: trigger.Pos, Modeled: modeled, AttackerControl: control}
		if !modeled {
			entry.AttackerControl = controlUnmodeled
			unmodeled[trigger.Event] = struct{}{}
		} else if control == controlForkDefinition {
			forkDefinition = true
		}
		if trigger.Event == "pull_request_target" || trigger.Event == "issue_comment" {
			pullRequestContext = true
		}
		classification.Triggers = append(classification.Triggers, entry)
	}

	c := &classifier{}
	for i := range wf.Jobs {
		c.job(&wf.Jobs[i])
	}

	classification.PRHeadCheckouts = sortPositions(c.prHeadCheckouts)

	switch {
	case forkDefinition:
		classification.Trust = TrustForkDefinitionControlled
	case pullRequestContext && len(c.prHeadCheckouts) > 0:
		classification.Trust = TrustPRHeadBuilder
	case pullRequestContext && len(c.dynamicCheckouts) > 0:
		classification.Trust = TrustUnknown
		classification.UnknownReasons = unknownReasons(c.dynamicCheckouts)
	default:
		classification.Trust = TrustTrustedDefinition
	}

	for event := range unmodeled {
		classification.Unmodeled = append(classification.Unmodeled, event)
	}
	sort.Strings(classification.Unmodeled)

	classification.Prerequisites = prerequisites(wf, c, profile, forkDefinition)
	return classification
}

func workflowName(wf *model.Workflow) string {
	if wf.Name != nil && wf.Name.Value != "" {
		return wf.Name.Value
	}
	return wf.Path
}

func (c *classifier) job(job *model.Job) {
	c.dependabotIf(job.If)
	for _, label := range job.RunsOn {
		if strings.Contains(label.Value, "self-hosted") {
			c.selfHostedJobs = append(c.selfHostedJobs, job.Pos)
			break
		}
	}
	for i := range job.Steps {
		c.step(&job.Steps[i])
	}
	if job.Callee != nil && job.Callee.Workflow != nil {
		for i := range job.Callee.Workflow.Jobs {
			c.job(&job.Callee.Workflow.Jobs[i])
		}
	}
}

func (c *classifier) step(step *model.Step) {
	c.dependabotIf(step.If)
	if step.Local != nil {
		for i := range step.Local.Steps {
			c.step(&step.Local.Steps[i])
		}
	}
	if step.Run != nil && strings.Contains(step.Run.Value, "gh pr checkout") {
		c.prHeadCheckouts = append(c.prHeadCheckouts, step.Pos)
		return
	}
	if !isCheckout(step) {
		return
	}
	switch checkoutRefKind(step) {
	case checkoutPRHead:
		c.prHeadCheckouts = append(c.prHeadCheckouts, step.Pos)
	case checkoutDynamic:
		c.dynamicCheckouts = append(c.dynamicCheckouts, step.Pos)
	}
}

func (c *classifier) dependabotIf(cond *model.Str) {
	if cond != nil && strings.Contains(cond.Value, "dependabot[bot]") {
		c.dependabotIfs = append(c.dependabotIfs, cond.Pos)
	}
}

func isCheckout(step *model.Step) bool {
	if step.Uses == nil {
		return false
	}
	uses := strings.TrimSpace(step.Uses.Value)
	at := strings.LastIndexByte(uses, '@')
	if at <= 0 {
		return false
	}
	return strings.EqualFold(uses[:at], "actions/checkout")
}

func checkoutRefKind(step *model.Step) checkoutKind {
	kind := checkoutPlain
	for i := range step.With {
		kv := &step.With[i]
		if !strings.EqualFold(kv.Key, "ref") && !strings.EqualFold(kv.Key, "repository") {
			continue
		}
		if isPRHeadValue(&kv.Value) {
			return checkoutPRHead
		}
		if kind == checkoutPlain && !isPlainValue(&kv.Value) {
			kind = checkoutDynamic
		}
	}
	return kind
}

func isPRHeadValue(value *model.Str) bool {
	if strings.Contains(value.Value, "refs/pull/") {
		return true
	}
	for _, expr := range value.Exprs {
		for _, ref := range expr.Refs {
			if _, ok := prHeadRefs[ref]; ok {
				return true
			}
		}
	}
	return false
}

func isPlainValue(value *model.Str) bool {
	for _, expr := range value.Exprs {
		if expr.Err != "" {
			return false
		}
		for _, ref := range expr.Refs {
			if !isKnownBaseRef(ref) {
				return false
			}
		}
	}
	return true
}

func isKnownBaseRef(ref string) bool {
	if _, ok := knownBaseRefs[ref]; ok {
		return true
	}
	for _, prefix := range knownBaseRefPrefixes {
		if strings.HasPrefix(ref, prefix) {
			return true
		}
	}
	return false
}

func sortPositions(positions []model.Pos) []model.Pos {
	sort.SliceStable(positions, func(i, j int) bool {
		if positions[i].Line != positions[j].Line {
			return positions[i].Line < positions[j].Line
		}
		if positions[i].Col != positions[j].Col {
			return positions[i].Col < positions[j].Col
		}
		return positions[i].File < positions[j].File
	})
	if len(positions) == 0 {
		return nil
	}
	return positions
}

func unknownReasons(positions []model.Pos) []string {
	seen := make(map[string]struct{})
	var reasons []string
	for _, pos := range positions {
		reason := fmt.Sprintf("%s:%d: checkout ref is dynamic and not provably the base commit", pos.File, pos.Line)
		if _, ok := seen[reason]; ok {
			continue
		}
		seen[reason] = struct{}{}
		reasons = append(reasons, reason)
	}
	return reasons
}

func prerequisites(wf *model.Workflow, c *classifier, profile config.RepoProfile, forkDefinition bool) []model.Prerequisite {
	var out []model.Prerequisite
	add := func(setting, description string, pos *model.Pos) {
		out = append(out, model.Prerequisite{Setting: setting, Description: description, Pos: pos})
	}

	if forkDefinition {
		if description, ok := forkApprovalDescription(profile.ForkPRWorkflowApproval); ok {
			add("fork_pr_workflow_approval", description, nil)
		}
	}

	for i := range wf.Triggers {
		trigger := &wf.Triggers[i]
		switch trigger.Event {
		case "push":
			add("branch_protection", fmt.Sprintf("pushes to %s are restricted to trusted committers", branchFilters(trigger.Branches)), nil)
		case "workflow_dispatch":
			add("dispatcher_trust", "only trusted users can dispatch this workflow (requires write access)", nil)
		}
	}

	for i := range c.selfHostedJobs {
		pos := c.selfHostedJobs[i]
		add("self_hosted_runner_isolation", "self-hosted runner is not shared with less-trusted workflows", &pos)
	}
	for i := range c.dependabotIfs {
		pos := c.dependabotIfs[i]
		add("dependabot_event", "Dependabot-triggered runs are treated as trusted", &pos)
	}

	return sortPrerequisites(out)
}

func forkApprovalDescription(approval string) (string, bool) {
	switch approval {
	case config.ApprovalNone:
		return "", false
	case config.ApprovalFirstTimeContributors:
		return "the attacker is a prior contributor or a maintainer approves the fork run", true
	case config.ApprovalAllOutsideCollaborators:
		return "a maintainer approves the attacker's fork run", true
	default:
		return "fork pull request workflow runs are not held for maintainer approval (repository/organization fork PR approval policy)", true
	}
}

func branchFilters(branches []model.Str) string {
	if len(branches) == 0 {
		return "any branch"
	}
	values := make([]string, 0, len(branches))
	for _, branch := range branches {
		values = append(values, branch.Value)
	}
	return strings.Join(values, ", ")
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
