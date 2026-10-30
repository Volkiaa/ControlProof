package graph

import (
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/Volkiaa/ControlProof/internal/config"
	"github.com/Volkiaa/ControlProof/internal/model"
	"github.com/Volkiaa/ControlProof/internal/semantics/permissions"
	"github.com/Volkiaa/ControlProof/internal/semantics/triggers"
	"github.com/Volkiaa/ControlProof/internal/summaries"
)

type Guard struct {
	Expr     string    `json:"expr"`
	Pos      model.Pos `json:"pos"`
	Adequacy string    `json:"adequacy"`
}

type Node struct {
	ID    string            `json:"id"`
	Kind  string            `json:"kind"`
	Label string            `json:"label"`
	Pos   *model.Pos        `json:"pos,omitempty"`
	Attrs map[string]string `json:"attrs,omitempty"`
}

type Edge struct {
	From           string            `json:"from"`
	To             string            `json:"to"`
	Kind           string            `json:"kind"`
	SourceLocation model.Pos         `json:"source_location"`
	EvidenceClass  string            `json:"evidence_class"`
	Confidence     string            `json:"confidence"`
	Guards         []Guard           `json:"guards"`
	Attrs          map[string]string `json:"attrs,omitempty"`
}

type Unsupported struct {
	Kind   string    `json:"kind"`
	Pos    model.Pos `json:"pos"`
	Reason string    `json:"reason"`
}

type Graph struct {
	Nodes       []Node        `json:"nodes"`
	Edges       []Edge        `json:"edges"`
	Unsupported []Unsupported `json:"unsupported"`
}

const (
	kindWorkflow   = "Workflow"
	kindTrigger    = "Trigger"
	kindJob        = "Job"
	kindStep       = "Step"
	kindArtifact   = "Artifact"
	kindFile       = "File"
	edgeCanTrigger = "CAN_TRIGGER"
	edgeProduces   = "PRODUCES"
	edgeDownloads  = "DOWNLOADS"
	edgeExtractsTo = "EXTRACTS_TO"

	evidenceLiteral     = "literal"
	evidenceDocumented  = "documented_semantics"
	confidenceHigh      = "high"
	confidenceCandidate = "candidate"
	confidenceUnknown   = "unknown"

	contentArbitrary      = "arbitrary"
	contentBounded        = "bounded-by-upload-path"
	contentTrusted        = "trusted-definition"
	contentUnknown        = "unknown"
	workspaceDir          = "$GITHUB_WORKSPACE"
	runnerTempDir         = "$RUNNER_TEMP"
	unknownDir            = "UNKNOWN"
	unknownProducer       = "unknown"
	dynamicExtractionPath = "dynamic extraction path"
	escapedExtractionPath = "extraction path escapes the workspace"
)

type upload struct {
	jobID      string
	artifactID string
	name       string
	content    string
	dynamic    bool
}

type producerCandidate struct {
	path  string
	trust triggers.Trust
}

type workflowInfo struct {
	wf             *model.Workflow
	classification triggers.Classification
}

type stepContext struct {
	wf     *model.Workflow
	trust  triggers.Trust
	job    *model.Job
	step   *model.Step
	jobID  string
	stepID string
}

type builder struct {
	profile     config.RepoProfile
	workflows   []workflowInfo
	nodes       map[string]Node
	edges       []Edge
	unsupported []Unsupported
	uploads     map[string][]upload
	producers   map[string][]producerCandidate
}

func Build(workflows []model.Workflow, profile config.RepoProfile) *Graph {
	b := &builder{
		profile:   profile,
		nodes:     make(map[string]Node),
		uploads:   make(map[string][]upload),
		producers: make(map[string][]producerCandidate),
	}
	for i := range workflows {
		wf := &workflows[i]
		info := workflowInfo{wf: wf, classification: triggers.Classify(wf, profile)}
		b.workflows = append(b.workflows, info)
		b.addWorkflowNode(wf, info.classification)
	}

	for i := range b.workflows {
		b.addTriggers(&b.workflows[i])
	}
	for i := range b.workflows {
		b.collect(&b.workflows[i])
	}
	for i := range b.workflows {
		b.downloads(&b.workflows[i])
	}
	return b.result()
}

func (b *builder) addWorkflowNode(wf *model.Workflow, classification triggers.Classification) {
	b.addNode(Node{
		ID:    workflowID(wf.Path),
		Kind:  kindWorkflow,
		Label: classification.Name,
		Attrs: map[string]string{
			"name":  classification.Name,
			"trust": string(classification.Trust),
		},
	})
}

func (b *builder) addTriggers(info *workflowInfo) {
	wf := info.wf
	for i := range wf.Triggers {
		trigger := &wf.Triggers[i]
		triggerID := "trigger:" + wf.Path + "#" + trigger.Event
		pos := trigger.Pos
		b.addNode(Node{ID: triggerID, Kind: kindTrigger, Label: trigger.Event, Pos: &pos})
		b.addEdge(Edge{
			From:           triggerID,
			To:             workflowID(wf.Path),
			Kind:           edgeCanTrigger,
			SourceLocation: trigger.Pos,
			EvidenceClass:  evidenceLiteral,
			Confidence:     confidenceHigh,
		})
		if trigger.Event != "workflow_run" {
			continue
		}
		for j := range trigger.Workflows {
			b.addWorkflowRunCandidate(wf, &trigger.Workflows[j])
		}
	}
}

func (b *builder) addWorkflowRunCandidate(consumer *model.Workflow, name *model.Str) {
	want := name.Value
	syntheticPath := "fork-added:" + want
	syntheticID := workflowID(syntheticPath)
	b.addNode(Node{
		ID:    syntheticID,
		Kind:  kindWorkflow,
		Label: want,
		Attrs: map[string]string{
			"name":      want,
			"synthetic": "true",
			"trust":     string(triggers.TrustForkDefinitionControlled),
		},
	})
	b.addEdge(Edge{
		From:           syntheticID,
		To:             workflowID(consumer.Path),
		Kind:           edgeCanTrigger,
		SourceLocation: name.Pos,
		EvidenceClass:  evidenceDocumented,
		Confidence:     confidenceCandidate,
		Attrs: map[string]string{
			"match":        "workflow-name",
			"name":         want,
			"prerequisite": "fork_pr_workflow_approval",
		},
	})
	b.addProducer(consumer.Path, producerCandidate{
		path:  syntheticPath,
		trust: triggers.TrustForkDefinitionControlled,
	})

	for i := range b.workflows {
		candidate := &b.workflows[i]
		if candidate.classification.Name != want {
			continue
		}
		b.addEdge(Edge{
			From:           workflowID(candidate.wf.Path),
			To:             workflowID(consumer.Path),
			Kind:           edgeCanTrigger,
			SourceLocation: name.Pos,
			EvidenceClass:  evidenceDocumented,
			Confidence:     confidenceCandidate,
			Attrs: map[string]string{
				"match": "workflow-name",
				"name":  want,
			},
		})
		b.addProducer(consumer.Path, producerCandidate{
			path:  candidate.wf.Path,
			trust: candidate.classification.Trust,
		})
	}
}

func (b *builder) addProducer(consumer string, candidate producerCandidate) {
	for _, existing := range b.producers[consumer] {
		if existing.path == candidate.path {
			return
		}
	}
	b.producers[consumer] = append(b.producers[consumer], candidate)
}

func (b *builder) collect(info *workflowInfo) {
	b.walk(info, func(ctx stepContext) {
		if ctx.step.Uses == nil {
			return
		}
		rec := summaries.Recognize(*ctx.step)
		if rec.Opaque || rec.Kind != "upload-artifact" {
			return
		}
		b.addUpload(ctx, rec)
	})
}

func (b *builder) addUpload(ctx stepContext, rec summaries.Recognition) {
	artifactName := "*"
	pathValue := ""
	confidence := confidenceHigh
	dynamicName := false
	if input, ok := rec.Inputs["name"]; ok {
		if input.Dynamic {
			confidence = confidenceUnknown
			dynamicName = true
		} else {
			artifactName = input.Value
		}
	}
	if input, ok := rec.Inputs["path"]; ok {
		pathValue = input.Value
	}
	artifactID := artifactID(ctx.wf.Path, artifactName)
	pos := ctx.step.Pos
	b.addNode(Node{
		ID:    artifactID,
		Kind:  kindArtifact,
		Label: artifactName,
		Pos:   &pos,
		Attrs: map[string]string{
			"name":     artifactName,
			"producer": ctx.wf.Path,
			"content":  contentForTrust(ctx.trust),
		},
	})
	b.addEdge(Edge{
		From:           ctx.stepID,
		To:             artifactID,
		Kind:           edgeProduces,
		SourceLocation: ctx.step.Pos,
		EvidenceClass:  evidenceLiteral,
		Confidence:     confidence,
		Attrs: map[string]string{
			"name": artifactName,
			"path": pathValue,
		},
	})
	b.uploads[ctx.wf.Path] = append(b.uploads[ctx.wf.Path], upload{
		jobID:      ctx.jobID,
		artifactID: artifactID,
		name:       artifactName,
		content:    contentForTrust(ctx.trust),
		dynamic:    dynamicName,
	})
}

func (b *builder) downloads(info *workflowInfo) {
	b.walk(info, func(ctx stepContext) {
		if ctx.step.Uses == nil {
			return
		}
		rec := summaries.Recognize(*ctx.step)
		if rec.Opaque {
			if isArtifactAction(rec.Uses) {
				b.addUnsupported("opaque-downloader", ctx.step.Pos, "opaque artifact action: "+rec.OpaqueReason)
			}
			return
		}
		switch rec.Kind {
		case "download-artifact":
			b.actionsDownload(ctx, rec)
		case "dawidd6-download-artifact":
			b.dawidd6Download(ctx, rec)
		}
	})
}

func (b *builder) actionsDownload(ctx stepContext, rec summaries.Recognition) {
	if b.crossRepository(rec, "repository") {
		b.addUnsupported("cross-repository", ctx.step.Pos, "artifact download from another repository is outside the §3.2 same-repository scope")
		b.addExtraction(ctx, rec, "actions")
		return
	}

	runID, hasRunID := rec.Inputs["run-id"]
	switch {
	case hasRunID && exactExpr(runID.Value, "github.event.workflow_run.id"):
		b.linkProducers(ctx, rec, b.producers[ctx.wf.Path], "workflow-run-event", nil)
	case !hasRunID || runID.Default:
		b.linkSameRun(ctx, rec, "same-run")
	default:
		b.addUnsupported("dynamic-selector", ctx.step.Pos, "artifact run-id is neither the workflow_run event nor the same-run default")
		artifactName := selectorUnknownName(rec)
		artifactID := artifactID(unknownProducer, artifactName)
		pos := ctx.step.Pos
		b.addNode(Node{
			ID:    artifactID,
			Kind:  kindArtifact,
			Label: artifactName,
			Pos:   &pos,
			Attrs: map[string]string{
				"name":     artifactName,
				"producer": unknownProducer,
				"content":  contentUnknown,
			},
		})
		b.addDownloadEdge(ctx, artifactID, contentUnknown, "other", confidenceUnknown, evidenceDocumented, map[string]string{"run_id_binding": "other"})
	}
	b.addExtraction(ctx, rec, "actions")
}

func (b *builder) dawidd6Download(ctx stepContext, rec summaries.Recognition) {
	if b.crossRepository(rec, "repo") {
		b.addUnsupported("cross-repository", ctx.step.Pos, "artifact download from another repository is outside the §3.2 same-repository scope")
		b.addExtraction(ctx, rec, "dawidd6")
		return
	}

	var candidates []producerCandidate
	if workflow, ok := rec.Inputs["workflow"]; !ok || workflow.Dynamic || strings.TrimSpace(workflow.Value) == "" {
		b.addUnsupported("dynamic-selector", ctx.step.Pos, "workflow selector is dynamic or missing")
	} else {
		want := strings.TrimSpace(workflow.Value)
		for i := range b.workflows {
			candidate := &b.workflows[i]
			if path.Base(candidate.wf.Path) == want || candidate.classification.Name == want {
				candidates = append(candidates, producerCandidate{
					path:  candidate.wf.Path,
					trust: candidate.classification.Trust,
				})
			}
		}
		if len(candidates) == 0 {
			b.addUnsupported("dynamic-selector", ctx.step.Pos, fmt.Sprintf("no workflow in the repository matches %s", want))
		}
	}

	binding := "other"
	if runID, ok := rec.Inputs["run_id"]; !ok || runID.Default {
		binding = "latest-run"
	} else if exactExpr(runID.Value, "github.event.workflow_run.id") {
		binding = "workflow-run-event"
	}
	extras := map[string]string{}
	if allowForks, ok := rec.Inputs["allow_forks"]; ok {
		extras["allow_forks"] = allowForks.Value
	}
	b.linkProducers(ctx, rec, candidates, binding, extras)
	b.addExtraction(ctx, rec, "dawidd6")
}

func (b *builder) linkSameRun(ctx stepContext, rec summaries.Recognition, binding string) {
	selector := newSelector(rec)
	candidate := producerCandidate{path: ctx.wf.Path, trust: ctx.trust}
	matched := false
	for _, item := range b.uploads[ctx.wf.Path] {
		if item.jobID == ctx.jobID {
			continue
		}
		evidence, matchConfidence, nameMatch, ok := selector.matchUpload(item)
		if !ok {
			continue
		}
		matched = true
		b.addDownloadEdge(ctx, item.artifactID, item.content, binding, downloadMatchConfidence(binding, matchConfidence), evidence, downloadMatchAttrs(binding, nil, nameMatch))
	}
	if !matched && candidate.trust == triggers.TrustForkDefinitionControlled {
		b.addAttackerChosen(ctx, rec, candidate, binding, nil)
	}
}

func (b *builder) linkProducers(ctx stepContext, rec summaries.Recognition, candidates []producerCandidate, binding string, extras map[string]string) {
	selector := newSelector(rec)
	for _, candidate := range candidates {
		matched := false
		for _, item := range b.uploads[candidate.path] {
			evidence, matchConfidence, nameMatch, ok := selector.matchUpload(item)
			if !ok {
				continue
			}
			matched = true
			b.addDownloadEdge(ctx, item.artifactID, item.content, binding, downloadMatchConfidence(binding, matchConfidence), evidence, downloadMatchAttrs(binding, extras, nameMatch))
		}
		if !matched && candidate.trust == triggers.TrustForkDefinitionControlled {
			b.addAttackerChosen(ctx, rec, candidate, binding, extras)
		}
	}
}

func (b *builder) addAttackerChosen(ctx stepContext, rec summaries.Recognition, candidate producerCandidate, binding string, extras map[string]string) {
	selector := newSelector(rec)
	artifactName := selector.attackerName()
	artifactID := artifactID(candidate.path, artifactName)
	pos := ctx.step.Pos
	content := contentForTrust(candidate.trust)
	b.addNode(Node{
		ID:    artifactID,
		Kind:  kindArtifact,
		Label: artifactName,
		Pos:   &pos,
		Attrs: map[string]string{
			"name":     artifactName,
			"producer": candidate.path,
			"content":  content,
		},
	})
	attrs := downloadAttrs(binding, extras)
	attrs["name_match"] = "attacker-chosen"
	b.addDownloadEdge(ctx, artifactID, content, binding, confidenceForBinding(binding), evidenceDocumented, attrs)
}

func (b *builder) addDownloadEdge(ctx stepContext, artifactID, content, binding, confidence, evidence string, attrs map[string]string) {
	out := map[string]string{"content": content}
	for key, value := range attrs {
		out[key] = value
	}
	b.addEdge(Edge{
		From:           ctx.stepID,
		To:             artifactID,
		Kind:           edgeDownloads,
		SourceLocation: ctx.step.Pos,
		EvidenceClass:  evidence,
		Confidence:     confidence,
		Guards:         guards(ctx.job.If, ctx.step.If),
		Attrs:          out,
	})
}

func (b *builder) addExtraction(ctx stepContext, rec summaries.Recognition, kind string) {
	extraction := extractionDir(rec, kind)
	fileID := "file:" + ctx.stepID + "@" + extraction.dir
	pos := ctx.step.Pos
	b.addNode(Node{
		ID:    fileID,
		Kind:  kindFile,
		Label: extraction.dir,
		Pos:   &pos,
		Attrs: map[string]string{
			"dir":    extraction.dir,
			"layout": layout(rec, kind),
		},
	})
	b.addEdge(Edge{
		From:           ctx.stepID,
		To:             fileID,
		Kind:           edgeExtractsTo,
		SourceLocation: ctx.step.Pos,
		EvidenceClass:  extraction.evidence,
		Confidence:     extraction.confidence,
	})
	if extraction.unsupported != "" {
		b.addUnsupported("dynamic-selector", ctx.step.Pos, extraction.unsupported)
	}
}

func (b *builder) crossRepository(rec summaries.Recognition, key string) bool {
	input, ok := rec.Inputs[key]
	if !ok || input.Default {
		return false
	}
	return !exactExpr(input.Value, "github.repository")
}

func (b *builder) walk(info *workflowInfo, fn func(stepContext)) {
	resolved := permissions.Resolve(info.wf, b.profile)
	for i := range info.wf.Jobs {
		jobID := jobID(info.wf.Path, info.wf.Jobs[i].ID.Value)
		b.walkJob(info, &info.wf.Jobs[i], &resolved[i], jobID, fn)
	}
}

func (b *builder) walkJob(info *workflowInfo, job *model.Job, privilege *permissions.JobPrivilege, jobID string, fn func(stepContext)) {
	b.addJobNode(job, privilege, jobID)
	for i := range job.Steps {
		step := &job.Steps[i]
		b.walkStep(info, job, step, jobID, stepID(jobID, step.Index), fn)
	}
	if job.Callee == nil || job.Callee.Workflow == nil {
		return
	}
	callee := job.Callee.Workflow
	for i := range callee.Jobs {
		childJob := &callee.Jobs[i]
		childJobID := jobID + "/" + callee.Path + "#" + childJob.ID.Value
		var childPrivilege *permissions.JobPrivilege
		if privilege != nil && i < len(privilege.CalleeJobs) {
			childPrivilege = &privilege.CalleeJobs[i]
		}
		b.walkJob(info, childJob, childPrivilege, childJobID, fn)
	}
}

func (b *builder) walkStep(info *workflowInfo, job *model.Job, step *model.Step, jobID, stepID string, fn func(stepContext)) {
	b.addStepNode(step, stepID)
	fn(stepContext{
		wf:     info.wf,
		trust:  info.classification.Trust,
		job:    job,
		step:   step,
		jobID:  jobID,
		stepID: stepID,
	})
	if step.Local == nil {
		return
	}
	for i := range step.Local.Steps {
		child := &step.Local.Steps[i]
		b.walkStep(info, job, child, jobID, stepID+"/"+strconv.Itoa(child.Index), fn)
	}
}

func (b *builder) addJobNode(job *model.Job, privilege *permissions.JobPrivilege, jobID string) {
	attrs := map[string]string{}
	if privilege != nil {
		attrs["token_source"] = privilege.Privilege.Token.Source
		attrs["cache_write"] = privilege.Privilege.CacheWrite
	}
	pos := job.Pos
	b.addNode(Node{ID: jobID, Kind: kindJob, Label: job.ID.Value, Pos: &pos, Attrs: attrs})
}

func (b *builder) addStepNode(step *model.Step, stepID string) {
	pos := step.Pos
	b.addNode(Node{ID: stepID, Kind: kindStep, Label: stepLabel(step), Pos: &pos})
}

func (b *builder) addNode(node Node) {
	if _, ok := b.nodes[node.ID]; ok {
		return
	}
	b.nodes[node.ID] = node
}

func (b *builder) addEdge(edge Edge) {
	if edge.Guards == nil {
		edge.Guards = []Guard{}
	}
	b.edges = append(b.edges, edge)
}

func (b *builder) addUnsupported(kind string, pos model.Pos, reason string) {
	b.unsupported = append(b.unsupported, Unsupported{Kind: kind, Pos: pos, Reason: reason})
}

func (b *builder) result() *Graph {
	nodes := make([]Node, 0, len(b.nodes))
	for _, node := range b.nodes {
		nodes = append(nodes, node)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })

	edges := dedupeEdges(b.edges)
	unsupported := dedupeUnsupported(b.unsupported)
	return &Graph{Nodes: nodes, Edges: edges, Unsupported: unsupported}
}

func dedupeEdges(edges []Edge) []Edge {
	seen := make(map[string]struct{}, len(edges))
	unique := make([]Edge, 0, len(edges))
	for _, edge := range edges {
		key := edgeKey(edge)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, edge)
	}
	sort.Slice(unique, func(i, j int) bool { return lessEdge(unique[i], unique[j]) })
	return unique
}

func dedupeUnsupported(unsupported []Unsupported) []Unsupported {
	seen := make(map[string]struct{}, len(unsupported))
	unique := make([]Unsupported, 0, len(unsupported))
	for _, item := range unsupported {
		key := item.Pos.File + "\x00" + strconv.Itoa(item.Pos.Line) + "\x00" + strconv.Itoa(item.Pos.Col) + "\x00" + item.Kind + "\x00" + item.Reason
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, item)
	}
	sort.Slice(unique, func(i, j int) bool {
		if unique[i].Pos.File != unique[j].Pos.File {
			return unique[i].Pos.File < unique[j].Pos.File
		}
		if unique[i].Pos.Line != unique[j].Pos.Line {
			return unique[i].Pos.Line < unique[j].Pos.Line
		}
		if unique[i].Pos.Col != unique[j].Pos.Col {
			return unique[i].Pos.Col < unique[j].Pos.Col
		}
		if unique[i].Kind != unique[j].Kind {
			return unique[i].Kind < unique[j].Kind
		}
		return unique[i].Reason < unique[j].Reason
	})
	return unique
}

func lessEdge(a, b Edge) bool {
	if a.Kind != b.Kind {
		return a.Kind < b.Kind
	}
	if a.From != b.From {
		return a.From < b.From
	}
	if a.To != b.To {
		return a.To < b.To
	}
	if a.SourceLocation.File != b.SourceLocation.File {
		return a.SourceLocation.File < b.SourceLocation.File
	}
	if a.SourceLocation.Line != b.SourceLocation.Line {
		return a.SourceLocation.Line < b.SourceLocation.Line
	}
	if a.SourceLocation.Col != b.SourceLocation.Col {
		return a.SourceLocation.Col < b.SourceLocation.Col
	}
	return edgeKey(a) < edgeKey(b)
}

func edgeKey(edge Edge) string {
	encoded, err := json.Marshal(edge)
	if err != nil {
		return fmt.Sprintf("%#v", edge)
	}
	return string(encoded)
}

func guards(conditions ...*model.Str) []Guard {
	out := []Guard{}
	for _, condition := range conditions {
		if condition == nil {
			continue
		}
		out = append(out, Guard{Expr: condition.Value, Pos: condition.Pos, Adequacy: "unknown"})
	}
	return out
}

func contentForTrust(trust triggers.Trust) string {
	switch trust {
	case triggers.TrustForkDefinitionControlled:
		return contentArbitrary
	case triggers.TrustPRHeadBuilder:
		return contentBounded
	case triggers.TrustTrustedDefinition:
		return contentTrusted
	default:
		return contentUnknown
	}
}

func confidenceForBinding(binding string) string {
	if binding == "workflow-run-event" || binding == "same-run" {
		return confidenceHigh
	}
	return confidenceCandidate
}

func downloadAttrs(binding string, extras map[string]string) map[string]string {
	attrs := map[string]string{"run_id_binding": binding}
	for key, value := range extras {
		attrs[key] = value
	}
	return attrs
}

func downloadMatchAttrs(binding string, extras map[string]string, nameMatch string) map[string]string {
	attrs := downloadAttrs(binding, extras)
	if nameMatch != "" {
		attrs["name_match"] = nameMatch
	}
	return attrs
}

func downloadMatchConfidence(binding, matchConfidence string) string {
	if matchConfidence != "" {
		return matchConfidence
	}
	return confidenceForBinding(binding)
}

func workflowID(workflowPath string) string {
	return "workflow:" + workflowPath
}

func jobID(workflowPath, job string) string {
	return "job:" + workflowPath + "#" + job
}

func stepID(jobID string, index int) string {
	return "step:" + strings.TrimPrefix(jobID, "job:") + "#" + strconv.Itoa(index)
}

func artifactID(producerPath, name string) string {
	return "artifact:" + producerPath + "#" + name
}

func stepLabel(step *model.Step) string {
	if step.Name != nil && step.Name.Value != "" {
		return step.Name.Value
	}
	if step.Uses != nil && step.Uses.Value != "" {
		return step.Uses.Value
	}
	if step.ID != nil && step.ID.Value != "" {
		return step.ID.Value
	}
	return "step"
}

func isArtifactAction(uses string) bool {
	return strings.Contains(strings.ToLower(uses), "artifact")
}

func exactExpr(value, ref string) bool {
	trimmed := strings.TrimSpace(value)
	if !strings.HasPrefix(trimmed, "${{") || !strings.HasSuffix(trimmed, "}}") {
		return false
	}
	inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trimmed, "${{"), "}}"))
	return inner == ref
}

func selectorUnknownName(rec summaries.Recognition) string {
	if input, ok := rec.Inputs["name"]; ok && !input.Dynamic && strings.TrimSpace(input.Value) != "" {
		return input.Value
	}
	return "*"
}

type selector struct {
	name       string
	pattern    string
	hasName    bool
	hasPattern bool
	dynamic    bool
}

func newSelector(rec summaries.Recognition) selector {
	var out selector
	if input, ok := rec.Inputs["name"]; ok {
		if input.Dynamic {
			out.dynamic = true
		} else if strings.TrimSpace(input.Value) != "" {
			out.name = input.Value
			out.hasName = true
		}
	}
	if input, ok := rec.Inputs["pattern"]; ok {
		if input.Dynamic {
			out.dynamic = true
		} else if strings.TrimSpace(input.Value) != "" {
			out.pattern = input.Value
			out.hasPattern = true
		}
	}
	return out
}

func (s selector) matchUpload(item upload) (string, string, string, bool) {
	switch {
	case item.dynamic:
		return evidenceDocumented, confidenceUnknown, "dynamic-upload-name", true
	case s.dynamic:
		return evidenceDocumented, confidenceUnknown, "dynamic-selector", true
	default:
		evidence, ok := s.match(item.name)
		return evidence, "", "", ok
	}
}

func (s selector) match(artifactName string) (string, bool) {
	switch {
	case s.hasName:
		return evidenceLiteral, artifactName == s.name
	case s.hasPattern:
		matched, err := path.Match(s.pattern, artifactName)
		if err != nil {
			return evidenceDocumented, false
		}
		return evidenceDocumented, matched
	default:
		return evidenceDocumented, true
	}
}

func (s selector) attackerName() string {
	if s.hasName {
		return s.name
	}
	return "*"
}

func layout(rec summaries.Recognition, kind string) string {
	if input, ok := rec.Inputs["name"]; ok && (input.Dynamic || strings.TrimSpace(input.Value) != "") {
		return "flat"
	}
	if merged(rec, "merge-multiple") || (kind == "dawidd6" && merged(rec, "merge_multiple")) {
		return "flat"
	}
	return "per-artifact-subdir"
}

func merged(rec summaries.Recognition, key string) bool {
	input, ok := rec.Inputs[key]
	return ok && strings.EqualFold(strings.TrimSpace(input.Value), "true")
}

type extraction struct {
	dir         string
	evidence    string
	confidence  string
	unsupported string
}

func extractionDir(rec summaries.Recognition, kind string) extraction {
	input, ok := rec.Inputs["path"]
	if !ok || input.Default {
		return extraction{dir: workspaceDir, evidence: evidenceDocumented, confidence: confidenceHigh}
	}
	value := strings.TrimSpace(input.Value)
	if value == "" || value == "." || (kind == "dawidd6" && value == "./") {
		return extraction{dir: workspaceDir, evidence: evidenceDocumented, confidence: confidenceHigh}
	}

	if ref, rest, ok := leadingExpr(value); ok {
		if strings.Contains(rest, "${{") {
			return extraction{dir: unknownDir, evidence: evidenceDocumented, confidence: confidenceUnknown, unsupported: dynamicExtractionPath}
		}
		base := ""
		switch ref {
		case "runner.temp":
			base = runnerTempDir
		case "github.workspace":
			base = workspaceDir
		default:
			return extraction{dir: unknownDir, evidence: evidenceDocumented, confidence: confidenceUnknown, unsupported: dynamicExtractionPath}
		}
		if rest == "" {
			return extraction{dir: base, evidence: evidenceDocumented, confidence: confidenceHigh}
		}
		cleaned := path.Clean(strings.TrimPrefix(rest, "/"))
		if cleaned == "." {
			return extraction{dir: base, evidence: evidenceDocumented, confidence: confidenceHigh}
		}
		if escapes(cleaned) {
			return extraction{dir: unknownDir, evidence: evidenceDocumented, confidence: confidenceUnknown, unsupported: escapedExtractionPath}
		}
		return extraction{dir: base + "/" + cleaned, evidence: evidenceDocumented, confidence: confidenceHigh}
	}

	if strings.Contains(value, "${{") {
		return extraction{dir: unknownDir, evidence: evidenceDocumented, confidence: confidenceUnknown, unsupported: dynamicExtractionPath}
	}
	if path.IsAbs(value) {
		return extraction{dir: path.Clean(value), evidence: evidenceLiteral, confidence: confidenceHigh}
	}
	cleaned := path.Clean(value)
	if cleaned == "." {
		return extraction{dir: workspaceDir, evidence: evidenceDocumented, confidence: confidenceHigh}
	}
	if escapes(cleaned) {
		return extraction{dir: unknownDir, evidence: evidenceDocumented, confidence: confidenceUnknown, unsupported: escapedExtractionPath}
	}
	return extraction{dir: workspaceDir + "/" + cleaned, evidence: evidenceLiteral, confidence: confidenceHigh}
}

func leadingExpr(value string) (string, string, bool) {
	if !strings.HasPrefix(value, "${{") {
		return "", "", false
	}
	end := strings.Index(value, "}}")
	if end < 0 {
		return "", "", false
	}
	ref := strings.TrimSpace(value[3:end])
	rest := value[end+2:]
	return ref, rest, true
}

func escapes(cleaned string) bool {
	return cleaned == ".." || strings.HasPrefix(cleaned, "../")
}
