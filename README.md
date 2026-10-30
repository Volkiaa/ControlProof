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
```

`debug ir [path]` prints the typed workflow IR as JSON (`controlproof-ir/0`), including parse diagnostics. It exits 2 when any workflow produced an error.

`scan` is not implemented yet: it prints `controlproof: scan is not implemented yet` and exits with status 2, so an unfinished scanner never reads as "no findings".

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
