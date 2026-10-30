package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/Volkiaa/ControlProof/internal/model"
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
	if args[0] != "ir" {
		fmt.Fprintf(stderr, "controlproof: unknown debug subcommand %q\n", args[0])
		fmt.Fprint(stderr, usage)
		return exitError
	}
	if len(args) > 2 {
		fmt.Fprintln(stderr, "controlproof: debug ir takes at most one path")
		fmt.Fprint(stderr, usage)
		return exitError
	}

	path := "."
	if len(args) == 2 {
		path = args[1]
	}
	return runDebugIR(path, stdout, stderr)
}

func runDebugIR(path string, stdout, stderr io.Writer) int {
	ws, err := workspace.Open(path, workspace.DefaultLimits)
	if err != nil {
		fmt.Fprintf(stderr, "controlproof: %v\n", err)
		return exitError
	}

	files, err := ws.Workflows()
	if err != nil {
		fmt.Fprintf(stderr, "controlproof: %v\n", err)
		ws.Close()
		return exitError
	}

	workflows := []model.Workflow{}
	failed := false
	for _, file := range files {
		wf, parseErr := workflow.Parse(file, workflow.DefaultLimits)
		if wf != nil {
			workflows = append(workflows, *wf)
		}
		if parseErr != nil {
			fmt.Fprintf(stderr, "controlproof: %v\n", parseErr)
			failed = true
		}
	}

	if err := ws.Close(); err != nil {
		fmt.Fprintf(stderr, "controlproof: %v\n", err)
		return exitError
	}

	output := struct {
		Schema    string           `json:"schema"`
		Workflows []model.Workflow `json:"workflows"`
	}{
		Schema:    "controlproof-ir/0",
		Workflows: workflows,
	}
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
