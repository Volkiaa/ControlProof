package summaries

import (
	"sort"
	"strconv"
	"strings"

	"github.com/Volkiaa/ControlProof/internal/model"
)

type Summary struct {
	Action     string `json:"action"`
	Kind       string `json:"kind"`
	MinMajor   int    `json:"min_major"`
	MaxMajor   int    `json:"max_major"`
	Provenance string `json:"provenance"`
}

type Input struct {
	Value   string    `json:"value"`
	Pos     model.Pos `json:"pos"`
	Dynamic bool      `json:"dynamic,omitempty"`
	Default bool      `json:"default,omitempty"`
}

type Recognition struct {
	Uses         string           `json:"uses"`
	Action       string           `json:"action,omitempty"`
	Major        int              `json:"major,omitempty"`
	Kind         string           `json:"kind,omitempty"`
	Opaque       bool             `json:"opaque"`
	OpaqueReason string           `json:"opaque_reason,omitempty"`
	Summary      *Summary         `json:"summary,omitempty"`
	Inputs       map[string]Input `json:"inputs,omitempty"`
}

const (
	reasonNoSummary         = "no-summary"
	reasonUnversionedRef    = "unversioned-ref"
	reasonVersionOutOfRange = "version-out-of-range"
	reasonLocal             = "local"
	reasonDocker            = "docker"
)

const reviewDate = "2026-10-30"

var summaries = []Summary{
	{Action: "actions/checkout", Kind: "checkout", MinMajor: 4, MaxMajor: 7, Provenance: provenance("actions/checkout", 7)},
	{Action: "actions/download-artifact", Kind: "download-artifact", MinMajor: 4, MaxMajor: 8, Provenance: provenance("actions/download-artifact", 8)},
	{Action: "actions/github-script", Kind: "github-script", MinMajor: 6, MaxMajor: 9, Provenance: provenance("actions/github-script", 9)},
	{Action: "actions/upload-artifact", Kind: "upload-artifact", MinMajor: 4, MaxMajor: 7, Provenance: provenance("actions/upload-artifact", 7)},
	{Action: "dawidd6/action-download-artifact", Kind: "dawidd6-download-artifact", MinMajor: 2, MaxMajor: 27, Provenance: provenance("dawidd6/action-download-artifact", 27)},
}

type capture struct {
	name        string
	def         string
	hasDef      bool
	defForMajor func(int) string
}

var captures = map[string][]capture{
	"upload-artifact": {
		{name: "name", def: "artifact", hasDef: true},
		{name: "path"},
		{name: "include-hidden-files", def: "false", hasDef: true},
	},
	"download-artifact": {
		{name: "name"},
		{name: "pattern"},
		{name: "merge-multiple", def: "false", hasDef: true},
		{name: "path", def: "", hasDef: true},
		{name: "run-id", def: "${{ github.run_id }}", hasDef: true},
		{name: "repository", def: "${{ github.repository }}", hasDef: true},
		{name: "github-token"},
		{name: "artifact-ids"},
	},
	"dawidd6-download-artifact": {
		{name: "run_id"},
		{name: "workflow"},
		{name: "workflow_search", def: "false", hasDef: true},
		{name: "branch"},
		{name: "ref"},
		{name: "commit"},
		{name: "pr"},
		{name: "event"},
		{name: "workflow_conclusion", def: "success", hasDef: true},
		{name: "allow_forks", hasDef: true, defForMajor: allowForksDefault},
		{name: "search_artifacts", def: "false", hasDef: true},
		{name: "name"},
		{name: "name_is_regexp", def: "false", hasDef: true},
		{name: "repo", def: "${{ github.repository }}", hasDef: true},
		{name: "path", def: "./", hasDef: true},
		{name: "merge_multiple", def: "false", hasDef: true},
		{name: "github_token", def: "${{ github.token }}", hasDef: true},
	},
	"checkout": {
		{name: "repository", def: "${{ github.repository }}", hasDef: true},
		{name: "ref"},
		{name: "path"},
		{name: "persist-credentials", def: "true", hasDef: true},
		{name: "sparse-checkout"},
		{name: "token"},
	},
	"github-script": {
		{name: "script"},
		{name: "github-token"},
		{name: "result-encoding", def: "json", hasDef: true},
	},
}

func provenance(action string, maxMajor int) string {
	return "https://github.com/" + action + "/blob/v" + strconv.Itoa(maxMajor) + "/action.yml (reviewed " + reviewDate + ")"
}

func allowForksDefault(major int) string {
	if major >= 4 {
		return "false"
	}
	return "true"
}

func All() []Summary {
	out := make([]Summary, len(summaries))
	copy(out, summaries)
	sort.Slice(out, func(i, j int) bool {
		return out[i].Action < out[j].Action
	})
	return out
}

func Recognize(step model.Step) Recognition {
	if step.Uses == nil {
		return Recognition{}
	}
	rec := Recognition{Uses: step.Uses.Value}
	uses := strings.TrimSpace(step.Uses.Value)
	if uses == "" {
		rec.Opaque = true
		rec.OpaqueReason = reasonNoSummary
		return rec
	}
	if strings.HasPrefix(uses, "./") {
		rec.Opaque = true
		rec.OpaqueReason = reasonLocal
		return rec
	}
	if strings.HasPrefix(uses, "docker://") {
		rec.Opaque = true
		rec.OpaqueReason = reasonDocker
		return rec
	}

	name, ref := uses, ""
	if at := strings.LastIndexByte(uses, '@'); at >= 0 {
		name, ref = uses[:at], uses[at+1:]
	}
	action := actionKey(name)
	if action == "" {
		rec.Opaque = true
		rec.OpaqueReason = reasonNoSummary
		return rec
	}
	rec.Action = action

	summary, known := lookup(action)
	if !known {
		rec.Opaque = true
		rec.OpaqueReason = reasonNoSummary
		return rec
	}
	major, ok := parseMajor(ref)
	if !ok {
		rec.Opaque = true
		rec.OpaqueReason = reasonUnversionedRef
		return rec
	}
	rec.Major = major
	rec.Kind = summary.Kind
	if major < summary.MinMajor || major > summary.MaxMajor {
		rec.Opaque = true
		rec.OpaqueReason = reasonVersionOutOfRange
		return rec
	}

	rec.Summary = &summary
	rec.Inputs = captureInputs(step.With, rec.Kind, major, step.Pos)
	return rec
}

func actionKey(name string) string {
	segments := strings.Split(name, "/")
	if len(segments) != 2 || segments[0] == "" || segments[1] == "" {
		return ""
	}
	return strings.ToLower(name)
}

func lookup(action string) (Summary, bool) {
	for _, summary := range summaries {
		if summary.Action == action {
			return summary, true
		}
	}
	return Summary{}, false
}

func parseMajor(ref string) (int, bool) {
	if len(ref) < 2 || ref[0] != 'v' {
		return 0, false
	}
	parts := strings.Split(ref[1:], ".")
	if len(parts) > 3 {
		return 0, false
	}
	for _, part := range parts {
		if part == "" || !allDigits(part) {
			return 0, false
		}
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, false
	}
	return major, true
}

func allDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func captureInputs(with []model.KV, kind string, major int, pos model.Pos) map[string]Input {
	specs := captures[kind]
	present := make(map[string]*model.Str, len(with))
	for i := range with {
		present[strings.ToLower(with[i].Key)] = &with[i].Value
	}

	inputs := make(map[string]Input, len(specs))
	for _, spec := range specs {
		if value, ok := present[spec.name]; ok {
			inputs[spec.name] = Input{Value: value.Value, Pos: value.Pos, Dynamic: len(value.Exprs) > 0}
			continue
		}
		if !spec.hasDef {
			continue
		}
		def := spec.def
		if spec.defForMajor != nil {
			def = spec.defForMajor(major)
		}
		inputs[spec.name] = Input{Value: def, Pos: pos, Default: true}
	}
	if len(inputs) == 0 {
		return nil
	}
	return inputs
}
