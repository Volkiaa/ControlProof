package main

import (
	"fmt"
	"io"
	"os"

	"github.com/Volkiaa/ControlProof/internal/version"
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
  scan [path]   scan a repository for CP-CICD-* findings (not implemented yet)
  version       print the controlproof version
  help          print this message

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
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return exitCompleted
	default:
		fmt.Fprintf(stderr, "controlproof: unknown command %q\n", args[0])
		fmt.Fprint(stderr, usage)
		return exitError
	}
}
