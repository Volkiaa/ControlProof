# ControlProof

Offline Go CLI that evidences GitHub Actions trust-boundary failures across workflows: an untrusted producer's artifact reaching a privileged consumer's execution or destination sinks, with explicit prerequisites, honest unknowns and an accountable remediation trail.

Status: CLI skeleton and workflow parser; no analysis is implemented yet. See [docs/spec/controlproof-v0.2.md](docs/spec/controlproof-v0.2.md).

Work is tracked in [GitHub issues](../../issues), grouped by roadmap week in [milestones](../../milestones).

## Build

Requires Go 1.26 or newer. The workflow parser uses `github.com/rhysd/actionlint` (pinned) for the GitHub Actions AST and its `go.yaml.in/yaml/v4` dependency for the bounded YAML pre-check.

```bash
go build ./cmd/controlproof
./controlproof version
./controlproof debug ir .
./controlproof debug semantics .
```

`debug ir [path]` prints the typed workflow IR as JSON (`controlproof-ir/0`), including parse diagnostics and inlined local composite actions and reusable workflows. It exits 2 when any workflow produced a parse or inline error.

`debug semantics [--policy FILE] [path]` prints the trigger model and attacker-control classification per workflow as JSON (`controlproof-semantics/0`): every `on:` event with whether it is modeled and its `attacker_control`, the workflow trust class (`fork-definition-controlled`, `pr-head-builder`, `trusted-definition` or `unknown`), PR-head checkouts, unmodeled triggers, unknown reasons and the repository settings a finding would depend on. Each workflow also lists its jobs' effective privilege (`jobs`): the token scope map with the level that decided it, id-token use, secret references and inheritance, checkout credential persistence, environment, app tokens, runner type, cache write reach and prerequisites. `--policy` loads `.controlproof.yaml` and reports its SHA-256 in the output; without it the built-in `unknown` repo profile applies.

`scan` is not implemented yet: it prints `controlproof: scan is not implemented yet` and exits with status 2, so an unfinished scanner never reads as "no findings".

## Policy file

`.controlproof.yaml` currently supports `schema_version: 1` and a `repo_profile` with `provider: github`, `repository_default_token_permissions` (`read`, `write` or `unknown`) and `fork_pr_workflow_approval` (`all-outside-collaborators`, `first-time-contributors`, `none` or `unknown`); unknown keys are rejected.

## Checks

CI (`.github/workflows/ci.yml`) runs these on every pull request and on `main`:

```bash
gofmt -l .
go vet ./...
go test -race ./...
go tool -modfile=tools/go.mod staticcheck ./...
go tool -modfile=tools/go.mod govulncheck ./...
```

staticcheck and govulncheck are pinned in the separate `tools/go.mod`, so they never become dependencies of the scanner. Dependabot updates them, the Go modules and the SHA-pinned actions weekly.
