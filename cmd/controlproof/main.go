package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/Volkiaa/ControlProof/internal/config"
	"github.com/Volkiaa/ControlProof/internal/model"
	"github.com/Volkiaa/ControlProof/internal/semantics/permissions"
	"github.com/Volkiaa/ControlProof/internal/semantics/triggers"
	"github.com/Volkiaa/ControlProof/internal/version"
	"github.com/Volkiaa/ControlProof/internal/workflow"
	"github.com/Volkiaa/ControlProof/internal/workspace"
)

const (
	exitCompleted      = 0
	exitPolicyFailed   = 1
	exitError          = 2
	exitStrictCoverage = 3
)

var usage = fmt.Sprintf(`controlproof evidences GitHub Actions trust-boundary failures.

usage:
  controlproof <command> [arguments]

commands:
  scan [path]        scan a repository for CP-CICD-* findings (not implemented yet)
  debug ir [path]    print the parsed workflow IR as JSON
  debug semantics [--policy FILE] [path]
                     print the trigger, attacker-control and privilege classification as JSON
  version            print the controlproof version
  help               print this message

exit codes:
  %d  completed; no policy gate failure
  %d  completed; policy gate failed
  %d  scan, parser, config or internal error
  %d  strict-coverage failure
`, exitCompleted, exitPolicyFailed, exitError, exitStrictCoverage)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return exitError
	}

	switch args[0] {
	case "version":
		if len(args) > 1 {
			fmt.Fprintln(stderr, "controlproof: version takes no arguments")
			fmt.Fprint(stderr, usage)
			return exitError
		}
		fmt.Fprintf(stdout, "controlproof %s\n", version.Version)
		return exitCompleted
	case "scan":
		fmt.Fprintln(stderr, "controlproof: scan is not implemented yet")
		return exitError
	case "debug":
		return runDebug(args[1:], stdout, stderr)
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return exitCompleted
	default:
		fmt.Fprintf(stderr, "controlproof: unknown command %q\n", args[0])
		fmt.Fprint(stderr, usage)
		return exitError
	}
}

func runDebug(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "controlproof: debug requires a subcommand")
		fmt.Fprint(stderr, usage)
		return exitError
	}
	switch args[0] {
	case "ir":
		return runDebugIR(args[1:], stdout, stderr)
	case "semantics":
		return runDebugSemantics(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "controlproof: unknown debug subcommand %q\n", args[0])
		fmt.Fprint(stderr, usage)
		return exitError
	}
}

func runDebugIR(args []string, stdout, stderr io.Writer) int {
	if len(args) > 1 {
		fmt.Fprintln(stderr, "controlproof: debug ir takes at most one path")
		fmt.Fprint(stderr, usage)
		return exitError
	}

	path := "."
	if len(args) == 1 {
		path = args[0]
	}

	workflows, failed, code := loadWorkflows(path, stderr)
	if code != exitCompleted {
		return code
	}

	output := struct {
		Schema    string           `json:"schema"`
		Workflows []model.Workflow `json:"workflows"`
	}{
		Schema:    "controlproof-ir/0",
		Workflows: workflows,
	}
	return emitJSON(output, failed, stdout, stderr)
}

type semanticsWorkflow struct {
	Path     string                     `json:"path"`
	Triggers triggers.Classification    `json:"triggers"`
	Jobs     []permissions.JobPrivilege `json:"jobs"`
}

func runDebugSemantics(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("debug semantics", flag.ContinueOnError)
	flags.SetOutput(stderr)
	policyPath := flags.String("policy", "", "policy file that supplies the repo profile")
	if err := flags.Parse(args); err != nil {
		return exitError
	}
	if flags.NArg() > 1 {
		fmt.Fprintln(stderr, "controlproof: debug semantics takes at most one path")
		fmt.Fprint(stderr, usage)
		return exitError
	}

	policy := config.Default()
	if *policyPath != "" {
		loaded, err := config.Load(*policyPath)
		if err != nil {
			fmt.Fprintf(stderr, "controlproof: %v\n", err)
			return exitError
		}
		policy = loaded
	}

	path := "."
	if flags.NArg() == 1 {
		path = flags.Arg(0)
	}

	workflows, failed, code := loadWorkflows(path, stderr)
	if code != exitCompleted {
		return code
	}

	classified := make([]semanticsWorkflow, 0, len(workflows))
	for i := range workflows {
		wf := &workflows[i]
		classified = append(classified, semanticsWorkflow{
			Path:     wf.Path,
			Triggers: triggers.Classify(wf, policy.RepoProfile),
			Jobs:     permissions.Resolve(wf, policy.RepoProfile),
		})
	}

	output := struct {
		Schema       string              `json:"schema"`
		PolicySHA256 string              `json:"policy_sha256"`
		Workflows    []semanticsWorkflow `json:"workflows"`
	}{
		Schema:       "controlproof-semantics/0",
		PolicySHA256: policy.SHA256,
		Workflows:    classified,
	}
	return emitJSON(output, failed, stdout, stderr)
}

func loadWorkflows(path string, stderr io.Writer) ([]model.Workflow, bool, int) {
	ws, err := workspace.Open(path, workspace.DefaultLimits)
	if err != nil {
		fmt.Fprintf(stderr, "controlproof: %v\n", err)
		return nil, false, exitError
	}

	files, err := ws.Workflows()
	if err != nil {
		fmt.Fprintf(stderr, "controlproof: %v\n", err)
		ws.Close()
		return nil, false, exitError
	}

	workflows := []model.Workflow{}
	failed := false
	for _, file := range files {
		wf, parseErr := workflow.Parse(file, workflow.DefaultLimits)
		if parseErr != nil {
			fmt.Fprintf(stderr, "controlproof: %v\n", parseErr)
			failed = true
		}
		if wf == nil {
			continue
		}
		if parseErr == nil {
			if err := workflow.Inline(ws, wf, workflow.DefaultLimits); err != nil {
				fmt.Fprintf(stderr, "controlproof: %v\n", err)
				failed = true
			}
		}
		workflows = append(workflows, *wf)
	}

	if err := ws.Close(); err != nil {
		fmt.Fprintf(stderr, "controlproof: %v\n", err)
		return nil, false, exitError
	}
	return workflows, failed, exitCompleted
}

func emitJSON(output any, failed bool, stdout, stderr io.Writer) int {
	encoded, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "controlproof: %v\n", err)
		return exitError
	}
	fmt.Fprintln(stdout, string(encoded))

	if failed {
		return exitError
	}
	return exitCompleted
}
