# ControlProof

Offline Go CLI that evidences GitHub Actions trust-boundary failures across workflows: an untrusted producer's artifact reaching a privileged consumer's execution or destination sinks, with explicit prerequisites, honest unknowns and an accountable remediation trail.

Status: CLI skeleton only; no analysis is implemented yet. See [docs/spec/controlproof-v0.2.md](docs/spec/controlproof-v0.2.md).

Work is tracked in [GitHub issues](../../issues), grouped by roadmap week in [milestones](../../milestones).

## Build

Requires Go 1.26 or newer. There are no third-party dependencies yet.

```bash
go build ./cmd/controlproof
./controlproof version
```

`scan` is not implemented yet: it prints `controlproof: scan is not implemented yet` and exits with status 2, so an unfinished scanner never reads as "no findings".
